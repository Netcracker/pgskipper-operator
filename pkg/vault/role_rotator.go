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

package vault

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Netcracker/pgskipper-operator-core/pkg/reconciler"
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	pghelper "github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const operatorName = "postgres-operator"

var (
	rotController *rotationController
	dbaasLabels   = map[string]string{"app": "dbaas-postgres-adapter"}
)

type rotationController struct {
	vaultClient *Client
	helper      *pghelper.Helper
	k8sClient   crclient.Client
	cr          *qubershipv1.PatroniServices
	coreCr      *patroniv1.PatroniCore
	isEnabled   bool
	cluster     *patroniv1.PatroniClusterSettings
}

func EnableRotationController(client *Client) {
	rotController.enable(client)
}

func (rc *rotationController) enable(client *Client) {
	rc.isEnabled = true
	var err error
	rc.cr, err = rc.helper.GetPostgresServiceCR()
	if err != nil {
		logger.Error("Can't get PatroniServices CR for Vault client")
		panic(err)
	}
	rc.coreCr, err = rc.helper.GetPatroniCoreCR()
	if err != nil {
		logger.Error("Can't get PatroniCore CR for Vault client")
		panic(err)
	}
	rc.vaultClient = client
}

func (rc *rotationController) rotate(response http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !rc.isEnabled {
		logger.Info("Rotation is disabled")
		return
	}

	if err := rc.vaultClient.vaultRotatePgRoles(); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := rc.helper.UpdatePatroniReplicas(0, rc.cluster.ClusterName); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
	}
	if err := rc.waitForDeleteByLabels(rc.cluster.PatroniLabels); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := rc.helper.UpdatePatroniReplicas(1, rc.cluster.ClusterName); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
	}
	if err := util.WaitForPatroni(rc.coreCr, rc.cluster.PatroniMasterSelectors, rc.cluster.PatroniReplicasSelector); err != nil {
		logger.Error("error during waiting Patroni", zap.Error(err))
	}
	if rc.vaultClient.isMetricCollectorInstalled {
		if err := rc.restartPodWithLabels(reconciler.MetricCollectorLabels); err != nil {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if err := rc.restartPodWithLabels(dbaasLabels); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	if rc.vaultClient.isBackupDaemonInstalled {
		if err := rc.restartPodWithLabels(reconciler.BackupDaemonLabels); err != nil {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	if err := rc.vaultClient.UpdatePgClientPass(); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (rc *rotationController) restartPodWithLabels(podLabels map[string]string) error {
	podList, err := rc.helper.GetNamespacePodListBySelectors(podLabels)
	if err != nil {
		logger.Error(fmt.Sprintf("Cannot find pods with labels %s", podLabels), zap.Error(err))
	}

	for podIdx := 0; podIdx < len(podList.Items); podIdx++ {
		podForRestart := podList.Items[podIdx]
		logger.Info(fmt.Sprintf("Restart %v", podForRestart.ObjectMeta.Name))
		if err = rc.k8sClient.Delete(context.Background(), &podForRestart); err != nil {
			logger.Error(fmt.Sprintf("Pod %s cannot being restarted", podForRestart.Name), zap.Error(err))
		}
	}
	return nil
}

func (rc *rotationController) waitForDeleteByLabels(podLabels map[string]string) error {
	podList, err := rc.helper.GetNamespacePodListBySelectors(podLabels)
	if err != nil {
		logger.Error(fmt.Sprintf("Cannot find pods with labels %s", podLabels), zap.Error(err))
	}
	for podIdx := 0; podIdx < len(podList.Items); podIdx++ {
		podForRestart := podList.Items[podIdx]
		if err = util.WaitDeletePod(&podForRestart); err != nil {
			logger.Error(fmt.Sprintf("error during %s pod restart waiting", podForRestart.Name), zap.Error(err))
		}
	}
	return nil
}

func Init() {
	client, _ := util.GetClient()
	helper := pghelper.GetHelper()
	cr, err := helper.GetPostgresServiceCR()
	if err != nil {
		logger.Error("cannot fetch CR", zap.Error(err))
		return
	}
	rotController = &rotationController{
		helper:    helper,
		k8sClient: client,
		cluster:   util.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName),
	}
	if err := exposeRotatorPort(client); err != nil {
		logger.Error("can't expose rotate role port.")
		// service is used by Site Manager, so we need to handle this error
		panic(err)
	}
	http.HandleFunc("/rotate-roles", rotController.rotate)
}

func exposeRotatorPort(client crclient.Client) error {
	namespace := util.GetNameSpace()

	oServ := &corev1.Service{}
	err := client.Get(context.Background(), types.NamespacedName{
		Name: operatorName, Namespace: namespace,
	}, oServ)
	if err != nil {
		if errors.IsNotFound(err) {
			// this is almost copy 'n paste of /operator-framework/operator-sdk@v0.8.0/pkg/metrics/metrics.go#initOperatorService
			// but this does not set ownerReference to the service
			// hence we do not need finalizers rights
			logger.Info("Operator service not found, creating new one.")
			oServ = getOperatorService(operatorName, namespace)
			if err = helper.GetHelper().CreateServiceIfNotExists(oServ); err != nil {
				logger.Error(fmt.Sprintf("can't create service: %s", operatorName), zap.Error(err))
				return err
			}
		} else {
			logger.Error("can't get operator service", zap.Error(err))
		}
		return err
	}
	for _, port := range oServ.Spec.Ports {
		// checking if rotate already exists
		// to avoid: Service "postgres-operator" is invalid: spec.ports[2].name: Duplicate value: "rotate"
		if port.Name == "rotate" || port.Port == 8080 {
			logger.Info("Rotate port exists, no need to update, exiting.")
			return nil
		}
	}
	oServ.Spec.Ports = append(oServ.Spec.Ports, corev1.ServicePort{
		Name:     "rotate",
		Protocol: corev1.ProtocolTCP,
		Port:     8080,
		TargetPort: intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 8080,
		},
	})

	if err = client.Update(context.TODO(), oServ); err != nil {
		logger.Error(fmt.Sprintf("can't update service: %s", operatorName), zap.Error(err))
		return err
	}

	return nil
}

func getOperatorService(operatorName string, namespace string) *corev1.Service {
	label := map[string]string{"name": operatorName}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorName,
			Namespace: namespace,
			Labels:    label,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     8383,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8383,
					},
					Name: "metrics",
				},
				{
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8080,
					},
					Name: "rotate",
				},
				{
					Port:     8443,
					Protocol: corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8443,
					},
					Name: "web-tls",
				},
			},
			Selector: label,
		},
	}
}
