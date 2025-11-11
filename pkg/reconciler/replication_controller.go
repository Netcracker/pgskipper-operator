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
	"fmt"

	netcrackev1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/replicationcontroller"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type RCReconciler struct {
	cr      *netcrackev1.PatroniServices
	helper  *helper.Helper
	cluster *patroniv1.PatroniClusterSettings
}

func NewRCReconciler(cr *netcrackev1.PatroniServices, helper *helper.Helper, cluster *patroniv1.PatroniClusterSettings) *RCReconciler {
	return &RCReconciler{
		cr:      cr,
		helper:  helper,
		cluster: cluster,
	}
}

func (r *RCReconciler) Reconcile() error {
	cr := *r.cr
	srv := replicationcontroller.GetService()
	rcDeployment := replicationcontroller.NewRCDeployment(cr, cr.Spec.ServiceAccountName, r.cluster.ClusterName, r.cluster.PostgreSQLPort)
	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			rcDeployment.Spec.Template.Spec.ImagePullSecrets = append(rcDeployment.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to Replication Controller Deployment")
		rcDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	// Add Secret Hash
	err := manager.AddCredHashToPodTemplate(credentials.PostgresSecretNames, &rcDeployment.Spec.Template)
	if err != nil {
		logger.Error(fmt.Sprintf("can't add secret HASH to annotations for %s", rcDeployment.Name), zap.Error(err))
		return err
	}

	//Adding SecurityContext
	rcDeployment.Spec.Template.Spec.Containers[0].SecurityContext = opUtil.GetDefaultSecurityContext()

	if cr.Spec.Tls != nil {
		if cr.Spec.Tls.Enabled {
			// update RC deployment
			rcDeployment.Spec.Template.Spec.Containers[0].Env = append(rcDeployment.Spec.Template.Spec.Containers[0].Env, replicationcontroller.GetTLSEnv())
			rcDeployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(rcDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, opUtil.GetTlsSecretVolumeMount())
			rcDeployment.Spec.Template.Spec.Volumes = append(rcDeployment.Spec.Template.Spec.Volumes, opUtil.GetTlsSecretVolume(cr.Spec.Tls.CertificateSecretName))

			rcDeployment.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Scheme = "HTTPS"
			rcDeployment.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Port = intstr.IntOrString{IntVal: replicationcontroller.HttpsPort}

			rcDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Scheme = "HTTPS"
			rcDeployment.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Port = intstr.IntOrString{IntVal: replicationcontroller.HttpsPort}
			rcDeployment.Spec.Template.Spec.Containers[0].Ports = append(rcDeployment.Spec.Template.Spec.Containers[0].Ports, replicationcontroller.GetTLSContainerPort())
			// update RC service
			srv.Spec.Ports = append(srv.Spec.Ports, replicationcontroller.GetTLSPort())
		}
	}

	if err := r.helper.CreateOrUpdateDeploymentForce(rcDeployment, false); err != nil {
		logger.Error("error during creation of the Replication Controller deployment", zap.Error(err))
		return err
	}

	if err := r.helper.CreateOrUpdateService(srv); err != nil {
		logger.Error("error during create Replication Controller service", zap.Error(err))
		return err
	}

	return nil
}
