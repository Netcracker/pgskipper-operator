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
	v1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

const SSHKeysSecret = "pgbackrest-keys"
const SSHKeysPath = "/keys"

func getPgBackRestContainer(deploymentIdx int, clustername string, patroniCoreSpec *v1.PatroniCoreSpec) corev1.Container {
	pgBackRestContainer := corev1.Container{
		Name:            "pgbackrest-sidecar",
		Image:           patroniCoreSpec.PgBackRest.DockerImage,
		ImagePullPolicy: "Always",
		SecurityContext: util.GetDefaultSecurityContext(),
		Command:         []string{"sh"},
		Args:            []string{"/opt/start.sh"},
		Env: append([]corev1.EnvVar{
			{
				Name: "PGPASSWORD",
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
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.namespace",
					},
				},
			},
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "status.podIP",
					},
				},
			},
			{
				Name:  "PGHOST",
				Value: "localhost",
			},
			{
				Name:  "POD_IDENTITY",
				Value: fmt.Sprintf("node%v", deploymentIdx),
			},
		}, GetPgBackrestEvs(deploymentIdx, clustername, *patroniCoreSpec.PgBackRest)...),
		Ports: []corev1.ContainerPort{
			{ContainerPort: 3000, Name: "backrest", Protocol: corev1.ProtocolTCP},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
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
				MountPath: "/etc/pgbackrest",
				Name:      "pgbackrest-conf",
			},
		},
		Resources: getPgbackRestResources(patroniCoreSpec),
	}
	if strings.ToLower(patroniCoreSpec.PgBackRest.RepoType) == "rwx" {
		backrestVolumeMount := corev1.VolumeMount{
			MountPath: "/var/lib/pgbackrest",
			Name:      "pgbackrest",
		}
		pgBackRestContainer.VolumeMounts = append(pgBackRestContainer.VolumeMounts, backrestVolumeMount)
	}
	return pgBackRestContainer
}

func getPgbackRestResources(patroniCoreSpec *v1.PatroniCoreSpec) corev1.ResourceRequirements {
	if patroniCoreSpec.PgBackRest.Resources == nil {
		return *patroniCoreSpec.Patroni.Resources
	}
	return *patroniCoreSpec.PgBackRest.Resources
}

func GetPgBackRestCM(cr *v1.PatroniCore) *corev1.ConfigMap {

	isStandby := patroni.IsStandbyClusterConfigurationExist(cr)
	settings := getPgBackRestSettings(cr.Spec.PgBackRest, isStandby)

	pgBackRestCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pgbackrest-conf",
			Namespace: util.GetNameSpace(),
		},
		Data: map[string]string{"pgbackrest.conf": settings},
	}
	return pgBackRestCM
}

func GetPgBackRestService(labels map[string]string, standby bool) *corev1.Service {
	serviceName := "backrest"
	if standby {
		serviceName = "backrest-standby"
	}
	ports := []corev1.ServicePort{
		{Name: "backrest", Port: 3000},
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: util.GetNameSpace(),
		},

		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    ports,
		},
	}
}

func GetBackrestHeadless() *corev1.Service {
	labels := map[string]string{"app": "patroni"}
	ports := []corev1.ServicePort{
		{Name: "backrest", Port: 3000},
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backrest-headless",
			Namespace: util.GetNameSpace(),
		},

		Spec: corev1.ServiceSpec{
			Selector:  labels,
			Ports:     ports,
			ClusterIP: "None",
		},
	}
}

func getPgBackRestSettings(pgBackrestSpec *v1.PgBackRest, isStandby bool) string {
	var listSettings []string
	listSettings = append(listSettings, "[global]")

	listSettings = append(listSettings, fmt.Sprintf("repo1-retention-full=%d", pgBackrestSpec.FullRetention))
	listSettings = append(listSettings, fmt.Sprintf("repo1-retention-diff=%d", pgBackrestSpec.DiffRetention))

	if pgBackrestSpec.RepoType == "s3" {
		listSettings = addS3RepoSettings(listSettings, pgBackrestSpec.RepoType, pgBackrestSpec.RepoPath, pgBackrestSpec.S3, 1)
		if isStandby && pgBackrestSpec.DRS3.Bucket != "" {
			listSettings = addS3RepoSettings(listSettings, pgBackrestSpec.RepoType, pgBackrestSpec.RepoPath, pgBackrestSpec.DRS3, 2)
		}
	}
	if pgBackrestSpec.RepoType == "rwx" {
		listSettings = append(listSettings, "repo1-path=/var/lib/pgbackrest")
	}
	listSettings = append(listSettings, pgBackrestSpec.ConfigParams...)
	settings := strings.Join(listSettings[:], "\n")
	return settings
}

func addS3RepoSettings(listSettings []string, repoType string, repoPath string, s3 v1.S3, number int) []string {
	resultListSettings := listSettings
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-type=%s", number, repoType))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-path=%s", number, repoPath))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-bucket=%s", number, s3.Bucket))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-endpoint=%s", number, s3.Endpoint))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-key=%s", number, s3.Key))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-key-secret=%s", number, s3.Secret))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-region=%s", number, s3.Region))
	resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-uri-style=path", number))
	if !s3.VerifySsl {
		resultListSettings = append(resultListSettings, fmt.Sprintf("repo%d-s3-verify-ssl=n", number))
	}
	return resultListSettings
}

func GetPgBackrestEvs(deploymentIdx int, clusterName string, pgBackRest v1.PgBackRest) []corev1.EnvVar {
	resultVars := []corev1.EnvVar{
		{
			Name:  "PGBACKREST_PG1_PATH",
			Value: fmt.Sprintf("/var/lib/pgsql/data/postgresql_node%v/", deploymentIdx),
		},
		{
			Name:  "PGBACKREST_STANZA",
			Value: "patroni",
		},
	}

	if pgBackRest.BackupFromStandby {
		standbyVars := []corev1.EnvVar{
			{
				Name:  "PGBACKREST_PG2_PATH",
				Value: fmt.Sprintf("/var/lib/pgsql/data/postgresql_node%v/", getOppositeIdx(deploymentIdx)),
			},
			{
				Name:  "PGBACKREST_PG2_HOST",
				Value: fmt.Sprintf("pg-%s", clusterName),
			},
			{
				Name:  "PGBACKREST_BACKUP_STANDBY",
				Value: "prefer",
			},
		}

		resultVars = append(resultVars, standbyVars...)
	}

	return resultVars
}

func getOppositeIdx(idx int) int {
	if idx == 1 {
		return 2
	} else {
		return 1
	}
}
