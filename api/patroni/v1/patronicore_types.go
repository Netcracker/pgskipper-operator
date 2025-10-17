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

package v1

import (
	types "github.com/Netcracker/pgskipper-operator-core/api/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PatroniCoreSpec defines the desired state of PatroniCore
// +k8s:openapi-gen=true
type PatroniCoreSpec struct {
	Patroni               *Patroni                 `json:"patroni,omitempty"`
	AuthSecret            string                   `json:"authSecret,omitempty"`
	IntegrationTests      *IntegrationTests        `json:"integrationTests,omitempty"`
	ConsulRegistration    *ConsulRegistration      `json:"consulRegistration,omitempty"`
	VaultRegistration     *types.VaultRegistration `json:"vaultRegistration,omitempty"`
	Policies              *Policies                `json:"policies,omitempty"`
	ServiceAccountName    string                   `json:"serviceAccountName,omitempty"`
	CloudSql              *types.CloudSql          `json:"cloudSql,omitempty"`
	Tls                   *Tls                     `json:"tls,omitempty"`
	PgBackRest            *PgBackRest              `json:"pgBackRest,omitempty"`
	Ldap                  *LdapConfig              `json:"ldap,omitempty"`
	InstallationTimestamp string                   `json:"installationTimestamp,omitempty"`
	PrivateRegistry       PrivateRegistry          `json:"privateRegistry,omitempty"`
}

type PrivateRegistry struct {
	Enabled bool     `json:"enabled,omitempty"`
	Names   []string `json:"names,omitempty"`
}

// PatroniCoreStatusCondition contains description of status of PatroniCore
// +k8s:openapi-gen=true
type PatroniCoreStatusCondition struct {
	Type               string `json:"type,omitempty"`
	Status             bool   `json:"status,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// PatroniCoreStatus defines the observed state of PatroniCore
// +k8s:openapi-gen=true

//+kubebuilder:object:root=true

// PatroniCore is the Schema for the postgresservices API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type PatroniCore struct {
	metav1.TypeMeta   `json:",inline,omitempty"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              *PatroniCoreSpec  `json:"spec,omitempty"`
	Status            PatroniCoreStatus `json:"status,omitempty"`
	RunTestsTime      string            `json:"runTestsTime,omitempty"`
	Upgrade           *Upgrade          `json:"majorUpgrade,omitempty"`
}

// +kubebuilder:object:root=true

// PatroniCoreList contains a list of PatroniCore
type PatroniCoreList struct {
	metav1.TypeMeta `json:",inline,omitempty"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PatroniCore `json:"items,omitempty"`
}

// Patroni contains Patroni-specific configuration
type Patroni struct {
	Resources                    *v1.ResourceRequirements `json:"resources,omitempty"`
	Replicas                     int                      `json:"replicas,omitempty"`
	DockerImage                  string                   `json:"image,omitempty"`
	Storage                      *types.Storage           `json:"storage,omitempty"`
	Affinity                     v1.Affinity              `json:"affinity,omitempty"`
	PostgreSQLParams             []string                 `json:"postgreSQLParams,omitempty"`
	PatroniParams                []string                 `json:"patroniParams,omitempty"`
	StandbyCluster               *StandbyCluster          `json:"standbyCluster,omitempty"`
	EnableShmVolume              bool                     `json:"enableShmVolume,omitempty"`
	PriorityClassName            string                   `json:"priorityClassName,omitempty"`
	CreateEndpoint               bool                     `json:"createEndpoint,omitempty"`
	SynchronousMode              bool                     `json:"synchronousMode,omitempty"`
	Dcs                          Dcs                      `json:"dcs,omitempty"`
	Scope                        string                   `json:"scope,omitempty"`
	Tags                         map[string]string        `json:"tags,omitempty"`
	PodLabels                    map[string]string        `json:"podLabels,omitempty"`
	PgHba                        []string                 `json:"pgHba,omitempty"`
	Powa                         Powa                     `json:"powa,omitempty"`
	VaultRegistration            *types.VaultRegistration `json:"vaultRegistration,omitempty"`
	SecurityContext              *v1.PodSecurityContext   `json:"securityContext,omitempty"`
	Unlimited                    bool                     `json:"unlimited,omitempty"`
	PgWalStorageAutoManage       bool                     `json:"pgWalStorageAutoManage,omitempty"`
	ForceCollationVersionUpgrade bool                     `json:"forceCollationVersionUpgrade,omitempty"`
	PgWalStorage                 *types.Storage           `json:"pgWalStorage,omitempty"`
	ClusterName                  string                   `json:"clusterName,omitempty"`
	IgnoreSlots                  bool                     `json:"ignoreSlots,omitempty"`
	IgnoreSlotsPrefix            string                   `json:"ignoreSlotsPrefix,omitempty"`
	External                     *External                `json:"external,omitempty"`
	PodAnnotations               map[string]string        `json:"podAnnotations,omitempty"`
	ConfigMapAnnotations         map[string]string        `json:"configMapAnnotations,omitempty"`
	PatroniResourcesLimitMemory  string                   `json:"patroniResourcesLimitMemory,omitempty"`
}

type External struct {
	Pvc []PVC `json:"pvc,omitempty"`
}

type PVC struct {
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath,omitempty"`
}

type StandbyCluster struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type StandbyClusterHealthCheck struct {
	RetriesLimit        int `json:"retriesLimit,omitempty"`
	FailureRetriesLimit int `json:"failureRetriesLimit,omitempty"`
	RetriesWaitTimeout  int `json:"retriesWaitTimeout,omitempty"`
}

type ConsulRegistration struct {
	CheckInterval   string            `json:"checkInterval,omitempty"`
	CheckTimeout    string            `json:"checkTimeout,omitempty"`
	DeregisterAfter string            `json:"deregisterAfter,omitempty"`
	Host            string            `json:"host,omitempty"`
	ServiceName     string            `json:"serviceName,omitempty"`
	Meta            map[string]string `json:"meta,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	LeaderMeta      map[string]string `json:"leaderMeta,omitempty"`
	LeaderTags      []string          `json:"leaderTags,omitempty"`
}

type Upgrade struct {
	Enabled            bool   `json:"enabled,omitempty"`
	InitDbParams       string `json:"initDbParams,omitempty"`
	DockerUpgradeImage string `json:"dockerUpgradeImage,omitempty"`
}

type Powa struct {
	Install bool `json:"install,omitempty"`
}

type PatroniClusterSettings struct {
	ClusterName                string
	PatroniLabels              map[string]string
	PatroniCommonLabels        map[string]string
	PostgresServiceName        string
	PatroniMasterSelectors     map[string]string
	PatroniReplicasSelector    map[string]string
	PatroniReplicasServiceName string
	PatroniUrl                 string
	PatroniCM                  string
	PatroniPropertiesCM        string
	PatroniTemplate            string
	ConfigMapKey               string
	PostgreSQLUserConf         string
	PostgreSQLPort             int
	PatroniDeploymentName      string
	PgHost                     string
}

type Dcs struct {
	Type  string   `json:"type,omitempty"`
	Hosts []string `json:"hosts,omitempty"`
}

type IntegrationTests struct {
	Resources        *v1.ResourceRequirements `json:"resources,omitempty"`
	DockerImage      string                   `json:"image,omitempty"`
	RunTestScenarios string                   `json:"runTestScenarios,omitempty"`
	TestList         []string                 `json:"testList,omitempty"`
	Replicas         int                      `json:"replicas,omitempty"`
	PgNodeQty        int                      `json:"pgNodeQty,omitempty"`
	PodLabels        map[string]string        `json:"podLabels,omitempty"`
	Affinity         v1.Affinity              `json:"affinity,omitempty"`
}

type Policies struct {
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
}

type Tls struct {
	Enabled               bool   `json:"enabled,omitempty"`
	CertificateSecretName string `json:"certificateSecretName,omitempty"`
}

type PatroniCoreStatus struct {
	Conditions []PatroniCoreStatusCondition `json:"conditions,omitempty"`
}

type PgBackRest struct {
	DockerImage       string                   `json:"dockerImage,omitempty"`
	RepoType          string                   `json:"repoType,omitempty"`
	RepoPath          string                   `json:"repoPath,omitempty"`
	DiffSchedule      string                   `json:"diffSchedule,omitempty"`
	IncrSchedule      string                   `json:"incrSchedule,omitempty"`
	S3                S3                       `json:"s3,omitempty"`
	DRS3              S3                       `json:"drS3,omitempty"`
	Rwx               *types.Storage           `json:"rwx,omitempty"`
	Resources         *v1.ResourceRequirements `json:"resources,omitempty"`
	FullRetention     int                      `json:"fullRetention,omitempty"`
	DiffRetention     int                      `json:"diffRetention,omitempty"`
	BackupFromStandby bool                     `json:"backupFromStandby,omitempty"`
	ConfigParams      []string                 `json:"configParams,omitempty"`
}

type S3 struct {
	Bucket    string `json:"bucket,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Key       string `json:"key,omitempty"`
	Secret    string `json:"secret,omitempty"`
	Region    string `json:"region,omitempty"`
	VerifySsl bool   `json:"verifySsl,omitempty"`
}

type LdapConfig struct {
	Enabled        bool   `json:"enabled,omitempty"`
	Server         string `json:"server,omitempty"`
	Port           int    `json:"port,omitempty"`
	BaseDN         string `json:"basedn,omitempty"`
	BindDN         string `json:"binddn,omitempty"`
	BindPasswd     string `json:"bindpasswd,omitempty"`
	LdapSearchAttr string `json:"ldapsearchattribute,omitempty"`
}

func init() {
	SchemeBuilder.Register(&PatroniCore{}, &PatroniCoreList{})
}
