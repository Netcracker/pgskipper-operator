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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	PatroniPgTypeLabelKey = "pgtype"
	PatroniClusterLabelKey  = "pgcluster"

	// PatroniRoleMaster is the leader pod label value used by Patroni < 4.x.
	PatroniRoleMaster = "master"
	// PatroniRolePrimary is the leader pod label value used by Patroni 4.x+.
	PatroniRolePrimary = "primary"
	PatroniRoleReplica = "replica"
)

var PatroniPrimaryRoleLabelValues = []string{PatroniRoleMaster, PatroniRolePrimary}

func IsPatroniPrimaryPgType(pgType string) bool {
	return pgType == PatroniRoleMaster || pgType == PatroniRolePrimary
}

func IsPatroniPrimaryRole(role string) bool {
	return role == PatroniRoleMaster || role == PatroniRolePrimary
}

func PatroniMasterLabelSelector(clusterName string) map[string]string {
	return map[string]string{
		PatroniPgTypeLabelKey: PatroniRolePrimary,
		PatroniClusterLabelKey: clusterName,
	}
}

func PatroniReplicasLabelSelector(clusterName string) map[string]string {
	return map[string]string{
		PatroniPgTypeLabelKey: PatroniRoleReplica,
		PatroniClusterLabelKey: clusterName,
	}
}

func PatroniPrimaryPodLabelSelector(clusterName string) labels.Selector {
	selector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      PatroniPgTypeLabelKey,
				Operator: metav1.LabelSelectorOpIn,
				Values:   PatroniPrimaryRoleLabelValues,
			},
		},
	}
	if clusterName != "" {
		selector.MatchLabels = map[string]string{PatroniClusterLabelKey: clusterName}
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return labels.SelectorFromSet(PatroniMasterLabelSelector(clusterName))
	}
	return parsed
}

func PatroniPrimaryPodListOptions(clusterName string) []client.ListOption {
	return []client.ListOption{
		client.InNamespace(GetNameSpace()),
		client.MatchingLabelsSelector{Selector: PatroniPrimaryPodLabelSelector(clusterName)},
	}
}

func PatroniPrimaryServiceSelector(clusterName string) map[string]string {
	return PatroniMasterLabelSelector(clusterName)
}

func UsesPatroniPrimaryPgTypeSelector(selectors map[string]string) bool {
	pgType, ok := selectors[PatroniPgTypeLabelKey]
	return ok && IsPatroniPrimaryPgType(pgType)
}
