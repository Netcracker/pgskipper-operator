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
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func getPgBackRestContainer(deploymentIdx int, patroniCoreSpec *v1.PatroniCoreSpec) corev1.Container {
	pgBackRestContainer := corev1.Container{
		Name:            "pgbackrest-sidecar",
		Image:           patroniCoreSpec.PgBackRest.DockerImage,
		ImagePullPolicy: "Always",
		SecurityContext: util.GetDefaultSecurityContext(),
		Command:         []string{"sh"},
		Args:            []string{"/opt/start.sh"},
		Env: []corev1.EnvVar{
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
				Name:  "PGHOST",
				Value: "pg-patroni",
			},
			{
				Name:  "POD_IDENTITY",
				Value: fmt.Sprintf("node%v", deploymentIdx),
			},
		},
		Ports: []corev1.ContainerPort{
			{ContainerPort: 3000, Name: "pgbackrest", Protocol: corev1.ProtocolTCP},
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

func GetPgBackRestCM(pgBackrestSpec *v1.PgBackRest) *corev1.ConfigMap {

	settings := getPgBackRestSettings(pgBackrestSpec)

	pgBackRestCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pgbackrest-conf",
			Namespace: util.GetNameSpace(),
		},
		Data: map[string]string{"pgbackrest.conf": settings},
	}
	return pgBackRestCM
}

func GetPgBackRestService(labels map[string]string) *corev1.Service {
	ports := []corev1.ServicePort{
		{Name: "pgbackrest", Port: 3000},
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pgbackrest",
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
		{Name: "pgbackrest", Port: 3000},
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

func getPgBackRestSettings(pgBackrestSpec *v1.PgBackRest) string {
	var listSettings []string
	listSettings = append(listSettings, "[global]")

	listSettings = append(listSettings, fmt.Sprintf("repo1-retention-full=%d", pgBackrestSpec.FullRetention))
	listSettings = append(listSettings, fmt.Sprintf("repo1-retention-diff=%d", pgBackrestSpec.DiffRetention))

	if pgBackrestSpec.RepoType == "s3" {
		listSettings = append(listSettings, fmt.Sprintf("repo1-type=%s", pgBackrestSpec.RepoType))
		listSettings = append(listSettings, fmt.Sprintf("repo1-path=%s", pgBackrestSpec.RepoPath))
		listSettings = append(listSettings, fmt.Sprintf("repo1-s3-bucket=%s", pgBackrestSpec.S3.Bucket))
		listSettings = append(listSettings, fmt.Sprintf("repo1-s3-endpoint=%s", pgBackrestSpec.S3.Endpoint))
		listSettings = append(listSettings, fmt.Sprintf("repo1-s3-key=%s", pgBackrestSpec.S3.Key))
		listSettings = append(listSettings, fmt.Sprintf("repo1-s3-key-secret=%s", pgBackrestSpec.S3.Secret))
		listSettings = append(listSettings, fmt.Sprintf("repo1-s3-region=%s", pgBackrestSpec.S3.Region))
		listSettings = append(listSettings, "repo1-s3-uri-style=path")
		if !pgBackrestSpec.S3.VerifySsl {
			listSettings = append(listSettings, "repo1-s3-verify-ssl=n")
		}
	}
	if pgBackrestSpec.RepoType == "rwx" {
		listSettings = append(listSettings, "repo1-path=/var/lib/pgbackrest")
	}
	listSettings = append(listSettings, pgBackrestSpec.ConfigParams...)
	settings := strings.Join(listSettings[:], "\n")
	return settings
}
