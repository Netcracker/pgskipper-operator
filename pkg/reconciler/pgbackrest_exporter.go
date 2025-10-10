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
	"github.com/Netcracker/pgskipper-operator/pkg/pgbackrestexporter"
	"go.uber.org/zap"
)

type PgBackRestExporterReconciler struct {
	cr      *netcrackev1.PatroniServices
	helper  *helper.Helper
	cluster *patroniv1.PatroniClusterSettings
}

func NewPgBackRestExporterReconciler(cr *netcrackev1.PatroniServices, helper *helper.Helper, cluster *patroniv1.PatroniClusterSettings) *PgBackRestExporterReconciler {
	return &PgBackRestExporterReconciler{
		cr:      cr,
		helper:  helper,
		cluster: cluster,
	}
}

func (r *PgBackRestExporterReconciler) Reconcile() error {
	cr := r.cr
	pgBackRestExporterSpec := *cr.Spec.Pgbackrestexporter

	deployment := pgbackrestexporter.NewPgBackRestExporterDeployment(pgBackRestExporterSpec, cr.Spec.ServiceAccountName)

	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to PgBackRest Exporter Deployment")
		deployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	if err := r.helper.CreateOrUpdateDeploymentForce(deployment, false); err != nil {
		logger.Error("error during creation of the PgBackRest Exporter deployment", zap.Error(err))
		return err
	}

	srv := pgbackrestexporter.GetPgBackRestExporterService()
	if err := r.helper.CreateOrUpdateService(srv); err != nil {
		logger.Error("error during create PgBackRest Exporter service", zap.Error(err))
		return err
	}

	return nil
}
