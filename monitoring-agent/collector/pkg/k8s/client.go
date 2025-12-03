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

package k8s

import (
	"context"
	"fmt"
	"os"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	logger    = util.GetLogger()
	k8sClient crclient.Client
	namespace = os.Getenv("NAMESPACE")
)

type PatroniNode struct {
	Name string
	IP   string
}

func init() {
	var err error
	k8sClient, err = util.CreateClient()
	if err != nil {
		logger.Error("Can't create k8s client")
		panic(err)
	}
}

func GetPodsByLabel(ctx context.Context, selectors map[string]string) (corev1.PodList, error) {
	podList := &corev1.PodList{}
	listOpts := []crclient.ListOption{
		crclient.InNamespace(namespace),
		crclient.MatchingLabels(selectors),
	}
	if err := k8sClient.List(ctx, podList, listOpts...); err != nil {
		logger.Error("Can not get pods by label", zap.Error(err))
		return *podList, err
	}
	return *podList, nil
}

func GetDeploymentsByLabel(ctx context.Context) (appsv1.DeploymentList, error) {
	deploymentList := &appsv1.DeploymentList{}
	listOpts := []crclient.ListOption{
		crclient.InNamespace(namespace),
	}
	if err := k8sClient.List(ctx, deploymentList, listOpts...); err != nil {
		logger.Error("Can not get pods by label", zap.Error(err))
		return *deploymentList, err
	}
	return *deploymentList, nil
}

func GetDeploymentInfo(deploymentName string) *appsv1.Deployment {
	dcInfo, _ := GetDeploymentsByLabel(context.Background())
	for _, deployment := range dcInfo.Items {
		if deployment.Name == deploymentName {
			logger.Debug(fmt.Sprintf("deployment with name %s\n%s", deploymentName, deploymentName))
			return &deployment
		}
	}
	return nil
}

func GetPatroniNodes(ctx context.Context, pgCluster string) []PatroniNode {
	result := make([]PatroniNode, 0)

	selectors := map[string]string{
		"app":       pgCluster,
		"pgcluster": pgCluster,
	}
	pods, err := GetPodsByLabel(ctx, selectors)
	if err != nil {
		return result
	}

	for _, pod := range pods.Items {
		nodeName := ""
		for _, env := range pod.Spec.Containers[0].Env {
			if env.Name == "POD_IDENTITY" {
				nodeName = env.Value
				result = append(result, PatroniNode{Name: nodeName, IP: pod.Status.PodIP})
			}
		}
	}
	return result
}

func GetConfigMaps() *corev1.ConfigMapList {

	configMapList := &corev1.ConfigMapList{}
	listOps := &crclient.ListOptions{
		Namespace: namespace,
	}
	if err := k8sClient.List(context.Background(), configMapList, listOps); err != nil {
		logger.Error("Error while listing config maps", zap.Error(err))
	}
	return configMapList
}
