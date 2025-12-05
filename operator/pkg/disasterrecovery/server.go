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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const operatorName = "postgres-operator"

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

	if (cr.Spec.SiteManager == nil) && (cloudSqlCm == nil) {
		log.Info("Site Manager is not enabled, skipping DR init")
		return
	}

	if cloudSqlCm != nil {
		pgManager = newCloudSQLDRManager(helper, cloudSqlCm)
	} else {
		coreCR, err := patroniHelper.GetPatroniCoreCR()
		if err != nil {
			log.Error("Can not init Site Manager", zap.Error(err))
			panic(err)
		}
		if coreCR == nil || coreCR.Name == "" || string(coreCR.UID) == "" {
			log.Info("PatroniCore not found; skipping DR init")
			return
		}
		err = patroniHelper.AddNameAndUID(coreCR.Name, coreCR.UID, coreCR.Kind)
		if err != nil {
			log.Error("Can not init Site Manager", zap.Error(err))
		}

		patroniClusterSettings := util.GetPatroniClusterSettings(coreCR.Spec.Patroni.ClusterName)
		pgManager = newPatroniDRManager(helper, patroniHelper, patroniClusterSettings)
	}
	if err := util.ExecuteWithRetries(pgManager.setStatus); err != nil {
		log.Warn("not able to set SM status with retries, ", zap.Error(err))
	}
	if err := ensureOperatorService(helper); err != nil {
		log.Error("Can not ensure operator service", zap.Error(err))
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

func ensureOperatorService(helper *k8sHelper.Helper) error {
	client, _ := util.GetClient()
	namespace := util.GetNameSpace()

	oServ := &corev1.Service{}
	err := client.Get(context.Background(), types.NamespacedName{
		Name: operatorName, Namespace: namespace,
	}, oServ)
	if err != nil {
		if errors.IsNotFound(err) {
			// this is almost copy 'n paste of /operator-framework/operator-sdk@v0.8.0/pkg/metrics/metrics.go#initOperatorService
			// but this does not set ownerReference to the service
			// hence we do not need finalizers rights
			log.Info("Operator service not found, creating new one.")
			oServ = getOperatorService(operatorName, namespace)
			if err = helper.CreateServiceIfNotExists(oServ); err != nil {
				log.Error(fmt.Sprintf("can't create service: %s", operatorName), zap.Error(err))
				return err
			}
		} else {
			log.Error("can't get operator service", zap.Error(err))
			return err
		}
		return err
	}
	for _, port := range oServ.Spec.Ports {
		// checking if site manager already exists
		if port.Name == "site-manager" || port.Port == 8080 {
			log.Info("Site manager port exists, no need to update, exiting.")
			return nil
		}
	}
	oServ.Spec.Ports = append(oServ.Spec.Ports, corev1.ServicePort{
		Name:     "site-manager",
		Protocol: corev1.ProtocolTCP,
		Port:     8080,
		TargetPort: intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 8080,
		},
	})

	if err = client.Update(context.TODO(), oServ); err != nil {
		log.Error(fmt.Sprintf("can't update service: %s", operatorName), zap.Error(err))
		return err
	}

	return nil
}

func getOperatorService(operatorName string, namespace string) *corev1.Service {
	label := map[string]string{"name": operatorName}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorName,
			Namespace: namespace,
			Labels:    label,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     8383,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8383,
					},
					Name: "metrics",
				},
				{
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8080,
					},
					Name: "site-manager",
				},
				{
					Port:     8443,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8443,
					},
					Name: "web-tls",
				},
			},
			Selector: label,
		},
	}
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
