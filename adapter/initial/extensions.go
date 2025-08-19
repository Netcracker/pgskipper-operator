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

package initial

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/basic"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/service"
	"go.uber.org/zap"
)

const SelectDbUsers = "SELECT USENAME FROM pg_catalog.pg_user where USENAME <> $1;"

func UpdateExtensions(adAdministration service.DbAdministration) {
	adapter := adAdministration.(*basic.ServiceAdapter)

	if !adapter.ExtUpdateRequired || len(adapter.DefaultExt) == 0 {
		return
	}

	log.Info(fmt.Sprintf("Extensions %s will be applied to all databases", adapter.DefaultExt))
	valid, err := adapter.ValidatePostgresAvailableExtensions(context.Background(), adapter.DefaultExt)
	if err != nil || !valid {
		return
	}

	databases := adapter.GetDatabases(context.Background())
	ctx := context.Background()
	for _, database := range databases {
		log.Debug(fmt.Sprintf("Create extensions for %s", database))
		createDatabaseExt(adapter, ctx, database)
	}

	updateExtensionsCM()
}

func createDatabaseExt(adapter *basic.ServiceAdapter, ctx context.Context, database string) {
	conn, err := adapter.GetConnectionToDb(context.Background(), database)
	if err != nil {
		log.Warn(fmt.Sprintf("Database %s has been skipped for extensions update", database))
		return
	}
	defer conn.Close()
	log.Info(fmt.Sprintf("Create extensions for \"%s\" database", database))

	username := ""
	if util.Contains(adapter.DefaultExt, basic.OracleFdw) {
		username = findUserName(ctx, conn, database)
	}

	basic.CreateExtFromSlice(ctx, conn, username, adapter.DefaultExt)
}

func findUserName(ctx context.Context, conn cluster.Conn, database string) string {
	userName := ""
	row := conn.QueryRow(ctx, SelectDbUsers)
	err := row.Scan(&userName)
	if err != nil {
		log.Warn(fmt.Sprintf("Error during get owner for db %s", database), zap.Error(err))
	}

	return userName
}

func updateExtensionsCM() {
	log.Debug("Update extensions CM")
	names := []string{basic.ExtensionConfigNameNew, basic.ExtensionConfigNameOld}

	for _, name := range names {
		cm := getCM(name)
		if cm == nil {
			continue
		}

		data := cm.Data
		if data == nil {
			data = map[string]string{}
		}

		extensionsData := data[basic.ExtensionName]

		var extensionMap map[string]interface{}
		err := json.Unmarshal([]byte(extensionsData), &extensionMap)
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to parse extensions file %s", basic.ExtensionPath+basic.ExtensionName), zap.Error(err))
			extensionMap = make(map[string]interface{})
		}

		extensionMap[basic.UpdateRequiredKey] = false

		updatedMap, err := json.Marshal(extensionMap)
		if err != nil {
			log.Warn("Failed to marshal updated result for extensions map")
		}

		data[basic.ExtensionName] = string(updatedMap)

		cm.Data = data
		updateCM(cm)
	}
}
