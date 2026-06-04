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
							Name:  "MONITORED_IMAGES",
							Value: testsSpec.MonitoredImages,
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
	if testsSpec.AtpReport == nil || !testsSpec.AtpReport.Enabled {
		setDirectRobotArgs(&pod.Spec.Containers[0], testsTags)
	}
	pod.Spec.Containers[0].Env = appendAtpEnvVarsServices(
		pod.Spec.Containers[0].Env,
		testsSpec,
		fmt.Sprintf("%s-atp-storage-secret", cr.Name),
	)
	addTestPodHardening(pod)

	return pod
}

func NewCoreIntegrationTests(cr *patroniv1.PatroniCore, cluster *patroniv1.PatroniClusterSettings) *corev1.Pod {
	testsSpec := cr.Spec.IntegrationTests
	var testsTags string
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
							Name:  "MONITORED_IMAGES",
							Value: testsSpec.MonitoredImages,
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
	if testsSpec.AtpReport == nil || !testsSpec.AtpReport.Enabled {
		setDirectRobotArgs(&pod.Spec.Containers[0], testsTags)
	}
	pod.Spec.Containers[0].Env = appendAtpEnvVarsCore(
		pod.Spec.Containers[0].Env,
		testsSpec,
		fmt.Sprintf("%s-atp-storage-secret", cr.Name),
	)
	addTestPodHardening(pod)

	return pod
}

func setDirectRobotArgs(container *corev1.Container, testsTags string) {
	if testsTags == "" {
		container.Args = []string{"robot", "-d", "/opt/robot/output", "/opt/robot/tests"}
		return
	}
	container.Args = []string{"robot", "-i", testsTags, "-d", "/opt/robot/output", "/opt/robot/tests"}
}

func addTestPodHardening(pod *corev1.Pod) {
	pod.Spec.Containers[0].SecurityContext = util.GetReadOnlyContainerSecurityContext()
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"},
	)
	pod.Spec.Volumes = append(pod.Spec.Volumes,
		util.GetTmpVolume(),
		corev1.Volume{Name: "robot-output", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	)
	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts,
		util.GetTmpVolumeMount(),
		corev1.VolumeMount{Name: "robot-output", MountPath: "/opt/robot/output"},
	)
}

func appendAtpEnvVarsServices(env []corev1.EnvVar, tests *v1.IntegrationTests, secretName string) []corev1.EnvVar {
	if tests == nil {
		return env
	}

	if tests.EnvironmentName != "" {
		env = append(env, corev1.EnvVar{Name: "ENVIRONMENT_NAME", Value: tests.EnvironmentName})
	}

	if tests.AtpStorage != nil && tests.AtpStorage.Provider != "" {
		env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_PROVIDER", Value: tests.AtpStorage.Provider})

		if tests.AtpStorage.Region != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_REGION", Value: tests.AtpStorage.Region})
		}
		if tests.AtpStorage.ServerUrl != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_SERVER_URL", Value: tests.AtpStorage.ServerUrl})
		}
		if tests.AtpStorage.ServerUiUrl != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_SERVER_UI_URL", Value: tests.AtpStorage.ServerUiUrl})
		}
		if tests.AtpStorage.Bucket != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_BUCKET", Value: tests.AtpStorage.Bucket})
		}
	}

	atpReportEnabled := false
	if tests.AtpReport != nil {
		atpReportEnabled = tests.AtpReport.Enabled
	}
	env = append(env, corev1.EnvVar{Name: "ATP_REPORT_ENABLED", Value: strconv.FormatBool(atpReportEnabled)})

	if atpReportEnabled {
		env = append(env,
			corev1.EnvVar{
				Name: "ATP_STORAGE_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "atp-storage-username",
					},
				},
			},
			corev1.EnvVar{
				Name: "ATP_STORAGE_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "atp-storage-password",
					},
				},
			},
		)
	}

	if tests.AtpReportViewUiUrl != "" {
		env = append(env, corev1.EnvVar{Name: "ATP_REPORT_VIEW_UI_URL", Value: tests.AtpReportViewUiUrl})
	}

	return env
}

func appendAtpEnvVarsCore(env []corev1.EnvVar, tests *patroniv1.IntegrationTests, secretName string) []corev1.EnvVar {
	if tests == nil {
		return env
	}

	if tests.EnvironmentName != "" {
		env = append(env, corev1.EnvVar{Name: "ENVIRONMENT_NAME", Value: tests.EnvironmentName})
	}

	if tests.AtpStorage != nil && tests.AtpStorage.Provider != "" {
		env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_PROVIDER", Value: tests.AtpStorage.Provider})

		if tests.AtpStorage.Region != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_REGION", Value: tests.AtpStorage.Region})
		}
		if tests.AtpStorage.ServerUrl != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_SERVER_URL", Value: tests.AtpStorage.ServerUrl})
		}
		if tests.AtpStorage.ServerUiUrl != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_SERVER_UI_URL", Value: tests.AtpStorage.ServerUiUrl})
		}
		if tests.AtpStorage.Bucket != "" {
			env = append(env, corev1.EnvVar{Name: "ATP_STORAGE_BUCKET", Value: tests.AtpStorage.Bucket})
		}
	}

	atpReportEnabled := false
	if tests.AtpReport != nil {
		atpReportEnabled = tests.AtpReport.Enabled
	}
	env = append(env, corev1.EnvVar{Name: "ATP_REPORT_ENABLED", Value: strconv.FormatBool(atpReportEnabled)})

	if atpReportEnabled {
		env = append(env,
			corev1.EnvVar{
				Name: "ATP_STORAGE_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "atp-storage-username",
					},
				},
			},
			corev1.EnvVar{
				Name: "ATP_STORAGE_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "atp-storage-password",
					},
				},
			},
		)
	}

	if tests.AtpReportViewUiUrl != "" {
		env = append(env, corev1.EnvVar{Name: "ATP_REPORT_VIEW_UI_URL", Value: tests.AtpReportViewUiUrl})
	}

	return env
}
