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
	"fmt"
	"sync"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/basic"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/service"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

const (
	MigrationKey = "isMigrationPerformed"
	limitPool    = 10
)

var excludedDatabases = []string{"template0", "template1", "postgres", "powa", "rdsadmin", "cloudsqladmin", "azure_maintenance", "azure_sys"}
var wg sync.WaitGroup

// PerformMigration Set replication privileges on admin user for each database
func PerformMigration(dbAdministration service.DbAdministration) {
	adapter := dbAdministration.(*basic.ServiceAdapter)

	if isEnabled := adapter.GetFeatures()[basic.FeatureMultiUsers]; !isEnabled {
		log.Info("Multiusers feature is disabled or api version is v1, skip migration...")
		return
	}
	log.Info("Migration will be performed for all roles")
	databases := adapter.GetDatabases(context.Background())
	ctx := context.Background()
	connCount := 0
	for _, database := range databases {
		wg.Add(1)
		connCount++
		go processDatabaseForMigration(ctx, adapter, database)
		if connCount >= limitPool {
			wg.Wait()
			connCount = 0
		}
	}
	wg.Wait()
}

func processDatabaseForMigration(ctx context.Context, adapter *basic.ServiceAdapter, database string) {
	defer wg.Done()
	if isExcludedDatabase(database) {
		return
	}

	// Prepare metadata
	originalMetadata, err := adapter.GetMetadataInternal(ctx, database)
	if err != nil {
		log.Error(fmt.Sprintf("cannot get metadata for %s", database), zap.Error(err))
		return
	}
	if _, ok := originalMetadata[basic.RolesVersionKey]; !ok {
		originalMetadata[basic.RolesVersionKey] = 0.0
	}

	if originalMetadata[basic.RolesKey] == nil {
		log.Warn(fmt.Sprintf("roles are empty for %s database, migration skipped...", database))
		return
	}

	metadata := cloneMetadata(originalMetadata)

	if isMigrationRequiredForDb(metadata) {
		migrationPerformed := performAlteringRoles(ctx, adapter, database, metadata)
		if migrationPerformed {
			updateRolesVersionInMetadata(ctx, adapter, database, originalMetadata)
		} else {
			err := adapter.UpdateMetadataInternal(ctx, originalMetadata, database)
			if err != nil {
				log.Error(fmt.Sprintf("cannot save original metadata for %s", database), zap.Error(err))
			}
			log.Error(fmt.Sprintf("Migration is not fully performed for %s", database))
		}
	} else {
		log.Info(fmt.Sprintf("Database roles migration is not required for %s", database))
	}
}

func cloneMetadata(metadataOrig map[string]interface{}) map[string]interface{} {
	metadata := maps.Clone(metadataOrig)
	metadata[basic.RolesKey] = maps.Clone(metadataOrig[basic.RolesKey].(map[string]interface{}))
	return metadata
}

func isExcludedDatabase(database string) bool {
	for _, excDatname := range excludedDatabases {
		if database == excDatname {
			return true
		}
	}
	return false
}

func updateRolesVersionInMetadata(ctx context.Context, adapter *basic.ServiceAdapter, database string, metadata map[string]interface{}) {
	metadata[basic.RolesVersionKey] = util.GetRolesVersion()
	err := adapter.UpdateMetadataInternal(ctx, metadata, database)
	if err != nil {
		log.Error(fmt.Sprintf("cannot save original metadata for %s", database), zap.Error(err))
	}
	log.Info(fmt.Sprintf("Roles successfully updated for database %s", database))
}

func isMigrationRequiredForDb(metadata map[string]interface{}) bool {
	if rolesVersionInterface, ok := metadata[basic.RolesVersionKey]; ok {
		return rolesVersionInterface.(float64) < util.GetRolesVersion()
	}
	return true
}

func performAlteringRoles(ctx context.Context, dbAdministration *basic.ServiceAdapter, database string, metadata map[string]interface{}) bool {
	log.Info(fmt.Sprintf("Start migration for database %s", database))
	roles := dbAdministration.GetRolesFromMetadata(metadata)
	validRoles, err := dbAdministration.GetValidRolesFromMetadata(ctx, database, metadata)
	if err != nil {
		log.Warn(fmt.Sprintf("Can't obtain roles for database %s", database))
		return false
	}

	err = dbAdministration.GrantUsersForAllSchemas(ctx, database, validRoles)
	if err != nil {
		log.Warn(fmt.Sprintf("cannot perform roles grant for database %s", database), zap.Error(err))
		return false
	}
	return maps.Equal(roles, validRoles)
}
