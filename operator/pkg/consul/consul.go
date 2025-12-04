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

package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"

	"k8s.io/apimachinery/pkg/types"

	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/util/constants"
	consulApi "github.com/hashicorp/consul/api"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DiscoveryConfigurationName string = "postgres-service-consul-discovery-data"
	ConsulClientPort           string = "8500"
	ConsulDiscoveryMeta        string = "meta"
	ConsulDiscoveryTags        string = "tags"
	ConsulDiscoveryLeaderTags  string = "leaderTags"
	ConsulDiscoveryLeaderMeta  string = "leaderMeta"
)

var (
	logger    = util.GetLogger()
	namespace = util.GetNameSpace()
	NodeIP    = os.Getenv("HOST_IP")

	k8sClient crclient.Client
)

type ConsulRegistrator struct {
	cr          *v1.PatroniCore
	helper      *helper.PatroniHelper
	scheme      *runtime.Scheme
	resVersions map[string]string
	cluster     *v1.PatroniClusterSettings
}

func init() {
	var err error
	k8sClient, err = util.GetClient()
	if err != nil {
		logger.Error("cannot get k8s client")
		panic(err)
	}
}

func NewRegistrator(cr *v1.PatroniCore, helper *helper.PatroniHelper, scheme *runtime.Scheme, resVersions map[string]string, cluster *v1.PatroniClusterSettings) *ConsulRegistrator {
	return &ConsulRegistrator{
		cr:          cr,
		helper:      helper,
		scheme:      scheme,
		resVersions: resVersions,
		cluster:     cluster,
	}
}

func (r *ConsulRegistrator) RegisterInConsul() error {
	cr := r.cr
	consulSpec := cr.Spec.ConsulRegistration
	if consulSpec == nil {
		return nil
	}

	discoveryConfigMap, err := util.FindCmInNamespaceByName(cr.Namespace, DiscoveryConfigurationName)
	if err != nil {
		logger.Info("Consul Discovery cm not exists, will create new one")
		discoveryConfigMap, err := r.getConsulRegistrationCm(consulSpec)
		if err != nil {
			return err
		}
		if _, err := r.helper.CreateOrUpdateConfigMap(discoveryConfigMap); err != nil {
			logger.Error("Failed to create config map consul registration, exiting", zap.Error(err))
			return err
		}
	}

	discoveredServiceName := consulSpec.ServiceName
	if len(discoveredServiceName) == 0 {
		logger.Info("Service Name is empty, setting it to default")
		discoveredServiceName = cr.Namespace + ":" + "postgres"
	}

	// register leader endpoint
	serviceIp, err := r.getServiceIp(cr.Namespace, r.cluster.PostgresServiceName)
	if err != nil {
		return err
	}

	tags, meta := r.getTagsAndMetaFromCm(discoveryConfigMap, ConsulDiscoveryTags, ConsulDiscoveryMeta)
	leaderTags, leaderMeta := r.getTagsAndMetaFromCm(discoveryConfigMap, ConsulDiscoveryLeaderTags, ConsulDiscoveryLeaderMeta)
	serviceChecks := consulApi.AgentServiceChecks{
		getServiceCheckByIp(serviceIp, consulSpec),
	}

	serviceDefinition := &consulApi.AgentServiceRegistration{
		Name:              discoveredServiceName,
		ID:                r.cluster.PostgresServiceName,
		Address:           serviceIp,
		Port:              constants.PostgreSQLPort,
		Tags:              append(tags, leaderTags...),
		Meta:              util.Merge(meta, leaderMeta),
		EnableTagOverride: true,
		Checks:            serviceChecks,
	}
	client, err := r.createConsulClient(consulSpec)
	if err != nil {
		return err
	}
	err = client.Agent().ServiceRegister(serviceDefinition)
	if err != nil {
		return err
	} else {
		logger.Info(fmt.Sprintf("Postgres instance with id:%s (ip: %s) was registered in Consul", r.cluster.PostgresServiceName, serviceIp))
	}

	serviceIp, err = r.getServiceIp(cr.Namespace, r.cluster.PatroniReplicasServiceName)
	if err != nil {
		return err
	}
	serviceDefinition.ID = r.cluster.PatroniReplicasServiceName
	serviceDefinition.Address = serviceIp
	serviceDefinition.Tags = append(tags, []string{"replicas"}...)
	serviceDefinition.Meta = util.Merge(meta, map[string]string{"pgtype": "replica"})
	serviceChecks = consulApi.AgentServiceChecks{
		getServiceCheckByIp(serviceIp, consulSpec),
	}
	serviceDefinition.Checks = serviceChecks

	err = client.Agent().ServiceRegister(serviceDefinition)
	if err != nil {
		return err
	} else {
		logger.Info(fmt.Sprintf("Postgres instance with id:%s (ip: %s) was registered in Consul", r.cluster.PatroniReplicasServiceName, serviceIp))
	}

	r.resVersions[discoveryConfigMap.Name] = discoveryConfigMap.ResourceVersion
	return nil
}

func getServiceCheckByIp(serviceIp string, consulSpec *v1.ConsulRegistration) *consulApi.AgentServiceCheck {
	return &consulApi.AgentServiceCheck{
		Name:                           "tcp-check",
		Interval:                       consulSpec.CheckInterval,
		Timeout:                        consulSpec.CheckTimeout,
		TCP:                            serviceIp + ":" + strconv.Itoa(constants.PostgreSQLPort),
		DeregisterCriticalServiceAfter: consulSpec.DeregisterAfter,
	}
}

func (r *ConsulRegistrator) getTagsAndMetaFromCm(configMap *corev1.ConfigMap, tagsKey string, metaKey string) ([]string, map[string]string) {
	var tags []string
	err := json.Unmarshal([]byte(configMap.Data[tagsKey]), &tags)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot parse %s for Consul", tagsKey), zap.Error(err))
	}
	var meta map[string]string
	err = json.Unmarshal([]byte(configMap.Data[metaKey]), &meta)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot parse %s for Consul", metaKey), zap.Error(err))
	}
	return tags, meta
}

func (r *ConsulRegistrator) getServiceIp(namespace, serviceName string) (string, error) {
	foundService := &corev1.Service{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: namespace}, foundService)
	return foundService.Spec.ClusterIP, err
}

func (r *ConsulRegistrator) getConsulRegistrationCm(consulSpec *v1.ConsulRegistration) (*corev1.ConfigMap, error) {
	tagsString, err := json.Marshal(consulSpec.Tags)
	if err != nil {
		logger.Error("Can't read tags from discovery config", zap.Error(err))
		return nil, err
	}
	metaString, err := json.Marshal(consulSpec.Meta)
	if err != nil {
		logger.Error("Can't read meta from discovery config", zap.Error(err))
		return nil, err
	}
	leaderTagsStrings, err := json.Marshal(consulSpec.LeaderTags)
	if err != nil {
		logger.Error("Can't read leader tags from discovery config", zap.Error(err))
		return nil, err
	}

	leaderMetaStrings, err := json.Marshal(consulSpec.LeaderMeta)
	if err != nil {
		logger.Error("Can't read leader meta from discovery config", zap.Error(err))
		return nil, err
	}
	discoveryConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DiscoveryConfigurationName,
			Namespace: namespace,
			Labels:    map[string]string{"app": "postgres"},
		},
		Data: map[string]string{
			ConsulDiscoveryTags:       string(tagsString),
			ConsulDiscoveryMeta:       string(metaString),
			ConsulDiscoveryLeaderTags: string(leaderTagsStrings),
			ConsulDiscoveryLeaderMeta: string(leaderMetaStrings),
		},
	}
	return discoveryConfig, nil

}

func (r *ConsulRegistrator) createConsulClient(consulSpec *v1.ConsulRegistration) (*consulApi.Client, error) {
	consulConfig := consulApi.DefaultConfig()
	consulAddress := consulSpec.Host
	if len(consulAddress) == 0 {
		consulAddress = NodeIP + ":" + ConsulClientPort
	}

	consulConfig.Address = consulAddress
	token, err := util.ReadTokenFromFile()
	if err != nil {
		return nil, err
	}
	consulConfig.Token = token
	consulConfig.Namespace = namespace

	client, err := consulApi.NewClient(consulConfig)
	return client, err
}
