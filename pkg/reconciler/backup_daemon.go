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

	pgTypes "github.com/Netcracker/pgskipper-operator-core/api/v1"
	"github.com/Netcracker/pgskipper-operator-core/pkg/reconciler"
	"github.com/Netcracker/pgskipper-operator-core/pkg/storage"
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/util/constants"
	"github.com/Netcracker/pgskipper-operator/pkg/vault"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	extCMName = "postgres-external"
)

type BackupDaemonReconciler struct {
	cr          *qubershipv1.PatroniServices
	helper      *helper.Helper
	vaultClient *vault.Client
	cluster     *patroniv1.PatroniClusterSettings
}

func NewBackupDaemonReconciler(cr *qubershipv1.PatroniServices, helper *helper.Helper, vaultClient *vault.Client, cluster *patroniv1.PatroniClusterSettings) *BackupDaemonReconciler {
	return &BackupDaemonReconciler{
		cr:          cr,
		helper:      helper,
		vaultClient: vaultClient,
		cluster:     cluster,
	}
}

func (r *BackupDaemonReconciler) Reconcile() error {
	cr := r.cr
	bdSpec := cr.Spec.BackupDaemon
	if bdSpec.Storage.Type != "ephemeral" && bdSpec.Storage.Type != "s3" {
		backupPvc := storage.NewPvc("postgres-backup-pvc", &bdSpec.Storage, 1)
		if err := r.helper.CreatePvcIfNotExists(backupPvc); err != nil {
			logger.Error(fmt.Sprintf("Cannot create pvc %s", backupPvc.Name), zap.Error(err))
			return err
		}
	}
	if bdSpec.ExternalPv != nil {
		logger.Info("External Pv for PostgreSQL Backup Daemon is not empty, start configuration")
		externalStorage := pgTypes.Storage{
			Type:         "pv",
			Size:         bdSpec.ExternalPv.Capacity,
			Volumes:      []string{bdSpec.ExternalPv.Name},
			StorageClass: bdSpec.ExternalPv.StorageClass,
		}
		externalBackupPvc := storage.NewPvc("external-postgres-backup-pvc", &externalStorage, 1)
		if err := r.helper.CreatePvcIfNotExists(externalBackupPvc); err != nil {
			logger.Error(fmt.Sprintf("Cannot create pvc %s", externalBackupPvc.Name), zap.Error(err))
			return err
		}
	}

	backupDaemonDeployment := reconciler.NewBackupDaemonDeployment(bdSpec, r.cluster.ClusterName, cr.Spec.ServiceAccountName)

	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to BackupDaemon Deployment")
		backupDaemonDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			backupDaemonDeployment.Spec.Template.Spec.ImagePullSecrets = append(backupDaemonDeployment.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	// Add Secret Hash
	err := manager.AddCredHashToPodTemplate(credentials.PostgresSecretNames, &backupDaemonDeployment.Spec.Template)
	if err != nil {
		logger.Error(fmt.Sprintf("can't add secret HASH to annotations for %s", backupDaemonDeployment.Name), zap.Error(err))
		return err
	}

	// Vault Section
	r.vaultClient.ProcessVaultSection(backupDaemonDeployment, vault.BackuperEntrypoint, Secrets)

	//Adding securityContexts
	backupDaemonDeployment.Spec.Template.Spec.Containers[0].SecurityContext = util.GetDefaultSecurityContext()

	// External database Section
	if cr.Spec.ExternalDataBase != nil {
		envValue := corev1.EnvVar{
			Name:  "EXTERNAL_POSTGRESQL",
			Value: cr.Spec.ExternalDataBase.Type,
		}
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env = append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, envValue)

		if strings.ToLower(cr.Spec.ExternalDataBase.Type) == constants.Azure {
			backupDaemonDeployment.Spec.Template.Spec.Containers[0].VolumeMounts =
				append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
					Name:      "azure-config",
					ReadOnly:  true,
					MountPath: "/app/config",
				},
					corev1.VolumeMount{
						Name:      extCMName,
						ReadOnly:  false,
						MountPath: "/app/server",
					},
				)

			namespaceEnv := corev1.EnvVar{
				Name: "NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			}
			backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env =
				append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, namespaceEnv)

			azureConfigVolume := corev1.Volume{
				Name: "azure-config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cr.Spec.ExternalDataBase.AuthSecretName,
					},
				},
			}
			azureCMVolume := corev1.Volume{
				Name: extCMName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: extCMName},
					},
				},
			}
			backupDaemonDeployment.Spec.Template.Spec.Volumes =
				append(backupDaemonDeployment.Spec.Template.Spec.Volumes, azureConfigVolume, azureCMVolume)
		}
		if strings.ToLower(cr.Spec.ExternalDataBase.Type) == constants.RDS {
			logger.Info("Process AWS Envs")
			backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env =
				append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, r.getAWSEnv(cr.Spec.ExternalDataBase)...)
		}
	}

	// S3 Storage Section
	if bdSpec.S3Storage != nil {
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env = append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, r.getS3StorageEnv(bdSpec)...)
	}
	// TLS Section
	if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled && cr.Spec.ExternalDataBase == nil {
		logger.Info("Mount TLS secret volume")
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, util.GetTlsSecretVolumeMount())
		backupDaemonDeployment.Spec.Template.Spec.Volumes = append(backupDaemonDeployment.Spec.Template.Spec.Volumes, util.GetTlsSecretVolume(cr.Spec.Tls.CertificateSecretName))
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env = append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, r.getTlsEnv())
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme = "HTTPS"
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme = "HTTPS"
	}
	if cr.Spec.Tracing != nil && cr.Spec.Tracing.Enabled {

		envValue := corev1.EnvVar{
			Name:  "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
			Value: cr.Spec.Tracing.Host,
		}
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env = append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, envValue)
	}
	if cr.Spec.PgBackRest != nil {
		envValue := []corev1.EnvVar{
			{
				Name:  "DIFF_SCHEDULE",
				Value: cr.Spec.PgBackRest.DiffSchedule,
			},
			{
				Name:  "INCR_SCHEDULE",
				Value: cr.Spec.PgBackRest.IncrSchedule,
			},
		}
		logger.Info("Set storage type as pgbackrest")
		for i := range backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env {
			if backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env[i].Name == "STORAGE_TYPE" {
				backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env[i].Value = "pgbackrest"
				break
			}
		}
		if cr.Spec.PgBackRest.BackupFromStandby {
			logger.Info("Set backup from standby parameter for backup-daemon")
			envValue = append(envValue, corev1.EnvVar{
				Name:  "BACKUP_FROM_STANDBY",
				Value: "true",
			})
		}
		backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env = append(backupDaemonDeployment.Spec.Template.Spec.Containers[0].Env, envValue...)
	}

	if err := r.helper.CreateOrUpdateDeploymentForce(backupDaemonDeployment, true); err != nil {
		logger.Error(fmt.Sprintf("Cannot create or update deployment %s", backupDaemonDeployment.Name), zap.Error(err))
		return err
	}
	if err := util.WaitForBackupDaemon(); err != nil {
		logger.Error("Failed to wait for backup daemon, exiting", zap.Error(err))
		return err
	}
	if cr.Spec.Patroni != nil && cr.Spec.ExternalDataBase == nil {
		if err := patroni.SetWalArchiving(*cr.Spec, r.cluster.PatroniUrl); err != nil {
			logger.Error("Failed to update WalArchiving property, exiting", zap.Error(err))
			return err
		}
	}
	if _, err := r.helper.CreateOrUpdateConfigMap(reconciler.ConfigMapForFullBackupsMonitoring(constants.TelegrafJsonKey)); err != nil {
		logger.Error("Failed to create config map for full backups monitoring, exiting", zap.Error(err))
		return err
	}

	if _, err := r.helper.CreateOrUpdateConfigMap(reconciler.ConfigMapForGranularBackupsMonitoring(constants.TelegrafJsonKey)); err != nil {
		logger.Error("Failed to create config map for granular backups monitoring, exiting", zap.Error(err))
		return err
	}

	backupDaemonService := reconcileService(reconciler.BackupDaemon, reconciler.BackupDaemonLabels,
		reconciler.BackupDaemonLabels, reconciler.GetPortsForBackupService(), false)
	// TLS section
	if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled {
		var tlsPorts = []corev1.ServicePort{
			{Name: "tls-web", Port: 8443, TargetPort: intstr.FromInt(8080)},
			{Name: "tls-granular", Port: 9443, TargetPort: intstr.FromInt(9000)},
		}
		backupDaemonService.Spec.Ports = append(backupDaemonService.Spec.Ports, tlsPorts...)
	}
	if err := r.helper.CreateOrUpdateService(backupDaemonService); err != nil {
		logger.Error(fmt.Sprintf("Cannot create service %s", backupDaemonService.Name), zap.Error(err))
		return err
	}
	if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled {
		logger.Info("Updating service with TLS ports")
		if err := UpdateServiceWithTls(backupDaemonService, r.getBackupDaemonTlsPorts()); err != nil {
			return err
		}
	}
	return nil
}

func (r *BackupDaemonReconciler) getS3StorageEnv(backupDaemon *pgTypes.BackupDaemon) []corev1.EnvVar {
	envValue := []corev1.EnvVar{
		{
			Name:  "AWS_S3_ENDPOINT_URL",
			Value: backupDaemon.S3Storage.Url,
		},
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "s3-storage-credentials"},
					Key:                  "key_id",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "s3-storage-credentials"},
					Key:                  "access_key",
				},
			},
		},
		{
			Name:  "CONTAINER",
			Value: backupDaemon.S3Storage.Bucket,
		},
		{
			Name:  "AWS_S3_UNTRUSTED_CERT",
			Value: strconv.FormatBool(backupDaemon.S3Storage.UntrustedCert),
		},
		{
			Name:  "AWS_S3_PREFIX",
			Value: backupDaemon.S3Storage.Prefix,
		},
		{
			Name:  "AWS_DEFAULT_REGION",
			Value: backupDaemon.S3Storage.Region,
		},
	}
	return envValue
}

func (r *BackupDaemonReconciler) getAWSEnv(externalDatabase *qubershipv1.ExternalDataBase) []corev1.EnvVar {
	envValue := []corev1.EnvVar{
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "aws-credentials"},
					Key:                  "key_id",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "aws-credentials"},
					Key:                  "access_key",
				},
			},
		},
		{
			Name:  "AWS_DEFAULT_REGION",
			Value: externalDatabase.Region,
		},
	}
	return envValue
}

func (r *BackupDaemonReconciler) getTlsEnv() corev1.EnvVar {
	envValue := corev1.EnvVar{
		Name:  "TLS",
		Value: "True",
	}
	return envValue
}

func (r *BackupDaemonReconciler) getBackupDaemonTlsPorts() []corev1.ServicePort {
	return []corev1.ServicePort{
		{Name: "tls-web", Port: 8443, TargetPort: intstr.FromInt(8080)},
		{Name: "tls-granular", Port: 9443, TargetPort: intstr.FromInt(9000)},
	}
}
