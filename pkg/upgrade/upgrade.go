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

package upgrade

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Netcracker/pgskipper-operator-core/pkg/util"
	v1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	namespace     = util.GetNameSpace()
	logger        = util.GetLogger()
	MasterLabel   = map[string]string{"pgtype": "master"}
	UpgradeLabels = map[string]string{"app": "pg-major-upgrade"}
	powaUILabels  = map[string]string{"name": "powa"}
	//noConnectionDatabases = []string{"template0", "template1"}
)

func Init(client client.Client) *Upgrade {
	return &Upgrade{client: client, helper: helper.GetPatroniHelper()}
}

type Upgrade struct {
	client client.Client
	helper *helper.PatroniHelper
}

func (u *Upgrade) GetCleanerInitContainer(dockerImage string) []corev1.Container {
	initContainer := []corev1.Container{
		{
			Name:            "pg-cleaner",
			Image:           dockerImage,
			SecurityContext: opUtil.GetDefaultSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/var/lib/pgsql/data",
					Name:      "data",
				},
			},

			Command: []string{
				"bash",
				"-c",
				"rm -rf /var/lib/pgsql/data/* && ls -la /var/lib/pgsql/data/",
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Limits: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
			},
		},
	}
	return initContainer
}

func (u *Upgrade) getLeaderName() (string, error) {
	var err error
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(MasterLabel),
	}
	if err = u.client.List(context.Background(), podList, listOpts...); err == nil {
		pod := podList.Items[0]
		return pod.Spec.Containers[0].Name, nil
	}
	return "", err
}

func (u *Upgrade) ProcessSuccessfulUpgrade(clusterName string) error {
	logger.Info("Exit code was 0 ")
	if err := u.CleanInitializeKey(clusterName); err != nil {
		logger.Error(fmt.Sprintf("Can't clean Init key, %s", err), zap.Error(err))
		return err
	}
	return nil
}

func (u *Upgrade) UpdateUpgradeToDone() error {
	if newCr, err := u.helper.GetPatroniCoreCR(); err == nil {
		newCr.Upgrade.Enabled = false
		err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			if e := u.client.Status().Update(context.TODO(), newCr); e != nil {
				logger.Error(fmt.Sprintf("Can't Update operator status. Error: %s, Retrying.", e))
				return false, err
			} else {
				return true, nil
			}
		})
		return err
	} else {
		return err
	}
}

func (u *Upgrade) CleanInitializeKey(clusterName string) error {
	var cm *corev1.ConfigMap
	cmName := fmt.Sprintf("%s-config", clusterName)
	cm, err := u.helper.ResourceManager.GetConfigMap(cmName)
	if err != nil {
		logger.Error(fmt.Sprintf("Can't get configmap %s-config", cmName), zap.Error(err))
		return err
	}
	cm.ObjectMeta.Annotations["initialize"] = ""
	delete(cm.ObjectMeta.Annotations, "initialize")
	if err = u.client.Update(context.TODO(), cm); err != nil {
		logger.Error(fmt.Sprintf("Could not update %s config map", cmName), zap.Error(err))
		return err
	}
	return nil
}

func (u *Upgrade) GetInitDbArgs(patroniTemplate string, configMapKey string) (string, error) {
	var cm *corev1.ConfigMap
	cmName := patroniTemplate
	cm, err := u.helper.ResourceManager.GetConfigMap(cmName)
	if err != nil {
		logger.Error(fmt.Sprintf("Can't get configmap %s", cmName), zap.Error(err))
		return "", err
	}
	var config map[string]interface{}
	err = yaml.Unmarshal([]byte(cm.Data[configMapKey]), &config)
	if err != nil {
		logger.Error(fmt.Sprintf("Could not unmarshal %s config map", cmName), zap.Error(err))
		return "", err
	}
	params := config["bootstrap"].(map[interface{}]interface{})["initdb"].([]interface{})
	logger.Info(fmt.Sprintf("Initdb params: %s", params))
	result := ""
	s := make([]string, len(params))
	for i, value := range params {
		s[i] = fmt.Sprint(value)

		if reflect.ValueOf(value).Kind() == reflect.Map {
			for k, v := range value.(map[interface{}]interface{}) {
				result = fmt.Sprintf("%s --%s=%s", result, k, v)
			}
		} else {
			s[i] = fmt.Sprint(value)
			result = result + " --" + s[i]
		}
	}
	return result, nil
}

func (u *Upgrade) CheckForAbsTimeUsage(pgHost string) error {
	pgC := pgClient.GetPostgresClient(pgHost)
	databaseList := helper.GetAllDatabases(pgC)
	absTimeDatabases := make([]string, 0)

	checkForAbsTimeQuery := "SELECT 1 FROM information_schema.columns WHERE data_type = 'abstime' AND table_schema <> 'pg_catalog';"

	for _, db := range databaseList {
		rows, err := pgC.QueryForDB(db, checkForAbsTimeQuery)
		if err != nil {
			logger.Warn(fmt.Sprintf("Cannot check incompatible data type 'abstime' on database %s", db), zap.Error(err))
		}

		if rows.Next() {
			absTimeDatabases = append(absTimeDatabases, db)
		}
	}

	if len(absTimeDatabases) > 0 {
		return fmt.Errorf("Upgrade Error. Next databases have incompatible 'abstime' data type: %v", absTimeDatabases)
	}

	return nil
}

func (u *Upgrade) decideInitDbArgs(cr *v1.PatroniCore, cluster *v1.PatroniClusterSettings) (args string, err error) {
	if cr.Upgrade.InitDbParams != "" {
		args = cr.Upgrade.InitDbParams
		logger.Info(fmt.Sprintf("InitDb Params found in CR: %s", args))
	} else {
		logger.Info("Getting InitDb Params from Patroni CM")
		args, err = u.GetInitDbArgs(cluster.PatroniTemplate, cluster.ConfigMapKey)
		if err != nil {
			logger.Error("Can't get InitDb Params", zap.Error(err))
			return "", err
		}
	}
	return
}

func (u *Upgrade) applyCleanerInitContainer(leaderName string, patroniSpec *v1.Patroni, cluster *v1.PatroniClusterSettings) error {
	var deploymentList []*appsv1.StatefulSet
	var err error
	cleanerInitContainer := u.GetCleanerInitContainer(patroniSpec.DockerImage)
	patroniDeploymentName := fmt.Sprintf("pg-%s-node", cluster.ClusterName)
	if deploymentList, err = u.helper.ResourceManager.GetStatefulsetByNameRegExp(patroniDeploymentName); err != nil {
		logger.Error("Can't get Patroni Deployments", zap.Error(err))
		return err
	}
	for _, dep := range deploymentList {
		if dep.Name == leaderName {
			continue
		}

		replicas := int32(1)
		dep.Spec.Template.Spec.InitContainers = append(cleanerInitContainer, dep.Spec.Template.Spec.InitContainers...)
		dep.Spec.Template.Spec.Containers[0].Image = patroniSpec.DockerImage
		dep.Spec.Replicas = &replicas

		if err := u.helper.ResourceManager.CreateOrUpdateStatefulset(dep, true); err != nil {
			logger.Error("Can't update Patroni deployment", zap.Error(err))
			return err
		}
	}
	return nil
}

func (u *Upgrade) pgUpgradeCheckFailed(upgradePod *corev1.Pod, cluster *v1.PatroniClusterSettings, cr *v1.PatroniCore) (bool, error) {
	exitCodes, err := opUtil.GetPodExitCodes(upgradePod)
	if err != nil {
		return false, err
	}
	code, ok := exitCodes["pg-upgrade"]
	if !ok {
		return false, nil
	}
	if code == 13 {
		logger.Error("Can't upgrade Patroni cluster. Rollback.")
		if err = u.helper.UpdatePatroniReplicas(1, cluster.ClusterName); err != nil {
			return false, err
		}
		if err = u.ScalePowaDeployment(1); err != nil {
			return false, err
		}
		if err = opUtil.WaitForPatroni(cr, cluster.PatroniMasterSelectors, cluster.PatroniReplicasSelector); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (u *Upgrade) CheckForPreparedTransactions(pgHost string) error {
	pgC := pgClient.GetPostgresClient(pgHost)
	checkForPreparedTxQuery := "SELECT DISTINCT database FROM pg_prepared_xacts;"

	rows, err := pgC.Query(checkForPreparedTxQuery)
	if err != nil {
		logger.Error("Failed to check for prepared transactions", zap.Error(err))
		return fmt.Errorf("failed to check for prepared transactions: %w", err)
	}
	defer rows.Close()

	preparedTxDatabases := make([]string, 0)

	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			logger.Warn("Error scanning prepared transactions query result", zap.Error(err))
			continue
		}
		preparedTxDatabases = append(preparedTxDatabases, dbName)
	}

	if len(preparedTxDatabases) > 0 {
		return fmt.Errorf("Upgrade Error. Prepared transactions exist in the following databases: %v. Please rollback or commit them before proceeding", preparedTxDatabases)
	}

	logger.Info("No prepared transactions found. Safe to proceed with upgrade.")
	return nil
}

func (u *Upgrade) ProceedUpgrade(cr *v1.PatroniCore, cluster *v1.PatroniClusterSettings) error {

	masterPod, err := u.helper.ResourceManager.GetPodsByLabel(cluster.PatroniMasterSelectors)
	if err != nil || len(masterPod.Items) == 0 {
		logger.Error("Can't get Patroni Leader for pg_dumpall execution, failing major upgrade", zap.Error(err))
		return err
	}
	masterPodName := masterPod.Items[0].Name
	namespace := util.GetNameSpace()

	command := "grep \"shared_preload_libraries\" /var/lib/pgsql/data/postgresql_${POD_IDENTITY}/postgresql.conf || echo \"not found\""
	result, _, err := u.helper.ExecCmdOnPatroniPod(masterPodName, namespace, command)
	if err != nil {
		logger.Error("Can't execute grep command, failing major upgrade", zap.Error(err))
		return err
	}
	if !strings.Contains(result, "shared_preload_libraries") {
		errMsg := "shared_preload_libraries is not found in PostgreSQL config, please check PostgreSQL params, failing major upgrade"
		logger.Error(errMsg, zap.Error(err))
		return errors.New(errMsg)
	}

	command = "pg_dumpall -v -U postgres -w --file=/tmp/test_db_dumpall.custom --schema-only"
	_, _, err = u.helper.ExecCmdOnPatroniPod(masterPodName, namespace, command)
	if err != nil {
		logger.Error("Can't execute pg_dumpall command, failing major upgrade", zap.Error(err))
		return err
	}

	removeCommand := "rm -rf /tmp/test_db_dumpall.custom"
	_, _, err = u.helper.ExecCmdOnPatroniPod(masterPodName, namespace, removeCommand)
	if err != nil {
		logger.Error("Error removing dump file", zap.Error(err))
		return err
	} else {
		logger.Info("Dump file removed successfully")
	}

	// Check for prepared transactions before upgrade
	if err := u.CheckForPreparedTransactions(cluster.PgHost); err != nil {
		return err
	}

	// check before upgrade
	if err := u.CheckForAbsTimeUsage(cluster.PgHost); err != nil {
		return err
	}

	patroniSpec := cr.Spec.Patroni
	config, _ := u.helper.GetPatroniClusterConfig(cluster.PatroniUrl)
	if !u.helper.IsPatroniClusterHealthy(config) {
		return errors.New("patroni cluster is not healthy enough for upgrade procedure. Exiting")
	}

	//Scaling down powa deployment before upgrade
	if err := u.ScalePowaDeployment(0); err != nil {
		return err
	}

	//deleting powa pod
	if err := u.helper.ResourceManager.DeletePodsByLabel(powaUILabels); err != nil {
		return err
	}

	leaderName, err := u.getLeaderName()

	if err != nil {
		logger.Error("Can't get Patroni Leader, failing major upgrade", zap.Error(err))
		return err
	}

	// wait until all patroni pods will power off
	patroniPods, err := u.helper.ResourceManager.GetNamespacePodListBySelectors(cluster.PatroniCommonLabels)
	if err != nil {
		return err
	}
	if err = u.helper.UpdatePatroniReplicas(0, cluster.ClusterName); err != nil {
		return err
	}
	for _, patroniPod := range patroniPods.Items {
		if err = opUtil.WaitDeletePod(&patroniPod); err != nil {
			logger.Error("waiting for Patroni deployment delete failed", zap.Error(err))
			return err
		}
	}

	initDbArgs, err := u.decideInitDbArgs(cr, cluster)
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("Leader name is %s", leaderName))
	deploymentIdx, _ := strconv.Atoi(leaderName[len(leaderName)-1:])
	patroniDeployment := deployment.NewPatroniStatefulset(cr, deploymentIdx, cluster.ClusterName,
		cluster.PatroniTemplate, cluster.PostgreSQLUserConf, cluster.PatroniLabels)
	upgradePod := u.getUpgradePod(patroniSpec, leaderName, initDbArgs, cr.Upgrade.DockerUpgradeImage)

	// copy nodeSelector, Volumes, SecurityContext from Deployment
	upgradePod.Spec.NodeSelector = patroniDeployment.Spec.Template.Spec.NodeSelector
	upgradePod.Spec.Volumes = patroniDeployment.Spec.Template.Spec.Volumes
	upgradePod.Spec.Containers[0].VolumeMounts = patroniDeployment.Spec.Template.Spec.Containers[0].VolumeMounts
	upgradePod.Spec.SecurityContext = patroniDeployment.Spec.Template.Spec.SecurityContext

	// create pod and wait till completed
	if err := u.helper.ResourceManager.CreatePod(upgradePod); err != nil {
		return err
	}

	if err = u.waitTillPodIsReady(upgradePod); err != nil {
		failed, err := u.pgUpgradeCheckFailed(upgradePod, cluster, cr)
		if failed {
			return errors.New("postgresql major upgrade failed, please, check logs of upgrade pod")
		}
		return err
	}

	// clean up init key
	if err := u.CleanInitializeKey(cluster.ClusterName); err != nil {
		return err
	}

	// upgrade completed, apply patroni deployment
	if err := u.helper.ResourceManager.CreateOrUpdateStatefulset(patroniDeployment, true); err != nil {
		logger.Error("Can't update Patroni deployment", zap.Error(err))
		return err
	}

	if err := opUtil.WaitForLeader(cluster.PatroniMasterSelectors); err != nil {
		return err
	}

	if err := u.applyCleanerInitContainer(leaderName, patroniSpec, cluster); err != nil {
		return err
	}

	if err := opUtil.WaitForPatroni(cr, cluster.PatroniMasterSelectors, cluster.PatroniReplicasSelector); err != nil {
		return err
	}

	// Store pg version after upgrade
	updatedMasterPod, err := u.helper.ResourceManager.GetPodsByLabel(cluster.PatroniMasterSelectors)
	if err != nil {
		logger.Info("Can not get master pod")
	}
	pgVersion := u.helper.GetPGVersionFromPod(updatedMasterPod.Items[0].Name)

	u.helper.StoreDataToCM("pg-version", pgVersion)

	//Upgrade complete, scaling up powa-ui deployment
	if err := u.ScalePowaDeployment(1); err != nil {
		return err
	}

	if err := u.UpdateUpgradeToDone(); err != nil {
		logger.Error("Can't update CR", zap.Error(err))
		return err
	}

	return nil
}

func (u *Upgrade) getUpgradePod(patroniSpec *v1.Patroni, leaderName string, initDbArgs string, upgradeImage string) *corev1.Pod {
	patroniIdx := leaderName[len(leaderName)-1:]
	upgradePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg-major-upgrade-" + strconv.Itoa(int(time.Now().Unix())),
			Labels:    util.Merge(UpgradeLabels, patroniSpec.PodLabels),
			Namespace: util.GetNameSpace(),
		},
		Spec: corev1.PodSpec{
			InitContainers: u.getPgVersionContainer(patroniSpec.DockerImage),
			RestartPolicy:  corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "pg-upgrade",
					Image:           upgradeImage,
					SecurityContext: opUtil.GetDefaultSecurityContext(),
					ImagePullPolicy: "IfNotPresent",
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/var/lib/pgsql/data",
							Name:      "data",
						},
					},
					Resources: *patroniSpec.Resources,
					Env: []corev1.EnvVar{
						{
							Name:  "DATA_DIR",
							Value: fmt.Sprintf("postgresql_node%s", patroniIdx),
						},
						{
							Name:  "TYPE",
							Value: "master",
						},
						{
							Name:  "OPERATOR",
							Value: "true",
						},
						{
							Name:  "INITDB_PARAMS",
							Value: initDbArgs,
						},
						{
							Name: "PGREPLPASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "replicator-credentials"},
									Key:                  "password",
								},
							},
						},
						{
							Name: "PGPASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "postgres-credentials"},
									Key:                  "password",
								},
							},
						},
						{
							Name:  "MIGRATION_PATH",
							Value: "/var/lib/pgsql/data",
						},
						{
							Name:  "PV_SIZE",
							Value: patroniSpec.Storage.Size,
						},
					},
				},
			},
		},
	}
	if patroniSpec.PgWalStorage != nil {
		logger.Info("Settings pg_wal volume mount for upgrade pod")
		upgradePod.Spec.InitContainers[0].VolumeMounts = append(upgradePod.Spec.InitContainers[0].VolumeMounts, deployment.GetPgWalVolumeMount())
		upgradePod.Spec.Containers[0].VolumeMounts = append(upgradePod.Spec.Containers[0].VolumeMounts, deployment.GetPgWalVolumeMount())
	}
	return upgradePod
}

func (u *Upgrade) getPgVersionContainer(targetDockerImage string) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "get-postgresql-target-version",
			Image:           targetDockerImage,
			SecurityContext: opUtil.GetDefaultSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/var/lib/pgsql/data",
					Name:      "data",
				},
			},
			Command: []string{
				"sh",
				"-c",
				"pg_config --version | grep -o \"[0-9]*\" | head -n 1 > /var/lib/pgsql/data/target_version",
				"echo target version is set to:",
				"cat /var/lib/pgsql/data/target_version",
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Limits: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
			},
		},
	}
}

func (u *Upgrade) ScalePowaDeployment(replicas int32) error {
	powaDeploymentName := "powa-ui"
	deploymentsToUpdate, err := u.helper.ResourceManager.GetDeploymentsByNameRegExp(powaDeploymentName)
	if err != nil {
		return err
	}

	if len(deploymentsToUpdate) > 0 {
		return nil
	}

	for _, dep := range deploymentsToUpdate {
		logger.Info(fmt.Sprintf("Scale %v to %v", dep.Name, replicas))
		dep.Spec.Replicas = &replicas
		if err := u.helper.ResourceManager.CreateOrUpdateDeployment(dep, true); err != nil {
			logger.Error("Can't update powa-ui deployment", zap.Error(err))
			return err
		}
	}
	logger.Info(fmt.Sprintf("powa-ui deployment have scaled to %v successfully", replicas))
	return nil
}

func (u *Upgrade) waitTillPodIsReady(pod *corev1.Pod) error {
	state, err := opUtil.WaitForCompletePod(pod)
	if state != "Succeeded" {
		return fmt.Errorf("pod State is not equals to Succeeded: %s", state)
	}
	return err
}

func (u *Upgrade) waitTillPodIsRunning(pod *corev1.Pod) error {
	state, err := opUtil.WaitForRunningPod(pod)
	if state != "Running" {
		return fmt.Errorf("pod State is not equals to Running: %s", state)
	}
	return err
}

func (u *Upgrade) RunUpgradePatroniPod(cr *v1.PatroniCore, cluster *v1.PatroniClusterSettings) (*corev1.Pod, error) {

	upgradeCheckPod := u.getUpgradeCheckPod(cr)
	if cr.Spec.PrivateRegistry.Enabled {
		for _, name := range cr.Spec.PrivateRegistry.Names {
			upgradeCheckPod.Spec.ImagePullSecrets = append(upgradeCheckPod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}
	// create pod and wait till completed
	if err := u.helper.ResourceManager.CreatePod(upgradeCheckPod); err != nil {
		return nil, err
	}
	if err := u.waitTillPodIsRunning(upgradeCheckPod); err != nil {
		return nil, err
	}
	return upgradeCheckPod, nil
}

func (u *Upgrade) getUpgradeCheckPod(cr *v1.PatroniCore) *corev1.Pod {
	patroniSpec := cr.Spec.Patroni
	upgradeCheckPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg-major-upgrade-check-" + strconv.Itoa(int(time.Now().Unix())),
			Namespace: util.GetNameSpace(),
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "pg-upgrade-check",
					Image:           patroniSpec.DockerImage,
					SecurityContext: opUtil.GetDefaultSecurityContext(),
					ImagePullPolicy: "IfNotPresent",
					Command:         []string{"sleep", "infinity"},
					Resources: corev1.ResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
						Limits: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("50Mi"),
						},
					},
				},
			},
		},
	}

	return upgradeCheckPod
}

func (u *Upgrade) CheckUpgrade(cr *v1.PatroniCore, cluster *v1.PatroniClusterSettings) bool {

	masterPod, err := u.helper.ResourceManager.GetPodsByLabel(cluster.PatroniMasterSelectors)
	if err != nil || len(masterPod.Items) == 0 {
		logger.Error("Can't check is major upgrade required. Master pod is not available", zap.Error(err))
		return false
	}
	currentVersion := u.helper.GetPGVersion(masterPod.Items[0].Name)
	u.helper.StoreDataToCM("pg-version", currentVersion)
	upgradeCheckPod, _ := u.RunUpgradePatroniPod(cr, cluster)
	targetVersion := u.helper.GetPGVersionFromPod(upgradeCheckPod.Name)
	if err := u.helper.ResourceManager.DeletePod(upgradeCheckPod); err != nil {
		logger.Warn("Can't delete pg-upgrade-check-pod", zap.Error(err))
	}

	return currentVersion != targetVersion
}
