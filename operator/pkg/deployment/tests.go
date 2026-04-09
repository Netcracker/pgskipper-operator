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
	"regexp"
	"strconv"
	"strings"

	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	TestsLabels = map[string]string{"app": "patroni-tests"}
)

func NewIntegrationTestsPod(cr *v1.PatroniServices, cluster *patroniv1.PatroniClusterSettings) *corev1.Pod {
	testsSpec := cr.Spec.IntegrationTests
	var testsTags string
	opt := true
	pgHost := cluster.PostgresServiceName
	scenario := strings.ToLower(testsSpec.RunTestScenarios)
	if scenario == "full" {
		if cr.Spec.BackupDaemon != nil && cr.Spec.BackupDaemon.Resources != nil {
			testsTags = "backup*ORdbaas*"
		}
	} else if scenario == "basic" {
		if cr.Spec.BackupDaemon != nil && cr.Spec.BackupDaemon.Resources != nil {
			testsTags = "backup_basic"
		}
	} else if scenario == "custom" && testsSpec.TestList != nil {
		testsTags = strings.Join(testsSpec.TestList, "OR")
		r := regexp.MustCompile(`\s+`)
		testsTags = r.ReplaceAllString(testsTags, "_")
	}
	dockerImage := testsSpec.DockerImage
	name := "integration-robot-tests"
	ssl_mode := "prefer"
	if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled {
		ssl_mode = "require"
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "supplementary-robot-tests",
			Namespace: cr.Namespace,
			Labels:    util.Merge(TestsLabels, testsSpec.PodLabels),
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: cr.Spec.ServiceAccountName,
			Affinity:           &testsSpec.Affinity,
			InitContainers:     []corev1.Container{},
			Containers: []corev1.Container{
				{
					Name:            name,
					Image:           dockerImage,
					ImagePullPolicy: cr.Spec.ImagePullPolicy,
					SecurityContext: util.GetDefaultSecurityContext(),
					Args:            []string{"robot", "-i", testsTags, "/test_runs/"},
					Env: []corev1.EnvVar{
						{
							Name: "POSTGRES_USER",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "username",
								},
							},
						},
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
							Name:  "PG_CLUSTER_NAME",
							Value: cluster.ClusterName,
						},
						{
							Name:  "PG_NODE_QTY",
							Value: strconv.Itoa(testsSpec.PgNodeQty),
						},
						{
							Name:  "TESTS_TAGS",
							Value: testsTags,
						},
						{
							Name:  "PG_HOST",
							Value: pgHost,
						},
						{
							Name:  "PGSSLMODE",
							Value: ssl_mode,
						},
						{
							Name:  "INTERNAL_TLS_ENABLED",
							Value: util.InternalTlsEnabled(),
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
							Name: "DD_IMAGES",
							ValueFrom: &corev1.EnvVarSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "supplementary-tests-config"},
									Key:                  "dd_images",
									Optional:             &opt,
								},
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	if testsSpec.Resources != nil {
		pod.Spec.Containers[0].Resources = *testsSpec.Resources
	}

	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	return pod
}

func NewCoreIntegrationTests(cr *patroniv1.PatroniCore, cluster *patroniv1.PatroniClusterSettings) *corev1.Pod {
	testsSpec := cr.Spec.IntegrationTests
	var testsTags string
	opt := true
	pgHost := cluster.PostgresServiceName
	scenario := strings.ToLower(testsSpec.RunTestScenarios)
	if cr.Spec.Patroni.StandbyCluster != nil {
		pgHost = fmt.Sprintf("pg-%s-external", cluster.ClusterName)
	}
	if strings.ToLower(cr.Spec.Patroni.Dcs.Type) != "kubernetes" {
		testsTags = "patroni_simple"
	} else {
		if scenario == "full" {
			testsTags = "patroni*"
		} else if scenario == "basic" {
			testsTags = "patroni_basic"
		} else if scenario == "custom" && len(testsSpec.TestList) > 0 {
			testsTags = strings.Join(testsSpec.TestList, "OR")
			r := regexp.MustCompile(`\s+`)
			testsTags = r.ReplaceAllString(testsTags, "_")
		}
	}
	dockerImage := testsSpec.DockerImage
	name := "patroni-robot-tests"
	ssl_mode := "prefer"
	if cr.Spec.Tls != nil && cr.Spec.Tls.Enabled {
		ssl_mode = "require"
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "integration-robot-tests",
			Namespace: cr.Namespace,
			Labels:    util.Merge(TestsLabels, testsSpec.PodLabels),
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: cr.Spec.ServiceAccountName,
			Affinity:           &testsSpec.Affinity,
			InitContainers:     []corev1.Container{},
			Containers: []corev1.Container{
				{
					Name:            name,
					Image:           dockerImage,
					ImagePullPolicy: cr.Spec.ImagePullPolicy,
					SecurityContext: util.GetDefaultSecurityContext(),
					Args:            []string{"robot", "-i", testsTags, "/test_runs/"},
					Env: []corev1.EnvVar{
						{
							Name: "POSTGRES_USER",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "username",
								},
							},
						},
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
							Name:  "PG_CLUSTER_NAME",
							Value: cluster.ClusterName,
						},
						{
							Name:  "UNLIMITED",
							Value: fmt.Sprintf("%t", cr.Spec.Patroni.Unlimited),
						},
						{
							Name:  "PG_NODE_QTY",
							Value: strconv.Itoa(testsSpec.PgNodeQty),
						},
						{
							Name:  "TESTS_TAGS",
							Value: testsTags,
						},
						{
							Name:  "PG_HOST",
							Value: pgHost,
						},
						{
							Name:  "PGSSLMODE",
							Value: ssl_mode,
						},
						{
							Name:  "INTERNAL_TLS_ENABLED",
							Value: util.InternalTlsEnabled(),
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
							Name: "DD_IMAGES",
							ValueFrom: &corev1.EnvVarSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "patroni-tests-config"},
									Key:                  "dd_images",
									Optional:             &opt,
								},
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	if testsSpec.Resources != nil {
		pod.Spec.Containers[0].Resources = *testsSpec.Resources
	}

	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	return pod
}
