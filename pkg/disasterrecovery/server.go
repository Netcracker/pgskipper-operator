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

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	k8sHelper "github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	log        = util.GetLogger()
	namespace  = util.GetNameSpace()
	secretName = "cloudsql-instance-credentials"
)

type GenericPostgreSQLDRManager interface {
	processSiteManagerRequest(response http.ResponseWriter, req *http.Request)
	processHealthRequest(response http.ResponseWriter, req *http.Request)
	setStatus() error
	processPreConfigureRequest(response http.ResponseWriter, req *http.Request)
}

func InitDRManager() {
	log.Info("Starting Site Manager Service")
	var pgManager GenericPostgreSQLDRManager
	helper := k8sHelper.GetHelper()
	patroniHelper := k8sHelper.GetPatroniHelper()
	cloudSqlCm := getCloudSqlCm(helper)

	cr, err := helper.GetPostgresServiceCR()
	if err != nil {
		log.Error("Can not init Site Manager", zap.Error(err))
	}
	err = helper.AddNameAndUID(cr.Name, cr.UID, cr.Kind)
	if err != nil {
		log.Error("Can not init Site Manager", zap.Error(err))
		panic(err)
	}

	coreCR, err := patroniHelper.GetPatroniCoreCR()
	if err != nil {
		log.Error("Can not init Site Manager", zap.Error(err))
		panic(err)
	}
	err = patroniHelper.AddNameAndUID(coreCR.Name, coreCR.UID, coreCR.Kind)
	if err != nil {
		log.Error("Can not init Site Manager", zap.Error(err))
	}

	patroniClusterSettings := util.GetPatroniClusterSettings(coreCR.Spec.Patroni.ClusterName)
	if cloudSqlCm != nil {
		pgManager = newCloudSQLDRManager(helper, cloudSqlCm)
	} else {
		pgManager = newPatroniDRManager(helper, patroniHelper, patroniClusterSettings)
	}
	if err := util.ExecuteWithRetries(pgManager.setStatus); err != nil {
		log.Warn("not able to set SM status with retries, ", zap.Error(err))
	}

	// Check if status running, then operator was restarted while site manager status was running, we decide to fail it
	if cr.Status.SiteManagerStatus.Status == "running" {
		log.Info("Looks like operator was restarted during switchover process, Site Manager status will be set to failed")
		if err := helper.UpdateSiteManagerStatus(cr.Status.SiteManagerStatus.Mode, "failed"); err == nil {
			log.Info(fmt.Sprintf("Successfully changed on %s mode on startup to failed", cr.Status.SiteManagerStatus.Mode))
		} else {
			log.Error("Can not update Site Manager Status", zap.Error(err))
		}
	}

	http.Handle("/sitemanager", helper.Middleware(http.HandlerFunc(pgManager.processSiteManagerRequest)))
	http.Handle("/health", helper.Middleware(http.HandlerFunc(pgManager.processHealthRequest)))
	http.Handle("/pre-configure", helper.Middleware(http.HandlerFunc(pgManager.processPreConfigureRequest)))
}

func getCloudSqlCm(helper *k8sHelper.Helper) *corev1.ConfigMap {
	cloudSqlCm, err := helper.GetConfigMap("cloud-sql-configuration")
	if err != nil {
		log.Info("Cloud SQL Configuration not found, proceeding with Patroni DR Manager")
		return nil
	} else {
		secretData, err := getCloudSQLSecret()
		if err == nil && secretData {
			log.Info("Cloud SQL Configuration found, proceeding with Cloud SQL DR Manager")
			return cloudSqlCm
		} else {
			log.Info("Data for cloudsql-instance-credentials secret is empty, not proceeding with call to Google API")
			return nil
		}
	}
}

func parseSiteManagerStatusFromRequest(req *http.Request) (qubershipv1.SiteManagerStatus, error) {
	var status qubershipv1.SiteManagerStatus
	err := json.NewDecoder(req.Body).Decode(&status)
	if err != nil {
		return qubershipv1.SiteManagerStatus{}, err
	}
	return status, nil
}

type Health struct {
	Status string `json:"status"`
}

func sendUpHealthResponse(w http.ResponseWriter) {
	sendResponse(w, http.StatusOK, Health{Status: "up"})
}

func sendDownHealthResponse(w http.ResponseWriter) {
	sendResponse(w, http.StatusInternalServerError, Health{Status: "down"})
}

func sendResponse(w http.ResponseWriter, statusCode int, response interface{}) {
	w.WriteHeader(statusCode)
	responseBody, _ := json.Marshal(response)
	_, _ = w.Write(responseBody)
	w.Header().Set("Content-Type", "application/json")
}

func getCloudSQLSecret() (bool, error) {
	foundSecret := &corev1.Secret{}
	k8sClient, err := util.GetClient()
	if err != nil {
		log.Error("can't get k8sClient", zap.Error(err))
		return false, err
	}
	err = k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: secretName, Namespace: namespace,
	}, foundSecret)
	if err != nil {
		log.Error(fmt.Sprintf("can't find the secret %s", secretName), zap.Error(err))
		return false, err
	}
	if foundSecret.Data == nil {
		log.Debug(fmt.Sprintf("No data on found inside secret %s", secretName))
		return false, nil
	}
	return true, nil
}
