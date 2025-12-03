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

package deployment

import (
	"os"
	"strconv"

	netcrackerv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	MetricCollectorLabels = map[string]string{"app": "monitoring-collector"}
	logger                = util.GetLogger()
)

const (
	MetricCollectorDeploymentName  = "monitoring-collector"
	MetricCollectorUserCredentials = "monitoring-credentials"
	influxDbAdminCredentials       = "influx-db-admin-credentials"
	telegrafConfig                 = "telegraf-configmap"
)

func NewMonitoringDeployment(metricCollector *netcrackerv1.MetricCollector, pgcluster string, serviceAccountName string) *appsv1.Deployment {
	// metricCollector := cr.Spec.MetricCollector
	sslMode := "prefer"
	if metricCollector.SslMode != "" {
		sslMode = metricCollector.SslMode
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MetricCollectorDeploymentName,
			Namespace: util.GetNameSpace(),
			Labels:    util.Merge(MetricCollectorLabels, metricCollector.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(MetricCollectorLabels, metricCollector.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(MetricCollectorLabels, metricCollector.PodLabels),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName,
					Affinity:           &metricCollector.Affinity,
					Volumes: []corev1.Volume{
						{
							Name: "telegraf-config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: telegrafConfig},
								},
							},
						},
					},
					InitContainers: []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:    MetricCollectorDeploymentName,
							Image:   metricCollector.DockerImage,
							Command: []string{},
							Args:    []string{},
							Env: append([]corev1.EnvVar{
								{
									Name: "MONITORING_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: MetricCollectorUserCredentials},
											Key:                  "username",
										},
									},
								},
								{
									Name: "MONITORING_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: MetricCollectorUserCredentials},
											Key:                  "password",
										},
									},
								},
								{
									Name: "PG_ROOT_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: GetRootSecretName(pgcluster)},
											Key:                  "username",
										},
									},
								},
								{
									Name: "PG_ROOT_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: GetRootSecretName(pgcluster)},
											Key:                  "password",
										},
									},
								},
								{
									Name: "INFLUXDB_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: influxDbAdminCredentials},
											Key:                  "username",
										},
									},
								},
								{
									Name: "INFLUXDB_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: influxDbAdminCredentials},
											Key:                  "password",
										},
									},
								},
								{
									Name: "NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name:  "INFLUXDB_URL",
									Value: metricCollector.InfluxDbHost,
								},
								{
									Name:  "INFLUXDB_DATABASE",
									Value: metricCollector.InfluxDatabase,
								},
								{
									Name:  "TELEGRAF_PLUGIN_TIMEOUT",
									Value: strconv.Itoa(metricCollector.TelegrafPluginTimeout),
								},
								{
									Name:  "METRIC_COLLECTION_INTERVAL",
									Value: strconv.Itoa(metricCollector.CollectionInterval),
								},
								{
									Name:  "METRIC_COLLECTOR_OC_EXEC_TIMEOUT",
									Value: strconv.Itoa(metricCollector.OcExecTimeout),
								},
								{
									Name:  "METRICS_PROFILE",
									Value: metricCollector.MetricsProfile,
								},
								{
									Name:  "PGCLUSTER",
									Value: pgcluster,
								},
								{
									Name:  "POSTGRESQL_CREDENTIALS",
									Value: GetRootSecretName(pgcluster),
								},
								{
									Name:  "PATRONI_ENTITY_TYPE",
									Value: "deployment",
								},
								{
									Name:  "PGSSLMODE",
									Value: sslMode,
								},
							}, getDevEnvs(metricCollector)...),
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/telegraf/telegraf_temp.conf",
									SubPath:   "telegraf_temp.conf",
									Name:      "telegraf-config-volume",
								},
							},
							Resources: *metricCollector.Resources,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(9273),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt(9273),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
								FailureThreshold:    30,
							},
						},
					},
					SecurityContext: &metricCollector.SecurityContext,
				},
			},
		},
	}
	if metricCollector.PriorityClassName != "" {
		deployment.Spec.Template.Spec.PriorityClassName = metricCollector.PriorityClassName
	}

	if metricCollector.InfluxDbHost != "" {
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, getInfluxConfigMapVolume())
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, getInfluxConfigMapVolumeMount())
	}
	return deployment
}

func ConfigMapForTelegraf() *corev1.ConfigMap {
	filePath := "/opt/operator/telegraf-configmap"
	bytes, e := os.ReadFile(filePath)
	if e != nil {
		logger.Error("Failed to read from file", zap.Error(e))
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      telegrafConfig,
			Namespace: util.GetNameSpace(),
			Labels:    MetricCollectorLabels,
		},
		Data: map[string]string{
			"telegraf_temp.conf": string(bytes),
		},
	}
	return configMap
}

func ConfigMapForInfluxdbTelegraf() *corev1.ConfigMap {
	filePath := "/opt/operator/influxdb-telegraf-configmap"
	bytes, e := os.ReadFile(filePath)
	if e != nil {
		logger.Error("Failed to read from file", zap.Error(e))
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "influxdb-telegraf-configmap",
			Namespace: util.GetNameSpace(),
			Labels:    map[string]string{"app": "monitoring-collector"},
		},
		Data: map[string]string{
			"influxdb-telegraf_temp.conf": string(bytes),
		},
	}
	return configMap
}

func GetPortsForMonitoringService() []corev1.ServicePort {
	return []corev1.ServicePort{
		{Name: "port", Port: 8000},
		{Name: "prometheus-port", Port: 9273},
	}
}

func getInfluxConfigMapVolume() corev1.Volume {
	return corev1.Volume{
		Name: "influxdb-telegraf-config-volume",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "influxdb-telegraf-configmap"},
			},
		},
	}
}

func getInfluxConfigMapVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: "/etc/telegraf/telegraf.d/influxdb-telegraf_temp.conf",
		SubPath:   "influxdb-telegraf_temp.conf",
		Name:      "influxdb-telegraf-config-volume",
	}
}

func getDevEnvs(metricCollector *netcrackerv1.MetricCollector) []corev1.EnvVar {
	if metricCollector.MetricsProfile == "dev" {
		return []corev1.EnvVar{
			{
				Name:  "DEV_METRICS_TIMEOUT",
				Value: strconv.Itoa(metricCollector.DevMetricsTimeout),
			},
			{
				Name:  "DEV_METRICS_INTERVAL",
				Value: strconv.Itoa(metricCollector.DevMetricsInterval),
			},
		}
	}
	return []corev1.EnvVar{}
}
