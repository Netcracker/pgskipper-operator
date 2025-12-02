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

package controllers

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	"github.com/Netcracker/pgskipper-operator/pkg/queryexporter"

	"github.com/Netcracker/pgskipper-operator/pkg/deployerrors"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/postgresexporter"
	"github.com/Netcracker/pgskipper-operator/pkg/reconciler"
	"github.com/Netcracker/pgskipper-operator/pkg/scheduler"
	"github.com/Netcracker/pgskipper-operator/pkg/upgrade"
	utils "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/util/constants"
	"github.com/Netcracker/pgskipper-operator/pkg/vault"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"fmt"

	"github.com/Netcracker/pgskipper-operator-core/pkg/util"
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	"github.com/Netcracker/qubership-credential-manager/pkg/informer"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PostgresServiceReconciler reconciles a PostgresService object

var (
	operatorLockCmName = "postgres-operator-lock"
)

// ReconcilePostgresService reconciles a PostgresService object
type PostgresServiceReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	Client       client.Client
	Scheme       *runtime.Scheme
	helper       *helper.Helper
	upgrade      *upgrade.Upgrade
	vaultClient  *vault.Client
	cluster      qubershipv1.PatroniServices
	namespace    string
	errorCounter int
	reason       string
	message      string
	logger       zap.Logger
	resVersions  map[string]string
	crHash       string
}

type PatroniClusterSettings struct {
	ClusterName                string
	PatroniLabels              map[string]string
	PatroniCommonLabels        map[string]string
	PostgresServiceName        string
	PatroniMasterSelector      map[string]string
	PatroniReplicasSelector    map[string]string
	PatroniReplicasServiceName string
	PatroniUrl                 string
	PatroniTemplate            string
	ConfigMapKey               string
	PostgreSQLUserConf         string
	PostgreSQLPort             int
	PatroniDeploymentName      string
}

func NewPostgresServiceReconciler(client client.Client, scheme *runtime.Scheme) *PostgresServiceReconciler {
	namespace := util.GetNameSpace()
	logger := util.GetLogger()
	logger.Info(fmt.Sprintf("Scheme: %v", scheme.Name()))
	return &PostgresServiceReconciler{
		Client:      client,
		Scheme:      scheme,
		helper:      helper.GetHelper(),
		cluster:     qubershipv1.PatroniServices{},
		upgrade:     upgrade.Init(client),
		vaultClient: vault.NewClient(),
		namespace:   namespace,
		logger:      *logger,
		resVersions: map[string]string{},
	}

}

//+kubebuilder:rbac:groups=netcracker.com,resources=postgresservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=netcracker.com,resources=postgresservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=netcracker.com,resources=postgresservices/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the PostgresService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *PostgresServiceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	// Fetch the PostgresService instance
	cr := &qubershipv1.PatroniServices{}
	r.logger.Info("Want to get PatroniServices")
	if err := r.Client.Get(context.TODO(), request.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		r.logger.Error("Cannot fetch CR status", zap.Error(err))
		if err := r.updateStatus(Failed, "CannotFetchCrStatus",
			fmt.Sprintf("Cannot fetch CR status. Error: %s", err.Error())); err != nil {
			r.logger.Error("Cannot update CR status", zap.Error(err))
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	// Fill Name and UID for the Helper
	if err := r.helper.AddNameAndUID(cr.Name, cr.UID, cr.Kind); err != nil {
		return reconcile.Result{}, err
	}
	if err := r.helper.SetCustomResource(cr); err != nil {
		return reconcile.Result{}, err
	}

	newResVersion := cr.ResourceVersion
	newCrHash := util.HashJson(cr.Spec)
	if (r.resVersions[cr.Name] == newResVersion ||
		r.crHash == newCrHash) && (len(cr.Status.Conditions) != 0 &&
		(cr.Status.Conditions[0].Type != Failed || (cr.Status.Conditions[0].Type == Failed && r.errorCounter == 0))) {
		InfoMsg := "ResourceVersion didn't change, skipping reconcile loop"
		if cr.Spec.ExternalDataBase != nil {
			r.logger.Info(InfoMsg)
			return reconcile.Result{}, nil
		}
		areCredsChanged, err := manager.AreCredsChanged(credentials.PostgresSecretNames)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !areCredsChanged {
			r.logger.Info(InfoMsg)
			return reconcile.Result{}, nil
		}
	}

	if r.errorCounter == 0 {
		r.reason = "StartPatroniServicesClusterReconcile"
		r.message = "Start Patroni Services cluster reconcile cycle"
	}

	r.logger.Info(fmt.Sprintf("CR newResVersion is set to: %s Local ResVersion is set to: %s", newResVersion, r.resVersions))
	if err := r.updateStatus(InProgress, "StartPatroniServicesClusterReconcile",
		"Start Postgres Service cluster reconcile cycle"); err != nil {
		r.logger.Error("Cannot update CR status", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	r.crHash = newCrHash

	maxReconcileAttempts, errStrConv := strconv.Atoi(util.GetEnv("PG_RECONCILE_RETRIES", "1"))
	if errStrConv != nil {
		//Adding a logger here to show that reconcile retries were not set by end user
		r.logger.Info("Reconcile retries were not set, setting default value as 2")
		maxReconcileAttempts = int(2)
	}

	err := credentials.SetNewPasswordForPgClient(&r.helper.ResourceManager)
	if err != nil {
		return reconcile.Result{}, nil
	}

	// Update secret annotations
	if cr.Spec.ExternalDataBase == nil {
		err = credentials.UpdateHelmDeployments(&r.helper.ResourceManager)
		if err != nil {
			return reconcile.Result{}, nil
		}
	}

	if len(cr.RunTestsTime) > 0 {
		r.logger.Info("runTestsOnly : true")
		if err := r.createTestsPods(cr); err != nil {
			switch err.(type) {
			case *deployerrors.TestsError:
				{
					return r.handleReconcileError(err, "Error during tests run", maxReconcileAttempts, newCrHash)
				}
			default:
				{
					r.logger.Error("Can not synchronize Tests state to cluster", zap.Error(err))
					if err := r.updateStatus(Failed, "CanNotSynchronizeTestsStateToCluster",
						fmt.Sprintf("Can not synchronize Tests state to cluster. Error: %s", err.Error())); err != nil {
						r.logger.Error("Cannot update CR status", zap.Error(err))
						return reconcile.Result{RequeueAfter: time.Minute}, err
					}
					return reconcile.Result{}, err
				}
			}
		}
		r.logger.Info("Reconcile cycle succeeded, only tests were runs")
		r.resVersions[cr.Name] = newResVersion
		if err := r.updateStatus(Successful, "ReconcileCycleSucceeded",
			"Postgres service reconcile cycle succeeded. Only tests were runs"); err != nil {
			r.logger.Error("Cannot update CR status", zap.Error(err))
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}
		r.resVersions[cr.Name] = newResVersion
		return reconcile.Result{}, nil
	}

	scheduler.StopAndClear()

	// update Cr for Vault client
	r.vaultClient.UpdateCr(cr.Kind)

	// update Postgres Password
	vaultRolesExist := r.vaultClient.IsVaultRolesExist()
	if vaultRolesExist {
		if err := r.vaultClient.UpdatePgClientPass(); err != nil {
			return reconcile.Result{}, err
		}
	}

	if err := r.reconcilePostgresServiceCluster(cr); err != nil {
		switch err.(type) {
		case *deployerrors.TestsError:
			{
				return r.handleReconcileError(err, "Error during tests run", maxReconcileAttempts, newCrHash)
			}
		case error:
			{
				return r.handleReconcileError(err, "Error during reconcile cycle", maxReconcileAttempts, newCrHash)
			}

		default:
			{
				r.logger.Error("Can not synchronize desired K8S instances state to cluster", zap.Error(err))
				if err := r.updateStatus(Failed, "ReconcilePatroniServicesClusterFailed",
					fmt.Sprintf("Postgres service reconcile cycle failed. Error: %s", err.Error())); err != nil {
					r.logger.Error("Cannot update CR status", zap.Error(err))
					return reconcile.Result{RequeueAfter: time.Minute}, err
				}
			}
		}
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	reconcFunc := func() {
		cr, _ := helper.GetHelper().GetPostgresServiceCR()
		cr.Spec.InstallationTimestamp = strconv.FormatInt(time.Now().Unix(), 10)

		if err := helper.GetHelper().UpdatePostgresService(cr); err != nil {
			r.logger.Error("Error occurred during setting new creds", zap.Error(err))
			return
		}
		if err := helper.GetHelper().WaitUntilReconcileIsDone(); err != nil {
			r.logger.Error("Creds change was failed", zap.Error(err))
			return
		}
	}

	err = informer.Watch(credentials.PostgresSecretNames, reconcFunc)
	if err != nil {
		r.logger.Error("cannot start watcher", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	// Enable rotation controller
	if cr.Spec.VaultRegistration.DbEngine.Enabled {
		vault.EnableRotationController(r.vaultClient)
	}

	if err := r.helper.UpdatePGService(); err != nil {
		r.logger.Error("error during update of pg services", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	if err := r.AddExcludeLabelToCm(r.Client, operatorLockCmName); err != nil {
		r.logger.Error("Cannot update operator config map", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	r.errorCounter = 0
	r.logger.Info("Reconcile cycle succeeded")
	r.resVersions[cr.Name] = newResVersion
	if err := r.updateStatus(Successful, "ReconcileCycleSucceeded",
		"Postgres service reconcile cycle succeeded"); err != nil {
		r.logger.Error("Cannot update CR status", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	r.resVersions[cr.Name] = newResVersion
	return reconcile.Result{}, nil
}

func (r *PostgresServiceReconciler) handleReconcileError(err error, errMsg string, maxReconcileAttempts int, newCrHash string) (ctrl.Result, error) {
	r.errorCounter++
	if r.errorCounter < maxReconcileAttempts {
		r.logger.Error(errMsg, zap.Error(err))
		r.logger.Error(fmt.Sprintf("Error counter for reconcile run: %d, let's try to run the reconcile again", r.errorCounter))
		return r.failReconcile("PostgresClusterTestsFailed", err, true)
	}

	return r.stopReconcile(newCrHash, "No reconcile attempts left", err)
}

func (r *PostgresServiceReconciler) reconcilePostgresServiceCluster(cr *qubershipv1.PatroniServices) error {
	// reconcile ExternalDatabase
	if r.isExternalResourcesRequired(cr) {
		err := r.processExternalResources(cr)
		if err != nil {
			return err
		}
	}

	// reconcile Pooler
	if cr.Spec.Pooler.Install {
		if err := r.reconcilePooler(cr); err != nil {
			return err
		}
	}

	// reconcile Backup daemon
	if cr.Spec.BackupDaemon != nil {
		if err := r.reconcileBackupDaemon(cr); err != nil {
			return err
		}
	}

	// reconcile Metric Collector
	if cr.Spec.MetricCollector != nil {
		if err := r.reconcileMetricCollector(cr); err != nil {
			return err
		}
	}

	// reconcile SiteManager
	if cr.Spec.SiteManager != nil {
		if err := r.reconcileSiteManager(cr); err != nil {
			return err
		}
	}

	// reconcile Powa UI
	if cr.Spec.PowaUI.Install {
		if err := r.reconcilePowaUI(cr); err != nil {
			return err
		}
	}

	// configure postgres-exporter user
	if cr.Spec.PostgresExporter != nil && cr.Spec.PostgresExporter.Install {
		skipInstall := false
		if cr.Spec.ExternalDataBase == nil {
			patroniCore, err := r.helper.GetPatroniCoreCR()
			if err != nil {
				r.logger.Error("Can't get PatroniCore CR")
				panic(err)
			}
			if patroniCore.Spec.Patroni.StandbyCluster != nil {
				r.logger.Info("PatroniCore indicates standby cluster; skipping postgres-exporter setup (read-only)")
				skipInstall = true
			}
		}
		if !skipInstall {
			if err := postgresexporter.SetUpExporter(cr.Spec.PostgresExporter); err != nil {
				return err
			}
		}
	}

	// reconcile Query Exporter
	if cr.Spec.QueryExporter.Install {
		if err := r.reconcileQueryExporter(cr); err != nil {
			return err
		}
	}

	// reconcile Replication Controller
	if cr.Spec.ReplicationController.Install {
		if err := r.reconcileRC(cr); err != nil {
			return err
		}
	}

	// watch postgres exporter custom queries
	if cr.Spec.PostgresExporter != nil {
		customQueries := cr.Spec.PostgresExporter.CustomQueries
		if customQueries != nil && customQueries.Enabled {
			postgresexporter.RemoveActiveWatcher()
			exporter := postgresexporter.NewPostgresExporterWatcher(
				r.helper, customQueries.NamespacesList, customQueries.Labels)
			if err := exporter.WatchCustomQueries(); err != nil {
				return err
			}
		}
	}

	// watch query exporter custom queries
	if cr.Spec.QueryExporter.Install {
		customQueries := cr.Spec.QueryExporter.CustomQueries
		if customQueries != nil && customQueries.Enabled {
			queryexporter.RemoveActiveWatcher()
			exporter := queryexporter.NewQueryExporterWatcher(
				r.helper, customQueries.NamespacesList, customQueries.Labels)
			if err := exporter.WatchCustomQueries(); err != nil {
				return err
			}
		}
	}

	// reconcile PgBackRest Exporter
	if cr.Spec.PgBackRestExporter != nil && cr.Spec.PgBackRestExporter.Install {
		if err := r.reconcilePgBackRestExporter(cr); err != nil {
			return err
		}
	}

	// Reconcile IntegrationTests
	if cr.Spec.IntegrationTests != nil {
		r.logger.Info("Tests Spec is not empty, proceeding with reconcile")

		if cr.Spec.ExternalDataBase == nil {
			newCr, err := r.helper.GetPatroniCoreCR()
			if err != nil {
				r.logger.Error("Can't get PatroniCore CR")
				panic(err)
			}

			if newCr.Spec.Patroni.StandbyCluster != nil {
				r.logger.Info("StandbyCluster is configured, skipping integration tests reconciliation")
				return nil
			}
		}

		if err := r.createTestsPods(cr); err != nil {
			r.logger.Error("Can not synchronize Tests state to cluster", zap.Error(err))
			return err
		}

	} else {
		r.logger.Info("Tests Spec is empty, skipping reconciliation")
	}

	// And delete secrets, that uploaded to vault
	if err := r.vaultClient.DeleteSecret(vault.DeletionLabels); err != nil {
		r.logger.Error("Cannot delete secret", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) createTestsPods(cr *qubershipv1.PatroniServices) error {
	if cr.Spec.IntegrationTests != nil {
		integrationTestsPod := deployment.NewIntegrationTestsPod(cr, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
		// Vault Section
		r.vaultClient.ProcessPodVaultSection(integrationTestsPod, reconciler.Secrets)
		state, err := utils.GetPodPhase(integrationTestsPod)
		if err != nil {
			return err
		}
		if state != "Running" {
			if state != "NotFound" {
				if err := r.helper.ResourceManager.DeletePodWithWaiting(integrationTestsPod); err != nil {
					r.logger.Error("Error deleting pod with tests. Let's try to continue.", zap.Error(err))
				}
			}
			if cr.Spec.Policies != nil {
				r.logger.Info("Policies is not empty, setting them to Test Pod")
				integrationTestsPod.Spec.Tolerations = cr.Spec.Policies.Tolerations
			}
			if err := r.helper.ResourceManager.CreatePod(integrationTestsPod); err != nil {
				return err
			}
		}
		state, err = utils.WaitForCompletePod(integrationTestsPod)
		if err != nil {
			return &deployerrors.TestsError{Msg: "State of the test pods is unknown."}
		}
		switch state {
		case "Succeeded":
			{
				return nil
			}
		case "Failed":
			{
				return &deployerrors.TestsError{Msg: "Tests pod ended with an error."}
			}
		case "Running":
			{
				return &deployerrors.TestsError{Msg: "Tests pod Phase: Running. Tests take too long to run"}
			}
		case "Pending":
			{
				return &deployerrors.TestsError{Msg: "Tests pod Phase: Pending."}
			}
		default:
			{
				return &deployerrors.TestsError{Msg: "State of the test pods is unknown."}
			}
		}
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcileBackupDaemon(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Backup Daemon Spec is not empty, proceeding with reconcile")
	bRec := reconciler.NewBackupDaemonReconciler(cr, r.helper, r.vaultClient, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := bRec.Reconcile(); err != nil {
		r.logger.Error("Can not synchronize Backup Daemon state to cluster", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcileMetricCollector(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Metric Collector Spec is not empty, proceeding with reconcile")
	mcRec := reconciler.NewMetricCollectorReconciler(cr, r.helper, r.vaultClient, r.Scheme, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := mcRec.Reconcile(); err != nil {
		r.logger.Error("Can not synchronize Metric Collector state to cluster", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcileSiteManager(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Site Manager Spec is not empty, proceeding with reconcile")
	smRec := reconciler.NewSiteManagerReconciler(cr, r.helper, r.Scheme, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := smRec.Reconcile(); err != nil {
		r.logger.Error("Can not reconcile SiteManager", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcilePowaUI(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Powa UI reconciliation started")
	pRec := reconciler.NewPowaUIReconciler(cr, r.helper, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := pRec.Reconcile(); err != nil {
		r.logger.Error("Can not reconcile Powa UI", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcileQueryExporter(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Query Exporter reconciliation started")
	pRec := reconciler.NewQueryExporterReconciler(cr, r.helper, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := pRec.Reconcile(); err != nil {
		r.logger.Error("Can not reconcile Query Exporter", zap.Error(err))
		return err
	}
	return nil
}

func (r PostgresServiceReconciler) reconcilePooler(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Pooler reconciliation started")
	pRec := reconciler.NewPoolerReconciler(cr, r.helper, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := pRec.Reconcile(); err != nil {
		r.logger.Error("Can not reconcile Pooler", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcileRC(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Replication Controller reconciliation started")
	pRec := reconciler.NewRCReconciler(cr, r.helper, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := pRec.Reconcile(); err != nil {
		r.logger.Error("Can not reconcile Replication Controller", zap.Error(err))
		return err
	}
	return nil
}

func (r *PostgresServiceReconciler) reconcilePgBackRestExporter(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("PgBackRest Exporter reconciliation started")
	pRec := reconciler.NewPgBackRestExporterReconciler(cr, r.helper, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := pRec.Reconcile(); err != nil {
		r.logger.Error("Can not reconcile PgBackRest Exporter", zap.Error(err))
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&qubershipv1.PatroniServices{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func (r *PostgresServiceReconciler) AddExcludeLabelToCm(c client.Client, cmName string) error {
	foundCm := &corev1.ConfigMap{}
	err := c.Get(context.TODO(), types.NamespacedName{
		Name: cmName, Namespace: r.namespace,
	}, foundCm)
	if err != nil && errors.IsNotFound(err) {
		r.logger.Info(fmt.Sprintf("ConfigMap %s not found", cmName))
		return nil
	}
	if foundCm.ObjectMeta.Labels == nil {
		foundCm.ObjectMeta.Labels = make(map[string]string)
		foundCm.ObjectMeta.Labels["velero.io/exclude-from-backup"] = "true"
		err = c.Update(context.TODO(), foundCm)
		if err != nil {
			r.logger.Error(fmt.Sprintf("Failed to update configMap %s", foundCm.ObjectMeta.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (r *PostgresServiceReconciler) isExternalResourcesRequired(cr *qubershipv1.PatroniServices) bool {
	if cr.Spec.ExternalDataBase != nil {
		extType := strings.ToLower(cr.Spec.ExternalDataBase.Type)
		if extType == constants.Azure || extType == constants.RDS {
			return true
		}
	}
	return false
}

func (r *PostgresServiceReconciler) processExternalResources(cr *qubershipv1.PatroniServices) error {
	r.logger.Info("Resources creation for external database started...")
	// Create service for managed PostgreSQL
	conn := cr.Spec.ExternalDataBase.ConnectionName
	service := &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: "pg-patroni", Namespace: r.namespace},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: conn},
	}
	err := r.helper.CreateServiceIfNotExists(service)
	if err != nil {
		return err
	}

	// Duplicate hostname in configmap mounted in backup-daemon pod
	hostCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postgres-external",
			Namespace: util.GetNameSpace(),
		},
		Data: map[string]string{"connectionName": conn},
	}
	err = r.helper.CreateConfigMapIfNotExists(hostCM)
	if err != nil {
		return err
	}

	// Store additional restore information in separate config
	restoreData := make(map[string]string)
	for key, value := range cr.Spec.ExternalDataBase.RestoreConfig {
		for keyInner, valueInner := range value {
			restoreData[fmt.Sprintf("%s.%s", key, keyInner)] = valueInner
		}
	}
	restoreConfigCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-restore-config",
			Namespace: util.GetNameSpace(),
		},
		Data: restoreData,
	}
	err = r.helper.CreateConfigMapIfNotExists(restoreConfigCM)
	if err != nil {
		return err
	}

	return nil
}

func (r *PostgresServiceReconciler) stopReconcile(newCrHash string, reason string, err error) (ctrl.Result, error) {
	r.logger.Error(fmt.Sprintf("Failed reconcile attempts: %d, updating crHash, resVersions", r.errorCounter))
	r.crHash = newCrHash
	r.errorCounter = 0
	return r.failReconcile(reason, err, false)
}

func (r *PostgresServiceReconciler) failReconcile(reason string, err error, requeue bool) (ctrl.Result, error) {
	r.reason = reason
	r.message = "Postgres-operator service reconcile cycle failed"
	if err := r.updateStatus(Failed, reason,
		fmt.Sprintf("Postgres service reconcile cycle failed. Error: %s", err.Error())); err != nil {
		r.logger.Error("Cannot update CR status", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	requireAfter := time.Duration(0)
	if requeue {
		requireAfter = time.Minute
	}

	return reconcile.Result{RequeueAfter: requireAfter, Requeue: requeue}, err
}
