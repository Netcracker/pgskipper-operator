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

package pgbackrestexporter

import (
	netcrackev1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	pgBackRestExporterLabels = map[string]string{"name": "pgbackrest-exporter", "app": "pgbackrest-exporter"}
)

func NewPgBackRestExporterDeployment(spec netcrackev1.PgBackRestExporter, sa string) *appsv1.Deployment {
	deploymentName := "pgbackrest-exporter"
	dockerImage := spec.Image

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: util.GetNameSpace(),
			Labels:    util.Merge(pgBackRestExporterLabels, spec.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
					MaxSurge:       &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(pgBackRestExporterLabels, spec.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(pgBackRestExporterLabels, spec.PodLabels),
				},
				Spec: corev1.PodSpec{
					Volumes:            getVolumes(),
					ServiceAccountName: sa,
					Affinity:           &spec.Affinity,
					Containers: []corev1.Container{
						{
							Name:    deploymentName,
							Image:   dockerImage,
							Ports: []corev1.ContainerPort{
								{ContainerPort: 9854, Name: "brexporter", Protocol: corev1.ProtocolTCP},
							},
							Env:          getEnvVariables(spec),
							VolumeMounts: getVolumeMounts(),
							Resources:    spec.Resources,
						},
					},
					SecurityContext: spec.SecurityContext,
				},
			},
		},
	}
	return dep
}

func getVolumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "pgbackrest-conf",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "pgbackrest-conf"},
				},
			},
		},
	}
}

func getVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			MountPath: "/etc/pgbackrest/pgbackrest.conf",
			Name:      "pgbackrest-conf",
			SubPath:   "pgbackrest.conf",
		},
	}
}

func GetPgBackRestExporterService() *corev1.Service {
	serviceName := "backrest-exporter"
	labels := map[string]string{"app": "pgbackrest-exporter"}
	ports := []corev1.ServicePort{
		{Name: "brexporter", Port: 9854},
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: util.GetNameSpace(),
			Labels:    labels,
		},

		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    ports,
		},
	}
}

func getEnvVariables(spec netcrackev1.PgBackRestExporter) []corev1.EnvVar {
	envs := []corev1.EnvVar{}
	for key, value := range spec.Env {
		envs = append(envs, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}
	return envs
}