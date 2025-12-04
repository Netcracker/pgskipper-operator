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

package credentials

import (
	"fmt"

	"time"

	"github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dbaasAdapterDeploymentName     = "dbaas-postgres-adapter"
	postgresExporterDeploymentName = "postgres-exporter"
	patroniCoreOperatorName        = "patroni-core-operator"

	passwordKey        = "password"
	PostgresSecretName = "postgres-credentials"
)

var (
	logger = util.GetLogger()

	PostgresSecretNames = []string{PostgresSecretName}
)

func ChangeCredsCore(newSecret, oldSecret *corev1.Secret) error {
	logger.Info("PostgreSQL credentials will be changed")
	cr, err := helper.GetPatroniHelper().GetPatroniCoreCR()
	if err != nil {
		return err
	}

	clusterSettings := util.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName)
	pgHost := clusterSettings.PgHost
	// Actualize creds for client
	client.UpdatePostgresClientPassword(string(oldSecret.Data[passwordKey]))

	// Change password in PostgreSQL
	pgClient := client.GetPostgresClient(pgHost)
	err = pgClient.Execute(fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", string(newSecret.Data["username"]), string(newSecret.Data[passwordKey])))
	if err != nil {
		return err
	}

	// Apply new creds for client
	client.UpdatePostgresClientPassword(string(newSecret.Data[passwordKey]))
	logger.Info("PostgreSQL credentials has been changed")
	return nil
}

func SetNewPasswordForPgClient(rm *helper.ResourceManager) error {
	oldSecret, err := rm.GetSecret(PostgresSecretName)
	if err != nil {
		return err
	}
	client.UpdatePostgresClientPassword(string(oldSecret.Data[passwordKey]))
	return err
}

func UpdateHelmDeployments(rm *helper.ResourceManager) error {
	dbaasDeployments, err := rm.GetDeploymentsByNameRegExp(dbaasAdapterDeploymentName)
	if err != nil {
		return err
	}

	pgExporterDeployments, err := rm.GetDeploymentsByNameRegExp(postgresExporterDeploymentName)
	if err != nil {
		return err
	}

	dbaasUpdate := len(dbaasDeployments) > 0
	pgExporterUpdate := len(pgExporterDeployments) > 0

	annotationName := manager.GetAnnotationName(0)
	patroniHash, err := manager.CalculateSecretDataHash(PostgresSecretName)
	if err != nil {
		return err
	}

	if dbaasUpdate {
		if patroniHash != dbaasDeployments[0].Spec.Template.Annotations[annotationName] {
			err = updateDeploymentWithHash(rm, annotationName, patroniHash, dbaasDeployments[0])
			if err != nil {
				return err
			}
		}
	}
	if pgExporterUpdate {
		if patroniHash != pgExporterDeployments[0].Annotations[annotationName] {
			err = updateDeploymentWithHash(rm, annotationName, patroniHash, pgExporterDeployments[0])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func updateDeploymentWithHash(rm *helper.ResourceManager, annotationName string, patroniHash string, deployment *appsv1.Deployment) error {
	annotations := map[string]string{
		annotationName: patroniHash,
	}
	if patroniHash != deployment.Spec.Template.Annotations[annotationName] {
		// Wait For Stability
		err := util.WaitForStabilityDepl(*deployment, deployment.Generation, -1)
		if err != nil {
			return err
		}
		// Wait for deployment renewal
		time.Sleep(5 * time.Second)

		deployments, err := rm.GetDeploymentsByNameRegExp(deployment.Name)
		if err != nil {
			return err
		}
		manager.AddAnnotationsToPodTemplate(&deployments[0].Spec.Template, annotations)
		err = rm.CreateOrUpdateDeployment(deployments[0], true)
		if err != nil {
			return err
		}
	}
	return nil
}

func ProcessCreds(ownerRef []metav1.OwnerReference) error {
	err := manager.ActualizeCreds(PostgresSecretName, ChangeCredsCore)
	if err != nil {
		logger.Error("cannot update Postgres creds", zap.Error(err))
		return err
	}
	err = manager.SetOwnerRefForSecretCopies(PostgresSecretNames, ownerRef)
	if err != nil {
		logger.Error("cannot update secrets Owner References", zap.Error(err))
		return err
	}
	return nil
}
