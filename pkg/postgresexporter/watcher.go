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

package postgresexporter

import (
	"context"
	"fmt"
	"sync"

	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	exporterCM   = "postgres-exporter-queries"
	queriesParam = "postgres-exporter-queries.yaml"
	initial      = "initial"
)

var (
	k8sClient         client.Client
	exporterPodLabels = map[string]string{"app": "postgres-exporter"}
	logger            = util.GetLogger()
	activeWatcher     *Watcher
	mutex             sync.Mutex
)

type Watcher struct {
	helper     *helper.Helper
	namespaces []string
	cmList     map[string][]string
	labels     map[string]string
	watchers   map[string]watch.Interface
}

func NewPostgresExporterWatcher(helper *helper.Helper, namespaces []string, labels map[string]string) *Watcher {
	return &Watcher{
		helper:     helper,
		namespaces: namespaces,
		cmList:     map[string][]string{},
		labels:     labels,
		watchers:   map[string]watch.Interface{},
	}
}

func init() {
	var err error
	k8sClient, err = util.GetClient()
	if err != nil {
		logger.Error("cannot get k8sClient", zap.Error(err))
		panic(err)
	}
}

func (exp *Watcher) WatchCustomQueries() error {
	logger.Info("Preparing custom queries for pg exporter")
	if err := exp.updateCM(); err != nil {
		return err
	}
	if err := exp.watchNamespaces(); err != nil {
		return err
	}
	return nil
}

func (exp *Watcher) updateCMData(cm *v1.ConfigMap) (updated bool, err error) {
	configmapsForMerge, err := exp.findConfigMaps()
	if err != nil {
		return
	}

	if len(configmapsForMerge) == 0 {
		logger.Info(fmt.Sprintf("No config maps with labels: %s for merge in namespaces: %s", exp.labels, exp.namespaces))
	} else {
		var cmData map[string]string
		cmData, err = appendDataToCM(cm, configmapsForMerge)
		if err != nil {
			return
		}
		cm.Data = cmData
	}

	updated, err = exp.helper.CreateOrUpdateConfigMap(cm)
	return
}

func (exp *Watcher) watchNamespaces() error {
	for _, namespace := range exp.namespaces {
		go exp.watchNamespace(namespace)
	}
	activeWatcher = exp
	return nil
}

func (exp *Watcher) isCmAlreadyPresent(cm *v1.ConfigMap) bool {
	for n, cmList := range exp.cmList {
		if n == cm.Namespace {
			for _, v := range cmList {
				if v == cm.Name {
					logger.Debug(fmt.Sprintf("CM %s from namespace %s is already present", cm.Name, cm.Namespace))
					return true
				}
			}
		}
	}
	return false
}

func (exp *Watcher) watchNamespace(namespace string) {
	clientSet := util.GetKubeClient()
	for {
		exp.handleWatcher(clientSet, namespace)
		logger.Info(fmt.Sprintf("Closed watcher for namespace %s", namespace))
	}
}

func (exp *Watcher) handleWatcher(clientSet *kubernetes.Clientset, namespace string) {
	timeout := int64(2000000000)
	watcher, err := clientSet.CoreV1().ConfigMaps(namespace).Watch(context.Background(), metav1.ListOptions{
		LabelSelector:  labels.SelectorFromSet(exp.labels).String(),
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		logger.Error(fmt.Sprintf("cannot create watcher for %s namespace", namespace))
		return
	}

	exp.replaceWatcher(namespace, watcher)

	for event := range watcher.ResultChan() {
		cm := event.Object.(*v1.ConfigMap)
		switch event.Type {
		case watch.Added:
			if exp.isCmAlreadyPresent(cm) {
				continue
			}
			exp.cmList[cm.Namespace] = append(exp.cmList[cm.Namespace], cm.Name)
			logger.Info(fmt.Sprintf("CM %s was added in namespace %s", cm.Name, cm.Namespace))
			if err := exp.updateCM(); err != nil {
				continue
			}
		case watch.Modified:
			logger.Info(fmt.Sprintf("CM %s was modified in namespace %s", cm.Name, cm.Namespace))
			if err := exp.updateCM(); err != nil {
				continue
			}
		case watch.Deleted:
			logger.Info(fmt.Sprintf("CM %s was deleted in namespace %s", cm.Name, cm.Namespace))
			exp.removeCMFromList(cm.Namespace, cm.Name)
			if err := exp.updateCM(); err != nil {
				continue
			}
		}
	}
}

func (exp *Watcher) replaceWatcher(namespace string, watcher watch.Interface) {
	oldWatcher, ok := exp.watchers[namespace]
	if ok {
		oldWatcher.Stop()
	}
	exp.watchers[namespace] = watcher
}

func (exp *Watcher) removeCMFromList(namespace string, cmName string) {
	for i, v := range exp.cmList[namespace] {
		if v == cmName {
			exp.cmList[namespace] = append(exp.cmList[namespace][:i], exp.cmList[namespace][i+1:]...)
			break
		}
	}
}

func (exp *Watcher) findConfigMaps() ([]v1.ConfigMap, error) {
	configMaps := make([]v1.ConfigMap, 0)

	for _, namespace := range exp.namespaces {
		configMapList := &v1.ConfigMapList{}
		listOps := &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(exp.labels),
			Namespace:     namespace,
		}
		if err := k8sClient.List(context.Background(), configMapList, listOps); err == nil {
			cmInNamespace := make([]string, 0)
			for _, configMap := range configMapList.Items {
				logger.Debug(fmt.Sprintf("Find %s CM in namespace %s", configMap.Name, namespace))
				configMaps = append(configMaps, configMap)
				cmInNamespace = append(cmInNamespace, configMap.Name)
			}
			exp.cmList[namespace] = cmInNamespace
		} else {
			logger.Error(fmt.Sprintf("cannot get config maps list for namespaces %s", namespace), zap.Error(err))
			return nil, err
		}
	}
	return configMaps, nil
}

func (exp *Watcher) updateCM() (err error) {
	mutex.Lock()
	defer mutex.Unlock()

	logger.Debug("Update Postgres Exporter queries")
	expCM, err := exp.helper.GetConfigMap(exporterCM)
	if err != nil {
		return
	}
	expCM.Data[queriesParam] = expCM.Data[initial]

	updated, err := exp.updateCMData(expCM)
	if updated {
		err = exp.helper.DeletePodsByLabel(exporterPodLabels)
	}

	return
}

func (exp *Watcher) stopAllWatchers() {
	logger.Info("Close all watchers")
	for _, w := range exp.watchers {
		w.Stop()
	}
}

func RemoveActiveWatcher() {
	if activeWatcher != nil {
		activeWatcher.stopAllWatchers()
	}
}

func appendDataToCM(expCM *v1.ConfigMap, configMaps []v1.ConfigMap) (map[string]string, error) {
	queries, ok := expCM.Data[queriesParam]
	if !ok {
		errMsg := fmt.Sprintf("no data in postgres exporter CM for key %s", queriesParam)
		logger.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}
	for _, cm := range configMaps {
		dataToAppend, ok := cm.Data[queriesParam]
		if !ok {
			logger.Info(fmt.Sprintf("no data in %s CM from namespace %s for key %s", cm.Name, cm.Namespace, queriesParam))
			continue
		}
		queries = queries + "\n" + dataToAppend
	}
	expCM.Data[queriesParam] = queries

	return expCM.Data, nil
}
