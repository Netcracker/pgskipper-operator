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

package reconciler

import (
	"encoding/json"
	"fmt"
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	portName            = "site-mngr"
	operatorServiceName = "postgres-operator"
	portNumber          = 8080
)

type SiteManagerReconciler struct {
	cr      *qubershipv1.PatroniServices
	helper  *helper.Helper
	scheme  *runtime.Scheme
	cluster *patroniv1.PatroniClusterSettings
}

func NewSiteManagerReconciler(cr *qubershipv1.PatroniServices, helper *helper.Helper, scheme *runtime.Scheme, cluster *patroniv1.PatroniClusterSettings) *SiteManagerReconciler {
	return &SiteManagerReconciler{
		cr:      cr,
		helper:  helper,
		scheme:  scheme,
		cluster: cluster,
	}
}

func (r *SiteManagerReconciler) Reconcile() error {
	cr := r.cr
	smSpec := cr.Spec.SiteManager

	// add site manager port to postgres-operator service
	if err := r.addSiteManagerPortToPostgresService(); err != nil {
		return err
	}

	cloudSqlCm, err := r.helper.GetConfigMap("cloud-sql-configuration")
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	if cloudSqlCm != nil {
		logger.Info("cloud-sql-configuration found, skipping mode set.")
		return nil
	}

	// Define current postgres cluster mode
	mode := "active"
	host := fmt.Sprintf("pg-%s.%s.svc.cluster.local", r.cluster.ClusterName, namespace)
	patroniConfig := fmt.Sprintf("%s-config", r.cluster.ClusterName)
	patroniConfigMap, err := r.helper.GetConfigMap(patroniConfig)
	if err != nil {
		logger.Error(fmt.Sprintf("Could not read %s cm", patroniConfig), zap.Error(err))
	} else {
		var config map[string]interface{}
		if err = json.Unmarshal([]byte(patroniConfigMap.Annotations["config"]), &config); err != nil {
			logger.Error(fmt.Sprintf("cannot unmarshal %s", patroniConfig), zap.Error(err))
			return err
		}
		if standbyCluster, ok := config["standby_cluster"]; ok {
			switch standbyCluster.(type) {
			default:
				logger.Info("Unknown standby_cluster config")
			case string:
				if standbyCluster != "" {
					mode = "standby"
					var standbyClusterConfig map[string]interface{}
					if err = json.Unmarshal([]byte(standbyCluster.(string)), &standbyClusterConfig); err != nil {
						logger.Error("cannot unmarshal standby cluster config", zap.Error(err))
						return err
					}
					if h, ok := standbyClusterConfig["host"]; ok {
						host = h.(string)
					}
				}
			case map[string]interface{}:
				mode = "standby"
				if h, ok := standbyCluster.(map[string]interface{})["host"]; ok {
					host = h.(string)
				}
			}
		}
	}
	// Set Site Manager status
	// Because that reconciler runs over switchovers, we must be sure, that site manager not running.

	logger.Info(fmt.Sprintf("Reconciler decides that cluster is %s", mode))
	if smStatus := r.helper.GetCurrentSiteManagerStatus(); smStatus.Status != "running" {
		if err = r.helper.UpdateSiteManagerStatus(mode, "done"); err != nil {
			return err
		}
	} else {
		logger.Info(fmt.Sprintf("Site manag" +
			"er is 'running', skipping update to 'done'"))
	}

	// Define host for external service
	if mode == "standby" && smSpec != nil {
		host = smSpec.ActiveClusterHost
	}
	externalServiceName := fmt.Sprintf("%s-external", r.cluster.PostgresServiceName)
	if externalService := r.helper.GetService(externalServiceName, util.GetNameSpace()); externalService != nil {
		if externalService.Spec.ExternalName != host {
			externalService.Spec.ExternalName = host
			if err = r.helper.UpdateService(externalService); err != nil {
				logger.Error(fmt.Sprintf("cannot update %s", externalServiceName), zap.Error(err))
			}
		}
	} else {
		siteManagerService := reconcileExternalService(externalServiceName, map[string]string{"app": ""}, host, false)
		if err := r.helper.CreateOrUpdateService(siteManagerService); err != nil {
			logger.Error(fmt.Sprintf("Cannot create service %s", siteManagerService.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (r *SiteManagerReconciler) addSiteManagerPortToPostgresService() error {
	service := r.helper.GetService(operatorServiceName, util.GetNameSpace())
	port := corev1.ServicePort{Port: portNumber, Name: portName, Protocol: corev1.ProtocolTCP}
	if !isPortAlreadyExists(service) {
		service.Spec.Ports = append(service.Spec.Ports, port)
		if err := r.helper.UpdateService(service); err != nil {
			logger.Error(fmt.Sprintf("cannot update service %s", operatorServiceName), zap.Error(err))
			return err
		}
	}

	return nil
}

func isPortAlreadyExists(service *corev1.Service) bool {
	for _, p := range service.Spec.Ports {
		if p.Port == portNumber {
			return true
		}
	}
	return false
}
