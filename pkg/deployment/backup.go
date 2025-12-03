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
	"fmt"
	"strconv"

	netcrackerv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/storage"
	opUtils "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/qubership-credential-manager/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	BackupDaemonLabels = map[string]string{"app": "postgres-backup-daemon", "name": "postgres-backup-daemon"}
)

const (
	BackupDaemon = "postgres-backup-daemon"
)

func NewBackupDaemonDeployment(backupDaemon *netcrackerv1.BackupDaemon, pgClusterName string, serviceAccountName string) *appsv1.Deployment {
	// func NewBackupDaemonDeployment(backupDaemon	*cr.Spec.BackupDaemon *
	// backupDaemon := &cr.Spec.BackupDaemon
	nodes := backupDaemon.Storage.Nodes
	pgHost := backupDaemon.PgHost
	sslMode := "prefer"
	if backupDaemon.SslMode != "" {
		sslMode = backupDaemon.SslMode
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupDaemon,
			Namespace: utils.GetNamespace(),
			Labels:    opUtils.Merge(BackupDaemonLabels, backupDaemon.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: opUtils.Merge(BackupDaemonLabels, backupDaemon.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: opUtils.Merge(BackupDaemonLabels, backupDaemon.PodLabels),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "backup-data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "postgres-backup-pvc",
									ReadOnly:  false,
								},
							},
						},
					},
					ServiceAccountName: serviceAccountName,
					Affinity:           &backupDaemon.Affinity,
					InitContainers:     []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:    BackupDaemon,
							Image:   backupDaemon.DockerImage,
							Command: []string{},
							Args:    []string{},
							Env: []corev1.EnvVar{
								{
									Name: "POSTGRES_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: GetRootSecretName(pgClusterName)},
											Key:                  "password",
										},
									},
								},
								{
									Name: "POSTGRES_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: GetRootSecretName(pgClusterName)},
											Key:                  "username",
										},
									},
								},
								{
									Name: "PGPASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: GetReplSecretName(pgClusterName)},
											Key:                  "password",
										},
									},
								},
								{
									Name:  "PG_CLUSTER_NAME",
									Value: pgClusterName,
								},
								{
									Name:  "PUBLIC_ENDPOINTS_WORKERS_NUMBER",
									Value: "2",
								},
								{
									Name:  "PRIVATE_ENDPOINTS_WORKERS_NUMBER",
									Value: "2",
								},
								{
									Name:  "ARCHIVE_ENDPOINTS_WORKERS_NUMBER",
									Value: "2",
								},
								{
									Name:  "GRANULAR_EVICTION",
									Value: backupDaemon.GranularEviction,
								},
								{
									Name:  "JOB_FLAG",
									Value: backupDaemon.JobFlag,
								},
								{
									Name:  "CONNECT_TIMEOUT",
									Value: backupDaemon.ConnectTimeout,
								},
								{
									Name:  "ALLOW_PREFIX",
									Value: strconv.FormatBool(backupDaemon.AllowPrefix),
								},
								{
									Name:  "EXCLUDED_EXTENSIONS",
									Value: backupDaemon.ExcludedExtensions,
								},
								{
									Name:  "COMPRESSION_LEVEL",
									Value: strconv.Itoa(backupDaemon.CompressionLevel),
								},
								{
									Name:  "ENCRYPTION",
									Value: strconv.FormatBool(backupDaemon.Encryption),
								},
								{
									Name:  "RETAIN_ARCHIVE_SETTINGS",
									Value: strconv.FormatBool(backupDaemon.RetainArchiveSettings),
								},
								{
									Name:  "BACKUP_TIMEOUT",
									Value: strconv.Itoa(backupDaemon.BackupTimeout),
								},
								{
									Name:  "GRANULAR_BACKUP_SCHEDULE",
									Value: backupDaemon.GranularBackupSchedule,
								},
								{
									Name:  "DATABASES_TO_SCHEDULE",
									Value: backupDaemon.DatabasesToSchedule,
								},
								{
									Name:  "USE_EVICTION_POLICY_FIRST",
									Value: backupDaemon.UseEvictionPolicyFirst,
								},
								{
									Name:  "EVICTION_POLICY_BINARY",
									Value: backupDaemon.EvictionBinaryPolicy,
								},
								{
									Name:  "AUTH",
									Value: "False",
								},
								{
									Name:  "POSTGRES_HOST",
									Value: pgHost,
								},
								{
									Name:  "POSTGRES_PORT",
									Value: "5432",
								},
								{
									Name:  "STORAGE_TYPE",
									Value: backupDaemon.Storage.Type,
								},
								{
									Name:  "EVICTION_POLICY",
									Value: backupDaemon.EvictionPolicy,
								},
								{
									Name:  "BACKUP_SCHEDULE",
									Value: backupDaemon.BackupSchedule,
								},
								{
									Name:  "PGSSLMODE",
									Value: sslMode,
								},
								{
									Name:  "ARCHIVE_EVICT_POLICY",
									Value: backupDaemon.ArchiveEvictionPolicy,
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{Name: "web", ContainerPort: 8080},
								{Name: "backups", ContainerPort: 8081},
								{Name: "archive", ContainerPort: 8082},
								{Name: "granular", ContainerPort: 9000},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/backup-storage",
									Name:      "backup-data",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/v2/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 20,
								PeriodSeconds:       10,
								FailureThreshold:    30,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/v2/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 20,
								PeriodSeconds:       10,
								FailureThreshold:    30,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
							},
							Resources: *backupDaemon.Resources,
						},
					},
					SecurityContext: &backupDaemon.SecurityContext,
				},
			},
		},
	}
	if nodes != nil {
		deployment.Spec.Template.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": nodes[0],
		}
	}
	if backupDaemon.PriorityClassName != "" {
		deployment.Spec.Template.Spec.PriorityClassName = backupDaemon.PriorityClassName
	}
	storageType := backupDaemon.Storage.Type
	if storageType == "ephemeral" || storageType == "s3" {
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "backup-data",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: new(corev1.EmptyDirVolumeSource),
				},
			},
		}
	} else {
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "backup-data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "postgres-backup-pvc",
						ReadOnly:  false,
					},
				},
			},
		}
	}
	if backupDaemon.ExternalPv != nil {
		deployment.Spec.Template.Spec.Volumes =
			append(deployment.Spec.Template.Spec.Volumes, getExternalBackupVolume())
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts =
			append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, getExternalBackupVolumeMount())
	}
	return deployment
}

func getExternalBackupVolume() corev1.Volume {
	return corev1.Volume{
		Name: "external-backup-data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "external-postgres-backup-pvc",
				ReadOnly:  false,
			},
		},
	}
}

func getExternalBackupVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: "/external/",
		Name:      "external-backup-data",
	}
}

func ConfigMapForFullBackupsMonitoring(telegrafJsonKey string) *corev1.ConfigMap {
	return storage.GetConfigMapByName("postgres-backup-daemon.collector-config", telegrafJsonKey)
}

func ConfigMapForGranularBackupsMonitoring(telegrafJsonKey string) *corev1.ConfigMap {
	return storage.GetConfigMapByName("postgres-granular-backup-daemon.collector-config", telegrafJsonKey)
}

func GetPortsForBackupService() []corev1.ServicePort {
	return []corev1.ServicePort{
		{Name: "web", Port: 8080},
		{Name: "backups", Port: 8081},
		{Name: "archive", Port: 8082},
		{Name: "granular", Port: 9000},
	}
}
func GetRootSecretName(pgClusterName string) string {
	if pgClusterName == "gpdb" {
		return "gpdb-pg-root-credentials"
	} else {
		return "postgres-credentials"
	}
}
func GetReplSecretName(pgClusterName string) string {
	if pgClusterName == "gpdb" {
		return "gpdb-pg-repl-credentials"
	} else {
		return "replicator-credentials"
	}
}
