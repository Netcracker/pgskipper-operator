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
	"strings"

	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"

	"github.com/Netcracker/pgskipper-operator/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ConfigMapForPatroni(clusterName string, patroniCM string, configMapKey string) *corev1.ConfigMap {
	configMapName := fmt.Sprintf("%s-%s", clusterName, patroniCM)
	return util.GetConfigMapByName(patroniCM, configMapName, configMapKey)
}

func ConfigMapForPostgreSQL(clusterName string, PatroniPropertiesCM string) *corev1.ConfigMap {
	configMapName := fmt.Sprintf("%s-%s.properties", PatroniPropertiesCM, clusterName)
	return util.GetConfigMapByName(PatroniPropertiesCM, configMapName, "postgresql.user.conf")
}

func PatroniSecret(nameSpace string, userName string, patroniLabels map[string]string) *corev1.Secret {
	secretName := fmt.Sprintf("%s-credentials", userName)
	stringData := map[string]string{
		"username": userName,
		"password": `p@ssWOrD1`,
	}
	secret := &corev1.Secret{
		Type: corev1.SecretTypeOpaque,
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: nameSpace,
			Labels:    patroniLabels,
		},
		StringData: stringData,
	}
	return secret
}

func GetPortsForPatroniService(clusterName string) []corev1.ServicePort {
	return []corev1.ServicePort{
		{Name: "pg-" + clusterName, Port: 5432},
		{Name: clusterName + "-api", Port: 8008},
	}
}

func ExtractParamsFromCRByName(cr *patroniv1.PatroniCore, paramName string) string {
	postgreSQLParams := cr.Spec.Patroni.PostgreSQLParams
	paramValue := "200"
	for _, param := range postgreSQLParams {
		param = strings.Replace(param, "=", ":", 1)
		splittedParam := strings.Split(param, ":")
		splittedParam[0] = strings.TrimSpace(splittedParam[0])
		splittedParam[1] = strings.TrimSpace(splittedParam[1])
		if splittedParam[0] == paramName {
			paramValue = splittedParam[1]
			break
		}
	}
	return paramValue
}

func getMaxConnections(cr *patroniv1.PatroniCore) string {
	return ExtractParamsFromCRByName(cr, "max_connections")
}

func getMaxPreparedTransactions(cr *patroniv1.PatroniCore) string {
	return ExtractParamsFromCRByName(cr, "max_prepared_transactions")
}

func getDefaultPatroniAffinity() *corev1.Affinity {
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "app.kubernetes.io/name",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"patroni"},
							},
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

func NewPatroniStatefulset(cr *patroniv1.PatroniCore, deploymentIdx int, clusterName string, patroniTemplate string, postgreSQLUserConf string, patroniLabels map[string]string) *appsv1.StatefulSet {
	logger := util.GetLogger()
	patroniSpec := cr.Spec.Patroni
	statefulsetName := fmt.Sprintf("pg-%s-node%v", clusterName, deploymentIdx)
	dockerImage := patroniSpec.DockerImage
	nodes := patroniSpec.Storage.Nodes

	// Create default affinity if not specified in CR
	affinity := patroniSpec.Affinity.DeepCopy()
	if affinity == nil {
		affinity = getDefaultPatroniAffinity()
	}

	stSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulsetName,
			Namespace: cr.Namespace,
			Labels:    util.Merge(patroniLabels, patroniSpec.PodLabels),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(patroniLabels, patroniSpec.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(patroniLabels, patroniSpec.PodLabels),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "patroni-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: patroniTemplate},
								},
							},
						},
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: fmt.Sprintf("%s-data-%v", clusterName, deploymentIdx),
									ReadOnly:  false,
								},
							},
						},
						{
							Name: "postgresql-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: postgreSQLUserConf},
								},
							},
						},
					},
					ServiceAccountName: cr.Spec.ServiceAccountName,
					Affinity:           affinity,
					InitContainers:     []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:            statefulsetName,
							Image:           dockerImage,
							SecurityContext: util.GetDefaultSecurityContext(),
							Command:         []string{},
							Args:            []string{},
							Env: []corev1.EnvVar{
								{
									Name: "PG_ROOT_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
											Key:                  "password",
										},
									},
								},
								{
									Name: "PG_REPL_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "replicator-credentials"},
											Key:                  "password",
										},
									},
								},
								{
									Name:  "PATRONI_TTL",
									Value: "60",
								},
								{
									Name:  "PG_MAX_CONNECTIONS",
									Value: getMaxConnections(cr),
								},
								{
									Name:  "PG_CONF_MAX_PREPARED_TRANSACTIONS",
									Value: getMaxPreparedTransactions(cr),
								},
								{
									Name:  "PATRONI_SYNCHRONOUS_MODE",
									Value: strconv.FormatBool(cr.Spec.Patroni.SynchronousMode),
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name: "POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
								{
									Name: "PATRONI_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name: "PG_RESOURCES_LIMIT_MEM",
									ValueFrom: &corev1.EnvVarSource{
										ResourceFieldRef: &corev1.ResourceFieldSelector{
											Resource: "limits.memory",
										},
									},
								},
								{
									Name:  "PATRONI_CLUSTER_NAME",
									Value: clusterName,
								},
								{
									Name:  "PATRONI_MAXIMUM_LAG_ON_FAILOVER",
									Value: "8388608",
								},
								{
									Name:  "DR_MODE",
									Value: "False",
								},
								{
									Name:  "POD_IDENTITY",
									Value: fmt.Sprintf("node%v", deploymentIdx),
								},
								{
									Name:  "RUN_PROPAGATE_SCRIPT",
									Value: "False",
								},
								{
									Name:  "PGBACKREST_PG1_PATH",
									Value: fmt.Sprintf("/var/lib/pgsql/data/postgresql_node%v/", deploymentIdx),
								},
								{
									Name:  "PGBACKREST_STANZA",
									Value: "patroni",
								},
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8008, Name: "patroni", Protocol: corev1.ProtocolTCP},
								{ContainerPort: 5432, Name: "postgresql", Protocol: corev1.ProtocolTCP},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/patroni-properties",
									Name:      "patroni-config",
								},
								{
									MountPath: "/var/lib/pgsql/data",
									Name:      "data",
								},
								{
									MountPath: "/properties",
									Name:      "postgresql-config",
								},
							},
							Resources: *patroniSpec.Resources,
						},
					},
					SecurityContext: patroniSpec.SecurityContext,
				},
			},
			ServiceName: "backrest-headless",
		},
	}
	if nodes != nil {
		stSet.Spec.Template.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": nodes[deploymentIdx-1],
		}
	}
	if patroniSpec.PriorityClassName != "" {
		stSet.Spec.Template.Spec.PriorityClassName = patroniSpec.PriorityClassName
	}

	// TLS Section
	if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled {
		logger.Info("Mount TLS secret volume")
		stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, util.GetTlsSecretVolumeMount())
		stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, util.GetTlsSecretVolume(cr.Spec.Tls.CertificateSecretName))
	}

	if patroniSpec.EnableShmVolume {
		logger.Info("Mount tmpfs volume")
		stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, util.GetShmVolumeMount())
		stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, util.GetShmVolume())
	}

	if patroniSpec.PgWalStorage != nil {
		pvcName := fmt.Sprintf("patroni-wals-data-%v", deploymentIdx)
		stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, GetPgWalVolumeMount())
		stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, GetPgWalVolume(pvcName))
	}

	if patroniSpec.External != nil && patroniSpec.External.Pvc != nil {
		logger.Info(fmt.Sprintf("Extended PVC: %s", patroniSpec.External.Pvc))
		for _, pvc := range patroniSpec.External.Pvc {
			stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, GetVolumeMount(pvc.Name, pvc.MountPath))
			stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, GetVolume(pvc.Name))
		}
	}

	if cr.Spec.PgBackRest != nil {
		stSet.Spec.Template.Spec.Containers[0].Env = append(stSet.Spec.Template.Spec.Containers[0].Env, GetPgBackrestEvs(deploymentIdx)...)
		stSet.Spec.Template.Spec.Containers = append(stSet.Spec.Template.Spec.Containers, getPgBackRestContainer(deploymentIdx, cr.Spec))
		stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, GetPgBackRestConfVolume())
		stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, GetPgBackRestConfVolumeMount())
		if strings.ToLower(cr.Spec.PgBackRest.RepoType) == "rwx" {
			stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, GetPgBackRestRWXVolume())
			stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, GetPgBackRestRWXVolumeMount())

		}
	}
	return stSet
}

func GetVolume(pvcName string) corev1.Volume {
	return corev1.Volume{
		Name: pvcName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  false,
			},
		},
	}
}

func GetVolumeMount(name string, mountPath string) corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: mountPath,
		Name:      name,
	}
}

func GetPgWalVolume(pvcName string) corev1.Volume {
	return corev1.Volume{
		Name: "pg-wal-data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  false,
			},
		},
	}
}

func GetPgWalVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: "/var/lib/pgsql/pg_wal/",
		Name:      "pg-wal-data",
	}
}

func GetPgBackRestConfVolume() corev1.Volume {
	return corev1.Volume{
		Name: "pgbackrest-conf",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "pgbackrest-conf"},
			},
		},
	}
}

func GetPgBackRestRWXVolume() corev1.Volume {
	return corev1.Volume{
		Name: "pgbackrest",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "pgbackrest-backups",
				ReadOnly:  false,
			},
		},
	}
}

func GetPgBackRestRWXVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: "/var/lib/pgbackrest",
		Name:      "pgbackrest",
	}
}

func GetPgBackRestConfVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: "/etc/pgbackrest",
		Name:      "pgbackrest-conf",
	}
}

func GetPgBackrestEvs(deploymentIdx int) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "PGBACKREST_PG1_PATH",
			Value: fmt.Sprintf("/var/lib/pgsql/data/postgresql_node%v/", deploymentIdx),
		},
		{
			Name:  "PGBACKREST_STANZA",
			Value: "patroni",
		},
	}
}
