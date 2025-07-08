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

package helper

import (
	"bytes"
	"context"
	genericerror "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var pHelper *PatroniHelper = nil

type PatroniHelper struct {
	ResourceManager
	cr qubershipv1.PatroniCore
}

func GetPatroniHelper() *PatroniHelper {
	if pHelper == nil {
		logger.Info("helper has not been initialized yet")
		kubeClient, _ := util.GetClient()
		pHelper = &PatroniHelper{
			ResourceManager: ResourceManager{
				kubeClient:    kubeClient,
				kubeClientSet: util.GetKubeClient(),
			},
		}
	}
	return pHelper
}

func (ph *PatroniHelper) UpdatePatroniCore(service *qubershipv1.PatroniCore) error {
	err := ph.kubeClient.Update(context.TODO(), service)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to update PatroniCore %v", service.ObjectMeta.Name), zap.Error(err))
		return err
	}
	return nil
}

func (ph *PatroniHelper) AddNameAndUID(name string, uid types.UID, kind string) error {
	if pHelper == nil {
		message := "cannot set Name and UID, patroni helper has not been initialized yet"
		err := fmt.Errorf("%s", message)
		logger.Error(message, zap.Error(err))
		return err
	}
	ph.ResourceManager.name = name
	ph.ResourceManager.uid = uid
	ph.ResourceManager.kind = kind
	return nil
}

func (ph *PatroniHelper) SetCustomResource(cr *qubershipv1.PatroniCore) error {
	if pHelper == nil {
		message := "cannot set Custom Resource, patroni helper has not been initialized yet"
		err := fmt.Errorf("%s", message)
		logger.Error(message, zap.Error(err))
		return err
	}
	ph.cr = *cr
	return nil
}

func (ph *PatroniHelper) GetCustomResource() qubershipv1.PatroniCore {
	return ph.cr
}

func (ph *PatroniHelper) UpdatePostgresService(service *qubershipv1.PatroniCore) error {
	err := ph.kubeClient.Update(context.TODO(), service)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to update PatroniCore %v", service.ObjectMeta.Name), zap.Error(err))
		return err
	}
	return nil
}

//func (ph *PatroniHelper) IsPatroniClusterHealthy(config *ClusterStatus) bool {
//	logger.Info("Check Is Patroni Cluster Healthy")
//
//	cr, _ := ph.GetPatroniCoreCR()
//	expectedMembersNum := cr.Spec.Patroni.Replicas
//	allMembersAreRunning := ph.arePatroniMembersRunning(*config)
//	isLeaderExist := ph.isLeaderExist(*config)
//	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
//	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Healthy: allMembersAreRunning: %t; isLeaderExist: %t; expectedMembersCount: %t;", allMembersAreRunning, isLeaderExist, expectedMembersCount))
//	return allMembersAreRunning && isLeaderExist && expectedMembersCount
//
//}
//
//func (ph *PatroniHelper) IsPatroniClusterDegraded(config *ClusterStatus, pgHost string) bool {
//	logger.Info("Check Is Patroni Cluster Degraded")
//
//	cr, _ := ph.GetPatroniCoreCR()
//	expectedMembersNum := cr.Spec.Patroni.Replicas
//	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
//	isExpectedReplicationCount := ph.isExpectedReplicationCount(pgHost, expectedMembersNum-1)
//	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Degraded: isExpectedReplicationCount: %t; expectedMembersCount: %t;", isExpectedReplicationCount, expectedMembersCount))
//	return !(isExpectedReplicationCount && expectedMembersCount)
//}func (ph *PatroniHelper) IsPatroniClusterHealthy(config *ClusterStatus) bool {
//	logger.Info("Check Is Patroni Cluster Healthy")
//
//	cr, _ := ph.GetPatroniCoreCR()
//	expectedMembersNum := cr.Spec.Patroni.Replicas
//	allMembersAreRunning := ph.arePatroniMembersRunning(*config)
//	isLeaderExist := ph.isLeaderExist(*config)
//	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
//	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Healthy: allMembersAreRunning: %t; isLeaderExist: %t; expectedMembersCount: %t;", allMembersAreRunning, isLeaderExist, expectedMembersCount))
//	return allMembersAreRunning && isLeaderExist && expectedMembersCount
//
//}
//
//func (ph *PatroniHelper) IsPatroniClusterDegraded(config *ClusterStatus, pgHost string) bool {
//	logger.Info("Check Is Patroni Cluster Degraded")
//
//	cr, _ := ph.GetPatroniCoreCR()
//	expectedMembersNum := cr.Spec.Patroni.Replicas
//	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
//	isExpectedReplicationCount := ph.isExpectedReplicationCount(pgHost, expectedMembersNum-1)
//	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Degraded: isExpectedReplicationCount: %t; expectedMembersCount: %t;", isExpectedReplicationCount, expectedMembersCount))
//	return !(isExpectedReplicationCount && expectedMembersCount)
//}

func (ph *PatroniHelper) arePatroniMembersRunning(config ClusterStatus) bool {
	logger.Debug("Check are Patroni Members Running")

	members := config.Members
	for i := 0; i < len(members); i++ {
		state := members[i].State
		if !slices.Contains(patroniRunningState, state) {
			logger.Error("Not all patroni cluster members are running")
			return false
		}
	}
	logger.Debug("Check are Patroni Members Running - True")
	return true
}

func (ph *PatroniHelper) isExpectedReplicationCount(pgHost string, count int) bool {
	logger.Debug("Check Replication Count")

	query := "select pid from pg_stat_replication where not exists (select active_pid from pg_replication_slots rep_slots where pid = rep_slots.active_pid and plugin is not null) and usename='replicator' and state='streaming'"
	pgC := pgClient.GetPostgresClient(pgHost)
	if pgC == nil {
		return false
	}
	if rows, err := pgC.Query(query); err == nil {
		var pids []int
		for rows.Next() {
			var pid int
			err = rows.Scan(&pid)
			if err != nil {
				logger.Error("Error occurred during obtain pid", zap.Error(err))
				return false
			}
			pids = append(pids, pid)
		}
		logger.Debug(fmt.Sprintf("Pids with streaming state: %d Lenght: %d. Expected %d", pids, len(pids), count))
		return len(pids) >= count
	}
	return false
}

func (ph *PatroniHelper) ifExpectedCountOfMembers(count int, config ClusterStatus) bool {
	logger.Debug("Check If Expected Count Of Members")

	members := config.Members
	if len(members) == count {
		logger.Debug("Check If Expected Count Of Members - True")
		return true
	}
	logger.Debug("Check If Expected Count Of Members - False")
	return false
}

func (ph *PatroniHelper) isLeaderExist(config ClusterStatus) bool {
	logger.Debug("Check is Leader Exist")
	members := config.Members
	for i := 0; i < len(members); i++ {
		role := members[i].Role
		if role == "leader" || role == "standby_leader" {
			logger.Debug("Check is Leader Exist - True")
			return true
		}
	}
	logger.Debug("Check is Leader Exist - False")
	return false
}

func (ph *PatroniHelper) WaitUntilReconcileIsDone() error {
	var cr *qubershipv1.PatroniCore
	time.Sleep(10 * time.Second)
	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		logger.Info("Waiting while reconcile status will be successful")
		if cr, err = ph.GetPatroniCoreCR(); err != nil {
			logger.Error("Error occurred during read of CR.", zap.Error(err))
			return false, err
		}
		if strings.ToLower(cr.Status.Conditions[0].Type) == "in progress" {
			logger.Info(fmt.Sprintf("Recocile status is not done yet, current status: %s.", cr.Status.Conditions[0].Type), zap.Error(err))
			return false, nil
		}
		if strings.ToLower(cr.Status.Conditions[0].Type) == "failed" {
			logger.Error("Recocile status failed, please fix your cluster and try again", zap.Error(err))
			return true, genericerror.New("Reconcile status failed")
		}
		return true, nil
	})
	return err
}

func GetAllDatabases(pgC *pgClient.PostgresClient) (databases []string) {
	if pgC == nil {
		logger.Warn("not able to get databases list, postgresql is empty")
		return
	}
	rows, err := pgC.Query("SELECT datname FROM pg_database where datname not in ('template0');")
	if err != nil {
		logger.Error("cannot get database list", zap.Error(err))
		return
	}

	for rows.Next() {
		var db string
		if err = rows.Scan(&db); err != nil {
			logger.Error("cannot read database from databases list", zap.Error(err))
		}
		databases = append(databases, db)
	}
	return
}

func UpdatePreloadLibraries(cr *qubershipv1.PatroniCore, preloadLibraries []string) {
	logger.Info(fmt.Sprintf("Shared preload libraries %v will be added to config", preloadLibraries))
	for i, param := range cr.Spec.Patroni.PostgreSQLParams {
		param = strings.Replace(param, "=", ":", 1)
		splittedParam := strings.Split(param, ":")
		splittedParam[0] = strings.TrimSpace(splittedParam[0])
		if splittedParam[0] == "shared_preload_libraries" {
			splittedParam[1] = strings.TrimSpace(splittedParam[1])
			for _, l := range preloadLibraries {
				if !strings.Contains(splittedParam[1], l) {
					splittedParam[1] = splittedParam[1] + ", " + l
				}
			}
			cr.Spec.Patroni.PostgreSQLParams[i] = splittedParam[0] + ": " + splittedParam[1]
		}
	}
	//helper.UpdatePostgresService()
}

// Execute a command in a Patroni pod's specific container
func (ph *PatroniHelper) ExecCmdOnPatroniPod(podName string, namespace string, command string) (string, string, error) {
	var container string
	if strings.Contains(podName, "pg-major-upgrade-check") {
		container = "pg-upgrade-check"
	} else {
		container = util.GetContainerNameForPatroniPod(podName)
	}
	return ph.ExecCmdOnPod(podName, namespace, container, command)
}

// Execute a command in any pod's container
func (ph *PatroniHelper) ExecCmdOnPod(podName string, namespace string, container string, command string) (string, string, error) {
	client := ph.kubeClientSet
	logger.Debug(fmt.Sprintf("Executing shell command: %s on pod %s, container %s", command, podName, container))

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	execParams := &v1.PodExecOptions{
		Command:   []string{"/bin/sh", "-c", command},
		Container: container,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}

	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	request := client.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(execParams, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", request.URL())
	if err != nil {
		return "", "", fmt.Errorf("error creating SPDY executor: %v", err)
	}

	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdout: buf,
		Stderr: errBuf,
	})

	if err != nil {
		logger.Error(fmt.Sprintf("Executing shell command: Error: %v\nstderr: %v", err, errBuf.String()))
		return "", errBuf.String(), fmt.Errorf("failed executing command %s on %s/%s: %w", command, namespace, podName, err)
	}

	return buf.String(), errBuf.String(), nil
}

func (ph *PatroniHelper) RevokeGrantOnPublicSchema(pgHost string) error {
	const dbName = "template1"
	pgC := pgClient.GetPostgresClient(pgHost)
	query := "REVOKE ALL ON SCHEMA public FROM public;"

	if err := pgC.ExecuteForDB(dbName, query); err != nil {
		logger.Error(fmt.Sprintf("Error during revoking grants from public schema for database %s", dbName), zap.Error(err))
	}
	return nil
}

func (ph *PatroniHelper) SyncReplicatorPassword(pgHost string) error {
	password := util.GetEnv("PG_REPLICATOR_PASSWORD", "replicator")
	pgC := pgClient.GetPostgresClient(pgHost)
	if pgC == nil {
		return errors.New("Can't create Postgres Client")
	}
	if _, err := pgC.Query(fmt.Sprintf("alter role replicator with password '%s';", pgClient.EscapeString(password))); err != nil {
		logger.Error("Error during change password", zap.Error(err))
		return err
	}
	return nil
}

func (ph *PatroniHelper) GetClient() client.Client {
	return ph.kubeClient
}

func (ph *PatroniHelper) TerminateActiveConnections(pgHost string) error {
	logger.Info("Termination of active connections")
	pgC := pgClient.GetPostgresClient(pgHost)
	if pgC == nil {
		logger.Warn("not able to drop active connections, postgresql is not working, skipping")
		return nil
	}
	_, err := pgC.Query(TerminateConnectionsQuery)
	return err
}

func (ph *PatroniHelper) CreatePgAdminRole(pgHost string) error {
	logger.Info("Creating role 'pgadminrole'")

	pgC := pgClient.GetPostgresClient(pgHost)
	if pgC == nil {
		logger.Warn("Not able to create 'pgadminrole', PostgreSQL client is not available, skipping")
		return nil
	}

	query := "CREATE ROLE pgadminrole WITH LOGIN;"

	_, err := pgC.Query(query)
	if err != nil {
		logger.Error("Error during creating role 'pgadminrole'", zap.Error(err))
		return err
	}

	logger.Info("Successfully created role 'pgadminrole'")
	return nil
}

func (ph *PatroniHelper) AddStandbyClusterConfigurationConfigMap(patroniUrl string) error {
	logger.Info("Add standby cluster configuration in patroni config map")
	logger.Info(fmt.Sprintf("Going to Patroni API url: %s", patroniUrl))
	standbyClusterParm := map[string]interface{}{
		"standby_cluster": ph.getStandbyClusterConfigurationFromSiteManager(),
	}
	if err := patroni.UpdatePatroniConfig(standbyClusterParm, patroniUrl); err != nil {
		logger.Error("Failed to update patroni config, exiting", zap.Error(err))
		return err
	}
	return nil
}

func (ph *PatroniHelper) ClearStandbyClusterConfigurationConfigMap(patroniUrl string) error {
	logger.Info("Clear standby cluster configuration in patroni config map")
	emptyStandbyClusterParm := map[string]interface{}{
		"standby_cluster": "",
	}
	if err := patroni.UpdatePatroniConfig(emptyStandbyClusterParm, patroniUrl); err != nil {
		logger.Error("Failed to update patroni config, exiting", zap.Error(err))
		return err
	}
	return nil
}

func (ph *PatroniHelper) getStandbyClusterConfigurationFromSiteManager() map[string]interface{} {
	if cr, err := ph.GetPostgresServiceCR(); err == nil {
		if coreCr, err := ph.GetPatroniCoreCR(); err == nil {
			host := cr.Spec.SiteManager.ActiveClusterHost
			port := cr.Spec.SiteManager.ActiveClusterPort
			return patroni.GetStandbyClusterConfigurationWithHost(coreCr, host, port)
		}
		return nil
	}
	return nil
}

func (ph *PatroniHelper) DeleteCleanerInitContainer(clusterName string) error {
	patroniDeploymentName := fmt.Sprintf("pg-%s-node", clusterName)
	if satefulsetsList, err := ph.ResourceManager.GetStatefulsetByNameRegExp(patroniDeploymentName); err != nil {
		logger.Error("Can't get Patroni Deployments", zap.Error(err))
		return err
	} else {
		return ph.ResourceManager.DeleteInitContainer(satefulsetsList, "pg-cleaner")
	}
}

func (ph *PatroniHelper) IsHealthy(patroniUrl string, pgHost string) bool {
	config, err := ph.GetPatroniClusterConfig(patroniUrl)
	if err == nil {
		isHealthy := ph.IsPatroniClusterHealthy(config)
		isDegraded := ph.IsPatroniClusterDegraded(config, pgHost)
		return isHealthy && !isDegraded
	}
	return false
}

func (ph *PatroniHelper) IsHealthyDuringUpdate(patroniUrl string, pgHost string, replicas int) bool {
	config, err := ph.GetPatroniClusterConfig(patroniUrl)
	if err == nil {
		isHealthy := ph.IsPatroniClusterHealthyDuringUpdate(config, replicas)
		isDegraded := ph.IsPatroniClusterDegradedDuringUpdate(config, pgHost, replicas)
		return isHealthy && !isDegraded
	}
	return false
}

func (ph *PatroniHelper) IsPatroniClusterHealthyDuringUpdate(config *ClusterStatus, replicas int) bool {
	logger.Info("Check Is Patroni Cluster Healthy during update")
	expectedMembersNum := replicas
	logger.Info(fmt.Sprintf("expectedMembersNum count in update: %v", expectedMembersNum))
	currentReplicaCountforLogs := len(config.Members)
	logger.Info(fmt.Sprintf("currentReplicaCountforLogs count in update: %v", currentReplicaCountforLogs))

	allMembersAreRunning := ph.arePatroniMembersRunning(*config)
	isLeaderExist := ph.isLeaderExist(*config)
	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Healthy: allMembersAreRunning: %t; isLeaderExist: %t; expectedMembersCount: %t;", allMembersAreRunning, isLeaderExist, expectedMembersCount))
	return allMembersAreRunning && isLeaderExist && expectedMembersCount

}
func (ph *PatroniHelper) IsPatroniClusterDegradedDuringUpdate(config *ClusterStatus, pgHost string, replicas int) bool {
	logger.Info("Check Is Patroni Cluster Degraded during update")
	expectedMembersNum := replicas
	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
	isExpectedReplicationCount := ph.isExpectedReplicationCount(pgHost, expectedMembersNum-1)
	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Degraded: isExpectedReplicationCount: %t; expectedMembersCount: %t;", isExpectedReplicationCount, expectedMembersCount))
	return !(isExpectedReplicationCount && expectedMembersCount)
}

func (ph *PatroniHelper) IsPatroniClusterHealthy(config *ClusterStatus) bool {
	logger.Info("Check Is Patroni Cluster Healthy")
	cr, err := ph.GetPatroniCoreCR()
	if err != nil {
		logger.Info(fmt.Sprintf("While getting PatroniCore CR an error occured: %v", err))
		return false
	}
	expectedMembersNum := cr.Spec.Patroni.Replicas
	allMembersAreRunning := ph.arePatroniMembersRunning(*config)
	isLeaderExist := ph.isLeaderExist(*config)
	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Healthy: allMembersAreRunning: %t; isLeaderExist: %t; expectedMembersCount: %t;", allMembersAreRunning, isLeaderExist, expectedMembersCount))
	return allMembersAreRunning && isLeaderExist && expectedMembersCount

}
func (ph *PatroniHelper) IsPatroniClusterDegraded(config *ClusterStatus, pgHost string) bool {
	logger.Info("Check Is Patroni Cluster Degraded")

	cr, _ := ph.GetPatroniCoreCR()
	expectedMembersNum := cr.Spec.Patroni.Replicas
	expectedMembersCount := ph.ifExpectedCountOfMembers(expectedMembersNum, *config)
	isExpectedReplicationCount := ph.isExpectedReplicationCount(pgHost, expectedMembersNum-1)
	logger.Info(fmt.Sprintf("Check Is Patroni Cluster Degraded: isExpectedReplicationCount: %t; expectedMembersCount: %t;", isExpectedReplicationCount, expectedMembersCount))
	return !(isExpectedReplicationCount && expectedMembersCount)
}

func (ph *PatroniHelper) StoreDataToCM(key string, value string) {
	logger.Info(fmt.Sprintf("Store key: %s, value: %s to deployment-info", key, value))
	deploymentInfoCM, err := util.FindCmInNamespaceByName(namespace, "deployment-info")
	if err == nil {
		deploymentInfoCM.Data[key] = strings.TrimSpace(value)
	} else {
		logger.Warn("Cant to find config map deployment-info for update. Creating new one", zap.Error(err))
		deploymentInfoCM = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deployment-info",
				Namespace: namespace,
			},
			Data: map[string]string{key: strings.TrimSpace(value)},
		}
	}
	if _, err := ph.CreateOrUpdateConfigMap(deploymentInfoCM); err != nil {
		logger.Error("Failed to create or update config map deployment-info", zap.Error(err))
	}
}

func (ph *PatroniHelper) GetPGVersion(podName string) string {

	versionCM, err := util.FindCmInNamespaceByName(namespace, "deployment-info")
	if err != nil || versionCM.Data["pg-version"] == "" {
		return ph.GetPGVersionFromPod(podName)
	}
	return strings.TrimSpace(versionCM.Data["pg-version"])
}

func (ph *PatroniHelper) GetPGVersionFromPod(podName string) string {
	command := "pg_config --version | awk '{print $2}' | cut -d'.' -f1"
	version, errMsg, err := ph.ExecCmdOnPatroniPod(podName, namespace, command)
	if err != nil || version == "" {
		logger.Warn(fmt.Sprintf("Can't read current postgres version. errMsg: %s", errMsg))
		return ""
	}
	return strings.TrimSpace(version)
}
func (ph *PatroniHelper) GetLocaleVersion(podName string) string {

	versionCM, err := util.FindCmInNamespaceByName(namespace, "deployment-info")
	if err != nil || versionCM.Data["locale-version"] == "" {
		return ph.GetLocaleVersionFromPod(podName)
	}
	return strings.TrimSpace(versionCM.Data["locale-version"])
}

func (ph *PatroniHelper) GetLocaleVersionFromPod(podName string) string {
	masterPodName := podName
	command := "locale --version | grep  \"[0-9]*\" | head -n 1 | awk -F ' ' '{ print $NF }'"
	version, errMsg, err := ph.ExecCmdOnPatroniPod(masterPodName, namespace, command)
	if err != nil || version == "" {
		logger.Warn(fmt.Sprintf("Can't read os locale version. errMsg: %s", errMsg))
		return ""
	}
	return strings.TrimSpace(version)
}

func (ph *PatroniHelper) PausePatroni(patroniUrl string) error {
	logger.Info("Pause Patroni")
	patch := map[string]interface{}{"pause": "true"}
	return patroni.UpdatePatroniConfig(patch, patroniUrl)
}

func (ph *PatroniHelper) ResumePatroni(patroniUrl string) error {
	logger.Info("Resume Patroni")
	patch := map[string]interface{}{"pause": "false"}
	return patroni.UpdatePatroniConfig(patch, patroniUrl)
}

func (ph *PatroniHelper) GetStatefulSetIds(statefulsets []*appsv1.StatefulSet) ([]int, error) {
	ids := []int{}
	for _, eStatefulset := range statefulsets {
		eStatefulSetName := eStatefulset.Name
		statefulsetIdx, err := strconv.Atoi(eStatefulSetName[len(eStatefulSetName)-1:])
		if err != nil {
			logger.Error(fmt.Sprintf("can't parse %v last symbol as int", eStatefulSetName), zap.Error(err))
			return nil, err
		}
		ids = append(ids, statefulsetIdx)
	}
	return ids, nil
}
