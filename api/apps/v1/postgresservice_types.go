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

// PatroniServicesSpec defines the desired state of PatroniServices
// +k8s:openapi-gen=true
type PatroniServicesSpec struct {
	Patroni               *Patroni                 `json:"patroni,omitempty"`
	BackupDaemon          *types.BackupDaemon      `json:"backupDaemon,omitempty"`
	MetricCollector       *types.MetricCollector   `json:"metricCollector,omitempty"`
	AuthSecret            string                   `json:"authSecret,omitempty"`
	IntegrationTests      *IntegrationTests        `json:"integrationTests,omitempty"`
	VaultRegistration     *types.VaultRegistration `json:"vaultRegistration,omitempty"`
	Policies              *Policies                `json:"policies,omitempty"`
	ServiceAccountName    string                   `json:"serviceAccountName,omitempty"`
	CloudSql              *types.CloudSql          `json:"cloudSql,omitempty"`
	SiteManager           *SiteManager             `json:"siteManager,omitempty"`
	PowaUI                PowaUI                   `json:"powaUI,omitempty"`
	ReplicationController ReplicationController    `json:"replicationController,omitempty"`
	Pooler                Pooler                   `json:"connectionPooler,omitempty"`
	Tracing               *Tracing                 `json:"tracing,omitempty"`
	ExternalDataBase      *ExternalDataBase        `json:"externalDataBase,omitempty"`
	PostgresExporter      *PostgresExporter        `json:"postgresExporter,omitempty"`
	QueryExporter         QueryExporter            `json:"queryExporter,omitempty"`
	Tls                   *Tls                     `json:"tls,omitempty"`
	PgBackRest            *PgBackRest              `json:"pgBackRest,omitempty"`
	InstallationTimestamp string                   `json:"installationTimestamp,omitempty"`
	PrivateRegistry       PrivateRegistry          `json:"privateRegistry,omitempty"`
}

type PrivateRegistry struct {
	Enabled bool     `json:"enabled,omitempty"`
	Names   []string `json:"names,omitempty"`
}

// PatroniServicesStatusConditions contains description of status of PatroniServices
// +k8s:openapi-gen=true
type PatroniServicesStatusCondition struct {
	Type               string `json:"type,omitempty"`
	Status             bool   `json:"status,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// PatroniServicesStatus defines the observed state of PatroniServices
// +k8s:openapi-gen=true
type PatroniServicesStatus struct {
	SiteManagerStatus SiteManagerStatus                `json:"siteManagerStatus,omitempty"`
	Conditions        []PatroniServicesStatusCondition `json:"conditions,omitempty"`
}

// SiteManagerStatus defines the observed state of Postgres SiteManager
// +k8s:openapi-gen=true
type SiteManagerStatus struct {
	Mode   string `json:"mode,omitempty"`
	Status string `json:"status,omitempty"`
	NoWait bool   `json:"no-wait,omitempty"`
}

//+kubebuilder:object:root=true

// PatroniServices is the Schema for the PatroniServicess API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type PatroniServices struct {
	metav1.TypeMeta   `json:",inline,omitempty"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              *PatroniServicesSpec  `json:"spec,omitempty"`
	Status            PatroniServicesStatus `json:"status,omitempty"`
	RunTestsTime      string                `json:"runTestsTime,omitempty"`
}

// +kubebuilder:object:root=true

// PatroniServicesList contains a list of PatroniServices
type PatroniServicesList struct {
	metav1.TypeMeta `json:",inline,omitempty"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PatroniServices `json:"items,omitempty"`
}

type SupplementaryPatroniServicessList struct {
	metav1.TypeMeta `json:",inline,omitempty"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PatroniServices `json:"items,omitempty"`
}

// Patroni contains description of Patroni-specific configuration of PatroniServices
// +k8s:openapi-gen=true
type Patroni struct {
	ClusterName string `json:"clusterName,omitempty"`
}

// Postgres contains description of Patroni-specific configuration of PatroniServices
// +k8s:openapi-gen=true
type Postgres struct {
	Host string `json:"host,omitempty"`
}

type PowaUI struct {
	Install         bool                    `json:"install,omitempty"`
	Image           string                  `json:"image,omitempty"`
	Resources       v1.ResourceRequirements `json:"resources,omitempty"`
	SecurityContext *v1.PodSecurityContext  `json:"securityContext,omitempty"`
	Affinity        v1.Affinity             `json:"affinity,omitempty"`
	PodLabels       map[string]string       `json:"podLabels,omitempty"`
	CookieSecret    string                  `json:"cookieSecret,omitempty"`
}

type ReplicationController struct {
	Install         bool                    `json:"install,omitempty"`
	Image           string                  `json:"image,omitempty"`
	Resources       v1.ResourceRequirements `json:"resources,omitempty"`
	SecurityContext *v1.PodSecurityContext  `json:"securityContext,omitempty"`
	Affinity        v1.Affinity             `json:"affinity,omitempty"`
	PodLabels       map[string]string       `json:"podLabels,omitempty"`
	SslMode         string                  `json:"sslMode,omitempty"`
}

type Pooler struct {
	Install         bool                         `json:"install,omitempty"`
	Image           string                       `json:"image,omitempty"`
	Resources       v1.ResourceRequirements      `json:"resources,omitempty"`
	SecurityContext *v1.PodSecurityContext       `json:"securityContext,omitempty"`
	Affinity        v1.Affinity                  `json:"affinity,omitempty"`
	Replicas        *int32                       `json:"replicas,omitempty"`
	PodLabels       map[string]string            `json:"podLabels,omitempty"`
	Config          map[string]map[string]string `json:"config,omitempty"`
}

type Tracing struct {
	Enabled bool   `json:"enabled,omitempty"`
	Host    string `json:"host,omitempty"`
}
type SiteManager struct {
	ActiveClusterHost         string                     `json:"activeClusterHost,omitempty"`
	ActiveClusterPort         int                        `json:"activeClusterPort,omitempty"`
	StandbyClusterHealthCheck *StandbyClusterHealthCheck `json:"standbyClusterHealthCheck,omitempty"`
}

//type StandbyCluster struct {
//	Host string `json:"host"`
//	Port int    `json:"port"`
//}

type StandbyClusterHealthCheck struct {
	RetriesLimit        int `json:"retriesLimit,omitempty"`
	FailureRetriesLimit int `json:"failureRetriesLimit,omitempty"`
	RetriesWaitTimeout  int `json:"retriesWaitTimeout,omitempty"`
}

type PostgresExporter struct {
	Install       bool           `json:"install,omitempty"`
	Uri           string         `json:"uri,omitempty"`
	CustomQueries *CustomQueries `json:"customQueries,omitempty"`
}

type QueryExporter struct {
	Install               bool                    `json:"install,omitempty"`
	Image                 string                  `json:"image,omitempty"`
	Resources             v1.ResourceRequirements `json:"resources,omitempty"`
	SecurityContext       *v1.PodSecurityContext  `json:"securityContext,omitempty"`
	Affinity              v1.Affinity             `json:"affinity,omitempty"`
	PodLabels             map[string]string       `json:"podLabels,omitempty"`
	PgHost                string                  `json:"pgHost,omitempty"`
	PgPort                int                     `json:"pgPort,omitempty"`
	MaxMasterConnections  int                     `json:"maxMasterConnections,omitempty"`
	MaxLogicalConnections int                     `json:"maxLogicalConnections,omitempty"`
	QueryTimeout          int                     `json:"queryTimeout,omitempty"`
	SelfMonitorDisabled   bool                    `json:"selfMonitorDisabled,omitempty"`
	CustomQueries         *CustomQueries          `json:"customQueries,omitempty"`
	ExcludeQueries        []string                `json:"excludeQueries,omitempty"`
	CollectionInterval    string                  `json:"collectionInterval,omitempty"`
	MaxFailedTimeouts     string                  `json:"maxFailedTimeouts,omitempty"`
}

type CustomQueries struct {
	Enabled        bool              `json:"enabled,omitempty"`
	NamespacesList []string          `json:"namespacesList,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
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

// ExternalDataBase defines the desired state of ExternalDataBase
// +k8s:openapi-gen=true
type ExternalDataBase struct {
	Type           string                       `json:"type,omitempty"`
	Project        string                       `json:"project,omitempty"`
	Instance       string                       `json:"instance,omitempty"`
	Port           int                          `json:"port,omitempty"`
	Region         string                       `json:"region,omitempty"`
	AuthSecretName string                       `json:"authSecretName,omitempty"`
	ConnectionName string                       `json:"connectionName,omitempty"`
	RestoreConfig  map[string]map[string]string `json:"restoreConfig,omitempty"`
}

type Policies struct {
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
}

type Tls struct {
	Enabled               bool   `json:"enabled,omitempty"`
	CertificateSecretName string `json:"certificateSecretName,omitempty"`
}

type PgBackRest struct {
	DockerImage       string         `json:"dockerImage,omitempty"`
	RepoType          string         `json:"repoType,omitempty"`
	RepoPath          string         `json:"repoPath,omitempty"`
	DiffSchedule      string         `json:"diffSchedule,omitempty"`
	IncrSchedule      string         `json:"incrSchedule,omitempty"`
	S3                S3             `json:"s3,omitempty"`
	Rwx               *types.Storage `json:"rwx,omitempty"`
	BackupFromStandby bool           `json:"backupFromStandby,omitempty"`
}

type S3 struct {
	Bucket    string `json:"bucket,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Key       string `json:"key,omitempty"`
	Secret    string `json:"secret,omitempty"`
	Region    string `json:"region,omitempty"`
	VerifySsl bool   `json:"verifySsl,omitempty"`
}

func init() {
	SchemeBuilder.Register(&PatroniServices{}, &PatroniServicesList{})
}
