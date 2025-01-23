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
	"time"

	"github.com/Netcracker/pgskipper-operator/pkg/credentials"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/Netcracker/pgskipper-operator/pkg/consul"
	"github.com/Netcracker/pgskipper-operator/pkg/deployerrors"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/Netcracker/pgskipper-operator/pkg/reconciler"
	"github.com/Netcracker/pgskipper-operator/pkg/scheduler"
	"github.com/Netcracker/pgskipper-operator/pkg/upgrade"
	utils "github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/vault"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"fmt"

	"github.com/Netcracker/pgskipper-operator-core/pkg/util"
	appsv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/qubership-credential-manager/pkg/informer"
	"github.com/Netcracker/qubership-credential-manager/pkg/manager"
)

// PatroniCoreReconciler reconciles a PatroniCore object

var (
	patroniCoreOperatorLockCmName = "patroni-core-operator-lock"
	//pgHost                          = util.GetEnv("POSTGRES_HOST", "pg-patroni")
)

// ReconcilePatroniCore reconciles a PatroniCore object
type PatroniCoreReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	Client       client.Client
	Scheme       *runtime.Scheme
	helper       *helper.PatroniHelper
	upgrade      *upgrade.Upgrade
	vaultClient  *vault.Client
	cluster      qubershipv1.PatroniCore
	appsCluster  appsv1.PatroniServices
	namespace    string
	errorCounter int
	reason       string
	message      string
	logger       zap.Logger
	resVersions  map[string]string
	crHash       string
}

func NewPatroniCoreReconciler(client client.Client, scheme *runtime.Scheme) *PatroniCoreReconciler {
	namespace := util.GetNameSpace()
	logger := util.GetLogger()
	return &PatroniCoreReconciler{
		Client:      client,
		Scheme:      scheme,
		helper:      helper.GetPatroniHelper(),
		cluster:     qubershipv1.PatroniCore{},
		appsCluster: appsv1.PatroniServices{},
		upgrade:     upgrade.Init(client),
		vaultClient: vault.NewClient(),
		namespace:   namespace,
		logger:      *logger,
		resVersions: map[string]string{},
	}

}

//+kubebuilder:rbac:groups=qubership.org,resources=patronicore,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=qubership.org,resources=patronicore/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=qubership.org,resources=patronicore/finalizers,verbs=update
//+kubebuilder:rbac:groups=qubership.org,resources=postgresservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=qubership.org,resources=postgresservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=qubership.org,resources=postgresservices/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the PatroniCore object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (pr *PatroniCoreReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	// Fetch the PatroniCore instance
	cr := &qubershipv1.PatroniCore{}
	if err := pr.Client.Get(context.TODO(), request.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		pr.logger.Error("Cannot fetch CR status", zap.Error(err))
		if err := pr.updateStatus(Failed, "CannotFetchCrStatus",
			fmt.Sprintf("Cannot fetch CR status. Error: %s", err.Error())); err != nil {
			pr.logger.Error("Cannot update CR status", zap.Error(err))
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	appsCr := &appsv1.PatroniServices{}
	pr.logger.Info("Want to get PatroniServices")
	if err := pr.Client.Get(context.TODO(), request.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		pr.logger.Error("Cannot fetch CR status", zap.Error(err))
		if err := pr.updateStatus(Failed, "CannotFetchCrStatus",
			fmt.Sprintf("Cannot fetch CR status. Error: %s", err.Error())); err != nil {
			pr.logger.Error("Cannot update CR status", zap.Error(err))
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	pr.logger.Info(fmt.Sprintf("PatroniCr: %v", appsCr))
	// Fill Name and UID for the Helper
	if err := pr.helper.AddNameAndUID(cr.Name, cr.UID, cr.Kind); err != nil {
		return reconcile.Result{}, err
	}
	if err := pr.helper.SetCustomResource(cr); err != nil {
		return reconcile.Result{}, err
	}

	newResVersion := cr.ResourceVersion
	newCrHash := util.HashJson(cr.Spec)
	if (pr.resVersions[cr.Name] == newResVersion ||
		pr.crHash == newCrHash) && len(cr.Status.Conditions) != 0 && cr.Status.Conditions[0].Type != Failed {
		areCredsChanged, err := manager.AreCredsChanged(credentials.PostgresSecretNames)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !areCredsChanged {
			pr.logger.Info("ResourceVersion didn't change, skipping reconcile loop")
			if err := pr.registerInConsul(cr); err != nil {
				return reconcile.Result{RequeueAfter: time.Minute}, err
			}
			return reconcile.Result{}, nil
		}
	}
	pr.logger.Info("Reconcile will be started...")
	time.Sleep(60 * time.Second)

	if pr.errorCounter == 0 {
		pr.reason = "StartPatroniCoreClusterReconcile"
		pr.message = "Start Patroni Core cluster reconcile cycle"
	}

	pr.logger.Info(fmt.Sprintf("CR newResVersion is set to: %s Local ResVersion is set to: %s", newResVersion, pr.resVersions))
	if err := pr.updateStatus(InProgress, "StartPostgresServiceClusterReconcile",
		"Start Postgres Service cluster reconcile cycle"); err != nil {
		pr.logger.Error("Cannot update CR status", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	pr.crHash = newCrHash

	maxReconcileAttempts, errStrConv := strconv.Atoi(util.GetEnv("PG_RECONCILE_RETRIES", "1"))
	if errStrConv != nil {
		//Adding a logger here to show that reconcile retries were not set by end user
		pr.logger.Info("Reconcile retries were not set, setting default value as 2")
		maxReconcileAttempts = int(2)
	}

	//Adding condition to skip reconcile retries when majorUpgrade is enabled
	if cr.Upgrade != nil && cr.Upgrade.Enabled {
		pr.logger.Info("Skipping reconciliation retries due to major upgrade enabled")
		maxReconcileAttempts = 1 // Set to 1 to skip retries
	}

	err := manager.ActualizeCreds(credentials.PostgresSecretName, credentials.ChangeCredsCore)
	if err != nil {
		pr.logger.Error("cannot update Postgres creds", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}
	err = manager.SetOwnerRefForSecretCopies(credentials.PostgresSecretNames, pr.helper.GetOwnerReferences())
	if err != nil {
		pr.logger.Error("cannot update secrets Owner References", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}

	if len(cr.RunTestsTime) > 0 {
		pr.logger.Info("runTestsOnly : true")
		if err := pr.createTestsPods(cr); err != nil {
			switch err.(type) {
			case *deployerrors.TestsError:
				{
					return pr.handleTestReconcileError(err, "Error during tests run", maxReconcileAttempts, newCrHash)
				}
			case error:
				{
					pr.errorCounter++
					maxReconcileAttempts, errStrConv := strconv.Atoi(util.GetEnv("PG_RECONCILE_RETRIES", "1"))
					if errStrConv != nil {
						maxReconcileAttempts = int(2)
					}

					if pr.errorCounter < maxReconcileAttempts {
						pr.logger.Error(fmt.Sprintf("Error counter: %d, let's try to run the reconcile again", pr.errorCounter))
						pr.reason = "ReconcilePostgresServiceClusterFailed"
						pr.message = "Postgres-operator service reconcile cycle failed"
						if err := pr.updateStatus(Failed, "ReconcilePostgresServiceClusterFailed",
							fmt.Sprintf("Postgres service reconcile cycle failed. Error: %s", err.Error())); err != nil {
							pr.logger.Error("Cannot update CR status", zap.Error(err))
							return reconcile.Result{RequeueAfter: time.Minute}, err
						}
						return reconcile.Result{RequeueAfter: time.Minute}, err
					}

					pr.logger.Error(fmt.Sprintf("Failed reconcile attempts: %d, updating crHash, resVersions", pr.errorCounter))
					pr.crHash = newCrHash
					pr.errorCounter = 0
					return reconcile.Result{RequeueAfter: time.Minute}, err
				}

			default:
				{
					pr.logger.Error("Can not synchronize Tests state to cluster", zap.Error(err))
					if err := pr.updateStatus(Failed, "CanNotSynchronizeTestsStateToCluster",
						fmt.Sprintf("Can not synchronize Tests state to cluster. Error: %s", err.Error())); err != nil {
						pr.logger.Error("Cannot update CR status", zap.Error(err))
						return reconcile.Result{RequeueAfter: time.Minute}, err
					}
					return reconcile.Result{}, err
				}
			}
		}
		pr.logger.Info("Reconcile cycle succeeded, only tests were runs")
		pr.resVersions[cr.Name] = newResVersion
		if err := pr.updateStatus(Successful, "ReconcileCycleSucceeded",
			"Postgres service reconcile cycle succeeded. Only tests were runs"); err != nil {
			pr.logger.Error("Cannot update CR status", zap.Error(err))
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}
		pr.resVersions[cr.Name] = newResVersion
		return reconcile.Result{}, nil
	}

	scheduler.StopAndClear()

	// update Cr for Vault client
	pr.vaultClient.UpdateCr(cr.Kind)
	if err := pr.reconcilePatroniCoreCluster(cr); err != nil {
		switch err.(type) {
		case *deployerrors.TestsError:
			{
				return pr.handleTestReconcileError(err, "Error during tests run", maxReconcileAttempts, newCrHash)
			}
		case error:
			{
				pr.errorCounter++

				if pr.errorCounter < maxReconcileAttempts {
					pr.logger.Error(fmt.Sprintf("Error counter: %d, let's try to run the reconcile again", pr.errorCounter))
					pr.reason = "ReconcilePatroniCoreClusterFailed"
					pr.message = "PatroniCore service reconcile cycle failed"
					if err := pr.updateStatus(Failed, "ReconcilePatroniCoreClusterFailed",
						fmt.Sprintf("Postgres service reconcile cycle failed. Error: %s", err.Error())); err != nil {
						pr.logger.Error("Cannot update CR status", zap.Error(err))
						return reconcile.Result{RequeueAfter: time.Minute}, err
					}
					return reconcile.Result{RequeueAfter: time.Minute}, err
				}

				pr.logger.Error(fmt.Sprintf("Failed reconcile attempts: %d, updating crHash, resVersions", pr.errorCounter))
				pr.crHash = newCrHash
				pr.errorCounter = 0
				return reconcile.Result{RequeueAfter: time.Minute}, err
			}

		default:
			{
				pr.logger.Error("Can not synchronize desired K8S instances state to cluster", zap.Error(err))
				if err := pr.updateStatus(Failed, "ReconcilePatroniClusterFailed",
					fmt.Sprintf("Patroni core reconcile cycle failed. Error: %s", err.Error())); err != nil {
					pr.logger.Error("Cannot update CR status", zap.Error(err))
					return reconcile.Result{RequeueAfter: time.Minute}, err
				}
			}
		}
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	if err := pr.helper.ResourceManager.UpdatePatroniConfigMaps(); err != nil {
		pr.logger.Error("error during update of patroni config maps", zap.Error(err))
		// will not return err because there is a slight chance, that
		// update could happen at the same time when patroni will update leader/config info
		//return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	//if err := pr.helper.RevokeGrantOnPublicSchema(pgHost); err != nil {
	//	pr.logger.Error("Error during revoking grants from public schema", zap.Error(err))
	//} else {
	//	pr.logger.Info("REVOKE statement executed successfully from template1")
	//}

	reconcFunc := func() {
		cr, _ := helper.GetPatroniHelper().GetPatroniCoreCR()
		cr.Spec.InstallationTimestamp = strconv.FormatInt(time.Now().Unix(), 10)

		if err := helper.GetPatroniHelper().UpdatePatroniCore(cr); err != nil {
			pr.logger.Error("Error occurred during setting new creds", zap.Error(err))
			return
		}
		if err := helper.GetPatroniHelper().WaitUntilReconcileIsDone(); err != nil {
			pr.logger.Error("Creds change was failed", zap.Error(err))
			return
		}
	}

	err = informer.Watch(credentials.PostgresSecretNames, reconcFunc)
	if err != nil {
		pr.logger.Error("cannot start watcher", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	if err := pr.AddExcludeLabelToCm(pr.Client, patroniCoreOperatorLockCmName); err != nil {
		pr.logger.Error("Cannot update operator config map", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	if cr.Spec.Patroni != nil && cr.Spec.Patroni.IgnoreSlots {
		scheduler.StartScheduler(cr)
	}
	pr.errorCounter = 0
	pr.logger.Info("Reconcile cycle succeeded")
	pr.resVersions[cr.Name] = newResVersion
	if err := pr.updateStatus(Successful, "ReconcileCycleSucceeded",
		"Postgres service reconcile cycle succeeded"); err != nil {
		pr.logger.Error("Cannot update CR status", zap.Error(err))
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	pr.resVersions[cr.Name] = newResVersion
	return reconcile.Result{}, nil
}

func (pr *PatroniCoreReconciler) handleTestReconcileError(err error, errMsg string, maxReconcileAttempts int, newCrHash string) (ctrl.Result, error) {
	pr.errorCounter++
	if pr.errorCounter < maxReconcileAttempts {
		pr.logger.Error(errMsg, zap.Error(err))
		pr.logger.Error(fmt.Sprintf("Error counter for tests run: %d, let's try to run the reconcile again", pr.errorCounter))
		pr.reason = "PatroniCoreTestsFailed"
		pr.message = "PatroniCore service reconcile cycle failed"
		if err := pr.updateStatus(Failed, "PatroniCoreTestsFailed", err.Error()); err != nil {
			pr.logger.Error("Cannot update CR status", zap.Error(err))
			return reconcile.Result{RequeueAfter: time.Minute}, err
		}
		return reconcile.Result{}, err
	}

	pr.logger.Error(fmt.Sprintf("Failed reconcile attempts: %d, updating crHash, resVersions", pr.errorCounter))
	pr.logger.Error("Reconciliation cycle failed due to test pod ended with error")
	pr.crHash = newCrHash
	pr.errorCounter = 0
	return reconcile.Result{RequeueAfter: time.Minute}, err
}

func (pr *PatroniCoreReconciler) reconcilePatroniCoreCluster(cr *qubershipv1.PatroniCore) error {
	consulRegistrationRequired := true
	// reconcile Patroni
	if cr.Spec.Patroni != nil {
		if err := pr.reconcilePatroni(cr); err != nil {
			return err
		}
		if patroni.IsStandbyClusterConfigurationExist(cr) {
			consulRegistrationRequired = false
		}
	} else {
		pr.logger.Info("Patroni Spec is empty. Skip Patroni reconcilation")
	}

	if consulRegistrationRequired {
		// if everything is OK proceed with registration in Consul
		if err := pr.registerInConsul(cr); err != nil {
			return err
		}
	}

	if cr.Spec.IntegrationTests != nil {
		if cr.Spec.Patroni.StandbyCluster == nil {
			pr.logger.Info("Tests Spec is not empty, proceeding with reconcile")
			if err := pr.createTestsPods(cr); err != nil {
				pr.logger.Error("Can not synchronize Tests state to cluster", zap.Error(err))
				return err
			}
		} else {
			pr.logger.Info("StandbyCluster is configured, skipping integration tests reconciliation")
		}
	}
	return nil
}

func (pr *PatroniCoreReconciler) reconcilePatroni(cr *qubershipv1.PatroniCore) error {
	pr.logger.Info("Patroni Spec is not empty, proceeding with reconcile")
	emptyCr := appsv1.PatroniServices{}
	appsCr, _ := pr.helper.GetPostgresServiceCR()
	if appsCr != &emptyCr {
		if patroni.IsPatroniHasDisabledStatus(appsCr) {
			pr.logger.Info("Patroni cluster disabled. Skip Patroni reconcilation")
			return nil
		}
	}
	pRec := reconciler.NewPatroniReconciler(cr, pr.helper, pr.vaultClient, pr.upgrade, pr.Scheme, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := pRec.Reconcile(); err != nil {
		pr.logger.Error("Can not synchronize desired Patroni state to cluster", zap.Error(err))
		return err
	}
	return nil
}
func (pr *PatroniCoreReconciler) registerInConsul(cr *qubershipv1.PatroniCore) error {
	cReg := consul.NewRegistrator(cr, pr.helper, pr.Scheme, pr.resVersions, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
	if err := cReg.RegisterInConsul(); err != nil {
		pr.logger.Error("Can not proceed with Consul Registration", zap.Error(err))
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (pr *PatroniCoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&qubershipv1.PatroniCore{}).
		Complete(pr)
}

func (pr *PatroniCoreReconciler) AddExcludeLabelToCm(c client.Client, cmName string) error {
	foundCm := &corev1.ConfigMap{}
	err := c.Get(context.TODO(), types.NamespacedName{
		Name: cmName, Namespace: pr.namespace,
	}, foundCm)
	if err != nil && errors.IsNotFound(err) {
		pr.logger.Info(fmt.Sprintf("ConfigMap %s not found", cmName))
		return nil
	}
	if foundCm.ObjectMeta.Labels == nil {
		foundCm.ObjectMeta.Labels = make(map[string]string)
		foundCm.ObjectMeta.Labels["velero.io/exclude-from-backup"] = "true"
		err = c.Update(context.TODO(), foundCm)
		if err != nil {
			pr.logger.Error(fmt.Sprintf("Failed to update configMap %s", foundCm.ObjectMeta.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (pr *PatroniCoreReconciler) createTestsPods(cr *qubershipv1.PatroniCore) error {
	if cr.Spec.IntegrationTests != nil {
		integrationTestsPod := deployment.NewCoreIntegrationTests(cr, utils.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName))
		// Vault Section
		pr.vaultClient.ProcessPodVaultSection(integrationTestsPod, reconciler.Secrets)
		state, err := utils.GetPodPhase(integrationTestsPod)
		if err != nil {
			return err
		}
		if state != "Running" {
			if state != "NotFound" {
				if err := pr.helper.ResourceManager.DeletePodWithWaiting(integrationTestsPod); err != nil {
					pr.logger.Error("Error deleting pod with tests. Let's try to continue.", zap.Error(err))
				}
			}
			if cr.Spec.Policies != nil {
				pr.logger.Info("Policies is not empty, setting them to Test Pod")
				integrationTestsPod.Spec.Tolerations = cr.Spec.Policies.Tolerations
			}
			if err := pr.helper.ResourceManager.CreatePod(integrationTestsPod); err != nil {
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
