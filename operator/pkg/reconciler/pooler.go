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

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/pooler"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

type PoolerReconciler struct {
	cr      *qubershipv1.PatroniServices
	helper  *helper.Helper
	cluster *patroniv1.PatroniClusterSettings
}

func NewPoolerReconciler(cr *qubershipv1.PatroniServices, helper *helper.Helper, cluster *patroniv1.PatroniClusterSettings) *PoolerReconciler {
	return &PoolerReconciler{
		cr:      cr,
		helper:  helper,
		cluster: cluster,
	}
}

func (r *PoolerReconciler) Reconcile() error {
	cr := r.cr
	updated, err := r.helper.CreateOrUpdateConfigMap(pooler.GetConfigMap(cr.Spec.Pooler))
	if err != nil {
		logger.Error("error during Pooler CM creation", zap.Error(err))
		return err
	}
	newPatroniName := fmt.Sprintf("pg-%s-direct", r.cluster.ClusterName)
	pgService := reconcileService(newPatroniName, r.cluster.PatroniLabels,
		r.cluster.PatroniMasterSelectors, deployment.GetPortsForPatroniService(r.cluster.ClusterName), false)
	if err = r.helper.CreateOrUpdateService(pgService); err != nil {
		logger.Error(fmt.Sprintf("Cannot create service %s", pgService.Name), zap.Error(err))
		return err
	}

	if updated {
		// Scale deployment for applying new configuration
		depl, err := r.helper.GetDeploymentsByNameRegExp(pooler.DeploymentName)
		if err != nil {
			return err
		}
		for _, d := range depl {
			zeroCount := int32(0)
			d.Spec.Replicas = &zeroCount
			if err = r.helper.CreateOrUpdateDeployment(d, false); err != nil {
				return err
			}
		}
	}

	creds, err := pooler.GetPgBouncerCreds()
	if err != nil {
		return err
	}

	// Create Super User for authentication check
	if err = pooler.SetUpDatabase(creds, newPatroniName); err != nil {
		return err
	}

	poolerDeployment := pooler.NewPoolerDeployment(cr, cr.Spec.ServiceAccountName)
	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			poolerDeployment.Spec.Template.Spec.ImagePullSecrets = append(poolerDeployment.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to Pooler Deployment")
		poolerDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	// Add Secret Hash
	err = manager.AddCredHashToPodTemplate(credentials.PostgresSecretNames, &poolerDeployment.Spec.Template)
	if err != nil {
		logger.Error(fmt.Sprintf("can't add secret HASH to annotations for %s", poolerDeployment.Name), zap.Error(err))
		return err
	}

	//Adding SecurityContext
	poolerDeployment.Spec.Template.Spec.Containers[0].SecurityContext = opUtil.GetDefaultSecurityContext()

	if err = r.helper.CreateOrUpdateDeploymentForce(poolerDeployment, false); err != nil {
		logger.Error("error during creation of the Pooler deployment", zap.Error(err))
	}

	if err = pooler.UpdatePatroniService(r.helper, r.cluster.PostgresServiceName); err != nil {
		return err
	}

	return nil
}
