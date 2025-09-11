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
	"context"
	genericerror "errors"
	"fmt"
	"strings"
	"time"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	k8sauth "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	logger              = util.GetLogger()
	namespace           = util.GetNameSpace()
	MasterLabel         = map[string]string{"pgtype": "master"}
	ReplicasLabel       = map[string]string{"pgtype": "replica"}
	authHeaders         = map[string]AuthPair{}
	patroniRunningState = []string{"running", "streaming"}

	helper *Helper = nil
)

type ClusterStatus struct {
	Members []Member
}

type Member struct {
	Name     string
	Role     string
	State    string
	ApiUrl   string
	Host     string
	Port     int
	Timeline int
}

type Helper struct {
	ResourceManager
	cr qubershipv1.PatroniServices
}

func GetHelper() *Helper {
	if helper == nil {
		logger.Info("Helper will be initialized")
		kubeClient, _ := util.GetClient()
		helper = &Helper{
			ResourceManager: ResourceManager{
				kubeClient:    kubeClient,
				kubeClientSet: util.GetKubeClient(),
			},
		}
	}
	return helper
}

func (h *Helper) AddNameAndUID(name string, uid types.UID, kind string) error {
	if helper == nil {
		message := "cannot set Name and UID, helper has not been initialized yet"
		err := fmt.Errorf("%s", message)
		logger.Error(message, zap.Error(err))
		return err
	}
	h.ResourceManager.name = name
	h.ResourceManager.uid = uid
	h.ResourceManager.kind = kind
	return nil
}
func (h *Helper) SetCustomResource(cr *qubershipv1.PatroniServices) error {
	if helper == nil {
		message := "cannot set Custom Resource, helper has not been initialized yet"
		err := fmt.Errorf("%s", message)
		logger.Error(message, zap.Error(err))
		return err
	}
	h.cr = *cr
	return nil
}

func (h *Helper) GetCustomResource() qubershipv1.PatroniServices {
	return h.cr
}

func (h *Helper) GetClient() client.Client {
	return h.kubeClient
}

func (h *Helper) UpdatePostgresService(service *qubershipv1.PatroniServices) error {
	err := h.kubeClient.Update(context.TODO(), service)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to update PatroniServices %v", service.ObjectMeta.Name), zap.Error(err))
		return err
	}
	return nil
}

func (h *Helper) PausePatroni(patroniUrl string) error {
	logger.Info("Pause Patroni")
	patch := map[string]interface{}{"pause": "true"}
	return patroni.UpdatePatroniConfig(patch, patroniUrl)
}

func (h *Helper) ResumePatroni(patroniUrl string) error {
	logger.Info("Resume Patroni")
	patch := map[string]interface{}{"pause": "false"}
	return patroni.UpdatePatroniConfig(patch, patroniUrl)
}

const (
	TerminateConnectionsQuery = "SELECT pg_terminate_backend(pid) FROM pg_stat_activity " +
		"WHERE datname != 'postgres' or datname is NULL and usename != 'replicator' or usename is NULL " +
		"AND pid <> pg_backend_pid()"
)

func (h *Helper) GetCloudSQLProxyDaemonSet(dsName string) (ds *appsv1.DaemonSet, err error) {
	foundDs := &appsv1.DaemonSet{}
	err = h.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: dsName, Namespace: namespace,
	}, foundDs)
	if err != nil {
		return nil, err
	}
	return foundDs, nil
}

func (h *Helper) DeleteDeployment(deployment *appsv1.Deployment) error {
	return h.kubeClient.Delete(context.TODO(), deployment)
}

func (h *Helper) WaitUntilReconcileIsDone() error {
	var cr *qubershipv1.PatroniServices
	time.Sleep(10 * time.Second)
	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		logger.Info("Waiting while reconcile status will be successful")
		if cr, err = h.GetPostgresServiceCR(); err != nil {
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

func authenticated(tokenReview *k8sauth.TokenReview) bool {
	if !tokenReview.Status.Authenticated {
		return false
	}
	userName := tokenReview.Status.User.Username
	return util.GetSmAuthUserName() == userName
}

func CreateExtensionsForDB(pgC *pgClient.PostgresClient, database string, extensions []string) {
	databaseSlice := []string{database}
	CreateExtensionsForDBs(pgC, databaseSlice, extensions)
}

func CreateExtensionsForDBs(pgC *pgClient.PostgresClient, databases, extensions []string) {
	for _, d := range databases {
		if d == "template0" {
			continue
		}
		logger.Info(fmt.Sprintf("extensions %v will be created for database %s", extensions, d))
		for _, e := range extensions {
			if err := pgC.ExecuteForDB(d, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", e)); err != nil {
				logger.Error(fmt.Sprintf("cannot create %s extension in the %s database", e, d), zap.Error(err))
			}
		}
	}
}

func IsUserExist(username string, pg *pgClient.PostgresClient) bool {
	conn, err := pg.GetConnection()
	if err != nil {
		return true
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname = '%s';", username))
	if err != nil {
		logger.Error("error during fetching info about Exporter user")
		return true
	}
	if rows.Next() {
		rows.Close()
		return true
	}
	return false
}

type AuthPair struct {
	time   time.Time
	review *k8sauth.TokenReview
}

func AlterUserPassword(pg *pgClient.PostgresClient, username, password string) error {
	username = pgClient.EscapeString(username)
	password = pgClient.EscapeString(password)
	logger.Info(fmt.Sprintf("Setting password for user \"%s\"", username))
	if err := pg.Execute(fmt.Sprintf("ALTER USER \"%s\" WITH PASSWORD '%s' ;", username, password)); err != nil {
		logger.Error(fmt.Sprintf("cannot modify user %s", username), zap.Error(err))
		return err
	}
	return nil
}

func (h *Helper) TerminateActiveConnectionsForHost(pgHost string) error {
	logger.Info("Termination of active connections")
	pgC := pgClient.GetPostgresClientForHost(pgHost)
	err := pgC.Execute(TerminateConnectionsQuery)
	return err
}

func (h *Helper) UpdateSiteManagerStatus(mode string, status string) error {
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		if cr, err := h.GetPostgresServiceCR(); err == nil {
			cr.Status.SiteManagerStatus = qubershipv1.SiteManagerStatus{Mode: mode, Status: status}
			logger.Info(fmt.Sprintf("Site Manager: Update status. mode=%s, status=%s", mode, status))
			if err = h.ResourceManager.kubeClient.Status().Update(context.TODO(), cr); err != nil {
				logger.Error(fmt.Sprintf(

					"Can't Update Site Manager status. Error: %s", err))
				return false, err
			}
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (h *Helper) GetCurrentSiteManagerStatus() *qubershipv1.SiteManagerStatus {
	if cr, err := h.GetPostgresServiceCR(); err == nil {
		return &cr.Status.SiteManagerStatus
	}
	return nil
}

//
//func (h *Helper) UpdateSiteManagerStatusWithRetry(mode string, status string, clusterName string, patroniUrl string, pgHost string) error {
//	patroniReg := fmt.Sprintf("pg-%s-node", clusterName)
//	logger.Info("Site Manager: Update status with retry")
//	newStatus := "failed"
//	err := wait.PollImmediate(time.Second, 1*time.Minute, func() (done bool, err error) {
//		if mode == "disabled" {
//			if podList, err := h.ResourceManager.GetNamespacePodList(); err != nil {
//				logger.Info("Can not get pods, retrying")
//				return false, nil
//			} else {
//				for podIdx := 0; podIdx < len(podList.Items); podIdx++ {
//					pod := podList.Items[podIdx]
//					if r, e := regexp.MatchString(patroniReg, pod.ObjectMeta.Name); e == nil && r {
//						logger.Info("Waiting for patroni pods scale down.")
//						return false, nil
//					}
//				}
//				newStatus = status
//				return true, nil
//			}
//		}
//		resp, err := http.Get(patroniUrl + clusterName)
//		if err != nil {
//			logger.Error("Get request to patroni status failed, retrying", zap.Error(err))
//			return false, nil
//		}
//		defer func() {
//			_ = resp.Body.Close()
//		}()
//		if resp.StatusCode == http.StatusOK {
//			responseAsJson := map[string]string{}
//			d := json.NewDecoder(resp.Body)
//			err := d.Decode(&responseAsJson)
//			if err != nil {
//				state := responseAsJson["state"]
//				if slices.Contains(patroniRunningState, state) {
//					logger.Info("Patroni has running state")
//					config, err := h.GetPatroniClusterConfig(patroniUrl)
//					if err == nil {
//						isHealthy := h.IsPatroniClusterHealthy(config)
//						isDegraded := h.IsPatroniClusterDegraded(config, pgHost)
//						if isHealthy && !isDegraded {
//							logger.Info("Patroni cluster has UP status")
//							newStatus = status
//							return true, nil
//						}
//						if !isHealthy {
//							logger.Info("Patroni cluster unhealthy, retrying")
//						}
//						if isDegraded {
//							logger.Info("Patroni cluster degraded, retrying")
//						}
//					}
//					return false, nil
//				} else {
//					logger.Info(fmt.Sprintf("Patroni has %s state, Retry", state))
//					return false, nil
//				}
//			}
//		}
//		return false, nil
//	})
//	if err != nil {
//		logger.Error("Can not update status", zap.Error(err))
//		_ = ph.UpdateSiteManagerStatus(mode, "failed")
//	} else {
//		_ = ph.UpdateSiteManagerStatus(mode, newStatus)
//	}
//	return err
//}
