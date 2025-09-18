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
	"context"
	"fmt"
	"time"

	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	Secrets = []string{"replicator", "postgres"}

	logger    = util.GetLogger()
	namespace = util.GetNameSpace()
	k8sClient crclient.Client
)

func init() {
	var err error
	k8sClient, err = util.GetClient()
	if err != nil {
		logger.Error("cannot get k8s client")
		panic(err)
	}
}

func GetService(service *corev1.Service) (*corev1.Service, error) {
	foundService := &corev1.Service{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: service.Name, Namespace: service.Namespace,
	}, foundService)
	if err != nil {
		logger.Error(fmt.Sprintf("There is an error during getting service %s", service.Name), zap.Error(err))
		return nil, err
	}
	logger.Info(fmt.Sprintf("Getting %s k8s service for patch", service.Name))
	return foundService, nil
}

func UpdateService(service *corev1.Service) error {
	logger.Info(fmt.Sprintf("Updating %s k8s service", service.Name))
	err := k8sClient.Update(context.TODO(), service)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to update service %v", service.Name), zap.Error(err))
		return err
	}
	return nil
}

func UpdateServiceWithTls(s *corev1.Service, tlsPort []corev1.ServicePort) error {
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		if initialService, err := GetService(s); initialService != nil && err == nil {
			if !IsTlsPortContains(initialService.Spec.Ports, tlsPort) {
				initialService.Spec.Ports = append(initialService.Spec.Ports, tlsPort...)
				if err := UpdateService(initialService); err != nil {
					return false, nil
				} else {
					return true, nil
				}
			} else {
				return true, nil
			}

		}
		return false, err
	})
	if err != nil {
		return err
	}
	return nil
}

func IsTlsPortContains(ports []corev1.ServicePort, tlsPorts []corev1.ServicePort) bool {
	for _, port := range ports {
		for _, tlsPort := range tlsPorts {
			if port.Name == tlsPort.Name && port.Port == tlsPort.Port {
				logger.Info("Tls port already exists, skipping...")
				return true
			}
		}
	}
	return false
}

func reconcileService(name string, labels map[string]string, selectors map[string]string, ports []corev1.ServicePort, headless bool) *corev1.Service {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},

		Spec: corev1.ServiceSpec{
			Selector: selectors,
			Ports:    ports,
		},
	}
	if headless {
		service.Spec.ClusterIP = "None"
		service.Spec.Selector = nil
	}
	return service
}

func reconcileExternalService(name string, labels map[string]string, externalHost string, headless bool) *corev1.Service {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},

		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: externalHost,
		},
	}
	if headless {
		service.Spec.ClusterIP = "None"
		service.Spec.Selector = nil
	}
	return service
}

func reconcileEndpointSlice(name string, labels map[string]string) *discoveryv1.EndpointSlice {
	endpointSlice := &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "discovery.k8s.io/v1",
			Kind:       "EndpointSlice",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"kubernetes.io/service-name": name,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Name:     ptr.To("pg"),
				Port:     ptr.To[int32](5432),
				Protocol: ptr.To(corev1.ProtocolTCP),
			},
			{
				Name:     ptr.To("patroni"),
				Port:     ptr.To[int32](8008),
				Protocol: ptr.To(corev1.ProtocolTCP),
			},
		},
		Endpoints: []discoveryv1.Endpoint{},
	}

	for k, v := range labels {
		endpointSlice.Labels[k] = v
	}
	return endpointSlice
}
