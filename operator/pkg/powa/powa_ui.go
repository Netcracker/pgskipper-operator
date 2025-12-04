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

package powa

import (
	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var powaUILabels = map[string]string{"name": "powa"}

func NewPowaUIDeployment(spec v1.PowaUI, sa string) *appsv1.Deployment {
	deploymentName := "powa-ui"
	dockerImage := spec.Image
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: util.GetNameSpace(),
			Labels:    util.Merge(powaUILabels, spec.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(powaUILabels, spec.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(powaUILabels, spec.PodLabels),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "powa-config",
								},
							},
						},
					},
					ServiceAccountName: sa,
					Affinity:           &spec.Affinity,
					InitContainers:     []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:    deploymentName,
							Image:   dockerImage,
							Command: []string{},
							Args:    []string{},
							Env:     []corev1.EnvVar{},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Name: "web", Protocol: corev1.ProtocolTCP},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/powa-web.conf",
									Name:      "config-volume",
									SubPath:   "powa-web.conf",
								},
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

func GetConfigSecret(spec v1.PowaUI, pgServiceName string, isTLSEnabled bool) *corev1.Secret {
	configStr := "servers={\n" +
		"'main': {\n " +
		"'host': '" + pgServiceName + "',\n " +
		"'port': '5432',\n " +
		"'database': 'powa',\n" +
		"}\n" +
		"}\n" +
		"cookie_secret=\"" + getCookieSecret(spec.CookieSecret) + "\"\n" +
		"port=8080\n"

	if isTLSEnabled {
		configStr += getConfigTLSAppender()
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "powa-config",
			Namespace: util.GetNameSpace(),
		},
		Data: map[string][]byte{"powa-web.conf": []byte(configStr)},
	}
}

func getConfigTLSAppender() string {
	return "certfile=\"/certs/tls.crt\"\n" +
		"keyfile=\"/certs/tls.key\""
}

func GetService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "powa-ui",
			Namespace: util.GetNameSpace(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "web",
				Protocol:   corev1.ProtocolTCP,
				Port:       8080,
				TargetPort: intstr.IntOrString{IntVal: 8080},
			}},
			Selector: powaUILabels,
		},
		Status: corev1.ServiceStatus{},
	}
}

func getCookieSecret(secret string) string {
	if len(secret) > 0 {
		return secret
	}
	return uuid.New().String()
}
