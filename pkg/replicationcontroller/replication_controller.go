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

package replicationcontroller

import (
	"fmt"
	"strconv"

	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	HttpPort  = 8080
	HttpsPort = 8443
)

var rcLabels = map[string]string{"name": "logical-replication-controller"}

func NewRCDeployment(cr v1.PatroniServices, sa, clusterName string, pgPort int) *appsv1.Deployment {
	spec := cr.Spec.ReplicationController
	deploymentName := "logical-replication-controller"
	dockerImage := spec.Image
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: util.GetNameSpace(),
			Labels:    util.Merge(rcLabels, spec.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(rcLabels, spec.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(rcLabels, spec.PodLabels),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: sa,
					Affinity:           &spec.Affinity,
					InitContainers:     []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:            deploymentName,
							ImagePullPolicy: cr.Spec.ImagePullPolicy,
							Image:           dockerImage,
							Command:         []string{},
							Args:            []string{},
							Env: []corev1.EnvVar{
								{
									Name:  "POSTGRES_HOST",
									Value: fmt.Sprintf("pg-%s", clusterName),
								},
								{
									Name:  "POSTGRES_PORT",
									Value: strconv.Itoa(pgPort),
								},
								{
									Name: "POSTGRES_ADMIN_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
											Key:                  "username",
										},
									},
								},
								{
									Name: "POSTGRES_ADMIN_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
											Key:                  "password",
										},
									},
								},
								{
									Name: "API_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "logical-replication-controller-creds"},
											Key:                  "username",
										},
									},
								},
								{
									Name: "API_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "logical-replication-controller-creds"},
											Key:                  "password",
										},
									},
								},
								{
									Name:  "PG_SSL",
									Value: spec.SslMode,
								},
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Name: "web", Protocol: corev1.ProtocolTCP},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								FailureThreshold:    10,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								FailureThreshold:    10,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
							},
							Resources: spec.Resources,
						},
					},
					SecurityContext: spec.SecurityContext,
				},
			},
		},
	}
	return dep
}

func GetService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "logical-replication-controller",
			Namespace: util.GetNameSpace(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "web",
				Protocol:   corev1.ProtocolTCP,
				Port:       HttpPort,
				TargetPort: intstr.IntOrString{IntVal: HttpPort},
			}},
			Selector: rcLabels,
		},
		Status: corev1.ServiceStatus{},
	}
}

func GetTLSEnv() corev1.EnvVar {
	envValue := corev1.EnvVar{
		Name:  "TLS_ENABLED",
		Value: "true",
	}
	return envValue
}

func GetTLSContainerPort() corev1.ContainerPort {
	return corev1.ContainerPort{ContainerPort: HttpsPort, Name: "tls", Protocol: corev1.ProtocolTCP}
}

func GetTLSPort() corev1.ServicePort {
	return corev1.ServicePort{
		Name:       "tls",
		Protocol:   corev1.ProtocolTCP,
		Port:       HttpsPort,
		TargetPort: intstr.IntOrString{IntVal: HttpsPort},
	}
}
