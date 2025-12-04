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

package util

import (
	"context"
	genericerrors "errors"
	"fmt"
	"os"
	"strconv"
	"time"

	v1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"go.uber.org/zap"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	backupDaemonLabels    = map[string]string{"app": "postgres-backup-daemon"}
	metricCollectorLabels = map[string]string{"app": "monitoring-collector"}
)

func WaitForStabilityDepl(dep appsv1.Deployment, revision int64, oldGeneration int64) error {
	uLog.Info(fmt.Sprintf("waitForStability of %s.", dep.Name))
	return wait.PollUntilContextTimeout(context.Background(), time.Second, 5*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		d := &appsv1.Deployment{}
		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, d); err != nil {
			uLog.Info("Deployment doesn't exist yet.")
			return false, nil
		}
		msg, status, error := deploymentStatus(dep, revision, oldGeneration)
		uLog.Info(fmt.Sprintf("Msg: %s, Status: %t, Error: %v", msg, status, error))
		if status {
			uLog.Info("Deployment has stabilized")
		}
		return status, nil
	})
}

func WaitForStabilityStatefulSet(stSet appsv1.StatefulSet, revision string, oldGeneration int64) error {
	uLog.Info(fmt.Sprintf("waitForStability of %s.", stSet.Name))
	return wait.PollUntilContextTimeout(context.Background(), time.Second, 5*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		d := &appsv1.StatefulSet{}
		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: stSet.Name, Namespace: stSet.Namespace}, d); err != nil {
			uLog.Info("Statefulset doesn't exist yet.")
			return false, nil
		}
		msg, status, error := statefulSetStatus(stSet, revision, oldGeneration)
		uLog.Info(fmt.Sprintf("Msg: %s, Status: %t, Error: %v", msg, status, error))
		if status {
			uLog.Info("Statefulset has stabilized")
		}
		return status, nil
	})
}

func GetRevision(depl appsv1.Deployment) (int64, error) {
	if deploymentRev := depl.GetAnnotations()["deployment.kubernetes.io/revision"]; deploymentRev != "" {
		uLog.Info(fmt.Sprintf("Deployment: %s | Revision: %s", depl.Name, deploymentRev))
		return strconv.ParseInt(deploymentRev, 10, 64)
	} else {
		return 0, genericerrors.New("deployment doesn't have a revision yet")
	}
}

func GetRevisionStatefulSet(statefulSet appsv1.StatefulSet) (string, error) {
	if statefulSetRev := statefulSet.Status.CurrentRevision; statefulSetRev != "" {
		uLog.Info(fmt.Sprintf("StatefulSet: %s | Revision: %s", statefulSet.Name, statefulSetRev))
		return statefulSetRev, nil
	} else {
		return "", genericerrors.New("StatefulSet doesn't have a revision yet")
	}
}

func deploymentStatus(dep appsv1.Deployment, revision int64, oldGeneration int64) (string, bool, error) {

	deployment := &appsv1.Deployment{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, deployment); err != nil {
		uLog.Info("Deployment doesn't exist yet.")
		return "", false, fmt.Errorf("failed get %T %v", dep.Name, err)
	}
	if revision > 0 {
		_, err := GetRevision(*deployment)
		if err != nil {
			return "", false, fmt.Errorf("cannot get the revision of deployment %q: %v", deployment.Name, err)
		}
	}
	if deployment.Generation <= oldGeneration {
		return fmt.Sprintf("Deployment %q was not updated yet. oldGeneration (%d) must be less then current one (%d) Waiting...", deployment.Name, oldGeneration, deployment.Generation), false, nil
	}
	if deployment.Generation <= deployment.Status.ObservedGeneration {
		cond := GetDeploymentCondition(deployment.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
			return "", false, fmt.Errorf("deployment %q exceeded its progress deadline", deployment.Name)
		}
		if deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas {
			return fmt.Sprintf("Waiting for deployment %q rollout to finish: %d out of %d new replicas have been updated...", deployment.Name, deployment.Status.UpdatedReplicas, *deployment.Spec.Replicas), false, nil
		}
		if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
			return fmt.Sprintf("Waiting for deployment %q rollout to finish: %d old replicas are pending termination...", deployment.Name, deployment.Status.Replicas-deployment.Status.UpdatedReplicas), false, nil
		}
		if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
			return fmt.Sprintf("Waiting for deployment %q rollout to finish: %d of %d updated replicas are available...", deployment.Name, deployment.Status.AvailableReplicas, deployment.Status.UpdatedReplicas), false, nil
		}
		return fmt.Sprintf("deployment %q successfully rolled out\n", deployment.Name), true, nil
	}
	return "Waiting for deployment spec update to be observed...", false, nil
}

func statefulSetStatus(dep appsv1.StatefulSet, revision string, oldGeneration int64) (string, bool, error) {

	statefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, statefulSet); err != nil {
		uLog.Info("StatefulSet doesn't exist yet.")
		return "", false, fmt.Errorf("failed get %T %v", dep.Name, err)
	}
	if revision != "" {
		_, err := GetRevisionStatefulSet(*statefulSet)
		if err != nil {
			return "", false, fmt.Errorf("cannot get the revision of StatefulSet %q: %v", statefulSet.Name, err)
		}
	}
	if statefulSet.Generation <= oldGeneration {
		return fmt.Sprintf("StatefulSet %q was not updated yet. oldGeneration (%d) must be less then current one (%d) Waiting...", statefulSet.Name, oldGeneration, statefulSet.Generation), false, nil
	}
	if statefulSet.Generation <= statefulSet.Status.ObservedGeneration {
		cond := GetStatefulSetCondition(statefulSet.Status, appsv1.StatefulSetConditionType("Progressing"))
		if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
			return "", false, fmt.Errorf("StatefulSet %q exceeded its progress deadline", statefulSet.Name)
		}
		if statefulSet.Spec.Replicas != nil && statefulSet.Status.UpdatedReplicas < *statefulSet.Spec.Replicas {
			return fmt.Sprintf("Waiting for StatefulSet %q rollout to finish: %d out of %d new replicas have been updated...", statefulSet.Name, statefulSet.Status.UpdatedReplicas, *statefulSet.Spec.Replicas), false, nil
		}
		if statefulSet.Status.Replicas > statefulSet.Status.UpdatedReplicas {
			return fmt.Sprintf("Waiting for StatefulSet %q rollout to finish: %d old replicas are pending termination...", statefulSet.Name, statefulSet.Status.Replicas-statefulSet.Status.UpdatedReplicas), false, nil
		}
		if statefulSet.Status.AvailableReplicas < statefulSet.Status.UpdatedReplicas {
			return fmt.Sprintf("Waiting for StatefulSet %q rollout to finish: %d of %d updated replicas are available...", statefulSet.Name, statefulSet.Status.AvailableReplicas, statefulSet.Status.UpdatedReplicas), false, nil
		}
		return fmt.Sprintf("StatefulSet %q successfully rolled out\n", statefulSet.Name), true, nil
	}
	return "Waiting for StatefulSet spec update to be observed...", false, nil
}

func GetDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func GetStatefulSetCondition(status appsv1.StatefulSetStatus, condType appsv1.StatefulSetConditionType) *appsv1.StatefulSetCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func checkDeletePod(pod *corev1.Pod) (done bool, err error) {
	GetLogger().Info(fmt.Sprintf("Waiting for the pod %v to be removed", pod.Name))
	foundPod := &corev1.Pod{}
	err = k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: pod.Name, Namespace: pod.Namespace,
	}, foundPod)
	if errors.IsNotFound(err) {
		return true, nil
	}
	return false, err
}

func WaitDeletePod(pod *corev1.Pod) error {
	return wait.PollUntilContextTimeout(context.Background(), 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		return checkDeletePod(pod)
	})
}

// func checkDeleteDeployment(deployment *appsv1.Deployment) (done bool, err error) {
// 	GetLogger().Info(fmt.Sprintf("Waiting for the pod %v to be removed", deployment.Name))
// 	foundPod := &corev1.Pod{}
// 	err = k8sClient.Get(context.TODO(), types.NamespacedName{
// 		Name: deployment.Name, Namespace: deployment.Namespace,
// 	}, foundPod)
// 	if errors.IsNotFound(err) {
// 		return true, nil
// 	}
// 	return false, err
// }

// func WaitDeleteDeployment(deployment *appsv1.Deployment) error {
// 	return wait.PollImmediate(2*time.Second, 5*time.Minute, func() (done bool, err error) {
// 		return checkDeleteDeployment(deployment)
// 	})
// }

func checkPodsByLabel(labelSelectors map[string]string, numberOfPods int) (done bool, err error) {
	uLog.Info(fmt.Sprintf("Will try to find %d Pod(s) with labels %q", numberOfPods, labelSelectors))
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labelSelectors),
		client.MatchingFields{"status.phase": "Running"},
	}
	if err = k8sClient.List(context.Background(), podList, listOpts...); err != nil {
		if errors.IsNotFound(err) {
			uLog.Info("Pods doesn't exist yet.")
			return false, nil
		}
		return false, err
	}
	if len(podList.Items) == numberOfPods {
		uLog.Info("Pods are exists, exiting.")
		return true, nil
	}
	return false, nil
}

func WaitForLeader(patroniMasterSelector map[string]string) error {
	return wait.PollUntilContextTimeout(context.Background(), time.Second, getWaitTimeout(), true, func(ctx context.Context) (done bool, err error) {
		return checkPodsByLabel(patroniMasterSelector, 1)
	})
}

func waitForReplicas(patroniReplicasSelector map[string]string, numberOfReplicas int) error {
	return wait.PollUntilContextTimeout(context.Background(), time.Second, getWaitTimeout(), true, func(ctx context.Context) (done bool, err error) {
		return checkPodsByLabel(patroniReplicasSelector, numberOfReplicas)
	})
}

func WaitForBackupDaemon() error {
	return wait.PollUntilContextTimeout(context.Background(), time.Second, getWaitTimeout(), true, func(ctx context.Context) (done bool, err error) {
		return checkPodsByLabel(backupDaemonLabels, 1)
	})
}

func WaitForMetricCollector() error {
	return wait.PollUntilContextTimeout(context.Background(), time.Second, getWaitTimeout(), true, func(ctx context.Context) (done bool, err error) {
		return checkPodsByLabel(metricCollectorLabels, 1)
	})
}

func WaitForPatroni(cr *v1.PatroniCore, patroniMasterSelector map[string]string, patroniReplicasSelector map[string]string) error {
	if cr.Spec.Patroni.Dcs.Type == "kubernetes" {
		if err := WaitForLeader(patroniMasterSelector); err != nil {
			uLog.Error("Failed to wait for master, exiting", zap.Error(err))
			return err
		}
		if err := waitForReplicas(patroniReplicasSelector, cr.Spec.Patroni.Replicas-1); err != nil {
			uLog.Error("Failed to wait for replicas, exiting", zap.Error(err))
			return err
		}
	}
	return nil
}

func GetPodPhase(pod *corev1.Pod) (string, error) {
	foundPod := &corev1.Pod{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: pod.Name, Namespace: pod.Namespace,
	}, foundPod)
	if err == nil {
		return string(foundPod.Status.Phase), nil
	} else if errors.IsNotFound(err) {
		return "NotFound", nil
	}
	return "Error", err
}

func GetPodExitCodes(pod *corev1.Pod) (map[string]int32, error) {
	foundPod := &corev1.Pod{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: pod.Name, Namespace: pod.Namespace,
	}, foundPod)
	exitCodes := make(map[string]int32)
	if err == nil {
		for _, container := range foundPod.Status.ContainerStatuses {
			exitCodes[container.Name] = container.State.Terminated.ExitCode
		}
		return exitCodes, nil
	} else if errors.IsNotFound(err) {
		return exitCodes, nil
	}
	return exitCodes, err
}

func WaitForCompletePod(pod *corev1.Pod) (string, error) {
	if pollErr := wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 240*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		state, err := GetPodPhase(pod)
		uLog.Info(fmt.Sprintf("Waiting for the pod phase Succeeded or Failed. Pod phase: %s", state))
		if state == "Succeeded" || state == "Failed" {
			return true, nil
		}
		return false, err
	}); pollErr != nil {
		return "TimeOut", pollErr
	}
	state, _ := GetPodPhase(pod)
	return state, nil
}

func WaitForRunningPod(pod *corev1.Pod) (string, error) {
	if pollErr := wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 240*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		state, err := GetPodPhase(pod)
		uLog.Info(fmt.Sprintf("Waiting for the pod phase Running. Pod phase: %s", state))
		if state == "Running" {
			return true, nil
		}
		return false, err
	}); pollErr != nil {
		return "TimeOut", pollErr
	}
	state, _ := GetPodPhase(pod)
	return state, nil
}

func getWaitTimeout() time.Duration {
	waitTimeoutStr, ok := os.LookupEnv("WAIT_TIMEOUT")
	if !ok {
		waitTimeoutStr = "10"
	}
	waitTimeout, _ := strconv.Atoi(waitTimeoutStr)
	return time.Duration(waitTimeout) * time.Minute
}
