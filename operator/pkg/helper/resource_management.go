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

package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	k8sauth "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kubeSysAnnotations = "kubernetes.io"

var pythonServices = []string{"postgres-backup-daemon"}

type ResourceManager struct {
	kubeClient    client.Client
	kubeClientSet *kubernetes.Clientset
	name          string
	uid           types.UID
	kind          string
}

func (rm *ResourceManager) GetPostgresServiceCR() (*qubershipv1.PatroniServices, error) {
	cr := &qubershipv1.PatroniServices{}
	if err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: util.GetEnv("RESOURCE_NAME", "patroni-services"), Namespace: namespace,
	}, cr); err != nil {
		if errors.IsNotFound(err) {
			return cr, nil
		}
		logger.Error("Cannot fetch PatroniServices CR", zap.Error(err))
		return cr, err
	}
	return cr, nil
}

func (rm *ResourceManager) GetPatroniCoreCR() (*patroniv1.PatroniCore, error) {
	cr := &patroniv1.PatroniCore{}
	if err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: "patroni-core", Namespace: namespace,
	}, cr); err != nil {
		if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
			logger.Info("Custom Resource patroni-core not found")
			return cr, nil
		}
		logger.Error("Cannot fetch PatroniCore CR", zap.Error(err))
		return nil, err
	}
	return cr, nil
}

func (rm *ResourceManager) GetConfigMap(name string) (*corev1.ConfigMap, error) {
	foundCm := &corev1.ConfigMap{}
	logger.Info(fmt.Sprintf("Start to check if %s cm exists", name))
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: name, Namespace: namespace,
	}, foundCm)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Config map %s is not found", name))
		return nil, err
	} else if err != nil {
		logger.Error(fmt.Sprintf("Failed to get configMap %s", name), zap.Error(err))
		return nil, err
	}
	return foundCm, nil
}

func (rm *ResourceManager) GetService(name string, namespace string) *corev1.Service {
	foundService := &corev1.Service{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, foundService)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Can not read %s k8s service", name))
		return nil

	} else if err != nil {
		return nil
	} else {
		return foundService
	}
}

func (rm *ResourceManager) GetNamespacePodList() (*corev1.PodList, error) {
	logger.Debug("Trying to get all pods in namespace")
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := rm.kubeClient.List(context.Background(), podList, listOpts...); err != nil {
		logger.Debug("Pods doesn't exist.")
		return nil, err
	}
	return podList, nil
}

func (rm *ResourceManager) GetNamespacePodListBySelectors(selectors map[string]string) (*corev1.PodList, error) {
	logger.Debug("Trying to get all pods in namespace")
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(selectors),
	}
	if err := rm.kubeClient.List(context.Background(), podList, listOpts...); err != nil {
		logger.Debug("Pods doesn't exist.")
		return nil, err
	}
	return podList, nil
}

func (rm *ResourceManager) GetDeploymentsByNameRegExp(pattern string) ([]*appsv1.Deployment, error) {
	var resultDeployments []*appsv1.Deployment
	deploymentList := &appsv1.DeploymentList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := rm.kubeClient.List(context.Background(), deploymentList, listOpts...); err == nil {
		for idx := 0; idx < len(deploymentList.Items); idx++ {
			deployment := &deploymentList.Items[idx]
			if r, e := regexp.MatchString(pattern, deployment.Name); e == nil && r {
				resultDeployments = append(resultDeployments, deployment)
			}
		}
	} else {
		logger.Error(fmt.Sprintf("Can't find deployments by %s", pattern), zap.Error(err))
		return nil, err
	}

	return resultDeployments, nil
}

func (rm *ResourceManager) GetStatefulsetByNameRegExp(pattern string) ([]*appsv1.StatefulSet, error) {
	var resultStatefulSets []*appsv1.StatefulSet
	statefulSetList := &appsv1.StatefulSetList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := rm.kubeClient.List(context.Background(), statefulSetList, listOpts...); err == nil {
		for idx := 0; idx < len(statefulSetList.Items); idx++ {
			deployment := &statefulSetList.Items[idx]
			if r, e := regexp.MatchString(pattern, deployment.Name); e == nil && r {
				resultStatefulSets = append(resultStatefulSets, deployment)
			}
		}
	} else {
		logger.Error(fmt.Sprintf("Can't find statefulSets by %s", pattern), zap.Error(err))
		return nil, err
	}

	return resultStatefulSets, nil
}

func (rm *ResourceManager) GetStatefulsetCountByNameRegExp(pattern string) (int, error) {
	var count int
	statefulSetList := &appsv1.StatefulSetList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := rm.kubeClient.List(context.Background(), statefulSetList, listOpts...); err == nil {
		for idx := 0; idx < len(statefulSetList.Items); idx++ {
			deployment := &statefulSetList.Items[idx]
			if r, e := regexp.MatchString(pattern, deployment.Name); e == nil && r {
				count++
			}
		}
	} else {
		logger.Error(fmt.Sprintf("Can't find statefulSets by %s", pattern), zap.Error(err))
		return 0, err
	}

	return count, nil
}

func (rm *ResourceManager) GetPodsByLabel(selectors map[string]string) (corev1.PodList, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(selectors),
	}
	if err := rm.kubeClient.List(context.Background(), podList, listOpts...); err != nil {
		logger.Error("Can not get pods by label", zap.Error(err))
		return *podList, err
	}
	return *podList, nil
}

func (rm *ResourceManager) GetPodByName(name string) (corev1.Pod, error) {
	foundPod := &corev1.Pod{}
	if err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, foundPod); err != nil {
		logger.Error(fmt.Sprintf("cannot get pod by name %s", name), zap.Error(err))
		return *foundPod, err
	}
	return *foundPod, nil
}

func (rm *ResourceManager) IsPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (rm *ResourceManager) CreatePod(pod *corev1.Pod) error {
	logger.Info(fmt.Sprintf("Creating pod %v", pod.ObjectMeta.Name))
	pod.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
	err := rm.kubeClient.Create(context.TODO(), pod)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to create Pod %v", pod.ObjectMeta.Name), zap.Error(err))
		return err
	}
	return nil
}

func (rm *ResourceManager) UpdateService(service *corev1.Service) error {
	service.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
	err := rm.kubeClient.Update(context.TODO(), service)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to update service %v", service.ObjectMeta.Name), zap.Error(err))
		return err
	}
	return nil
}

func (rm *ResourceManager) UpdateDaemonSet(ds *appsv1.DaemonSet) (err error) {
	ds.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
	if err := rm.kubeClient.Update(context.TODO(), ds); err != nil {
		return err
	}
	return nil
}

// Returns true if configMap was updated
func (rm *ResourceManager) CreateOrUpdateConfigMap(cm *corev1.ConfigMap) (bool, error) {
	foundCm := &corev1.ConfigMap{}
	logger.Info(fmt.Sprintf("Start to check if %s cm exists", cm.Name))
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: cm.Name, Namespace: cm.Namespace,
	}, foundCm)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s configMap", cm.ObjectMeta.Name))
		cm.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		cm.ObjectMeta.Labels = rm.getLabels(cm.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), cm)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create configMap %s", cm.ObjectMeta.Name), zap.Error(err))
			return false, err
		}
	} else {
		if !reflect.DeepEqual(foundCm, cm) || !reflect.DeepEqual(foundCm.Data, cm.Data) {
			logger.Info(fmt.Sprintf("Updating %s k8s cm", cm.ObjectMeta.Name))
			cm.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
			if cm.ObjectMeta.Name != "patroni-leader" && cm.ObjectMeta.Name != "patroni-config" {
				cm.ObjectMeta.Labels = rm.getLabels(cm.ObjectMeta)
			}
			err = rm.kubeClient.Update(context.TODO(), cm)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to update cm %v", cm.ObjectMeta.Name), zap.Error(err))
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

func (rm *ResourceManager) CreateOrUpdateService(service *corev1.Service) error {
	foundSrv := &corev1.Service{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: service.Name, Namespace: service.Namespace,
	}, foundSrv)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s k8s service", service.ObjectMeta.Name))
		service.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		service.ObjectMeta.Labels = rm.getLabels(service.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), service)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create service %v", service.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else {
		if !reflect.DeepEqual(foundSrv, service) {
			logger.Info(fmt.Sprintf("Updating %s k8s service", service.ObjectMeta.Name))
			service.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
			service.ObjectMeta.Labels = rm.getLabels(service.ObjectMeta)
			// Service update requires resource version
			service.ResourceVersion = foundSrv.ResourceVersion
			err = rm.kubeClient.Update(context.TODO(), service)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to update service %v", service.ObjectMeta.Name), zap.Error(err))
				return err
			}
		}
	}
	return nil
}

// This method performs delete and re-create deployment in case update was failed
func (rm *ResourceManager) CreateOrUpdateDeploymentForce(deployment *appsv1.Deployment, waitStability bool) error {
	if err := rm.CreateOrUpdateDeployment(deployment, true); err != nil {
		logger.Error(fmt.Sprintf("Cannot create deployment %s", deployment.Name), zap.Error(err))

		if err = rm.DeleteDeployment(deployment.Name); err != nil {
			logger.Error(fmt.Sprintf("Cannot delete deployment %s", deployment.Name), zap.Error(err))
		} else {
			if err = rm.WaitTillDeploymentDeleted(deployment); err != nil {
				logger.Error(fmt.Sprintf("Deployment: %s was not deleted in time", deployment.Name), zap.Error(err))
			}
			if err = rm.CreateOrUpdateDeployment(deployment, true); err != nil {
				logger.Error(fmt.Sprintf("Cannot create deployment after delete %s", deployment.Name), zap.Error(err))
			}
		}
		return err
	}
	return nil
}

func (rm *ResourceManager) CreateOrUpdateDeployment(deployment *appsv1.Deployment, waitStability bool) error {
	err, deploymentBefore := rm.FindDeployment(deployment)
	oldGeneration, deplRevision := deployment.Generation, int64(0)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s k8s deployment", deployment.ObjectMeta.Name))
		deployment.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		deployment.ObjectMeta.Labels = rm.getLabels(deployment.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), deployment)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create deployment %v", deployment.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else {
		copySystemAnnotations(&deploymentBefore.Spec.Template, &deployment.Spec.Template)
		logger.Info(fmt.Sprintf("Updating %s k8s deployment", deployment.ObjectMeta.Name))
		deployment.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		deployment.ObjectMeta.Labels = rm.getLabels(deployment.ObjectMeta)
		err = rm.kubeClient.Update(context.TODO(), deployment)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to update deployment %v", deployment.ObjectMeta.Name), zap.Error(err))
			return err
		}
	}
	// Wait for patroni and Monitoring-collector deployment stability
	if waitStability {
		_, deploymentAfter := rm.FindDeployment(deployment)
		depBeforeHash := opUtil.HashJson(deploymentBefore.Spec)
		depAfterHash := opUtil.HashJson(deploymentAfter.Spec)
		if depBeforeHash != depAfterHash {
			logger.Info("Deployment.Spec hash has differences")
			deplRevision++
		} else {
			logger.Info("Deployment.Spec hash has no differences")
		}
		if err := opUtil.WaitForStabilityDepl(*deployment, deplRevision, oldGeneration); err != nil {
			logger.Error(fmt.Sprintf("Failed to wait for stable deployment: %s", deployment.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func copySystemAnnotations(source, target *corev1.PodTemplateSpec) {
	resAnnotations := target.Annotations
	if resAnnotations == nil {
		resAnnotations = make(map[string]string)
	}

	if copySystemAnnotationsToMap(source.Annotations, resAnnotations) {
		target.Annotations = resAnnotations
	}
}

func copySystemAnnotationsToMap(source, target map[string]string) bool {
	wasModified := false
	for annotationKey, annotationValue := range source {
		if strings.Contains(annotationKey, kubeSysAnnotations) {
			wasModified = true
			target[annotationKey] = annotationValue
		}
	}
	return wasModified
}

func (rm *ResourceManager) CreateOrUpdateStatefulset(statefulSet *appsv1.StatefulSet, waitStability bool) error {

	// Adding label to patroni statefulset for velero backup and restore
	statefulSet.ObjectMeta.Labels["clone-mode-type"] = "data"

	err, statefulSetBefore := rm.FindStatefulSet(statefulSet)
	oldGeneration, stSetRevision := statefulSet.Generation, statefulSet.Status.CurrentRevision

	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s k8s StatefulSet", statefulSet.ObjectMeta.Name))
		statefulSet.ObjectMeta.Labels = rm.getLabels(statefulSet.ObjectMeta)
		statefulSet.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		err = rm.kubeClient.Create(context.TODO(), statefulSet)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create StatefulSet %v", statefulSet.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else {
		if !equality.Semantic.DeepEqual(statefulSetBefore.Spec, statefulSet.Spec) {
			logger.Info(fmt.Sprintf("Updating %s k8s StatefulSet", statefulSet.ObjectMeta.Name))
			err = rm.kubeClient.Delete(context.TODO(), statefulSetBefore)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to delete StatefulSet %v", statefulSetBefore.ObjectMeta.Name), zap.Error(err))
				return err
			}
			statefulSet.ObjectMeta.Labels = rm.getLabels(statefulSet.ObjectMeta)
			statefulSet.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
			statefulSet.ObjectMeta.ResourceVersion = ""
			oldGeneration = 0

			err = rm.kubeClient.Create(context.TODO(), statefulSet)

			_, statefulSetAfter := rm.FindStatefulSet(statefulSet)
			stSetRevision = statefulSetAfter.Status.CurrentRevision

			if err != nil {
				logger.Error(fmt.Sprintf("Failed to create StatefulSet %v", statefulSet.ObjectMeta.Name), zap.Error(err))
				return err
			}
		}
	}
	// Wait for patroni and Monitoring-collector deployment stability
	if waitStability {
		_, stSetAfter := rm.FindStatefulSet(statefulSet)
		stSetBeforeHash := opUtil.HashJson(statefulSetBefore.Spec)
		stSetAfterHash := opUtil.HashJson(stSetAfter.Spec)
		if stSetBeforeHash != stSetAfterHash {
			logger.Info("StatefulSet.Spec hash has differences")
			stSetRevision = stSetAfter.Status.CurrentRevision
		} else {
			logger.Info("StatefulSet.Spec hash has no differences")
		}
		if err := opUtil.WaitForStabilityStatefulSet(*statefulSet, stSetRevision, oldGeneration); err != nil {
			logger.Error(fmt.Sprintf("Failed to wait for stable StatefulSet: %s", statefulSet.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) FindDeployment(deployment *appsv1.Deployment) (error, *appsv1.Deployment) {
	foundDepl := &appsv1.Deployment{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: deployment.Name, Namespace: deployment.Namespace,
	}, foundDepl)
	return err, foundDepl
}

func (rm *ResourceManager) FindStatefulSet(statefulset *appsv1.StatefulSet) (error, *appsv1.StatefulSet) {
	foundStSet := &appsv1.StatefulSet{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: statefulset.Name, Namespace: statefulset.Namespace,
	}, foundStSet)
	return err, foundStSet
}

func (rm *ResourceManager) CreatePvcIfNotExists(pvc *corev1.PersistentVolumeClaim) error {
	foundPvc := &corev1.PersistentVolumeClaim{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: pvc.Name, Namespace: pvc.Namespace,
	}, foundPvc)
	logger.Info(fmt.Sprintf("Start to check if pvc %s exists", pvc.ObjectMeta.Name))
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s PVC", pvc.ObjectMeta.Name))
		err = rm.kubeClient.Create(context.TODO(), pvc)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create pvc %s", pvc.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else {
		logger.Info(fmt.Sprintf("PVC %s exists, clearing owner reference...", pvc.ObjectMeta.Name))
		foundPvc.OwnerReferences = nil
		err := rm.kubeClient.Update(context.TODO(), foundPvc)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to clear Owner Reference for %s", pvc.ObjectMeta.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) CreateSecretIfNotExists(secret *corev1.Secret) error {
	foundSecret := &corev1.Secret{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: secret.Name, Namespace: secret.Namespace,
	}, foundSecret)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s secret", secret.ObjectMeta.Name))
		secret.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		secret.ObjectMeta.Labels = rm.getLabels(secret.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), secret)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create secret %s", secret.ObjectMeta.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) CreateOrUpdateSecret(secret *corev1.Secret) error {
	foundSecret := &corev1.Secret{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: secret.Name, Namespace: secret.Namespace,
	}, foundSecret)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s secret", secret.ObjectMeta.Name))
		secret.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		secret.ObjectMeta.Labels = rm.getLabels(secret.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), secret)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create secret %s", secret.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else {
		if !reflect.DeepEqual(foundSecret, secret) {
			logger.Info(fmt.Sprintf("Updating %s k8s secret", secret.ObjectMeta.Name))
			secret.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
			secret.ObjectMeta.Labels = rm.getLabels(secret.ObjectMeta)
			// Service update requires resource version
			secret.ResourceVersion = foundSecret.ResourceVersion
			err = rm.kubeClient.Update(context.TODO(), secret)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to update secret %v", secret.ObjectMeta.Name), zap.Error(err))
				return err
			}
		}
	}
	return nil
}

func (rm *ResourceManager) CreateEndpointIfNotExists(endpoint *corev1.Endpoints) error {
	foundEndpoint := &corev1.Endpoints{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: endpoint.Name, Namespace: endpoint.Namespace,
	}, foundEndpoint)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s k8s service", endpoint.ObjectMeta.Name))
		endpoint.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		err = rm.kubeClient.Create(context.TODO(), endpoint)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create service %s", endpoint.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func (rm *ResourceManager) DeletePodsByLabel(selectors map[string]string) (err error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(selectors),
	}
	if err := rm.kubeClient.List(context.Background(), podList, listOpts...); err != nil {
		return err
	}

	for _, pod := range podList.Items {
		if err := rm.kubeClient.Delete(context.Background(), &pod); err != nil {
			logger.Error("There is a error during pod drop", zap.Error(err))
			//return err
		}
	}

	return nil
}

func (rm *ResourceManager) DeletePod(pod *corev1.Pod) error {
	foundPod := &corev1.Pod{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: pod.Name, Namespace: pod.Namespace,
	}, foundPod)
	if err != nil && !errors.IsNotFound(err) {
		logger.Error(fmt.Sprintf("ERROR DELETE POD %v ", foundPod.ObjectMeta.Name), zap.Error(err))
		return err
	}
	if err := rm.kubeClient.Delete(context.TODO(), foundPod); err != nil {
		logger.Error(fmt.Sprintf("ERROR DELETE POD %v", foundPod.ObjectMeta.Name), zap.Error(err))
		return err
	}

	return nil
}

func (rm *ResourceManager) DeletePodWithWaiting(pod *corev1.Pod) error {
	err := rm.DeletePod(pod)
	if err != nil {
		return err
	}
	err = opUtil.WaitDeletePod(pod)
	if err != nil {
		return err
	}
	return nil
}

func (rm *ResourceManager) DeleteDeployment(deploymentName string) error {
	foundDeployment := &appsv1.Deployment{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: deploymentName, Namespace: util.GetNameSpace(),
	}, foundDeployment)
	if err != nil && !errors.IsNotFound(err) {
		logger.Error(fmt.Sprintf("error during Deployment deletion %v ", foundDeployment.ObjectMeta.Name), zap.Error(err))
		return err
	} else if err != nil {
		logger.Info(fmt.Sprintf("Deployment %s is not exist", deploymentName))
		return nil
	}
	if err := rm.kubeClient.Delete(context.TODO(), foundDeployment); err != nil {
		logger.Error(fmt.Sprintf("error during Deployment deletion %v", foundDeployment.ObjectMeta.Name), zap.Error(err))
		return err
	}
	err = rm.WaitTillDeploymentDeleted(foundDeployment)
	if err != nil {
		return err
	}
	return nil
}

func (rm *ResourceManager) GetOwnerReferences() []metav1.OwnerReference {
	controller := true
	block := true
	return []metav1.OwnerReference{
		{
			APIVersion:         qubershipv1.GroupVersion.String(),
			Kind:               rm.kind,
			Name:               rm.name,
			UID:                rm.uid,
			Controller:         &controller,
			BlockOwnerDeletion: &block,
		},
	}
}

func (rm *ResourceManager) UpdatePGService() error {
	var svcNames = []string{"postgres-operator", "dbaas-postgres-adapter"}
	for _, svcName := range svcNames {
		svc := rm.GetService(svcName, util.GetNameSpace())
		svc.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		svc.ObjectMeta.Labels = rm.getLabels(svc.ObjectMeta)
		if err := rm.UpdateService(svc); err != nil {
			logger.Error("error during update of pgService in resource_management.go", zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) UpdatePatroniConfigMaps() error {
	var configMaps = []string{"patroni-leader", "patroni-config"}
	for _, configMap := range configMaps {
		cmap, err := rm.GetConfigMap(configMap)
		if err != nil {
			return err
		}
		cmap.ObjectMeta.OwnerReferences = rm.GetOwnerReferences()
		if _, err := rm.CreateOrUpdateConfigMap(cmap); err != nil {
			logger.Error("error during update of patroni configMap in resource_management.go", zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) DeleteInitContainer(statefulSetsList []*appsv1.StatefulSet, containerName string) error {

	logger.Info(fmt.Sprintf("Trying to delete init container with name %s", containerName))

	for _, stSet := range statefulSetsList {
		if len(stSet.Spec.Template.Spec.InitContainers) == 0 {
			logger.Info(fmt.Sprintf("skipping delete of initContainers for deployment: %s", stSet.Name))
			continue
		}
		for idx, container := range stSet.Spec.Template.Spec.InitContainers {
			if containerName == container.Name {
				containers := append(stSet.Spec.Template.Spec.InitContainers[:idx], stSet.Spec.Template.Spec.InitContainers[idx+1:]...)
				stSet.Spec.Template.Spec.InitContainers = containers
				break
			}
		}
		if err := rm.CreateOrUpdateStatefulset(stSet, true); err != nil {
			logger.Error(fmt.Sprintf("Can't update Patroni deployment to delete init container %s", containerName), zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) WaitTillDeploymentDeleted(deployment *appsv1.Deployment) error {
	return wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 10*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		found := &appsv1.Deployment{}
		err = rm.kubeClient.Get(context.TODO(), types.NamespacedName{
			Name: deployment.Name, Namespace: deployment.Namespace,
		}, found)

		if errors.IsNotFound(err) {
			return true, nil
		} else if err != nil {
			logger.Error(fmt.Sprintf("deployment %s still exists, retrying", deployment.Name), zap.Error(err))
			return false, nil
		}
		return false, nil
	})
}

func (rm *ResourceManager) CreateServiceIfNotExists(service *corev1.Service) error {
	foundService := &corev1.Service{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: service.Name, Namespace: service.Namespace,
	}, foundService)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s k8s service", service.ObjectMeta.Name))
		service.ObjectMeta.Labels = rm.getLabels(service.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), service)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create service %s", service.ObjectMeta.Name), zap.Error(err))
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}
func (rm *ResourceManager) CreateConfigMapIfNotExists(cm *corev1.ConfigMap) error {
	foundCm := &corev1.ConfigMap{}
	logger.Info(fmt.Sprintf("Start to check if %s cm exists", cm.Name))
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: cm.Name, Namespace: cm.Namespace,
	}, foundCm)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s configMap", cm.ObjectMeta.Name))
		cm.ObjectMeta.Labels = rm.getLabels(cm.ObjectMeta)
		err = rm.kubeClient.Create(context.TODO(), cm)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create configMap %s", cm.ObjectMeta.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (rm *ResourceManager) GetSecret(secretName string) (*corev1.Secret, error) {
	foundSecret := &corev1.Secret{}
	err := rm.kubeClient.Get(context.TODO(), types.NamespacedName{
		Name: secretName, Namespace: util.GetNameSpace(),
	}, foundSecret)
	if err != nil {
		logger.Error(fmt.Sprintf("can't find the secret %s", secretName), zap.Error(err))
		return foundSecret, err
	}
	return foundSecret, nil
}

func (rm *ResourceManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if !util.IsHttpAuthEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := strings.Split(r.Header.Get("Authorization"), "Bearer ")
		smCustomAudience := util.GetEnv("SM_CUSTOM_AUDIENCE", "")

		if len(authHeader) != 2 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var reviewRes *k8sauth.TokenReview
		expired := false
		currentTime := time.Now()

		authPair, ok := authHeaders[authHeader[1]]
		if ok {
			tokenSessionTimeout := time.Duration(util.GetEnvAsInt("TOKEN_SESSION_TIMEOUT", 5))
			authTime := authPair.time.Add(tokenSessionTimeout * time.Minute)
			expired = currentTime.After(authTime)
		}

		if !ok || expired {

			logger.Info("Session token expired, re-authentication.")

			tokenReview := &k8sauth.TokenReview{
				Spec: k8sauth.TokenReviewSpec{
					Token: authHeader[1],
				},
			}
			if smCustomAudience != "" {
				tokenReview.Spec.Audiences = []string{smCustomAudience}
			}

			tokenRes, err := rm.kubeClientSet.AuthenticationV1().TokenReviews().
				Create(context.TODO(), tokenReview, metav1.CreateOptions{})

			if err != nil {
				logger.Error("There is an error during TokenReview Request", zap.Error(err))
				w.WriteHeader(http.StatusInternalServerError)
			}
			authHeaders[authHeader[1]] = AuthPair{currentTime, tokenRes}
			reviewRes = tokenRes
		} else {
			reviewRes = authPair.review
		}

		if authenticated(reviewRes) {
			next.ServeHTTP(w, r)
			return
		} else {
			logger.Error(fmt.Sprintf("User %s is unauthorized. Check site-manager configuration", util.GetSmAuthUserName()))
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
}

func (rm *ResourceManager) UpdatePatroniReplicas(replicas int32, clusterName string) error {
	logger.Info(fmt.Sprintf("Trying to scale patroni cluster to %d", replicas))
	var stSetsToUpdate []*appsv1.StatefulSet
	stSetName := fmt.Sprintf("pg-%s-node", clusterName)
	stSetList := &appsv1.StatefulSetList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := rm.kubeClient.List(context.Background(), stSetList, listOpts...); err == nil {
		for idx := 0; idx < len(stSetList.Items); idx++ {
			stSet := &stSetList.Items[idx]
			if r, e := regexp.MatchString(stSetName, stSet.Name); e == nil && r {
				logger.Info(fmt.Sprintf("Scale %v to %v", stSet.Name, replicas))
				stSet.Spec.Replicas = &replicas
				stSetsToUpdate = append(stSetsToUpdate, stSet)
			}
		}
	} else {
		logger.Error("patroni statefulsets cannot be listed", zap.Error(err))
		return err
	}

	for idx := 0; idx < len(stSetsToUpdate); idx++ {
		err := rm.kubeClient.Update(context.TODO(), stSetsToUpdate[idx])
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to update statefulsets %v to scale to %v", stSetsToUpdate[idx].ObjectMeta.Name, replicas), zap.Error(err))
			return err
		}
	}
	logger.Info(fmt.Sprintf("%s cluster statefulsets have scaled to %v successfuly", clusterName, replicas))
	return nil
}

func (ph *PatroniHelper) IsHealthyWithTimeout(timeout time.Duration, patroniUrl string, pgHost string) (bool, error) {
	if err := wait.PollUntilContextTimeout(context.Background(), time.Second, timeout, true, func(ctx context.Context) (done bool, err error) {
		if ph.IsHealthy(patroniUrl, pgHost) {
			return true, nil
		}
		logger.Info("Patroni cluster is not healthy. Retry...")
		return false, nil
	}); err != nil {
		logger.Info("Patroni cluster is not healthy. No retries left")
		return false, err
	}
	logger.Info("Patroni cluster is healthy.")
	return true, nil
}

func (ph *PatroniHelper) IsHealthyWithTimeoutDuringUpdate(timeout time.Duration, patroniUrl string, pgHost string, replicas int) (bool, error) {
	if err := wait.PollUntilContextTimeout(context.Background(), time.Second, timeout, true, func(ctx context.Context) (done bool, err error) {
		if ph.IsHealthyDuringUpdate(patroniUrl, pgHost, replicas) {
			return true, nil
		}
		logger.Info("Patroni cluster is not healthy. Retry...")
		return false, nil
	}); err != nil {
		logger.Info("Patroni cluster is not healthy. No retries left")
		return false, err
	}
	logger.Info("Patroni cluster is healthy.")
	return true, nil
}

func (rm *ResourceManager) GetPatroniClusterConfig(patroniUrl string) (*ClusterStatus, error) {
	httpC := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := httpC.Get(patroniUrl + "cluster")
	if err != nil {
		logger.Error("Get request to patroni cluster failed, retrying")
		return &ClusterStatus{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusOK {
		var status ClusterStatus
		d := json.NewDecoder(resp.Body)
		err := d.Decode(&status)
		if err == nil {
			return &status, nil
		} else {
			logger.Error("Failure while config reading", zap.Error(err))
			return &ClusterStatus{}, err
		}
	}
	return &ClusterStatus{}, nil
}

func (rm *ResourceManager) GetChartVersion() string {
	operatorLabels, err := rm.getOperatorLabels()
	if err != nil {
		return ""
	}
	version, found := operatorLabels["app.kubernetes.io/version"]
	if !found {
		return ""
	}
	return version
}

func (rm *ResourceManager) GetDeploymentSessionId() string {
	operatorLabels, err := rm.getOperatorLabels()
	if err != nil {
		return ""
	}
	sessionId, found := operatorLabels["deployment.netcracker.com/sessionId"]
	if !found {
		return ""
	}
	return sessionId
}

func (rm *ResourceManager) getOperatorLabels() (map[string]string, error) {
	deploymentName := strings.ToLower(os.Getenv("OPERATOR_NAME"))

	if deploymentName == "patroni-services" {
		deploymentName = "postgres-operator"
	}

	foundDeployment, err := rm.GetDeploymentsByNameRegExp(deploymentName)
	if err != nil {
		return nil, err
	}
	if len(foundDeployment) == 0 {
		return nil, nil
	}

	deployment := foundDeployment[0]
	labels := deployment.Spec.Template.ObjectMeta.Labels
	return labels, nil
}

func (rm *ResourceManager) getLabels(meta metav1.ObjectMeta) map[string]string {
	mergedLabels := make(map[string]string, len(meta.Labels))
	maps.Copy(mergedLabels, meta.Labels)

	commonLabels := rm.commonLabels(meta.Name)

	for k, v := range commonLabels {
		mergedLabels[k] = v
	}

	return mergedLabels
}

func (rm *ResourceManager) commonLabels(name string) map[string]string {
	chartVersion := rm.GetChartVersion()
	sessionId := rm.GetDeploymentSessionId()

	operatorName := strings.ToLower(os.Getenv("OPERATOR_NAME"))

	component := "postgres"
	if operatorName == "patroni-services" {
		component = "postgres-services"
		operatorName = "postgres-operator"
	}

	labels := map[string]string{
		"name":                       name,
		"app.kubernetes.io/instance": name,
		"app.kubernetes.io/name":     name,

		"app.kubernetes.io/component":           "backend",
		"app.kubernetes.io/part-of":             component,
		"app.kubernetes.io/managed-by":          "operator",
		"app.kubernetes.io/managed-by-operator": operatorName,
		"app.kubernetes.io/technology":          "go",
	}
	if chartVersion != "" {
		labels["app.kubernetes.io/version"] = chartVersion
	}
	if sessionId != "" {
		labels["deployment.netcracker.com/sessionId"] = sessionId
	}

	if slices.Contains(pythonServices, name) {
		labels["app.kubernetes.io/technology"] = "python"
	}

	return labels
}
