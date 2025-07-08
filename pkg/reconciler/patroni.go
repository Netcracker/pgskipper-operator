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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/pgskipper-operator-core/pkg/storage"
	v1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/Netcracker/pgskipper-operator/pkg/powa"
	"github.com/Netcracker/pgskipper-operator/pkg/queryexporter"
	"github.com/Netcracker/pgskipper-operator/pkg/upgrade"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/vault"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var wg sync.WaitGroup

var commands = []string{
	"pg_ctl stop -D /var/lib/pgsql/data/postgresql_node%[1]s",
	"cp -R /var/lib/pgsql/data/postgresql_node%[1]s/pg_wal/* /var/lib/pgsql/pg_wal/",
	"rm -rf /var/lib/pgsql/data/postgresql_node%[1]s/pg_wal_backup",
	"mv /var/lib/pgsql/data/postgresql_node%[1]s/pg_wal /var/lib/pgsql/data/postgresql_node%[1]s/pg_wal_backup",
	"ln -s /var/lib/pgsql/pg_wal /var/lib/pgsql/data/postgresql_node%[1]s",
	"pg_ctl start -D /var/lib/pgsql/data/postgresql_node%[1]s",
	"rm -rf /var/lib/pgsql/data/postgresql_node%[1]s/pg_wal_backup",
}

type PatroniReconciler struct {
	cr          *v1.PatroniCore
	helper      *helper.PatroniHelper
	vaultClient *vault.Client
	upgrade     *upgrade.Upgrade
	scheme      *runtime.Scheme
	cluster     *v1.PatroniClusterSettings
}

func NewPatroniReconciler(cr *v1.PatroniCore, helper *helper.PatroniHelper, vaultClient *vault.Client, upgrade *upgrade.Upgrade, scheme *runtime.Scheme, cluster *v1.PatroniClusterSettings) *PatroniReconciler {
	return &PatroniReconciler{
		cr:          cr,
		helper:      helper,
		vaultClient: vaultClient,
		upgrade:     upgrade,
		scheme:      scheme,
		cluster:     cluster,
	}
}

func (r *PatroniReconciler) Reconcile() error {
	cr := r.cr
	patroniSpec := cr.Spec.Patroni
	patroniConfigMap := deployment.ConfigMapForPatroni(r.cluster.ClusterName, r.cluster.PatroniCM, r.cluster.ConfigMapKey)
	isStandbyClusterPresent := patroni.IsStandbyClusterConfigurationExist(cr)
	isPgbackrestUsed := cr.Spec.PgBackRest != nil

	if cr.Upgrade != nil && cr.Upgrade.Enabled {
		logger.Info("Starting an upgrade procedure")
		time.Sleep(30 * time.Second)
		if err := r.upgrade.ProceedUpgrade(cr, r.cluster); err != nil {
			logger.Error("Cannot upgrade patroni", zap.Error(err))
			return err
		}
	}

	if isStandbyClusterPresent {
		patroni.AddStandbyClusterSettings(cr, patroniConfigMap, r.cluster.ConfigMapKey)
	} else {
		patroni.DeleteStandbyClusterSettings(patroniConfigMap, r.cluster.ConfigMapKey)
	}
	if (patroniSpec.Dcs.Type == "etcd") || (patroniSpec.Dcs.Type == "etcd3") {
		patroni.AddEtcdSettings(cr, patroniConfigMap, r.cluster.ConfigMapKey)
		patroni.UpdatePatroniConfigMap(patroniConfigMap, patroniSpec.Scope, "scope", r.cluster.ConfigMapKey)

	}

	if patroniSpec.Tags != nil {
		patroni.AddTagsSettings(cr, patroniConfigMap, r.cluster.ConfigMapKey)
	}

	if isPgbackrestUsed {
		err := r.preparePgbackRest(cr, patroniConfigMap)
		if err != nil {
			return err
		}
	}

	if _, err := r.helper.ResourceManager.CreateOrUpdateConfigMap(patroniConfigMap); err != nil {
		logger.Error(fmt.Sprintf("Cannot create or update config map %s", patroniConfigMap.Name), zap.Error(err))
		return err
	}

	pgParamsConfigMap := deployment.ConfigMapForPostgreSQL(r.cluster.ClusterName, r.cluster.PatroniPropertiesCM)
	if _, err := r.helper.ResourceManager.CreateOrUpdateConfigMap(pgParamsConfigMap); err != nil {
		logger.Error(fmt.Sprintf("Cannot create config map %s", pgParamsConfigMap.Name), zap.Error(err))
		return err
	}

	for _, userName := range Secrets {
		logger.Info(fmt.Sprintf("Checking for %s secret existence", userName))
		pgSecret := deployment.PatroniSecret(cr.Namespace, userName, r.cluster.PatroniLabels)
		if err := r.helper.ResourceManager.CreateSecretIfNotExists(pgSecret); err != nil {
			logger.Error(fmt.Sprintf("Cannot create secret %s", pgSecret.Name), zap.Error(err))
			return err
		}

		if err := r.vaultClient.ProcessRoleSecret(pgSecret); err != nil {
			return err
		}
	}

	vaultRolesExist := r.vaultClient.IsVaultRolesExist()

	if vaultRolesExist {
		if err := r.vaultClient.UpdatePgClientPass(); err != nil {
			return err
		}
	}

	// find possible deployments by pods
	// try to get master pod
	masterPod, err := r.helper.ResourceManager.GetPodsByLabel(r.cluster.PatroniMasterSelectors)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		logger.Info("Can not get master pod")
	}

	if len(masterPod.Items) != 0 {
		// Ensure services
		err := r.processPatroniServices(cr, patroniSpec)
		if err != nil {
			return err
		}

		isUpgrade := r.upgrade.CheckUpgrade(cr, r.cluster)
		if isUpgrade {
			logger.Info("Proceed Major Upgrade")
			cr.Upgrade.Enabled = true
			return r.Reconcile()
		}
		// check if cluster healthy
		var statefulCount int
		logger.Info("Update Postgres Service")
		patroniStatefulSetName := fmt.Sprintf("pg-%s-node", r.cluster.ClusterName)
		statefulCount, _ = r.helper.ResourceManager.GetStatefulsetCountByNameRegExp(patroniStatefulSetName)

		if statefulCount == 0 {
			patroniDeploymentName := fmt.Sprintf("pg-%s-node", r.cluster.ClusterName)
			foundDeployment, err := r.helper.ResourceManager.GetDeploymentsByNameRegExp(patroniDeploymentName)
			if err != nil {
				return err
			}
			statefulCount = len(foundDeployment)
			logger.Info(fmt.Sprintf("Statefulset count after deployment check: %v", statefulCount))

		} else {

			if statefulCount, err = r.helper.ResourceManager.GetStatefulsetCountByNameRegExp(patroniStatefulSetName); err != nil {
				logger.Error("Can't get existing Patroni Deployments", zap.Error(err))
				return err
			}
			logger.Info(fmt.Sprintf("Statefulset count in update: %v", statefulCount))
		}

		sslVal := deployment.ExtractParamsFromCRByName(cr, "ssl")
		logger.Info(fmt.Sprintf("SSL value obtained from postgreSQLParams: %s", sslVal))
		if sslVal == "off" {
			_ = patroni.SetSslStatus(cr, r.cluster.PatroniUrl)
		}

		if _, err := r.helper.IsHealthyWithTimeoutDuringUpdate(3*time.Minute, r.cluster.PatroniUrl, r.cluster.PgHost, statefulCount); err == nil {

			// check locale version, because different versions can affect postgres data
			localeVersion := r.helper.GetLocaleVersion(masterPod.Items[0].Name)

			replicaPods, err := r.helper.ResourceManager.GetPodsByLabel(r.cluster.PatroniReplicasSelector)
			if err != nil {
				logger.Error("Can not get replica pods")
				return err
			}
			for _, replica := range replicaPods.Items {
				statefulsetName := replica.Spec.Containers[0].Name
				statefulsetIdx, _ := strconv.Atoi(statefulsetName[len(statefulsetName)-1:])
				logger.Info(fmt.Sprintf("Update replica deployment: %s", statefulsetName))
				if err = r.processPatroniStatefulset(cr, statefulsetIdx); err != nil {
					return err
				}
			}
			// check cluster status with timeout
			logger.Debug("Update replica deployment is successful")
			if _, err := r.helper.IsHealthyWithTimeoutDuringUpdate(3*time.Minute, r.cluster.PatroniUrl, r.cluster.PgHost, statefulCount); err != nil {
				logger.Error("Patroni cluster is not healthy after replicas update")
				return err
			}
			// update master deployment
			statefulsetName := masterPod.Items[0].Spec.Containers[0].Name
			masterStatefulsetIdx, _ := strconv.Atoi(statefulsetName[len(statefulsetName)-1:])
			logger.Debug(fmt.Sprintf("Update master deployment: %s", statefulsetName))
			if err = r.processPatroniStatefulset(cr, masterStatefulsetIdx); err != nil {
				return err
			}

			if len(replicaPods.Items) == 0 && (cr.Spec.Patroni.Replicas-1 > 0) {
				logger.Info("creating new replica deployment")
				for statefulsetIdx := 2; statefulsetIdx <= cr.Spec.Patroni.Replicas; statefulsetIdx++ {
					if err = r.processPatroniStatefulset(cr, statefulsetIdx); err != nil {
						return err
					}
				}
			}

			existingStatefulsets, err := r.helper.ResourceManager.GetStatefulsetByNameRegExp(patroniStatefulSetName)
			if err != nil {
				return err
			}

			if len(existingStatefulsets) < cr.Spec.Patroni.Replicas {
				existingIds, err := r.helper.GetStatefulSetIds(existingStatefulsets)
				if err != nil {
					return err
				}
				for eId := 1; eId <= cr.Spec.Patroni.Replicas; eId++ {
					if opUtil.SliceContains(existingIds, eId) {
						continue
					}
					expectedStatefulsetName := fmt.Sprintf("pg-%s-node%d", r.cluster.ClusterName, eId)
					logger.Info(fmt.Sprintf("StatefulSet %s is missing, creating it now", expectedStatefulsetName))
					if err := r.processPatroniStatefulset(cr, eId); err != nil {
						logger.Error(fmt.Sprintf("Failed to create StatefulSet %s", expectedStatefulsetName), zap.Error(err))
						return err
					}
				}
			}

			if _, err := r.helper.IsHealthyWithTimeout(3*time.Minute, r.cluster.PatroniUrl, r.cluster.PgHost); err != nil {
				logger.Error("Patroni cluster is not healthy after master update")
				return err
			}

			// compare locale versions and run fix for collation in postres
			updatedMasterPod, _ := r.helper.ResourceManager.GetPodsByLabel(r.cluster.PatroniMasterSelectors)
			newLocaleVersion := r.helper.GetLocaleVersionFromPod(updatedMasterPod.Items[0].Name)
			pgVersion, err := strconv.ParseInt(r.helper.GetPGVersionFromPod(updatedMasterPod.Items[0].Name), 10, 64)
			if err != nil {
				logger.Error("cannot parse pg version")
				return err
			}
			if localeVersion != newLocaleVersion || cr.Spec.Patroni.ForceCollationVersionUpgrade {
				logger.Warn(fmt.Sprintf("New os locale version is %s, but previous was %s. A collation version mismatch occured in databases. Run locale fix script", newLocaleVersion, localeVersion))
				r.helper.StoreDataToCM("locale-version", newLocaleVersion)
				r.runLocaleFixScript(pgVersion)
			}

		} else {
			logger.Error("Patroni cluster is not healthy. Skip Patroni update")
			return err
		}
	} else {
		logger.Info("Postgres service not installed yet. Process new deployment...")
		replicas := patroniSpec.Replicas
		for statefulsetIdx := 1; statefulsetIdx <= replicas; statefulsetIdx++ {
			if err = r.processPatroniStatefulset(cr, statefulsetIdx); err != nil {
				return err
			}
		}

		err := r.processPatroniServices(cr, patroniSpec)
		if err != nil {
			return err
		}
	}

	if isStandbyClusterPresent {
		if err := patroni.AddStandbyClusterConfigurationConfigMap(cr, r.cluster.PatroniUrl); err != nil {
			return err
		}
	} else {
		if err := patroni.ClearStandbyClusterConfigurationConfigMap(r.cluster.PatroniUrl); err != nil {
			return err
		}
	}
	// Set replicator password from Secret
	if !cr.Spec.VaultRegistration.DbEngine.Enabled {
		if err := r.helper.SyncReplicatorPassword(r.cluster.PgHost); err != nil {
			return err
		}
	}

	// add necessary shared_preload_libraries and settings
	if cr.Spec.Patroni.Powa.Install {
		powa.UpdatePgSettings(cr)
		powa.UpdatePreloadLibraries(cr)
	}
	// We decide to update preload libraries for exporter by default. In case of supplementary service separation
	queryexporter.UpdatePreloadLibraries(cr)

	if err := patroni.UpdatePatroniParams(patroniSpec, r.cluster.PatroniUrl); err != nil {
		logger.Error("Failed to update Patroni Params, exiting", zap.Error(err))
		return err
	}
	if err := patroni.UpdatePostgreSQLParams(patroniSpec, r.cluster.PatroniUrl); err != nil {
		logger.Error("Failed to update PostgreSQL Params, exiting", zap.Error(err))
		return err
	}

	//ldap integration settings
	if cr.Spec.Ldap != nil && cr.Spec.Ldap.Enabled {
		logger.Info("setting up ldap params")
		if err := r.helper.CreatePgAdminRole(r.cluster.PgHost); err != nil {
			logger.Error("Can not create pgadminrole for ldap", zap.Error(err))
			return err
		}
		_ = patroni.SetLdapConfig(cr, r.cluster.PatroniUrl)
	}

	if err := opUtil.WaitForPatroni(cr, r.cluster.PatroniMasterSelectors, r.cluster.PatroniReplicasSelector); err != nil {
		return err
	}

	if patroniSpec.PgWalStorage != nil && patroniSpec.PgWalStorageAutoManage {

		if err := r.helper.PausePatroni(r.cluster.PatroniUrl); err != nil {
			logger.Error("Can not pause patroni to execute pg_wal copying command")
			return err
		}
		if err := r.processPgWalStorageExternal(); err != nil {
			logger.Error("Can not process pg_wal copying. skipping", zap.Error(err))
			//TODO: do we really need to return err here to fail reconciliation ?
		}
		if err := r.helper.ResumePatroni(r.cluster.PatroniUrl); err != nil {
			logger.Error("Can not resume patroni after pg_wal copying command")
			return err
		}
	}

	if err := opUtil.WaitForPatroni(cr, r.cluster.PatroniMasterSelectors, r.cluster.PatroniReplicasSelector); err != nil {
		return err
	}

	// Activating Vault PostgreSQL plugin if it enabled
	if err := r.vaultClient.PrepareDbEngine(vaultRolesExist, r.cluster); err != nil {
		return err
	}

	if patroniSpec.Powa.Install {
		if err := powa.SetUpPOWA(r.cluster.PgHost); err != nil {
			return err
		}
	}
	return nil
}

func (r *PatroniReconciler) processPatroniServices(cr *v1.PatroniCore, patroniSpec *v1.Patroni) error {
	logger.Info("patroni services reconcile started...")
	if (patroniSpec.Dcs.Type == "etcd") || (patroniSpec.Dcs.Type == "etcd3") {
		logger.Info("Creating headless services")
		if err := r.createServicesForEtcdAsDcs(); err != nil {
			logger.Error("Failed to create headless service", zap.Error(err))
			return err
		}
		logger.Info("Creating endpoints for headless services")
		if patroniSpec.CreateEndpoint {
			if err := r.createEndpointsForEtcdAsDcs(); err != nil {
				logger.Error("Failed to create endpoints", zap.Error(err))
				return err
			}
		}
	} else {
		pgService := reconcileService(r.cluster.PostgresServiceName, r.cluster.PatroniLabels,
			r.cluster.PatroniMasterSelectors, deployment.GetPortsForPatroniService(r.cluster.ClusterName), false)
		if err := r.helper.ResourceManager.CreateOrUpdateService(pgService); err != nil {
			logger.Error(fmt.Sprintf("Cannot create service %s", pgService.Name), zap.Error(err))
			return err
		}
		pgReadOnlyService := reconcileService(r.cluster.PostgresServiceName+"-ro", r.cluster.PatroniLabels,
			r.cluster.PatroniReplicasSelector, deployment.GetPortsForPatroniService(r.cluster.ClusterName), false)
		if err := r.helper.ResourceManager.CreateServiceIfNotExists(pgReadOnlyService); err != nil {
			logger.Error(fmt.Sprintf("Cannot create service %s", pgReadOnlyService.Name), zap.Error(err))
			return err
		}
		patroniApiService := reconcileService(r.cluster.PostgresServiceName+"-api", r.cluster.PatroniLabels,
			r.cluster.PatroniCommonLabels, deployment.GetPortsForPatroniService(r.cluster.ClusterName), false)
		if err := r.helper.ResourceManager.CreateServiceIfNotExists(patroniApiService); err != nil {
			logger.Error(fmt.Sprintf("Cannot create service %s", pgService.Name), zap.Error(err))
			return err
		}
		if cr.Spec.PgBackRest != nil {
			pgBackRestService := deployment.GetPgBackRestService(r.cluster.PatroniMasterSelectors, false)
			if err := r.helper.ResourceManager.CreateOrUpdateService(pgBackRestService); err != nil {
				logger.Error(fmt.Sprintf("Cannot create service %s", pgService.Name), zap.Error(err))
				return err
			}
			if cr.Spec.PgBackRest.BackupFromStandby {
				pgBackRestStandbyService := deployment.GetPgBackRestService(r.cluster.PatroniReplicasSelector, true)
				if err := r.helper.ResourceManager.CreateOrUpdateService(pgBackRestStandbyService); err != nil {
					logger.Error(fmt.Sprintf("Cannot create service %s", pgBackRestStandbyService.Name), zap.Error(err))
					return err
				}
			}
			pgBackRestHeadless := deployment.GetBackrestHeadless()
			if err := r.helper.ResourceManager.CreateServiceIfNotExists(pgBackRestHeadless); err != nil {
				logger.Error(fmt.Sprintf("Cannot create service %s", pgBackRestHeadless.Name), zap.Error(err))
				return err
			}
		}
	}
	return nil
}

func (r *PatroniReconciler) processPatroniStatefulset(cr *v1.PatroniCore, deploymentIdx int) error {
	// delete patroni deproyments for further replacing
	deploymentName := fmt.Sprintf("pg-%s-node%v", r.cluster.ClusterName, deploymentIdx)
	err := r.helper.DeleteDeployment(deploymentName)
	if err != nil {
		return err
	}

	vaultRolesExist := r.vaultClient.IsVaultRolesExist()
	patroniSpec := cr.Spec.Patroni
	pvc := storage.NewPvc(fmt.Sprintf("%s-data-%v", opUtil.GetPatroniClusterName(cr.Spec.Patroni.ClusterName), deploymentIdx), patroniSpec.Storage, deploymentIdx)
	if err := r.helper.ResourceManager.CreatePvcIfNotExists(pvc); err != nil {
		logger.Error(fmt.Sprintf("Cannot create pvc %s", pvc.Name), zap.Error(err))
		return err
	}
	if patroniSpec.PgWalStorage != nil {
		pvc := storage.NewPvc(fmt.Sprintf("%s-wals-data-%v", opUtil.GetPatroniClusterName(cr.Spec.Patroni.ClusterName), deploymentIdx), patroniSpec.PgWalStorage, deploymentIdx)
		if err := r.helper.ResourceManager.CreatePvcIfNotExists(pvc); err != nil {
			logger.Error(fmt.Sprintf("Cannot create pvc %s", pvc.Name), zap.Error(err))
			return err
		}
	}
	if cr.Spec.PgBackRest != nil && strings.ToLower(cr.Spec.PgBackRest.RepoType) == "rwx" {
		pgBackrestStorage := cr.Spec.PgBackRest.Rwx
		pgBackrestStorage.AccessModes = []string{"ReadWriteMany"}
		pvc = storage.NewPvc("pgbackrest-backups", pgBackrestStorage, 1)
		if err := r.helper.ResourceManager.CreatePvcIfNotExists(pvc); err != nil {
			logger.Error(fmt.Sprintf("Cannot create pvc %s", pvc.Name), zap.Error(err))
			return err
		}
	}

	// check deployments
	patroniDeployment := deployment.NewPatroniStatefulset(cr, deploymentIdx, r.cluster.ClusterName, r.cluster.PatroniTemplate, r.cluster.PostgreSQLUserConf, r.cluster.PatroniLabels)

	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			patroniDeployment.Spec.Template.Spec.ImagePullSecrets = append(patroniDeployment.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	if cr.Spec.Policies != nil {
		logger.Info("Policies is not empty, setting them to Patroni Statefulset")
		patroniDeployment.Spec.Template.Spec.Tolerations = cr.Spec.Policies.Tolerations
	}

	// Add Secret Hash
	err = manager.AddCredHashToPodTemplate(credentials.PostgresSecretNames, &patroniDeployment.Spec.Template)
	if err != nil {
		logger.Error(fmt.Sprintf("can't add secret HASH to annotations for %s", patroniDeployment.Name), zap.Error(err))
		return err
	}

	// Vault Section
	// For DbEngine case this section processed later for patroni
	if vaultRolesExist || (cr.Spec.VaultRegistration.Enabled && !cr.Spec.VaultRegistration.DbEngine.Enabled) {
		r.vaultClient.ProcessVaultSectionStatefulset(patroniDeployment, vault.PatroniEntrypoint, Secrets)
	}

	if err := r.helper.ResourceManager.CreateOrUpdateStatefulset(patroniDeployment, true); err != nil {
		logger.Error(fmt.Sprintf("Cannot create or update deployment %s", patroniDeployment.Name), zap.Error(err))
		return err
	}
	return nil
}

func (r *PatroniReconciler) createEndpointsForEtcdAsDcs() error {
	pgEndpoint := reconcileEndpoint(r.cluster.PostgresServiceName, r.cluster.PatroniLabels)
	if err := r.helper.ResourceManager.CreateEndpointIfNotExists(pgEndpoint); err != nil {
		logger.Error(fmt.Sprintf("Cannot create endpoint %s", pgEndpoint.Name), zap.Error(err))
		return err
	}
	pgReadOnlyEndpoint := reconcileEndpoint(r.cluster.PostgresServiceName, r.cluster.PatroniLabels)
	if err := r.helper.ResourceManager.CreateEndpointIfNotExists(pgReadOnlyEndpoint); err != nil {
		logger.Error(fmt.Sprintf("Cannot create endpoint %s", pgReadOnlyEndpoint.Name), zap.Error(err))
		return err
	}
	return nil
}

func (r *PatroniReconciler) createServicesForEtcdAsDcs() error {
	pgService := reconcileService(r.cluster.PostgresServiceName, r.cluster.PatroniLabels,
		r.cluster.PatroniMasterSelectors, deployment.GetPortsForPatroniService(r.cluster.ClusterName), true)
	if err := r.helper.ResourceManager.CreateOrUpdateService(pgService); err != nil {
		logger.Error(fmt.Sprintf("Cannot create service %s", pgService.Name), zap.Error(err))
		return err
	}
	pgReadOnlyService := reconcileService(r.cluster.PatroniReplicasServiceName+"-ro", r.cluster.PatroniLabels,
		r.cluster.PatroniReplicasSelector, deployment.GetPortsForPatroniService(r.cluster.ClusterName), true)
	if err := r.helper.ResourceManager.CreateOrUpdateService(pgReadOnlyService); err != nil {
		logger.Error(fmt.Sprintf("Cannot create service %s", pgReadOnlyService.Name), zap.Error(err))
		return err
	}
	return nil
}

func (r *PatroniReconciler) runLocaleFixScript(pgVersion int64) {
	pgC := pgClient.GetPostgresClient(r.cluster.PgHost)
	databaseList := helper.GetAllDatabases(pgC)
	wg.Wait()
	connCount := 0
	limitPool := 10
	for _, db := range databaseList {
		wg.Add(1)
		connCount++
		go r.fixCollationVersionForDB(pgC, pgVersion, db)
		if connCount >= limitPool {
			wg.Wait()
			connCount = 0
		}
	}
}

func (r *PatroniReconciler) fixCollationVersionForDB(pgClient *pgClient.PostgresClient, pgVersion int64, db string) {
	defer wg.Done()
	logger.Info(fmt.Sprintf("fix locale for database: %s", db))
	isPassed := true

	err := pgClient.ExecuteForDB(db, "REINDEX DATABASE;")
	if err != nil {
		isPassed = false
		logger.Warn(fmt.Sprintf("Cannot reindex database for db: %s", db), zap.Error(err))
	}

	err = pgClient.ExecuteForDB(db, "REINDEX SYSTEM;")
	if err != nil {
		isPassed = false
		logger.Warn(fmt.Sprintf("Cannot reindex system for db: %s", db), zap.Error(err))
	}

	if pgVersion >= 15 {
		err = pgClient.Execute(fmt.Sprintf("ALTER DATABASE \"%s\" REFRESH COLLATION VERSION", db))
		if err != nil {
			isPassed = false
			logger.Warn(fmt.Sprintf("Cannot alter locale version for db: %s", db), zap.Error(err))
		}
	}

	err = r.refreshDependCollationsVersion(pgClient, db)
	if err != nil {
		isPassed = false
		logger.Warn(fmt.Sprintf("Cannot alter dependent collations version for db: %s", db), zap.Error(err))
	}

	if isPassed {
		logger.Info(fmt.Sprintf("fix locale for database: %s - OK", db))
	} else {
		logger.Info(fmt.Sprintf("fix locale for database: %s - HAVE PROBLEMS", db))
	}
}

func (r *PatroniReconciler) refreshDependCollationsVersion(pgClient *pgClient.PostgresClient, db string) error {
	cForRefresh, err := r.getCollationsForRefresh(pgClient, db)
	if err != nil {
		return err
	}
	for _, collation := range cForRefresh {
		err := pgClient.ExecuteForDB(db, fmt.Sprintf("ALTER COLLATION \"%s\" REFRESH VERSION", collation))
		if err != nil {
			logger.Warn(fmt.Sprintf("Cannot alter collation %s version for db: %s", collation, db), zap.Error(err))
			return err
		}
	}
	return nil
}

// Get all collations with version mismatch for database
func (r *PatroniReconciler) getCollationsForRefresh(pgClient *pgClient.PostgresClient, db string) ([]string, error) {
	cForRefresh := make([]string, 0)
	rows, err := pgClient.QueryForDB(db, `SELECT distinct c.collname AS "Collation"
             FROM pg_depend d
             JOIN pg_collation c ON (refclassid = 'pg_collation'::regclass AND refobjid = c.oid)
			 WHERE c.collversion <> pg_collation_actual_version(c.oid) or c.collversion is null;`)
	if err != nil {
		logger.Error(fmt.Sprintf("error during fetching collations for database %s", db))
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var collation string
		err = rows.Scan(&collation)
		if err != nil {
			logger.Error(fmt.Sprintf("error during scan collation for database %s", db))
			return nil, err
		}
		cForRefresh = append(cForRefresh, collation)
	}
	return cForRefresh, nil
}

func (r *PatroniReconciler) processPgWalStorageExternal() error {

	logger.Info("Start pg_wal copying process")

	replicaPods, err := r.helper.ResourceManager.GetPodsByLabel(r.cluster.PatroniReplicasSelector)
	if err != nil {
		logger.Warn("Can not get replica pods to execute pg_wal copying command")
	}

	for _, replica := range replicaPods.Items {
		replicaIdx := replica.Spec.Containers[0].Name[len(replica.Spec.Containers[0].Name)-1:]
		if !r.checkSymlinkAlreadyExist(replica.Name, replicaIdx) {
			if err = r.execWalMovingForNode(replica.Name, replicaIdx); err != nil {
				logger.Error("Can not copy pg_wal files", zap.Error(err))
				return err
			}
		} else {
			logger.Info("pg_wal symlink already exist - skip pg_wal copying process ")
		}
	}

	masterPod, err := r.helper.ResourceManager.GetPodsByLabel(r.cluster.PatroniMasterSelectors)
	if err != nil {
		logger.Warn("Can not get master pod to execute pg_wal copying command")
		return err
	}

	masterIdx := masterPod.Items[0].Spec.Containers[0].Name[len(masterPod.Items[0].Spec.Containers[0].Name)-1:]
	if !r.checkSymlinkAlreadyExist(masterPod.Items[0].Name, masterIdx) {
		if err = r.execWalMovingForNode(masterPod.Items[0].Name, masterIdx); err != nil {
			logger.Error("Can not copy pg_wal files", zap.Error(err))
			return err
		}
	} else {
		logger.Info("pg_wal symlink already exist - skip pg_wal copying process ")
	}

	return nil
}

func (r PatroniReconciler) execWalMovingForNode(podName, podIdentity string) error {
	logger.Info(fmt.Sprintf("Exec pg_wal copying process for node: %s", podName))

	for _, cmd := range commands {
		cmd = fmt.Sprintf(cmd, podIdentity)
		_, errMsg, err := r.helper.ExecCmdOnPatroniPod(podName, namespace, cmd)
		if err != nil {
			logger.Error(fmt.Sprintf("Command: %s - failed. errMsg: %s", cmd, errMsg), zap.Error(err))
			return err
		}
	}
	return nil
}

func (r PatroniReconciler) checkSymlinkAlreadyExist(podName, podIdentity string) bool {

	cmd := fmt.Sprintf("if [ -L /var/lib/pgsql/data/postgresql_node%s/pg_wal ]; then echo true ; else echo false; fi", podIdentity)
	result, _, err := r.helper.ExecCmdOnPatroniPod(podName, namespace, cmd)
	if err != nil {
		logger.Error("Check if symlink already exist failed", zap.Error(err))
		return false
	}
	return strings.TrimSpace(result) == "true"
}

func (r *PatroniReconciler) preparePgbackRest(cr *v1.PatroniCore, patroniConfigMap *corev1.ConfigMap) error {
	// Prepare pgbackrest configuration CM
	pgBackRestCm := deployment.GetPgBackRestCM(cr.Spec.PgBackRest)
	if _, err := r.helper.ResourceManager.CreateOrUpdateConfigMap(pgBackRestCm); err != nil {
		logger.Error(fmt.Sprintf("Cannot create or update config map %s", "pgbackrest-config"), zap.Error(err))
		return err
	}

	// Add pgbackrest section to patroni CM
	pgbackrest := map[string]string{
		"command":   "pgbackrest --stanza=patroni --delta --log-level-file=detail restore",
		"keep_data": "true",
		"no_params": "true",
	}
	_, err := patroni.UpdatePgbackRestSettings(patroniConfigMap, pgbackrest, r.cluster.ConfigMapKey)
	if err != nil {
		logger.Error("Failed to update pgbackrest settings", zap.Error(err))
		return err
	}

	// Generate SSH keys for patroni ssh connection via pgbackrest
	if cr.Spec.PgBackRest.BackupFromStandby {
		_, err := r.helper.GetSecret(deployment.SSHKeysSecret)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Generation of RSA keys for pgbackrest started...")
				srvPrivateRSA, srvPublicRSA, _ := opUtil.GenerateSSHKeyPair(4096)
				secretData := map[string][]byte{"id_rsa": []byte(srvPrivateRSA), "id_rsa.pub": []byte(srvPublicRSA)}

				secret := &corev1.Secret{
					Type: corev1.SecretTypeOpaque,
					ObjectMeta: metav1.ObjectMeta{
						Name:      deployment.SSHKeysSecret,
						Namespace: opUtil.GetNameSpace(),
						Labels:    r.cluster.PatroniLabels,
					},
					Data: secretData,
				}
				err := r.helper.CreateSecretIfNotExists(secret)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}
	return nil
}
