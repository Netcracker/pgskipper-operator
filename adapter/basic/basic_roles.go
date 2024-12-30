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
	"context"
	"fmt"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"go.uber.org/zap"
)

const FeatureMultiUsers = "multiusers"
const FeatureTls = "tls"
const FeatureNotStrictTLS = "tlsNotStrict"

func (sa ServiceAdapter) IsMultiUsersEnabled() bool {
	return sa.features[FeatureMultiUsers]
}

func (sa ServiceAdapter) GetSupportedRoles() []string {
	if sa.IsMultiUsersEnabled() {
		return sa.roles
	}
	return []string{"admin"}
}

func (sa ServiceAdapter) CreateRoles(ctx context.Context, roles []dao.AdditionalRole) (success []dao.Success, failure *dao.Failure) {
	logger := util.ContextLogger(ctx)
	logger.Info("Roles creation started")
	var additionalRoleIdInProcess string

	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("error during additional roles creation %s", r))
			if failure == nil {
				failure = &dao.Failure{
					Id:      additionalRoleIdInProcess,
					Message: fmt.Sprintf("%s", r),
				}
			}
		}
	}()

	for _, additionalRole := range roles {
		existingRoles := make([]string, 0)
		additionalRoleIdInProcess = additionalRole.Id
		dbName := additionalRole.DbName
		logger.Info(fmt.Sprintf("Additional role in process id=%s, dbName=%s", additionalRoleIdInProcess, dbName))
		err := sa.createMetadata(ctx, dbName, sa.GetUser(), sa.GetPassword(), nil)
		if err != nil {
			return success, &dao.Failure{
				Id:      additionalRole.Id,
				Message: err.Error(),
			}
		}
		for _, connectionProperties := range additionalRole.ConnectionProperties {
			roleForCheck := connectionProperties["role"].(string) //TODO
			existingRoles = append(existingRoles, roleForCheck)

			// handle managed databases case
			username := connectionProperties["username"].(string)
			logger.Debug(fmt.Sprintf("Role %s is found with name %s, perform grants preparation", roleForCheck, username))

			if util.GetEnv("EXTERNAL_POSTGRESQL", "") != "" {
				if err := sa.grantRightsToPostgres(ctx, username); err != nil {
					logger.Error(fmt.Sprintf("Couldn't GRANT %s TO %s;", username, sa.GetUser()), zap.Error(err))
					return success, &dao.Failure{
						Id:      additionalRole.Id,
						Message: err.Error(),
					}
				}
			}

			if roleForCheck != "admin" {
				continue
			}

			err = sa.grantUserAndSave(ctx, dbName, username, roleForCheck)
			if err != nil {
				return success, &dao.Failure{
					Id:      additionalRole.Id,
					Message: err.Error(),
				}
			}
		}
		newConProps := make([]dao.ConnectionProperties, 0)
		newResources := make([]dao.DbResource, 0)
		for _, role := range sa.GetSupportedRoles() {
			if !util.Contains(existingRoles, role) {
				logger.Info(fmt.Sprintf("Role %s is not exist for database %s, creation...", role, dbName))
				userCreateRequest := dao.UserCreateRequest{
					DbName: dbName,
					Role:   role,
				}

				createdUser, err := sa.CreateUser(ctx, "", userCreateRequest)
				if err != nil {
					return success, &dao.Failure{
						Id:      additionalRole.Id,
						Message: err.Error(),
					}
				}

				newConProps = append(newConProps, createdUser.ConnectionProperties)
				newResources = append(newResources, createdUser.Resources...)
			}
		}
		success = append(success, dao.Success{
			Id:                   additionalRole.Id,
			ConnectionProperties: newConProps,
			Resources:            newResources,
			DbName:               dbName,
		})
	}
	return success, failure
}

func (sa ServiceAdapter) GrantUsersAccordingRoles(ctx context.Context, dbName string, users map[string]string, schemas []string) error {
	logger := util.ContextLogger(ctx)
	isExternalPg := util.IsExternalPostgreSQl()
	queries := make([]string, 0)
	isOwnerChanged := false
	metadataOwner, _ := sa.getCurrentUserForDb(ctx, dbName)
	logger.Debug(fmt.Sprintf("Metadata owner: %s", metadataOwner))
	adminUsers := []string{sa.GetUser()}
	RWUsers := make([]string, 0)
	ROUsers := make([]string, 0)

	for userName, role := range users {
		switch role {
		case "admin":
			adminUsers = append(adminUsers, userName)
		case "streaming":
			adminUsers = append(adminUsers, userName)
		case "rw":
			RWUsers = append(RWUsers, userName)
		case "ro":
			ROUsers = append(ROUsers, userName)
		}
	}

	// grant admin users for external database
	if isExternalPg {
		for _, adminUser := range adminUsers {
			if adminUser != sa.GetUser() {
				log.Debug(fmt.Sprintf("Grant %s role to %s query was added to queries list", adminUser, sa.GetUser()))
				queries = append(queries, grantUserToAdmin(adminUser, sa.GetUser()))
			}
		}
	}

	for _, schema := range schemas {
		queries = append(queries, revokeRights())

		for userName, role := range users {
			// grant connection to database
			queries = append(queries, grantConnectionToDB(dbName, userName))
			if util.Contains(sa.GetSupportedRoles(), role) {
				// grant schema usage
				queries = append(queries, grantSchemaUsage(schema, userName))
				// grant for already existing tables
				queries = append(queries, grantUserOnTablesQuery(schema, userName, role))
				// grant for already existing sequences
				queries = append(queries, grantUserOnSequencesQuery(schema, userName, role))
			}
		}

		for _, adminUserName := range adminUsers {
			// grant create and previous admin grants
			queries = append(queries, grantSchemaCreate(schema, adminUserName))
		}

		// change metadata owner in case owner was postgres
		if metadataOwner == sa.GetUser() {
			for _, adminUser := range adminUsers {
				// select future owner for metadata
				if adminUser != sa.GetUser() && users[adminUser] != "streaming" {
					metadataOwner = adminUser
					isOwnerChanged = true
					logger.Debug(fmt.Sprintf("New metadata owner: %s", metadataOwner))
					break
				}
			}
		}
		if isOwnerChanged {
			// altering owners for DB resources
			alterOperations := map[string]func(string, string, string) string{
				getTablesListQuery(schema, isExternalPg):      alterOwnerForTable,
				getSequenceListQuery(schema, isExternalPg):    alterOwnerForTable,
				getLargeObjectsListQuery(isExternalPg):        alterOwnerForLargeObject,
				getViewsListQuery(schema, isExternalPg):       alterViewOwnerQuery,
				getFunctionsListQuery(schema, isExternalPg):   alterFunctionOwnerQuery,
				getProceduresListQuery(schema, isExternalPg):  alterProcedureOwnerQuery,
				getCustomTypesListQuery(schema, isExternalPg): alterTypeOwnerQuery,
			}

			for resourceQuery, alterQuery := range alterOperations {
				alterOwnerQueries, errCh := sa.executeForResourceQueries(ctx, schema, dbName, metadataOwner, resourceQuery, alterQuery)
				if errCh != nil {
					return errCh
				}
				queries = append(queries, alterOwnerQueries...)
			}
		}

		if metadataOwner != "" {
			queries = append(queries, alterOwnerForSchema(schema, metadataOwner))
			queries = append(queries, alterDatabaseOwnerQuery(dbName, metadataOwner))
		}
	}

	for _, adminUserName := range adminUsers {
		// grant all database privileges
		if adminUserName != sa.GetUser() {
			queries = append(queries, grantAllRightsOnDatabase(dbName, adminUserName))

			// grant replication for admin
			queries = append(queries, AllowReplicationForUser(adminUserName))
		}

		// grant user if not metadata owner and owner is not postgres
		if metadataOwner != "" && adminUserName != metadataOwner && metadataOwner != sa.ClusterAdapter.GetUser() {
			queries = append(queries, setGrantAsForRole(metadataOwner, adminUserName))
		}

		// grant RO and RW for future tables and schemas created by admin
		for _, ROUserName := range ROUsers {
			queries = append(queries, setReadOnlyRoleDefaultGrants(adminUserName, ROUserName))
			queries = append(queries, setSchemasDefaultGrants(adminUserName, ROUserName))
			queries = append(queries, setSequencesRODefaultGrants(adminUserName, ROUserName))
		}
		for _, RWUserName := range RWUsers {
			queries = append(queries, setReadWriteRoleDefaultGrants(adminUserName, RWUserName))
			queries = append(queries, setSchemasDefaultGrants(adminUserName, RWUserName))
			queries = append(queries, setSequencesRWDefaultGrants(adminUserName, RWUserName))
		}
	}

	// should be the last operation to ensure all objects are owned by new owner
	queries = append(queries, alterOwnerMetaTable(metadataOwner))

	return sa.executeQueries(ctx, dbName, queries)
}

func (sa ServiceAdapter) executeQueries(ctx context.Context, dbName string, queries []string) error {
	logger := util.ContextLogger(ctx)
	sa.Mutex.Lock()
	defer sa.Mutex.Unlock()
	connDb, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return err
	}
	defer connDb.Close(ctx)
	for _, query := range queries {
		log.Debug(fmt.Sprintf("[%s] Query for exec: %s", dbName, query))
		if _, err = connDb.Exec(ctx, query); err != nil {
			logger.Error(fmt.Sprintf("cannot execute in database %s the query %s", dbName, query), zap.Error(err))
			return err
		}
	}

	return nil
}

func grantUserOnTablesQuery(schema, username, role string) string {
	var query string
	switch role {
	case "rw":
		query = setReadWriteRoleGrants(schema, username)
	case "ro":
		query = setReadOnlyRoleGrants(schema, username)
	default:
		query = setAdminGrants(schema, username)
	}
	return query
}

func grantUserOnSequencesQuery(schema, username, role string) string {
	var query string
	switch role {
	case "rw":
		query = setReadWriteRoleSequencesGrants(schema, username)
	case "ro":
		query = setReadOnlyRoleSequencesGrants(schema, username)
	default:
		query = setAdminRoleSequencesGrants(schema, username)
	}
	return query
}

func (sa ServiceAdapter) grantCreateOnSchema(ctx context.Context, dbName, schema, username string) error {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Grant create on schema %s for %s", schema, username))
	connDb, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return err
	}
	defer connDb.Close(ctx)
	_, err = connDb.Exec(ctx, grantSchemaCreate(schema, username))
	if err != nil {
		return err
	}
	return nil
}

func (sa ServiceAdapter) GrantAllOnSchemaToUser(ctx context.Context, dbName, schema, username string) error {
	grantUserSQL := fmt.Sprintf("GRANT ALL ON SCHEMA %s TO %s", schema, username)
	connDb, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return err
	}
	defer connDb.Close(ctx)
	_, err = connDb.Exec(ctx, grantUserSQL)
	if err != nil {
		return err
	}
	return nil
}

func (sa ServiceAdapter) validateRole(ctx context.Context, conn cluster.Conn, username, role string) (bool, error) {
	logger := util.ContextLogger(ctx)
	privilegeType := "TRUNCATE"
	switch role {
	case "ro":
		privilegeType = "SELECT"
	case "rw":
		privilegeType = "UPDATE"
	}
	rows, err := conn.Query(ctx, getMetadataGrantQuery(username, privilegeType))
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't get user %s", username), zap.Error(err))
		return false, err
	}
	defer rows.Close()
	isUserValid := rows.Next()

	return isUserValid, nil
}

func (sa ServiceAdapter) executeForResourceQueries(ctx context.Context, schema, dbName, userName, resourceQuery string, appliedQuery func(schema string, resource string, newOwner string) string) ([]string, error) {
	queries := make([]string, 0)
	resources, err := sa.getStringsFromQuery(ctx, dbName, resourceQuery)
	if err != nil {
		return nil, err
	}
	for _, resource := range resources {
		queries = append(queries, appliedQuery(schema, resource, userName))
	}
	return queries, nil
}

func (sa ServiceAdapter) getStringsFromQuery(ctx context.Context, dbName, query string) ([]string, error) {
	logger := util.ContextLogger(ctx)
	conn, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	log.Debug(fmt.Sprintf("[%s] Resource query for exec: %s", dbName, query))
	rows, err := conn.Query(ctx, query)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to perform in db %s the next query: %s", dbName, query), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	resultStrings := make([]string, 0)
	for rows.Next() {
		currentString := ""
		err = rows.Scan(&currentString)
		if err != nil {
			logger.Error(fmt.Sprintf("failed to scan string from db %s", dbName), zap.Error(err))
			return nil, err
		}
		resultStrings = append(resultStrings, currentString)
	}
	return resultStrings, nil
}

func (sa *ServiceAdapter) grantUserAndSave(ctx context.Context, dbName, username, role string) error {
	logger := util.ContextLogger(ctx)
	metadata, err := sa.GetMetadataInternal(ctx, dbName)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot get metadata for database %s", dbName), zap.Error(err))
		return err
	}
	rolesMap, err := sa.GetValidRolesFromMetadata(ctx, dbName, metadata)
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't read valid roles from metadata for db %s", dbName), zap.Error(err))
		return err
	}
	rolesMap[username] = role

	err = sa.GrantUsersForAllSchemas(ctx, dbName, rolesMap)
	return err
}

func (sa *ServiceAdapter) GrantUsersForAllSchemas(ctx context.Context, dbName string, rolesMap map[string]string) error {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Grant roles on all schemas for %s", dbName))
	if err := sa.saveRolesInMetadata(ctx, dbName, rolesMap); err == nil {
		schemasList, err := sa.GetSchemas(dbName)
		if err != nil {
			return err
		}
		logger.Debug(fmt.Sprintf("Schemas list: %s", schemasList))
		err = sa.GrantUsersAccordingRoles(context.Background(), dbName, rolesMap, schemasList)
		if err != nil {
			return err
		}
	} else {
		logger.Error(fmt.Sprintf("can't save roles in metadata for %s", dbName))
		return err
	}
	return nil
}

func (sa *ServiceAdapter) grantRightsToPostgres(ctx context.Context, user string) error {

	conn, err := sa.GetConnection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, grantUserToAdmin(user, sa.GetUser()))

	if err != nil {
		return err
	}

	return nil
}
