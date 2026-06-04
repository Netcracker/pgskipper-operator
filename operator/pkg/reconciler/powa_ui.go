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
	netcrackev1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/powa"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type PowaUIReconciler struct {
	cr      *netcrackev1.PatroniServices
	helper  *helper.Helper
	cluster *patroniv1.PatroniClusterSettings
}

func NewPowaUIReconciler(cr *netcrackev1.PatroniServices, helper *helper.Helper, cluster *patroniv1.PatroniClusterSettings) *PowaUIReconciler {
	return &PowaUIReconciler{
		cr:      cr,
		helper:  helper,
		cluster: cluster,
	}
}

func (r *PowaUIReconciler) Reconcile() error {
	cr := r.cr
	powaUISpec := cr.Spec.PowaUI

	isTLSEnabled := cr.Spec.Tls != nil && cr.Spec.Tls.Enabled

	err := r.helper.CreateOrUpdateSecret(powa.GetConfigSecret(powaUISpec, r.cluster.PostgresServiceName, isTLSEnabled))
	if err != nil {
		logger.Error("error during Powa UI CM creation", zap.Error(err))
		return err
	}

	powaUIDeployment := powa.NewPowaUIDeployment(powaUISpec, cr.Spec.ServiceAccountName)

	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to Powa UI Deployment")
		powaUIDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	// TLS Section
	if isTLSEnabled && cr.Spec.ExternalDataBase == nil {
		logger.Info("Mount TLS secret volume for POWA UI")
		powaUIDeployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(powaUIDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, util.GetTlsSecretVolumeMount())
		powaUIDeployment.Spec.Template.Spec.Volumes = append(powaUIDeployment.Spec.Template.Spec.Volumes, util.GetTlsSecretVolume(cr.Spec.Tls.CertificateSecretName))
	}

	//Adding SecurityContext
	powaUIDeployment.Spec.Template.Spec.Containers[0].SecurityContext = util.GetReadOnlyContainerSecurityContext()
	powaUIDeployment.Spec.Template.Spec.Volumes = append(powaUIDeployment.Spec.Template.Spec.Volumes,
		util.GetTmpVolume(),
		getNginxCacheVolume(),
		getNginxRunVolume(),
	)
	powaUIDeployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(powaUIDeployment.Spec.Template.Spec.Containers[0].VolumeMounts,
		util.GetTmpVolumeMount(),
		getNginxCacheVolumeMount(),
		getNginxRunVolumeMount(),
	)

	if err = r.helper.CreateOrUpdateDeploymentForce(powaUIDeployment, false); err != nil {
		logger.Error("error during creation of the Powa deployment", zap.Error(err))
		return err
	}

	srv := powa.GetService()
	if err = r.helper.CreateOrUpdateService(srv); err != nil {
		logger.Error("error during create Powa UI service", zap.Error(err))
		return err
	}

	return nil
}

func getNginxCacheVolume() corev1.Volume {
	limit := resource.MustParse("10Mi")
	return corev1.Volume{
		Name: "nginx-cache",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &limit},
		},
	}
}

func getNginxRunVolume() corev1.Volume {
	limit := resource.MustParse("1Mi")
	return corev1.Volume{
		Name: "nginx-run",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &limit},
		},
	}
}

func getNginxCacheVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{Name: "nginx-cache", MountPath: "/var/cache/nginx"}
}

func getNginxRunVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{Name: "nginx-run", MountPath: "/var/run"}
}
