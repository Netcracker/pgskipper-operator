// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package disasterrecovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/avast/retry-go/v4"
	"go.uber.org/zap"
	"google.golang.org/api/googleapi"
	sqladmin "google.golang.org/api/sqladmin/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	ReadReplicaType   = "READ_REPLICA_INSTANCE"
	CloudSqlType      = "CLOUD_SQL_INSTANCE"
	CloudSqlProxyHost = "pg-cloudsql-proxy"
	ReplCheckQuery    = "select pg_wal_lsn_diff(sent_lsn, flush_lsn) as lag from " +
		"pg_stat_replication where usename = 'cloudsqlreplica';"
	CloudSQLProxySelectors = map[string]string{"app": "cloudsql-proxy"}
)

type CloudSQLDRManager struct {
	sqlClient CloudSqlClient
	helper    *helper.Helper
}

type CloudSqlClient struct {
	project string
	region  string
	service *sqladmin.Service
}

func newSqlAdminService() *sqladmin.Service {
	service, err := sqladmin.NewService(context.Background())
	if err != nil {
		panic(err)
	}
	return service
}

func newCloudSQLDRManager(helper *helper.Helper, cm *corev1.ConfigMap) GenericPostgreSQLDRManager {
	return &CloudSQLDRManager{
		helper: helper,
		sqlClient: CloudSqlClient{
			project: cm.Data["project"],
			region:  cm.Data["region"],
			service: newSqlAdminService(),
		},
	}
}

func (manager *CloudSQLDRManager) setStatus() error {
	var mode string
	instance, err := manager.sqlClient.getActiveInstanceInCurrentRegion()
	if err != nil {
		log.Error("there is an error during get active instance, skipping status set", zap.Error(err))
		return err
	}
	if instance != nil {
		log.Info(fmt.Sprintf("Active Instance: %s in region: %s found, setting Active Mode", instance.Name, manager.sqlClient.region))
		mode = "active"
	} else {
		replicaInstance, err := manager.sqlClient.getReplicaInCurrentRegion()
		if err != nil {
			log.Error("there is an error during get replica instance, skipping status set", zap.Error(err))
			return err
		}
		if replicaInstance != nil {
			log.Info(fmt.Sprintf("Standby Instance: %s in region: %s found, setting Standby Mode", replicaInstance.Name, manager.sqlClient.region))
			mode = "standby"
		}
	}
	if err := manager.helper.UpdateSiteManagerStatus(mode, "done"); err != nil {
		return err
	}

	if err := manager.setStatusForPreConfigure(mode, "done"); err != nil {
		return err
	}

	return nil
}

func (manager *CloudSQLDRManager) processSiteManagerRequest(response http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		if err := manager.getStatus(response); err != nil {
			_, _ = fmt.Fprintf(response, "Get Status error: %v", err)
			return
		}
	case "POST":
		statusRequest, err := parseSiteManagerStatusFromRequest(req)
		currentStatus := manager.helper.GetCurrentSiteManagerStatus()
		if currentStatus.Status == "running" {
			log.Info("Received request during running procedure, return current state")
			sendResponse(response, http.StatusOK, currentStatus)
		} else if statusRequest.Mode == currentStatus.Mode &&
			currentStatus.Status == "done" {
			log.Info("Desired status equals to current status, return current state")
			sendResponse(response, http.StatusOK, currentStatus)
			return
		}
		if err != nil {
			log.Error("Failed to parse sm status from request", zap.Error(err))
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := manager.helper.UpdateSiteManagerStatus(statusRequest.Mode, "running"); err != nil {
			log.Error("Failed to set sm status", zap.Error(err))
			http.Error(response, err.Error(), http.StatusInternalServerError)
		}

		// send running
		if err := manager.getStatus(response); err != nil {
			log.Error("Failed to get sm status", zap.Error(err))
			return
		}

		// async process of request
		go manager.processRequest(statusRequest)

	default:
		_, _ = fmt.Fprintf(response, "Only GET and POST methods are supported.")
	}
}

func (manager *CloudSQLDRManager) processHealthRequest(response http.ResponseWriter, req *http.Request) {
	dbInstance, err := manager.sqlClient.getInstanceInCurrentRegion()
	if err != nil {
		sendDownHealthResponse(response)
	} else {
		switch dbInstance.State {
		case "RUNNABLE":
			sendUpHealthResponse(response)
		default:
			sendDownHealthResponse(response)
		}
	}
}

// need move to server.go as a common part
func (manager *CloudSQLDRManager) getStatus(response http.ResponseWriter) error {
	log.Info("Site Manager: Get Status")
	sendResponse(response, http.StatusOK, manager.helper.GetCurrentSiteManagerStatus())
	return nil
}

func (manager *CloudSQLDRManager) changeMode(mode string) error {
	cloudSQlClient := manager.sqlClient
	log.Info(fmt.Sprintf("Received change to  %s, processing ...", mode))
	if mode == "standby" {
		primaryDbInstance, err := cloudSQlClient.getPrimaryNotInCurrentRegion()
		if err != nil {
			return err
		}

		readReplica, _ := cloudSQlClient.getReplicaInCurrentRegion()
		if readReplica == nil {
			log.Info("Read replica does not exists in current region, creating ...")
			if err := cloudSQlClient.createReadReplicaForPrimary(primaryDbInstance); err != nil {
				return err
			}
		} else {
			log.Info(fmt.Sprintf("Read Replica in current region exists: %s", readReplica.Name))
		}

		primaryDbInstanceInCurRegion, err := cloudSQlClient.getActiveInstanceInCurrentRegion()
		if err != nil {
			return err
		}

		if primaryDbInstanceInCurRegion == nil {
			log.Info("Primary instance in current region not found, skipping delete ...")
			return nil
		}

		log.Info(fmt.Sprintf("primaryDbInstanceInCurRegion in region: %s found: %s",
			primaryDbInstanceInCurRegion.Region, primaryDbInstanceInCurRegion.Name))

		if err := cloudSQlClient.dropInstance(primaryDbInstanceInCurRegion.Name); err != nil {
			return err
		}
	} else if mode == "active" {
		dbInstance, err := cloudSQlClient.getReplicaInCurrentRegion()
		if err != nil {
			return err
		}
		if dbInstance == nil {
			log.Info("There is no read replica in current region, trying to find primary ...")
			dbInstance, err = cloudSQlClient.getInstanceInCurrentRegion()
			if err != nil {
				return err
			}
		} else {
			log.Info(fmt.Sprintf("Replica found: %s", dbInstance.Name))

			if err := cloudSQlClient.promoteReplica(dbInstance.Name); err != nil {
				return err
			}

			if err := cloudSQlClient.enableHaForInstance(dbInstance.Name); err != nil {
				return err
			}

			// kill connections
			if err := manager.helper.TerminateActiveConnectionsForHost(CloudSqlProxyHost); err != nil {
				return err
			}
		}
		if err := manager.reconfigureCloudSqlProxy(dbInstance.ConnectionName); err != nil {
			return err
		}

	}
	return nil
}

func (manager *CloudSQLDRManager) reconfigureCloudSqlProxy(connectionName string) error {
	log.Info(fmt.Sprintf("Reconfiguring Cloud SQL Proxy with new connection string: %s", connectionName))
	daemonSet, err := manager.helper.GetCloudSQLProxyDaemonSet("cloudsql-proxy")
	if err != nil {
		return err
	}

	connectionNameEnv := "INSTANCES"
	envs := daemonSet.Spec.Template.Spec.Containers[0].Env

	for idx, value := range envs {
		if value.Name == connectionNameEnv {
			envs[idx] = corev1.EnvVar{
				Name:  connectionNameEnv,
				Value: connectionName + "=tcp:0.0.0.0:5432",
			}
		}
	}

	if err := manager.helper.UpdateDaemonSet(daemonSet); err != nil {
		return err
	}
	if err := manager.helper.DeletePodsByLabel(CloudSQLProxySelectors); err != nil {
		return err
	}

	return nil
}

func (manager *CloudSQLDRManager) waitTillStandbyIsSynced() error {
	pgC := pgClient.GetPostgresClientForHost(CloudSqlProxyHost)
	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 15*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		conn, err := pgC.GetConnection()
		if err != nil {
			return false, err
		}
		defer conn.Release()

		rows, err := conn.Query(context.Background(), ReplCheckQuery)
		if err != nil {
			return false, err
		}
		var replicationLag int
		for rows.Next() {
			err = rows.Scan(&replicationLag)
			if err != nil {
				log.Error("Error occurred during scan databases row", zap.Error(err))
				return false, err
			}
		}
		log.Info(fmt.Sprintf("Replication Lag Value: %v", replicationLag))
		if replicationLag != 0 {
			log.Warn("There is replication lag for standby database, retrying ...")
			return false, nil
		}
		log.Info("There is no replication lag, proceeding with the flow ...")
		return true, nil
	})
}

func (manager *CloudSQLDRManager) processRequest(request v1.SiteManagerStatus) {
	if err := retry.Do(
		func() error {
			return manager.changeMode(request.Mode)
		}); err != nil {
		log.Error("Failed to change mode", zap.Error(err))
		if err := manager.helper.UpdateSiteManagerStatus(request.Mode, "failed"); err != nil {
			log.Error("Failed to update site manager status", zap.Error(err))
		}
	}
	if err := manager.helper.UpdateSiteManagerStatus(request.Mode, "done"); err != nil {
		log.Error("Failed to update site manager status", zap.Error(err))
	}
}

func (sqlClient *CloudSqlClient) waitForOperationComplete(opName string) error {
	log.Info(fmt.Sprintf("Waiting for operation: %s to complete", opName))
	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 15*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		getInstanceCall := sqlClient.service.Operations.Get(sqlClient.project, opName)
		operation, callErr := getInstanceCall.Do()
		if callErr != nil {
			return false, callErr
		}
		log.Info(fmt.Sprintf("Status: %s of operation: %s", operation.Status, opName))
		if operation.Status != "DONE" {
			log.Info("Operation status is not DONE yet")
			if operation.Error != nil {
				opErr := operation.Error
				for _, e := range opErr.Errors {
					log.Error(fmt.Sprintf("there is a opError with kind: %s, message: %s "+
						"code: %s for operation: %s", e.Kind, e.Message, e.Code, opName))
				}
				return false, fmt.Errorf("operation %s failed due to error during operation", opName)
			}
			return false, nil
		} else {
			return true, nil
		}
	})
	return err
}

func (sqlClient *CloudSqlClient) promoteReplica(instance string) error {
	log.Info(fmt.Sprintf("Start to promote replica: %s", instance))
	start := time.Now()
	op := &sqladmin.Operation{}
	if err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 15*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		log.Info("Trying to insert new CloudSQL Instance with retries.")
		promoteCall := sqlClient.service.Instances.PromoteReplica(sqlClient.project, instance)
		op, err = promoteCall.Do()
		if googleapi.IsNotModified(err) {
			log.Warn("Instance has not been modified, seems like replica already promoted")
			return true, nil
		} else if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 409 {
			log.Warn(fmt.Sprintf("There is a 409 error: %s, retrying", gerr.Message))
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}
	if err := sqlClient.waitForOperationComplete(op.Name); err != nil {
		return err
	}
	elapsed := time.Since(start)
	log.Info(fmt.Sprintf("PromoteReplica took %s", elapsed))
	return nil
}

func (sqlClient *CloudSqlClient) enableHaForInstance(instance string) error {
	log.Info(fmt.Sprintf("Start to enabling HA for instance: %s", instance))
	database := sqladmin.DatabaseInstance{Settings: &sqladmin.Settings{AvailabilityType: "REGIONAL"}}
	start := time.Now()
	if err := sqlClient.patchInstance(instance, database); err != nil {
		return err
	}
	elapsed := time.Since(start)
	log.Info(fmt.Sprintf("Enabling HA took %s", elapsed))
	return nil
}

func (sqlClient *CloudSqlClient) patchInstance(instance string, database sqladmin.DatabaseInstance) error {
	op := &sqladmin.Operation{}
	if err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		log.Info("Trying to patch CloudSQL Instance with retries.")
		patchCall := sqlClient.service.Instances.Patch(sqlClient.project, instance, &database)
		op, err = patchCall.Do()
		if googleapi.IsNotModified(err) {
			log.Warn("Instance has not been modified, seems like replica already promoted")
			return true, nil
		} else if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 409 {
			log.Warn(fmt.Sprintf("There is a 409 error: %s, retrying", gerr.Message))
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}
	if err := sqlClient.waitForOperationComplete(op.Name); err != nil {
		return err
	}
	return nil
}

func (sqlClient *CloudSqlClient) getInstanceInCurrentRegion() (*sqladmin.DatabaseInstance, error) {
	listInstanceCall := sqlClient.service.Instances.List(sqlClient.project)
	listResponse, err := listInstanceCall.Do()
	if err != nil {
		return &sqladmin.DatabaseInstance{}, err
	}
	for _, instance := range listResponse.Items {
		labels, region := instance.Settings.UserLabels, instance.Region

		if region != sqlClient.region {
			continue
		}

		nsCheck := false

		if nsValue, ok := labels["namespace"]; ok {
			nsCheck = nsValue == namespace
		} else {
			continue
		}

		if nsCheck {
			return instance, nil
		}
		continue
	}
	return nil, nil
}

func (sqlClient *CloudSqlClient) getPrimaryNotInCurrentRegion() (*sqladmin.DatabaseInstance, error) {
	listInstanceCall := sqlClient.service.Instances.List(sqlClient.project)
	listResponse, err := listInstanceCall.Do()
	if err != nil {
		return &sqladmin.DatabaseInstance{}, err
	}
	for _, instance := range listResponse.Items {
		labels, region := instance.Settings.UserLabels, instance.Region

		if region == sqlClient.region {
			continue
		}

		nsCheck := false

		if nsValue, ok := labels["namespace"]; ok {
			nsCheck = nsValue == namespace
		} else {
			continue
		}

		if nsCheck {
			return instance, nil
		}
		continue
	}
	return nil, nil
}

func (sqlClient *CloudSqlClient) createReadReplicaForPrimary(primaryInstance *sqladmin.DatabaseInstance) error {
	instanceName := namespace + "-" + sqlClient.region + "-" + strconv.Itoa(int(time.Now().Unix()))
	log.Info(fmt.Sprintf("Will create instance with name: %s in region: %s", instanceName, sqlClient.region))
	primaryInstance.Settings.IpConfiguration.ForceSendFields = []string{"Ipv4Enabled"}
	primaryInstance.Settings.IpConfiguration.Ipv4Enabled = false
	database := sqladmin.DatabaseInstance{
		Name:               instanceName,
		DatabaseVersion:    primaryInstance.DatabaseVersion,
		InstanceType:       "READ_REPLICA_INSTANCE",
		MasterInstanceName: primaryInstance.Name,
		Region:             sqlClient.region,
		Settings: &sqladmin.Settings{
			UserLabels: map[string]string{
				"namespace": namespace,
			},
			Tier:            primaryInstance.Settings.Tier,
			IpConfiguration: primaryInstance.Settings.IpConfiguration,
		},
	}

	op := &sqladmin.Operation{}
	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		log.Info("Trying to insert new CloudSQL Instance with retries.")
		databasesInsertCall := sqlClient.service.Instances.Insert(sqlClient.project, &database)
		op, err = databasesInsertCall.Do()
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 409 {
			log.Warn(fmt.Sprintf("There is a 409 error: %s, retrying", gerr.Message))
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return err
	}

	if err := sqlClient.waitForOperationComplete(op.Name); err != nil {
		return err
	}
	return nil
}

func (sqlClient *CloudSqlClient) getReplicaInCurrentRegion() (*sqladmin.DatabaseInstance, error) {
	return sqlClient.getInstanceInCurrentRegionByType(ReadReplicaType)
}

func (sqlClient *CloudSqlClient) getActiveInstanceInCurrentRegion() (*sqladmin.DatabaseInstance, error) {
	return sqlClient.getInstanceInCurrentRegionByType(CloudSqlType)
}

func (sqlClient *CloudSqlClient) getInstanceInCurrentRegionByType(instanceType string) (*sqladmin.DatabaseInstance, error) {
	//retr
	listInstanceCall := sqlClient.service.Instances.List(sqlClient.project)
	listResponse, err := listInstanceCall.Do()
	if err != nil {
		return nil, err
	}
	for _, instance := range listResponse.Items {

		labels, region := instance.Settings.UserLabels, instance.Region
		if region != sqlClient.region {
			continue
		}
		if instanceType != instance.InstanceType {
			continue
		}

		nsCheck := false
		typeCheck := instanceType == instance.InstanceType

		if nsValue, ok := labels["namespace"]; ok {
			nsCheck = nsValue == namespace
		} else {
			continue
		}

		if typeCheck && nsCheck {
			return instance, nil
		}
		continue
	}
	return nil, nil
}

func (sqlClient *CloudSqlClient) dropInstance(instance string) error {
	log.Info(fmt.Sprintf("Will drop instance %s", instance))
	operation := &sqladmin.Operation{}
	if err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		log.Info("Trying to drop CloudSQL Instance with retries.")
		deleteInstanceCall := sqlClient.service.Instances.Delete(sqlClient.project, instance)
		operation, err = deleteInstanceCall.Do()
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 409 {
			log.Warn(fmt.Sprintf("There is a 409 error: %s, retrying", gerr.Message))
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Drop Instance Operation Name %s", operation.Name))
	return nil
}

func (manager *CloudSQLDRManager) processPreConfigureRequest(response http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		preConfigureStatus, err := manager.getPreConfigureStatus()
		if err != nil {
			log.Error("there is an error during pre-configure status read ", zap.Error(err))
			sendResponse(response, http.StatusInternalServerError, preConfigureStatus)
		}
		sendResponse(response, http.StatusOK, preConfigureStatus)
	case "POST":
		statusRequest, err := parseSiteManagerStatusFromRequest(req)
		if err != nil {
			log.Error("Failed to parse sm status from request", zap.Error(err))
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		preConfigureStatus, err := manager.getPreConfigureStatus()
		if err != nil {
			log.Error("there is an error during pre-configure status read ", zap.Error(err))
			sendResponse(response, http.StatusInternalServerError, preConfigureStatus)
		}
		if preConfigureStatus.Status == "running" {
			log.Info("Received request during running procedure, return current state")
			sendResponse(response, http.StatusOK, preConfigureStatus)
		} else if statusRequest.Mode == preConfigureStatus.Mode &&
			preConfigureStatus.Status == "done" {
			log.Info("Desired status equals to current status, return current state")
			sendResponse(response, http.StatusOK, preConfigureStatus)
			return
		}
		log.Info(fmt.Sprintf("processPreConfigureRequest invoked with mode: %s", statusRequest.Mode))
		if err := manager.setStatusForPreConfigure(statusRequest.Mode, "running"); err != nil {
			log.Error("Failed to set sm status", zap.Error(err))
			http.Error(response, err.Error(), http.StatusInternalServerError)
		}
		preConfigureStatus, err = manager.getPreConfigureStatus()
		if err != nil {
			log.Error("there is an error during pre-configure status read ", zap.Error(err))
			sendResponse(response, http.StatusInternalServerError, preConfigureStatus)
		}
		sendResponse(response, http.StatusOK, preConfigureStatus)

		// async process of request
		go manager.doPreConfigure(statusRequest)
	}
}

func (manager *CloudSQLDRManager) doPreConfigure(request v1.SiteManagerStatus) {
	if err := func(mode string, noWait bool) error {
		if mode == "active" {
			log.Info("Skipping Pre Configuration for standby -> active change")
			time.Sleep(30 * time.Second)
		} else if mode == "standby" {
			if !noWait {
				log.Info("No-Wait flag has been passed as false, doing replication check")
				_ = manager.waitTillStandbyIsSynced()
				// if err := manager.waitTillStandbyIsSynced(); err != nil {
				// 	//return err
				// }
			}
			cloudSQlClient := manager.sqlClient
			dbInstance, err := cloudSQlClient.getPrimaryNotInCurrentRegion()
			log.Info(fmt.Sprintf("Replica found: %s", dbInstance.Name))
			if err != nil {
				return err
			}
			// terminate connections
			if err := manager.helper.TerminateActiveConnectionsForHost(CloudSqlProxyHost); err != nil {
				log.Error("Failed to terminate active connections", zap.Error(err))
				return err
			}

			// reconfigure cloudsql proxy with standby
			if err := manager.reconfigureCloudSqlProxy(dbInstance.ConnectionName); err != nil {
				log.Error("Failed to reconfigure CloudSQL proxy", zap.Error(err))
				return err
			}
		}
		return nil
	}(request.Mode, request.NoWait); err != nil {
		log.Error("There is an error, during pre-configure call", zap.Error(err))
		_ = manager.setStatusForPreConfigure(request.Mode, "failed")
	} else {
		_ = manager.setStatusForPreConfigure(request.Mode, "done")
	}
}

func (manager *CloudSQLDRManager) setStatusForPreConfigure(mode string, status string) error {
	currentStatus := v1.SiteManagerStatus{Mode: mode, Status: status}
	statusAsJson, _ := json.Marshal(currentStatus)
	if err := os.WriteFile("/tmp/.pre-configure-status.json", statusAsJson, 0644); err != nil {
		return err
	}
	return nil
}

func (manager *CloudSQLDRManager) getPreConfigureStatus() (v1.SiteManagerStatus, error) {
	status := v1.SiteManagerStatus{}
	file, err := os.ReadFile("/tmp/.pre-configure-status.json")
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(file, &status); err != nil {
		return status, err
	}
	return status, nil
}
