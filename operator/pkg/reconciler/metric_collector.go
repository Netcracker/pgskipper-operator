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
	"fmt"
	"strconv"
	"strings"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/util/constants"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type MetricCollectorReconciler struct {
	cr      *qubershipv1.PatroniServices
	helper  *helper.Helper
	scheme  *runtime.Scheme
	cluster *patroniv1.PatroniClusterSettings
}

func NewMetricCollectorReconciler(cr *qubershipv1.PatroniServices, helper *helper.Helper, scheme *runtime.Scheme, cluster *patroniv1.PatroniClusterSettings) *MetricCollectorReconciler {
	return &MetricCollectorReconciler{
		cr:      cr,
		helper:  helper,
		scheme:  scheme,
		cluster: cluster,
	}
}

func (r *MetricCollectorReconciler) Reconcile() error {
	cr := r.cr
	mcSpec := cr.Spec.MetricCollector
	telegrafConfigMap := deployment.ConfigMapForTelegraf()
	if _, err := r.helper.CreateOrUpdateConfigMap(telegrafConfigMap); err != nil {
		logger.Error(fmt.Sprintf("Cannot update config map %s", telegrafConfigMap.Name), zap.Error(err))
		return err
	}

	if mcSpec.InfluxDbHost != "" {
		influxTelegrafConfigMap := deployment.ConfigMapForInfluxdbTelegraf()
		if _, err := r.helper.CreateOrUpdateConfigMap(influxTelegrafConfigMap); err != nil {
			logger.Error(fmt.Sprintf("Cannot create config map %s", influxTelegrafConfigMap.Name), zap.Error(err))
			return err
		}
	}

	externalDatabase := cr.Spec.ExternalDataBase != nil

	// apply deployment
	monitoringDeployment := deployment.NewMonitoringDeployment(mcSpec, r.cluster.ClusterName, cr.Spec.ServiceAccountName)
	monitoringDeployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = cr.Spec.ImagePullPolicy
	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			monitoringDeployment.Spec.Template.Spec.ImagePullSecrets = append(monitoringDeployment.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	// Add Secret Hash
	err := manager.AddCredHashToPodTemplate(credentials.PostgresSecretNames, &monitoringDeployment.Spec.Template)
	if err != nil {
		logger.Error(fmt.Sprintf("can't add secret HASH to annotations for %s", monitoringDeployment.Name), zap.Error(err))
		return err
	}

	if coreCr, err := r.helper.GetPatroniCoreCR(); err == nil && !externalDatabase {
		pgNodesQty := corev1.EnvVar{Name: "POSTGRES_NODES_QTY", Value: fmt.Sprintf("%v", coreCr.Spec.Patroni.Replicas)}
		monitoringDeployment.Spec.Template.Spec.Containers[0].Env = append(monitoringDeployment.Spec.Template.Spec.Containers[0].Env, pgNodesQty)
	}
	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to Monitoring Agent Deployment")
		monitoringDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	if externalDatabase && isExtTypeSupported(cr.Spec.ExternalDataBase.Type) {
		logger.Info("External DB section is not empty, proceeding with env configuration")

		monitoringDeployment.Spec.Template.Spec.Containers[0].Env = append(monitoringDeployment.Spec.Template.Spec.Containers[0].Env, r.getExternalDataBaseEnv(cr.Spec.ExternalDataBase)...)
	} else {
		pgHost := corev1.EnvVar{Name: "POSTGRES_HOST", Value: fmt.Sprintf("pg-%s", r.cluster.ClusterName)}
		monitoringDeployment.Spec.Template.Spec.Containers[0].Env = append(monitoringDeployment.Spec.Template.Spec.Containers[0].Env, pgHost)
	}
	if cr.Spec.SiteManager != nil {
		monitoringDeployment.Spec.Template.Spec.Containers[0].Env = append(monitoringDeployment.Spec.Template.Spec.Containers[0].Env, r.getSiteManagerEnv())
	}

	if cr.Spec.Tls != nil {
		if cr.Spec.Tls.Enabled {
			monitoringDeployment.Spec.Template.Spec.Containers[0].Env = append(monitoringDeployment.Spec.Template.Spec.Containers[0].Env, r.getTLSEnv())
			monitoringDeployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(monitoringDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, opUtil.GetTlsSecretVolumeMount())
			monitoringDeployment.Spec.Template.Spec.Volumes = append(monitoringDeployment.Spec.Template.Spec.Volumes, opUtil.GetTlsSecretVolume(cr.Spec.Tls.CertificateSecretName))
		}
	}

	//Adding SecurityContext
	monitoringDeployment.Spec.Template.Spec.Containers[0].SecurityContext = opUtil.GetDefaultSecurityContext()

	if err := r.helper.CreateOrUpdateDeploymentForce(monitoringDeployment, true); err != nil {
		logger.Error(fmt.Sprintf("Cannot create or update deployment %s", monitoringDeployment.Name), zap.Error(err))
		return err
	}

	if err := opUtil.WaitForMetricCollector(); err != nil {
		logger.Error("Failed to wait for monitoring collector, exiting", zap.Error(err))
		return err
	}

	//apply metric collector service
	metricCollectorService := reconcileService(deployment.MetricCollectorDeploymentName, deployment.MetricCollectorLabels,
		deployment.MetricCollectorLabels, deployment.GetPortsForMonitoringService(), false)
	if err := r.helper.CreateOrUpdateService(metricCollectorService); err != nil {
		logger.Error(fmt.Sprintf("Cannot create service %s", metricCollectorService.Name), zap.Error(err))
		return err
	}
	return nil
}

func (r *MetricCollectorReconciler) getExternalDataBaseEnv(extDb *qubershipv1.ExternalDataBase) []corev1.EnvVar {
	envValue := []corev1.EnvVar{
		{
			Name:  "EXT_DB_INSTANCE_NAME",
			Value: extDb.Instance,
		},
		{
			Name:  "POSTGRES_PORT",
			Value: strconv.Itoa(extDb.Port),
		},
		{
			Name:  "EXT_DB_REGION",
			Value: extDb.Region,
		},
		{
			Name:  "EXT_DB_TYPE",
			Value: extDb.Type,
		},
		{
			Name:  "EXT_DB_PROJECT_NAME",
			Value: extDb.Project,
		},
	}

	hostValue := corev1.EnvVar{
		Name:  "POSTGRES_HOST",
		Value: extDb.ConnectionName,
	}
	if extDb.Type == constants.Azure || extDb.Type == constants.RDS {
		hostValue.Value = "pg-patroni"
	}

	envValue = append(envValue, hostValue)

	return envValue
}

func (r *MetricCollectorReconciler) getTLSEnv() corev1.EnvVar {
	envValue := corev1.EnvVar{
		Name:  "TLS",
		Value: "true",
	}
	return envValue
}

func (r *MetricCollectorReconciler) getSiteManagerEnv() corev1.EnvVar {
	envValue := corev1.EnvVar{
		Name:  "SITE_MANAGER",
		Value: "on",
	}
	return envValue
}

func isExtTypeSupported(extType string) bool {
	return strings.ToLower(extType) == constants.CloudSQL ||
		strings.ToLower(extType) == constants.RDS ||
		strings.ToLower(extType) == constants.Azure
}
