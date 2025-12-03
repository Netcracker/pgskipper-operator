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
	"fmt"
	"net/http"
	"time"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/upgrade"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type PatroniDRManager struct {
	helper        *helper.Helper
	patroniHelper *helper.PatroniHelper
	cluster       *patroniv1.PatroniClusterSettings
}

func newPatroniDRManager(helper *helper.Helper, patroniHelper *helper.PatroniHelper, cluster *patroniv1.PatroniClusterSettings) GenericPostgreSQLDRManager {
	return &PatroniDRManager{
		helper:        helper,
		patroniHelper: patroniHelper,
		cluster:       cluster,
	}
}

func (m *PatroniDRManager) setStatus() error {
	return nil
}

func (m *PatroniDRManager) processSiteManagerRequest(response http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		if err := m.getStatus(response); err != nil {
			_, _ = fmt.Fprintf(response, "Get Status error: %v", err)
			return
		}
	case "POST":
		statusRequest, err := parseSiteManagerStatusFromRequest(req)
		if err != nil {
			log.Error("Failed to parse sm status from request", zap.Error(err))
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := m.helper.UpdateSiteManagerStatus(statusRequest.Mode, "running"); err != nil {
			log.Error("Failed to set sm status", zap.Error(err))
			http.Error(response, err.Error(), http.StatusInternalServerError)
		}

		// send running
		if err := m.getStatus(response); err != nil {
			log.Error("Failed to get sm status", zap.Error(err))
			return
		}
		go m.processRequest(statusRequest)
	default:
		_, _ = fmt.Fprintf(response, "Only GET and POST methods are supported.")
	}

}

func (m *PatroniDRManager) processRequest(statusRequest qubershipv1.SiteManagerStatus) {
	if err := m.changeMode(statusRequest); err != nil {
		log.Error("Failed to change mode", zap.Error(err))
		if err := m.helper.UpdateSiteManagerStatus(statusRequest.Mode, "failed"); err != nil {
			log.Error("Failed to update site manager status", zap.Error(err))
		}
		return
	}
}

func (m *PatroniDRManager) getStatus(response http.ResponseWriter) error {
	log.Info("Site Manager: Get Status")
	sendResponse(response, http.StatusOK, m.helper.GetCurrentSiteManagerStatus())
	return nil
}

func (m *PatroniDRManager) processHealthRequest(response http.ResponseWriter, req *http.Request) {
	status := "down"
	config, err := m.helper.GetPatroniClusterConfig(m.cluster.PatroniUrl)
	if err == nil {
		isHealthy := m.patroniHelper.IsPatroniClusterHealthy(config)
		isDegraded := m.patroniHelper.IsPatroniClusterDegraded(config, m.cluster.PgHost)
		if isHealthy && !isDegraded {
			status = "up"
		}
		if isDegraded {
			status = "degraded"
		}
	}
	sendResponse(response, http.StatusOK, Health{Status: status})
}

func (m *PatroniDRManager) waitForClusterHealthy() (bool, error) {

	if postgresService, err := m.helper.GetPostgresServiceCR(); err != nil {
		return false, err
	} else {
		retries := 0
		retriesLimit := postgresService.Spec.SiteManager.StandbyClusterHealthCheck.RetriesLimit
		failureRetries := 0
		failureRetriesLimit := postgresService.Spec.SiteManager.StandbyClusterHealthCheck.FailureRetriesLimit
		waitTimeout := time.Duration(int32(postgresService.Spec.SiteManager.StandbyClusterHealthCheck.RetriesWaitTimeout)) * time.Second

		log.Info(fmt.Sprintf("Wait for %v successful health check with interval in %v seconds", retriesLimit, waitTimeout))

		for {
			if retries == retriesLimit {
				log.Info("Patroni cluster is healthy")
				return true, nil
			}
			if failureRetries == failureRetriesLimit {
				log.Info("Patroni cluster is not healthy, no retry left. Check Patroni master node logs")
				return false, nil
			}
			time.Sleep(waitTimeout)
			if m.patroniHelper.IsHealthy(m.cluster.PatroniUrl, m.cluster.PgHost) {
				log.Debug("healthy")
				retries++
			} else {
				log.Debug("not healthy")
				retries = 0
				failureRetries++
				continue
			}
		}
	}
}

func (m *PatroniDRManager) processStandByMode(healthy bool) (bool, error) {

	log.Info(fmt.Sprintf("Process Standby Mode with healty=%v", healthy))

	if healthy {
		if err := m.patroniHelper.TerminateActiveConnections(m.cluster.PgHost); err != nil {
			log.Error("Can not terminate active connections", zap.Error(err))
			return false, err
		}
	} else {
		log.Info("patroni cluster is not healthy before set mode to standby, proceeding with clean up")
		//patch standby to init
		if err := m.patroniHelper.AddStandbyClusterConfigurationConfigMap(m.cluster.PatroniUrl); err != nil {
			log.Error("Can not update config map with standby cluster configuration")
			return false, err
		}
		if err := m.reinitStandbyPods(); err != nil {
			return false, err
		}
	}
	// do we really need to wait for reconcile supplementary services after process standby mode?
	if err := m.addStandbyClusterConfigInCR(); err != nil {
		log.Error("Can not set standby configuration", zap.Error(err))
		return false, err
	}
	// do we really need to wait for reconcile supplementary services after process standby mode?
	if err := m.helper.WaitUntilReconcileIsDone(); err != nil {
		return false, err
	}

	if healthy, err := m.waitForClusterHealthy(); err != nil || !healthy {
		log.Error("Error occurred while reinit")
		return m.processStandByMode(healthy)
	}
	if err := m.patroniHelper.DeleteCleanerInitContainer(m.cluster.ClusterName); err != nil {
		return false, err
	}
	log.Info("patroni cluster is healthy after set mode to standby")
	return true, nil
}

func (m *PatroniDRManager) changeMode(request qubershipv1.SiteManagerStatus) error {
	mode := request.Mode
	log.Info(fmt.Sprintf("Received change to  %s, processing ...", mode))
	m.updateExternalService(mode)

	// do we really need to wait for supplementary services reconcile?
	if err := m.helper.WaitUntilReconcileIsDone(); err != nil {
		return err
	}

	switch mode {
	case "standby":
		// check that patroni is healthy
		healthy, _ := m.patroniHelper.IsHealthyWithTimeout(1*time.Minute, m.cluster.PatroniUrl, m.cluster.PgHost)
		if err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, 3*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			return m.processStandByMode(healthy)
		}); err != nil {
			log.Info("Standby cluster mode processing failed.")
			return err
		}
	case "active":
		if err := m.setActivePatroniCluster(m.cluster.ClusterName); err != nil {
			log.Error("Can not set active configuration", zap.Error(err))
			return err
		}
	case "disabled":
		if err := m.setDisablePatroniCluster(m.cluster.ClusterName); err != nil {
			log.Error("Can not set disabled mode", zap.Error(err))
			return err
		}
	default:
		log.Error(fmt.Sprintf("Error: mode %s not supported.\n", mode))
		m.revertExternalService(mode)
	}

	log.Info(fmt.Sprintf("Site Manager mode will be changed to %s", mode))
	status := m.helper.GetCurrentSiteManagerStatus()
	if status.Mode == mode && status.Status != "done" {
		if err := m.helper.UpdateSiteManagerStatus(mode, "done"); err == nil {
			log.Info(fmt.Sprintf("Successfully changed on %s mode", mode))
		} else {
			log.Error("Can not update Site Manager Status", zap.Error(err))
			return err
		}
	}
	return nil
}

func (m *PatroniDRManager) reinitStandbyPods() error {
	patroniPods, err := m.helper.GetNamespacePodListBySelectors(m.cluster.PatroniCommonLabels)
	if err != nil {
		return err
	}
	if err = m.setDisablePatroniCluster(m.cluster.ClusterName); err != nil {
		return err
	}
	for _, patroniPod := range patroniPods.Items {
		if err = opUtil.WaitDeletePod(&patroniPod); err != nil {
			log.Error("waiting for Patroni deployment delete failed", zap.Error(err))
			return err
		}
	}
	u := upgrade.Init(m.helper.GetClient())
	if err := u.CleanInitializeKey(m.cluster.ClusterName); err != nil {
		return err
	}

	var statefulsetList []*appsv1.StatefulSet
	patroniStatefulSetName := fmt.Sprintf("pg-%s-node", m.cluster.ClusterName)
	if statefulsetList, err = m.helper.GetStatefulsetByNameRegExp(patroniStatefulSetName); err != nil {
		log.Error("Can't get Patroni Deployments", zap.Error(err))
		return err
	}

	for _, dep := range statefulsetList {
		cleanerInitContainer := u.GetCleanerInitContainer(dep.Spec.Template.Spec.Containers[0].Image)
		replicas := int32(1)
		if len(dep.Spec.Template.Spec.InitContainers) == 0 {
			dep.Spec.Template.Spec.InitContainers = append(cleanerInitContainer, dep.Spec.Template.Spec.InitContainers...)
		}
		//dep.Spec.Template.Spec.Containers[0].Image = patroniSpec.DockerImage
		dep.Spec.Replicas = &replicas

		if err := m.helper.CreateOrUpdateStatefulset(dep, true); err != nil {
			log.Error("Can't update Patroni deployment", zap.Error(err))
			return err
		}
	}
	return nil
}

func (m *PatroniDRManager) addStandbyClusterConfigInCR() error {
	log.Info("Trying to add standby cluster config in PatroniServices")
	cr, err := m.helper.GetPostgresServiceCR()
	if err != nil {
		log.Error("Error occurred during standby configuration adding. Unable to get PatroniServices CR", zap.Error(err))
		return err
	}
	coreCr, err := m.patroniHelper.GetPatroniCoreCR()
	if err != nil {
		log.Error("Error occurred during standby configuration adding. Unable to get PatroniCore CR", zap.Error(err))
		return err
	}
	host := cr.Spec.SiteManager.ActiveClusterHost
	port := cr.Spec.SiteManager.ActiveClusterPort
	standbyConfig := &patroniv1.StandbyCluster{Host: host, Port: port}
	coreCr.Spec.Patroni.StandbyCluster = standbyConfig

	if err = wait.PollUntilContextTimeout(context.Background(), time.Second, 3*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		log.Info("Trying to add standby cluster config in PatroniServices with retry")
		if err := m.patroniHelper.UpdatePatroniCore(coreCr); err != nil {
			log.Error("Error occurred during standby configuration adding. Retrying", zap.Error(err))
			return false, err
		}
		return true, nil
	}); err != nil {
		log.Error("cannot create poll", zap.Error(err))
		return err
	}

	if err = m.patroniHelper.WaitUntilReconcileIsDone(); err != nil {
		return err
	}
	return err
}

func (m *PatroniDRManager) setDisablePatroniCluster(clusterName string) error {
	return m.helper.UpdatePatroniReplicas(0, clusterName)
}

func (m *PatroniDRManager) setActivePatroniCluster(clusterName string) error {
	if err := m.clearStandbyClusterConfigInCR(); err != nil {
		return err
	}

	if err := m.patroniHelper.ClearStandbyClusterConfigurationConfigMap(m.cluster.PatroniUrl); err != nil {
		return err
	}

	if err := m.helper.UpdatePatroniReplicas(1, clusterName); err != nil {
		return err
	}
	if healthy, err := m.waitForClusterHealthy(); err != nil || !healthy {
		log.Error("Error occurred while set active mode")
		return err
	}
	log.Info("patroni cluster is healthy after set mode to active")
	return nil
}

func (m *PatroniDRManager) clearStandbyClusterConfigInCR() error {
	crCopy := &patroniv1.PatroniCore{}
	log.Info("Trying to clear standby cluster config in PatroniServices")

	if cr, err := m.helper.GetPatroniCoreCR(); err != nil {
		log.Error("Error occurred during clear standby configuration.", zap.Error(err))
		return err
	} else {
		cr.DeepCopyInto(crCopy)
		standbyConfig := &patroniv1.StandbyCluster{}
		crCopy.Spec.Patroni.StandbyCluster = standbyConfig
		if err = wait.PollUntilContextTimeout(context.Background(), time.Second, 3*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			log.Info("Trying to clear standby cluster config in PatroniServices with retry")
			if err := m.patroniHelper.UpdatePatroniCore(crCopy); err != nil {
				log.Error("Error occurred during clear standby configuration. Retrying", zap.Error(err))
				return false, err
			}
			return true, nil
		}); err != nil {
			log.Error("cannot create poll", zap.Error(err))
			return err
		}
		if err = m.patroniHelper.WaitUntilReconcileIsDone(); err != nil {
			return err
		}
		return err
	}
}

func (m *PatroniDRManager) updateExternalService(mode string) {
	log.Info("Site Manager: Update external service")
	switch mode {
	case "standby":
		if cr, err := m.helper.GetPostgresServiceCR(); err == nil {
			activeHost := cr.Spec.SiteManager.ActiveClusterHost
			extService := m.helper.GetService("pg-"+m.cluster.ClusterName+"-external", namespace)
			extService.Spec.ExternalName = activeHost
			_ = m.helper.UpdateService(extService)
		}
	case "active":
		extService := m.helper.GetService("pg-"+m.cluster.ClusterName+"-external", namespace)
		extService.Spec.ExternalName = fmt.Sprintf("pg-%s.%s.svc.cluster.local", m.cluster.ClusterName, namespace)
		_ = m.helper.UpdateService(extService)
	}
}

func (m *PatroniDRManager) revertExternalService(mode string) {
	log.Info("Site Manager: Revert external service")

	if mode == "standby" {
		m.updateExternalService("active")
	}
	if mode == "active" {
		m.updateExternalService("standby")
	}
}

func (m *PatroniDRManager) processPreConfigureRequest(response http.ResponseWriter, req *http.Request) {

}
