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

// +kubebuilder:object:generate=true
// +groupName=netcracker.com

// Storage Describes Storage that will be used by patroni
type Storage struct {
	// +kubebuilder:validation:Pattern=`^[0-9]+(m|Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)$`
	Size         string   `json:"size,omitempty"`
	Type         string   `json:"type,omitempty"`
	StorageClass string   `json:"storageClass,omitempty"`
	Volumes      []string `json:"volumes,omitempty"`
	Nodes        []string `json:"nodes,omitempty"`
	Selectors    []string `json:"selectors,omitempty"`
	AccessModes  []string `json:"accessModes,omitempty"`
}
type CloudSql struct {
	Project        string `json:"project,omitempty"`
	Instance       string `json:"instance,omitempty"`
	AuthSecretName string `json:"authSecretName,omitempty"`
}
