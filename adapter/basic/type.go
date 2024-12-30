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

package basic

import (
	"sync"

	"go.uber.org/zap"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
)

type ServiceAdapter struct {
	cluster.ClusterAdapter
	Mutex *sync.Mutex
	*ExtensionConfig
	dao.ApiVersion
	roles    []string
	features map[string]bool
	Generator
	log *zap.Logger
}

type ExtensionConfig struct {
	DefaultExt        []string
	ExtUpdateRequired bool
}

type PostgresUpdateSettingsResult struct {
	SettingUpdateResult map[string]SettingUpdateResult `json:"settingUpdateResult"`
}

type PostgresUpdateSettingsRequest struct {
	CurrentSettings map[string]interface{} `json:"currentSettings"`
	NewSettings     map[string]interface{} `json:"newSettings"`
}

type SettingUpdateResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type DbInfo struct {
	Owner string `json:"owner"`
}
