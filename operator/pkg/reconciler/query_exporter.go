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
	"github.com/Netcracker/pgskipper-operator/pkg/queryexporter"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

const defaultDatabase = "postgres"

type QueryExporterReconciler struct {
	cr      *netcrackev1.PatroniServices
	helper  *helper.Helper
	cluster *patroniv1.PatroniClusterSettings
}

func NewQueryExporterReconciler(cr *netcrackev1.PatroniServices, helper *helper.Helper, cluster *patroniv1.PatroniClusterSettings) *QueryExporterReconciler {
	return &QueryExporterReconciler{
		cr:      cr,
		helper:  helper,
		cluster: cluster,
	}
}

func (r *QueryExporterReconciler) Reconcile() error {
	cr := r.cr

	pgHost := fmt.Sprintf("pg-%s", r.cluster.ClusterName)
	err := queryexporter.EnsureQueryExporterUser(pgHost)
	if err != nil {
		logger.Error("cannot ensure Query Exporter user", zap.Error(err))
		return err
	}
	queryexporter.CreateQueryExporterExtensions(pgHost, defaultDatabase)
	queryExporterDeployment := queryexporter.NewQueryExporterDeployment(cr, cr.Spec.ServiceAccountName)
	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to Query Exporter Deployment")
		queryExporterDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	// Add Secret Hash
	err = manager.AddCredHashToPodTemplate(credentials.PostgresSecretNames, &queryExporterDeployment.Spec.Template)
	if err != nil {
		logger.Error(fmt.Sprintf("can't add secret HASH to annotations for %s", queryExporterDeployment.Name), zap.Error(err))
		return err
	}

	//Adding SecurityContext
	queryExporterDeployment.Spec.Template.Spec.Containers[0].SecurityContext = opUtil.GetDefaultSecurityContext()
	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			queryExporterDeployment.Spec.Template.Spec.ImagePullSecrets = append(queryExporterDeployment.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	if err := r.helper.CreateOrUpdateDeployment(queryExporterDeployment, false); err != nil {
		logger.Error("error during creation of the Query Exporter deployment", zap.Error(err))
		return err
	}

	srv := queryexporter.GetService()
	if err := r.helper.CreateOrUpdateService(srv); err != nil {
		logger.Error("error during create Query Exporter service", zap.Error(err))
		return err
	}

	return nil
}
