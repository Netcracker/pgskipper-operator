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
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"

	uuid "github.com/satori/go.uuid"
	"go.uber.org/zap"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	coreUtils "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
)

const (
	DbKind             = "database"
	UserKind           = "user"
	DeletedStatus      = "DELETED"
	DeleteFailedStatus = "DELETE_FAILED"
	BadRequest         = "BAD_REQUEST"
	Failed             = "FAILED"
	Skipped            = "SKIPPED"
	Successful         = "SUCCESSFUL"

	ExtensionPath       = "/app/extensions/"
	ExtensionConfigName = "dbaas-postgres-adapter.extensions-config"
	ExtensionName       = "dbaas.default_extensions.json"

	UpdateRequiredKey     = "updateRequired"
	ExtensionsKey         = "extensions"
	Metadata              = "metadata"
	RolesVersionKey       = "rolesVersion"
	RolesKey              = "roles"
	DefaultPrefix         = "dbaas"
	CreationParametersKey = "dbCreationParameters"
	PGExtensionsKey       = "pgExtensions"
)

var (
	log           = util.GetLogger()
	defaultSchema = "public"
	DbaasMetadata = "_DBAAS_METADATA"
)

type PgSettings struct {
	Schemas            []string
	CreationParameters map[string]string
	Extensions         []string
	NotSupported       []string
}

type Generator interface {
	Generate() string
}

type UUIDGenerator struct{}

func NewServiceAdapter(clusterAdapter cluster.ClusterAdapter, version dao.ApiVersion, roles []string, features map[string]bool) *ServiceAdapter {

	extensionConfig := readDefaultExtFile()

	log.Debug(fmt.Sprintf("Extension config: %v", *extensionConfig))

	return &ServiceAdapter{
		ClusterAdapter:  clusterAdapter,
		Mutex:           &sync.Mutex{},
		ExtensionConfig: extensionConfig,
		ApiVersion:      version,
		roles:           roles,
		features:        features,
		Generator:       UUIDGenerator{},
		log:             util.GetLogger(),
	}
}

func (sa ServiceAdapter) GetVersion() dao.ApiVersion {
	return sa.ApiVersion
}

func (sa ServiceAdapter) validateSettings(ctx context.Context, settings map[string]interface{}) error {
	if settings != nil && settings["pgExtensions"] != nil {
		extensions, err := sa.getPostgresAvailableExtensions(ctx)
		if err != nil {
			return err
		}

		for _, requestExtension := range util.GetStringArrayFromInterface(settings["pgExtensions"]) {
			if !util.Contains(extensions, requestExtension) {
				return fmt.Errorf("request contains not valid Postgres extensions. %s", requestExtension)
			}
		}
	}
	return nil
}

func (sa ServiceAdapter) getPostgresAvailableExtensions(ctx context.Context) ([]string, error) {
	logger := util.ContextLogger(ctx)
	conn, err := sa.GetConnection(ctx)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	rows, _ := conn.Query(ctx, selectPostgresAvailableExtensions)

	var extensions []string
	for rows.Next() {
		var extension string
		err = rows.Scan(&extension)
		if err != nil {
			logger.Error("Error occurred during obtain extension rows", zap.Error(err))
			return nil, err
		}

		extensions = append(extensions, extension)
	}
	return extensions, nil
}

func (sa ServiceAdapter) dropDatabase(ctx context.Context, dbName string) {
	logger := util.ContextLogger(ctx)
	conn, err := sa.GetConnection(ctx)
	if err != nil {
		return
	}

	defer conn.Close()

	_, err = conn.Exec(ctx, dropDatabase(dbName))
	if err != nil {
		logger.Error("Failed to clear up db resource", zap.Error(err))
	}
}

func (sa ServiceAdapter) dropUsers(ctx context.Context, users map[string]string) {
	for user := range users {
		sa.dropUser(ctx, user)
	}
}

func (sa ServiceAdapter) dropUser(ctx context.Context, username string) {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Perform drop for user %s", username))
	conn, err := sa.GetConnection(ctx)
	if err != nil {
		return
	}

	defer conn.Close()

	_, err = conn.Exec(ctx, dropUser(username))

	if err != nil {
		logger.Error("Failed to clear up user resource", zap.Error(err))
	}
}

func (sa ServiceAdapter) GetFeatures() map[string]bool {
	return sa.features
}

func (sa ServiceAdapter) CreateDatabase(ctx context.Context, requestOnCreateDb dao.DbCreateRequest) (string, *dao.LogicalDatabaseDescribed, error) {
	logger := util.ContextLogger(ctx)
	logger.Info("Start database creation")

	var dbName string

	metadata := requestOnCreateDb.Metadata

	// if client provided its own dbName, userName or password then use them, otherwise - generate random ones
	if requestOnCreateDb.DbName != "" {
		dbName = requestOnCreateDb.DbName
	} else if requestOnCreateDb.NamePrefix != nil {
		if !validateDbIdentifierParam(ctx, "namePrefix", *requestOnCreateDb.NamePrefix, DbPrefixPattern) || *requestOnCreateDb.NamePrefix == "" {
			return "", nil, fmt.Errorf("namePrefix must comply to the pattern %s", DbPrefixPattern)
		}
		dbName = coreUtils.RegenerateDbName(*requestOnCreateDb.NamePrefix, util.GetPgDBLength())
	} else {
		namespace, msName, err := coreUtils.GetNsAndMsName(metadata)
		if err != nil {
			return "", nil, err
		}
		dbGeneratedName, err := coreUtils.PrepareDatabaseName(namespace, msName, util.GetPgDBLength())
		if err != nil {
			logger.Error("error during database name preparation", zap.Error(err))
			panic(err)
		}
		dbName = dbGeneratedName
	}

	if !validateDbIdentifierParam(ctx, "dbName", dbName, DbIdentifiersPattern) {
		return "", nil, fmt.Errorf("dbName must comply to the pattern %s", DbIdentifiersPattern)
	}

	if !validateDbIdentifierParam(ctx, "username", requestOnCreateDb.Username, DbIdentifiersPattern) {
		return "", nil, fmt.Errorf("userName must comply to the pattern %s", DbIdentifiersPattern)
	}

	err := sa.validateSettings(ctx, requestOnCreateDb.Settings)
	if err != nil {
		logger.Error("Error occurred during validate extensions", zap.Error(err))
		return "", nil, err
	}

	logger.Debug(fmt.Sprintf("Database name: %s", dbName))

	conn, err := sa.GetConnection(ctx)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	pgSettings, err := sa.getPgSettings(ctx, requestOnCreateDb.Settings)
	if err != nil {
		return "", nil, err
	}
	logger.Debug(fmt.Sprintf("Result settings: %s", pgSettings))

	_, err = conn.Exec(ctx, createDatabase(dbName, pgSettings.CreationParameters))
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't create DB %s", dbName), zap.Error(err))
		panic(err)
	}

	users := make(map[string]string)

	var username string
	var password string
	var adminUser string
	var adminPassword string

	connectionProps := make([]dao.ConnectionProperties, 0)
	resources := []dao.DbResource{{Kind: DbKind, Name: dbName}}

	if sa.GetVersion() == "v1" {
		if requestOnCreateDb.Username != "" {
			username = requestOnCreateDb.Username
		} else {
			username = fmt.Sprintf("dbaas_%s", sa.Generate())
		}

		if requestOnCreateDb.Password != "" {
			password = requestOnCreateDb.Password
		} else {
			password = sa.Generate()
		}

		adminUser = username
		adminPassword = password

		_, err = conn.Exec(ctx, createUser(username, password))
		if err == nil {
			users[username] = ""
			if util.GetEnv("EXTERNAL_POSTGRESQL", "") != "" {
				_, err = conn.Exec(ctx, grantUserToAdmin(username, sa.GetUser()))
				if err != nil {
					logger.Error(fmt.Sprintf("Couldn't GRANT %s TO %s;", username, sa.GetUser()), zap.Error(err))
					sa.dropDatabase(ctx, dbName)
					sa.dropUser(ctx, username)
					return "", nil, err
				}
			}
			_, err = conn.Exec(ctx, grantAllRightsOnDatabase(dbName, username))
			if err != nil {
				logger.Error(fmt.Sprintf("Cannot grant User %s", username), zap.Error(err))
				sa.dropDatabase(ctx, dbName)
				sa.dropUsers(ctx, users)
				return "", nil, err
			}

			err = sa.createMetadata(ctx, dbName, adminUser, adminPassword, metadata)
			if err != nil {
				sa.dropDatabase(ctx, dbName)
				sa.dropUser(ctx, adminUser)
				return "", nil, err
			}

			connectionProps = append(connectionProps, sa.getConnectionProperties(dbName, username, "", password))
			resources = append(resources, dao.DbResource{Kind: UserKind, Name: username})
		}
	} else {
		for _, role := range sa.GetSupportedRoles() {
			username = fmt.Sprintf("dbaas_%s", sa.Generate())
			password = sa.Generate()

			if role == "admin" {
				if requestOnCreateDb.Username != "" {
					username = requestOnCreateDb.Username
				}
				if requestOnCreateDb.Password != "" {
					password = requestOnCreateDb.Password
				}
				adminUser = username
				adminPassword = password
			}
			logger.Debug(fmt.Sprintf("Create user %s with role %s", username, role))
			_, err = conn.Exec(ctx, createUser(username, password))
			if err != nil {
				panic(err)
			}
			extDB := util.GetEnv("EXTERNAL_POSTGRESQL", "")
			if extDB != "" {
				logger.Debug(fmt.Sprintf("External database is %s, granting user to admin", extDB))
				_, err = conn.Exec(ctx, grantUserToAdmin(username, sa.GetUser()))
				if err != nil {
					logger.Error(fmt.Sprintf("Couldn't GRANT %s TO %s;", username, sa.GetUser()), zap.Error(err))
					sa.dropDatabase(ctx, dbName)
					sa.dropUser(ctx, username)
					return "", nil, err
				}
			}
			users[username] = role
			connectionProps = append(connectionProps, sa.getConnectionProperties(dbName, username, role, password))
			metadata[RolesKey] = users

			resources = append(resources, dao.DbResource{Kind: UserKind, Name: username})
		}

		err = sa.GrantAllOnSchemaToUser(ctx, dbName, defaultSchema, adminUser)
		if err != nil {
			fmt.Printf("Error granting CREATE privilege on public schema to %s: %v\n", adminUser, err)
		} else {
			fmt.Printf("Successfully granted ALL privilege on public schema to %s\n", adminUser)
		}

		// explicitely providing create grants for PG15
		err = sa.grantCreateOnSchema(ctx, dbName, defaultSchema, adminUser)
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't create User %s", username), zap.Error(err))
			sa.dropDatabase(ctx, dbName)
			sa.dropUsers(ctx, users)
			return "", nil, err
		}

		err = sa.createMetadata(ctx, dbName, adminUser, adminPassword, metadata)
		if err != nil {
			sa.dropDatabase(ctx, dbName)
			sa.dropUser(ctx, username)
			return "", nil, err
		}

		// grant roles for all schemas

		err = sa.GrantUsersForAllSchemas(ctx, dbName, users)
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't create User %s", username), zap.Error(err))
			sa.dropDatabase(ctx, dbName)
			sa.dropUsers(ctx, users)
			return "", nil, err
		}
	}

	sa.createExtensions(dbName, adminUser, pgSettings.Extensions)

	logger.Info(fmt.Sprintf("Created db resources: %+v", resources))
	// NO NAME Name: dbName, todo
	response := &dao.LogicalDatabaseDescribed{ConnectionProperties: connectionProps, Resources: resources}
	return dbName, response, nil
}

func getUsernameWithoutHost(username string) string {
	return strings.Split(username, "@")[0]
}

func (sa ServiceAdapter) enableDatabase(ctx context.Context, dbName string) {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Enable database %s", dbName))
	conn, err := sa.GetConnection(ctx)
	if err != nil {
		return
	}

	defer conn.Close()
	extDB := util.GetEnv("EXTERNAL_POSTGRESQL", "")
	if extDB != "" {
		logger.Debug(fmt.Sprintf("Enable database for external DB: %s", extDB))
		_, err = conn.Exec(context.Background(), allowConnectionsToDbExt(dbName))
	} else {
		_, err = conn.Exec(context.Background(), allowConnectionsToDb(dbName))
	}

	if err != nil {
		logger.Error(fmt.Sprintf("Failed to enable database %s after restoration, skip it", dbName), zap.Error(err))
		return
	}
}

func (sa ServiceAdapter) CreateUser(ctx context.Context, username string, postgresUserRequest dao.UserCreateRequest) (*dao.CreatedUser, error) {

	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Creation of user with role %s started", postgresUserRequest.Role))
	prefix := postgresUserRequest.UsernamePrefix
	if username == "" {
		if prefix == "" {
			prefix = DefaultPrefix
		}
		username = fmt.Sprintf("%s_%s", prefix, sa.Generate())
	} else {
		if prefix != "" {
			prefix = prefix + sa.GetDBPrefixDelimiter()
		}
		username = fmt.Sprintf("%s%s", prefix, getUsernameWithoutHost(username))
	}
	logger.Debug(fmt.Sprintf("User name with prefix: %s", username))
	if !validateDbIdentifierParam(ctx, "username", username, DbIdentifiersPattern) {
		return nil, fmt.Errorf("username must comply to the pattern %s", DbIdentifiersPattern)
	}

	if postgresUserRequest.DbName != "" && !validateDbIdentifierParam(ctx, "dbName", postgresUserRequest.DbName, DbIdentifiersPattern) {
		return nil, fmt.Errorf("dbName must comply to the pattern %s", DbIdentifiersPattern)
	}

	version := sa.GetVersion()
	if version == "v2" && !(util.Contains(sa.GetSupportedRoles(), postgresUserRequest.Role) || postgresUserRequest.Role == "none") {
		return nil, fmt.Errorf("unsupported role. Role should be one of the list %s", sa.GetSupportedRoles())
	}
	if version == "v2" && (postgresUserRequest.Role == "none" && postgresUserRequest.DbName == "") {
		return nil, fmt.Errorf("user with empty role can be created for defined database name. Specify dbName parameter in request")
	}

	var resources []dao.DbResource

	userCreated := false
	password := postgresUserRequest.Password

	if password == "" {
		password = sa.Generate()
	}

	conn, err := sa.GetConnection(ctx)
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	isUserExist, err := sa.isUserExist(ctx, conn, username)
	if err != nil {
		return nil, err
	}

	if !isUserExist {
		logger.Info(fmt.Sprintf("User %s is not exist, creation...", username))
		_, err = conn.Exec(ctx, createUser(username, password))
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't create user %s", username), zap.Error(err))
			panic(err)
		}

		extPostgres := util.GetEnv("EXTERNAL_POSTGRESQL", "")
		if extPostgres != "" {
			logger.Debug(fmt.Sprintf("external database: %s, grant user to admin", extPostgres))
			_, err = conn.Exec(ctx, grantUserToAdmin(username, sa.GetUser()))
			if err != nil {
				logger.Error(fmt.Sprintf("User %s not created! Couldn't GRANT %s TO %s;", username, username, sa.GetUser()), zap.Error(err))
				sa.dropUser(ctx, username)
				panic(err)
			}
		}
		userCreated = true
	} else {
		logger.Info(fmt.Sprintf("Change password for existing user %s", username))
		_, err = conn.Exec(ctx, changeUserPassword(username, password))
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't update user %s", username), zap.Error(err))
			panic(err)
		}
	}

	dbName := postgresUserRequest.DbName
	if dbName != "" {
		if version == "v1" {
			_, err = conn.Exec(ctx, grantAllRightsOnDatabase(dbName, username))
		} else {
			err = sa.grantUserAndSave(ctx, dbName, username, postgresUserRequest.Role)
			if err != nil {
				if userCreated {
					sa.dropUser(ctx, username)
				}

				logger.Error(fmt.Sprintf("Couldn't update metadata for user %s", username), zap.Error(err))
			}
		}

		if err != nil {
			if userCreated {
				sa.dropUser(ctx, username)
			}
			logger.Error(fmt.Sprintf("Couldn't update grants for user %s", username), zap.Error(err))
			panic(err)
		}

		resources = append(resources, dao.DbResource{
			Kind: DbKind,
			Name: dbName,
		})

		sa.enableDatabase(ctx, dbName)
		if version == "v1" {
			if err = sa.reassignGrantsToNewUserForDb(ctx, dbName, username); err != nil {
				if userCreated {
					sa.dropUser(ctx, username)
				}
				panic(err)
			}
		}
	}

	connectionProperties := sa.getConnectionProperties(dbName, username, postgresUserRequest.Role, password)
	resources = append(resources, dao.DbResource{
		Kind: UserKind,
		Name: username,
	})

	response := &dao.CreatedUser{
		ConnectionProperties: connectionProperties,
		Name:                 dbName,
		Resources:            resources,
		//Created:              userCreated, todo
	}
	return response, nil
}

func (sa ServiceAdapter) isUserExist(ctx context.Context, conn cluster.Conn, username string) (bool, error) {
	logger := util.ContextLogger(ctx)
	rows, err := conn.Query(ctx, getUser, username)
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't get user %s", username), zap.Error(err))
		return false, err
	}
	defer rows.Close()

	return rows.Next(), nil
}

func (sa ServiceAdapter) createExtensions(dbName string, username string, extensions []string) {
	if username == "" {
		sa.log.Warn("Can't create extensions. Username is Empty")
		return
	}
	ctx := context.Background()
	conn, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return
	}

	defer conn.Close()

	if len(extensions) > 0 {
		sa.log.Debug(fmt.Sprintf("Create extensions %s", extensions))
		CreateExtFromSlice(ctx, conn, username, extensions)
	}

	if sa.DefaultExt != nil {
		sa.log.Debug(fmt.Sprintf("Create default extensions %s", sa.DefaultExt))
		CreateExtFromSlice(ctx, conn, username, sa.DefaultExt)
	}
}

func (sa ServiceAdapter) createMetadata(ctx context.Context, dbName string, username string, password string, metadata map[string]interface{}) error {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Metadata creation initiated for %s", dbName))

	username = getUsernameWithoutHost(username)
	conn, err := sa.GetConnectionToDbWithUser(ctx, dbName, username, password)
	if err != nil {
		return err
	}

	defer conn.Close()

	_, err = conn.Exec(ctx, createMetaTable)
	if err != nil {
		logger.Error("Couldn't create metadata table", zap.Error(err))
		return err
	}

	if len(metadata) > 0 {
		metadata[RolesVersionKey] = util.GetRolesVersion()
		logger.Debug(fmt.Sprintf("Insert metadata to %s for %s", DbaasMetadata, dbName))
		metadataJson, errParse := json.Marshal(metadata)
		if errParse != nil {
			logger.Error("Error during marshal metadata", zap.Error(err))
			return errParse
		}

		_, err = conn.Exec(ctx, insertIntoMetaTable, Metadata, metadataJson)
		if err != nil {
			logger.Error("Couldn't insert data in metadata table", zap.Error(err))
			return err
		}

		_, err = conn.Exec(ctx, alterOwnerMetaTable(username))
		if err != nil {
			logger.Error("Couldn't change owner for metadata table", zap.Error(err))
			return err
		}
	}

	return nil
}

func (sa ServiceAdapter) GetDBPrefixDelimiter() string {
	return "_"
}

func (sa ServiceAdapter) GetDBPrefix() string {
	return "dbaas"
}

func (sa ServiceAdapter) GetDefaultCreateRequest() dao.DbCreateRequest {
	return dao.DbCreateRequest{}
}

func (sa ServiceAdapter) GetDefaultUserCreateRequest() dao.UserCreateRequest {
	return dao.UserCreateRequest{}
}

func (sa ServiceAdapter) PreStart() {}

func (sa ServiceAdapter) UpdateMetadata(ctx context.Context, metadata map[string]interface{}, dbName string) {
	metadataOld, err := sa.GetMetadataInternal(ctx, dbName)
	if err != nil {
		log.Warn(fmt.Sprintf("cannot get metadata from %s", dbName))
	} else {
		if roles := sa.GetRolesFromMetadata(metadataOld); len(roles) > 0 {
			metadata[RolesKey] = roles
		}
		if _, ok := metadataOld[RolesVersionKey]; ok {
			metadata[RolesVersionKey] = metadataOld[RolesVersionKey]
		}
	}
	err = sa.UpdateMetadataInternal(ctx, metadata, dbName)
	if err != nil {
		panic(err)
	}
}

func (sa ServiceAdapter) UpdateMetadataInternal(ctx context.Context, metadata map[string]interface{}, dbName string) error {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Update metadata initiated for %s", dbName))
	if !validateDbIdentifierParam(ctx, "dbName", dbName, DbIdentifiersPattern) {
		return fmt.Errorf("dbName must comply to the pattern %s", DbIdentifiersPattern)
	}

	conn, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, ok := metadata[RolesVersionKey]; !ok {
		metadata[RolesVersionKey] = util.GetRolesVersion()
	}

	metadataJson, err := json.Marshal(metadata)
	if err != nil {
		logger.Error(fmt.Sprintf("Error during marshal metadata in %s", dbName), zap.Error(err))
		return err
	}

	_, err = conn.Exec(ctx, createMetaTable)
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't create metadata table in %s", dbName), zap.Error(err))
		return err
	}

	_, err = conn.Exec(ctx, deleteMetaData)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot delete from metadata table for %s", dbName), zap.Error(err))
		return err
	}

	_, err = conn.Exec(ctx, insertIntoMetaTable, Metadata, metadataJson)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot insert in metadata table for %s", dbName), zap.Error(err))
		return err
	}

	return nil
}

func (sa ServiceAdapter) DescribeDatabases(ctx context.Context, logicalDatabases []string, showResources bool, showConnections bool) map[string]dao.LogicalDatabaseDescribed {
	logger := util.ContextLogger(ctx)
	logger.Info("Describe databases is executed")
	result := make(map[string]dao.LogicalDatabaseDescribed)
	return result
}

func (sa ServiceAdapter) GetRolesFromMetadata(metadata map[string]interface{}) map[string]string {
	roles := make(map[string]string)
	if _, ok := metadata[RolesKey]; ok {
		if rolesFromMeta, ok := metadata[RolesKey].(map[string]interface{}); ok {
			roles = util.GetMapStringFromMapInterface(rolesFromMeta)
		}
	}
	return roles
}

func (sa ServiceAdapter) GetValidRolesFromMetadata(ctx context.Context, dbName string, metadata map[string]interface{}) (map[string]string, error) {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Start roles validation from metadata for %s", dbName))
	conn, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	roles := sa.GetRolesFromMetadata(metadata)
	logger.Debug(fmt.Sprintf("Roles from metadata: %s", roles))

	for username, role := range roles {
		isUserExist, err := sa.isUserExist(ctx, conn, username)
		if err != nil {
			return nil, err
		}
		if !isUserExist {
			logger.Warn(fmt.Sprintf("Role %s is not exist", username))
			delete(roles, username)
			continue
		}

		isValid, err := sa.validateRole(ctx, conn, username, role)
		if err != nil {
			logger.Error(fmt.Sprintf("Error during validation user %s for database %s", username, dbName))
			return nil, err
		}
		if !isValid {
			logger.Warn(fmt.Sprintf("Role %s is not valid for database %s", username, dbName))
			delete(roles, username)
		}
	}

	logger.Debug(fmt.Sprintf("Valid roles from metadata: %s", roles))
	return roles, nil
}

func (sa ServiceAdapter) saveRolesInMetadata(ctx context.Context, dbName string, roles map[string]string) error {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Save roles %s in metadata for %s", roles, dbName))
	metadata, err := sa.GetMetadataInternal(ctx, dbName)
	if err != nil {
		return err
	}
	metadata[RolesKey] = roles
	err = sa.UpdateMetadataInternal(ctx, metadata, dbName)
	return err
}

func (sa ServiceAdapter) GetMetadataInternal(ctx context.Context, logicalDatabase string) (map[string]interface{}, error) {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Get metadata from %s", logicalDatabase))

	var metadata map[string]interface{}

	conn, err := sa.GetConnectionToDb(ctx, logicalDatabase)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(ctx, getMetadata, "metadata")
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't obtain data from metadata table for %s", logicalDatabase), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		err = rows.Scan(&metadata)
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't scan data from metadata table for %s", logicalDatabase), zap.Error(err))
			return nil, err
		}
	}

	if len(metadata) == 0 {
		rows.Close()
		conn.Close()

		metadata, err = sa.getOldFormatMetadata(ctx, logicalDatabase)
		if err != nil {
			return nil, err
		}
	}

	return metadata, nil
}

func (sa ServiceAdapter) getOldFormatMetadata(ctx context.Context, logicalDatabase string) (map[string]interface{}, error) {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Get metadata in old format for %s", logicalDatabase))
	metadata := make(map[string]interface{})
	conn, err := sa.GetConnectionToDb(ctx, logicalDatabase)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(ctx, getAllMetadata)
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't obtain data from metadata table for %s", logicalDatabase), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var value interface{}
		err = rows.Scan(&key, &value)
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't scan data from metadata table for %s", logicalDatabase), zap.Error(err))
			return nil, err
		}
		metadata[key] = value
	}
	return metadata, err
}

func (sa ServiceAdapter) GetMetadata(ctx context.Context, logicalDatabase string) map[string]interface{} {
	metadata, err := sa.GetMetadataInternal(ctx, logicalDatabase)
	if err != nil {
		panic(err)
	}
	return metadata
}

func (sa ServiceAdapter) MigrateToVault(ctx context.Context, logicalDatabase string, userName string) error {
	return nil
}

func (sa ServiceAdapter) disableDatabase(ctx context.Context, conn cluster.Conn, dbName string) error {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Disable database %s", dbName))
	var err error

	err = sa.rollbackPrepared(ctx, conn, dbName)
	if err != nil {
		return err
	}

	if util.GetEnv("EXTERNAL_POSTGRESQL", "") != "" {
		_, err = conn.Exec(ctx, fmt.Sprintf(prohibitConnectionsToDbExt, dbName))
	} else {
		_, err = conn.Exec(ctx, prohibitConnectionsToDb, dbName)
	}
	if err != nil {
		logger.Error(fmt.Sprintf("Error during prohibit connections to db %s", dbName), zap.Error(err))
		return err
	}

	if util.GetEnv("EXTERNAL_POSTGRESQL", "") == "" {
		err := sa.terminateListenConnections(ctx, conn)
		if err != nil {
			logger.Warn(fmt.Sprintf("Error during LISTEN connections cleanup: %v", err))
		}
	}

	_, err = conn.Exec(ctx, dropConnectionsToDb, dbName)
	if err != nil {
		logger.Error(fmt.Sprintf("Error during drop connections to db %s", dbName), zap.Error(err))
		return err
	}
	return nil
}

func transactionsExists(ctx context.Context, conn cluster.Conn, dbName string) (error, bool) {
	var prep bool
	row := conn.QueryRow(ctx, existsRollbackTransactions, dbName)
	err := row.Scan(&prep)
	if err != nil {
		return err, false
	}
	return nil, prep
}

func (sa ServiceAdapter) rollbackPrepared(ctx context.Context, conn cluster.Conn, dbName string) error {
	logger := util.ContextLogger(ctx)
	var idTransaction string
	var trans []string
	err, prep := transactionsExists(ctx, conn, dbName)
	if err != nil {
		logger.Error("error when checking if rollback transactions are exists")
		return err
	}

	if prep {
		connDb, err := sa.GetConnectionToDb(ctx, dbName)
		if err != nil {
			logger.Error("taking connection failed while rollback prepared transactions")
			return err
		}
		defer connDb.Close()

		rows, err := connDb.Query(ctx, selectPreparedTransactions, dbName)
		if err != nil {
			logger.Error("select idTransaction failed while rollback prepared transactions")
			return err
		}
		defer rows.Close()

		for rows.Next() {
			err = rows.Scan(&idTransaction)
			if err != nil {
				logger.Error("scan idTransaction failed while rollback prepared transactions")
				return err
			}
			trans = append(trans, idTransaction)
		}

		for _, tr := range trans {
			_, err = connDb.Exec(ctx, rollbackPreparedByGid(tr))
			if err != nil {
				logger.Error(fmt.Sprintf("rollback prepared failed with %s", tr))
				return err
			}
		}
	}
	return nil
}

func (sa ServiceAdapter) terminateListenConnections(ctx context.Context, conn cluster.Conn) error {
	logger := util.ContextLogger(ctx)
	logger.Debug("Cleaning up problematic LISTEN connections globally")

	_, err := conn.Exec(ctx, terminateListenConnections)
	if err != nil {
		logger.Warn(fmt.Sprintf("Error during terminate LISTEN connections: %v", err))
	}

	return nil
}

func (sa ServiceAdapter) disableUser(ctx context.Context, dbName string, resource dao.DbResource) error {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Disable user %s for database %s", resource.Name, dbName))
	connDb, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return err
	}

	defer connDb.Close()

	// if user deleted, we need to rollback prepared transactions in order to avoid lock on reassign grants
	err = sa.rollbackPrepared(ctx, connDb, dbName)
	if err != nil {
		return err
	}

	_, err = connDb.Exec(context.Background(), reassignGrants(getUsernameWithoutHost(resource.Name), sa.GetUser()))
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't reassign grants for user %s", resource.Name), zap.Error(err))
		return err
	}

	_, err = connDb.Exec(context.Background(), dropOwnedObjects(resource.Name))
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't drop owned objects for user %s", resource.Name), zap.Error(err))
		return err
	}

	_, err = connDb.Exec(context.Background(), dropUserConnectionsToDb, resource.Name)
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't drop user connections for user %s", resource.Name), zap.Error(err))
		return err
	}

	return nil
}

func (sa ServiceAdapter) dropResource(ctx context.Context, resource dao.DbResource, conn cluster.Conn) dao.DbResource {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Drop resource %s with name %s", resource.Kind, resource.Name))
	sa.Mutex.Lock()
	defer sa.Mutex.Unlock()

	if resource.Kind == DbKind {
		rows, err := conn.Query(ctx, getDatabase, resource.Name)
		if err != nil {
			logger.Error(fmt.Sprintf("Error during get information for DB %s", resource.Name), zap.Error(err))
			return dao.DbResource{
				Kind:         resource.Kind,
				Name:         resource.Name,
				Status:       DeleteFailedStatus,
				ErrorMessage: err.Error(),
			}
		}

		dbExist := rows.Next()
		rows.Close()

		if dbExist {
			logger.Debug(fmt.Sprintf("Database %s is already exist, skip name validation for DDL", resource.Name))
		} else {
			logger.Debug(fmt.Sprintf("Database %s is not exist, skip deletion", resource.Name))

			return dao.DbResource{
				Kind:   resource.Kind,
				Name:   resource.Name,
				Status: DeletedStatus,
			}
		}

		err = sa.disableDatabase(ctx, conn, resource.Name)
		if err != nil {
			return dao.DbResource{
				Kind:         resource.Kind,
				Name:         resource.Name,
				Status:       DeleteFailedStatus,
				ErrorMessage: err.Error(),
			}
		}

		_, err = conn.Exec(context.Background(), dropOldDatabase(resource.Name))
		if err != nil {
			logger.Error(fmt.Sprintf("Error during drop DB %s", resource.Name), zap.Error(err))
			return dao.DbResource{
				Kind:         resource.Kind,
				Name:         resource.Name,
				Status:       DeleteFailedStatus,
				ErrorMessage: err.Error(),
			}
		}

		return dao.DbResource{
			Kind:   resource.Kind,
			Name:   resource.Name,
			Status: DeletedStatus,
		}

	} else if resource.Kind == UserKind {
		rows, err := conn.Query(context.Background(), getUser, resource.Name)
		if err != nil {
			logger.Error(fmt.Sprintf("Error during get information for user %s", resource.Name), zap.Error(err))
			return dao.DbResource{
				Kind:         resource.Kind,
				Name:         resource.Name,
				Status:       DeleteFailedStatus,
				ErrorMessage: err.Error(),
			}
		}

		userExist := rows.Next()
		rows.Close()
		if userExist {
			logger.Debug(fmt.Sprintf("User %s is already exist, skip name validation for DDL", resource.Name))
		} else {
			logger.Debug(fmt.Sprintf("User %s is not exist, skip deletion", resource.Name))

			return dao.DbResource{
				Kind:   resource.Kind,
				Name:   resource.Name,
				Status: DeletedStatus,
			}
		}

		rows, err = conn.Query(context.Background(), getDependenceDbForRole, resource.Name, resource.Name)
		if err != nil {
			logger.Error("Couldn't get user roles", zap.Error(err))
			return dao.DbResource{
				Kind:         resource.Kind,
				Name:         resource.Name,
				Status:       DeleteFailedStatus,
				ErrorMessage: err.Error(),
			}
		}

		defer rows.Close()

		for rows.Next() {
			var dbName string
			err = rows.Scan(&dbName)

			if err != nil {
				logger.Error(fmt.Sprintf("Error occurred during scan db for user %s", resource.Name), zap.Error(err))

				return dao.DbResource{
					Kind:         resource.Kind,
					Name:         resource.Name,
					Status:       DeleteFailedStatus,
					ErrorMessage: err.Error(),
				}
			}

			err = sa.disableUser(ctx, dbName, resource)
			if err != nil {
				return dao.DbResource{
					Kind:         resource.Kind,
					Name:         resource.Name,
					Status:       DeleteFailedStatus,
					ErrorMessage: err.Error(),
				}
			}
		}

		_, err = conn.Exec(context.Background(), dropUser(resource.Name))
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't drop user %s", resource.Name), zap.Error(err))
			return dao.DbResource{
				Kind:         resource.Kind,
				Name:         resource.Name,
				Status:       DeleteFailedStatus,
				ErrorMessage: err.Error(),
			}
		}
	}

	return dao.DbResource{
		Kind:   resource.Kind,
		Name:   resource.Name,
		Status: DeletedStatus,
	}
}

func (sa ServiceAdapter) dropResourceByKind(ctx context.Context, resources []dao.DbResource, kind string, conn cluster.Conn) []dao.DbResource {
	var result []dao.DbResource
	for _, resource := range resources {
		if resource.Kind == kind {
			dropResult := sa.dropResource(ctx, resource, conn)
			result = append(result, dropResult)
		}
	}
	return result
}

func (sa ServiceAdapter) DropResources(ctx context.Context, resources []dao.DbResource) []dao.DbResource {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Drop resources %s", resources))
	var droppedResources []dao.DbResource

	conn, err := sa.GetConnection(ctx)
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	users := sa.dropResourceByKind(ctx, resources, UserKind, conn)
	droppedResources = append(droppedResources, users...)

	dataBases := sa.dropResourceByKind(ctx, resources, DbKind, conn)
	droppedResources = append(droppedResources, dataBases...)

	return droppedResources
}

func (sa ServiceAdapter) GetDatabases(ctx context.Context) []string {

	logger := util.ContextLogger(ctx)
	logger.Info("Get databases")
	conn, err := sa.GetConnection(ctx)
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	rows, err := conn.Query(ctx, getDatabases)
	if err != nil {
		logger.Error("Error occurred during obtain databases rows", zap.Error(err))
		panic(err)
	}

	defer rows.Close()

	var databasesNames []string
	for rows.Next() {
		var databaseName string
		err = rows.Scan(&databaseName)
		if err != nil {
			logger.Error("Error occurred during scan databases row", zap.Error(err))
			panic(err)
		}

		databasesNames = append(databasesNames, databaseName)
	}

	return databasesNames
}

func (sa ServiceAdapter) getConnectionProperties(dbName, username, role, password string) dao.ConnectionProperties {
	url := fmt.Sprintf("jdbc:postgresql://%s:%d/%s", sa.GetHost(), sa.GetPort(), dbName)
	connectionProps := dao.ConnectionProperties{
		"name":     dbName,
		"url":      url,
		"host":     sa.GetHost(),
		"port":     sa.GetPort(),
		"username": sa.getUserNameWithHostName(username),
		"password": password,
	}
	if len(role) > 0 {
		connectionProps["role"] = role
	}
	return connectionProps
}

func (generator UUIDGenerator) Generate() string {
	uuidName := uuid.NewV4()
	uuidString := uuidName.String()
	return strings.ReplaceAll(uuidString, "-", "")
}

func validateDbIdentifierParam(ctx context.Context, paramName string, paramValue string, pattern string) bool {
	logger := util.ContextLogger(ctx)
	if paramValue != "" {
		matched, err := regexp.MatchString(pattern, paramValue)
		if err != nil {
			logger.Error(fmt.Sprintf("Error during check %s", paramName), zap.Error(err))
			return false
		}

		if !matched {
			logger.Info(fmt.Sprintf("Provided %s does not meet the requirements", paramName))
		}

		return matched
	}
	return true
}

func (sa ServiceAdapter) ValidatePostgresAvailableExtensions(ctx context.Context, extensions []string) (bool, error) {
	logger := util.ContextLogger(ctx)
	availableExtensions, err := sa.getPostgresAvailableExtensions(ctx)
	if err != nil {
		return false, err
	}

	for _, extension := range extensions {
		if !util.Contains(availableExtensions, extension) {
			logger.Error(fmt.Sprintf("request contains not valid Postgres extensions. %s", extension))
			return false, nil
		}
	}
	return true, nil
}

func validateCreationParameters(ctx context.Context, parameters map[string]string) error {
	logger := util.ContextLogger(ctx)
	for k := range parameters {
		if !util.Contains(crParamsKeys, strings.ToUpper(k)) {
			logger.Error(fmt.Sprintf("Invalid parameter for create database: '%s'", k))
			return fmt.Errorf("request contains not valid parameter '%s' in the creation parameters", k)
		}
	}
	return nil
}

func (sa ServiceAdapter) deleteExtension(ctx context.Context, extension string, conn cluster.Conn) error {
	logger := util.ContextLogger(ctx)
	_, err := conn.Exec(context.Background(), deleteExtension(extension))
	if err != nil {
		logger.Error(fmt.Sprintf("Error during delete database extension %s", extension), zap.Error(err))
		return err
	}
	return nil
}

func (sa ServiceAdapter) updateExtensions(ctx context.Context, conn cluster.Conn, newPgExtensions []string, currentPgExtensions []string) (*SettingUpdateResult, error) {
	logger := util.ContextLogger(ctx)
	logger.Debug("Update extensions started")
	isValid, err := sa.ValidatePostgresAvailableExtensions(ctx, newPgExtensions)
	if err != nil {
		logger.Error("Error during validate new extensions", zap.Error(err))
		return nil, err
	}

	if !isValid {
		return getBadRequestSettingsResult(newPgExtensions)
	}

	if len(currentPgExtensions) == 0 && len(newPgExtensions) == 0 {
		message := "Nothing to update. Extensions list is empty."
		logger.Info(message)
		return &SettingUpdateResult{
			Status:  Skipped,
			Message: message,
		}, nil
	}

	if len(currentPgExtensions) > 0 {
		isValid, err = sa.ValidatePostgresAvailableExtensions(ctx, currentPgExtensions)
		if err != nil {
			logger.Error("Error during validate current extensions", zap.Error(err))
			return nil, err
		}

		if !isValid {
			return getBadRequestSettingsResult(currentPgExtensions)
		}

		for _, extension := range currentPgExtensions {
			if !util.Contains(newPgExtensions, extension) {
				err = sa.deleteExtension(ctx, extension, conn)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	for _, extension := range newPgExtensions {
		_, err = conn.Exec(context.Background(), createExtensionIfNotExist(extension))
		if err != nil {
			logger.Error(fmt.Sprintf("Error during update database extension %s", extension), zap.Error(err))
			return nil, err
		}
	}

	return &SettingUpdateResult{
		Status:  Successful,
		Message: "Update database extensions successful",
	}, nil
}

func (sa ServiceAdapter) updateSettings(dbName string, request PostgresUpdateSettingsRequest) (*PostgresUpdateSettingsResult, error) {
	sa.log.Debug("Update setting started")
	ctx := context.Background()
	results := make(map[string]SettingUpdateResult)
	if request.NewSettings == nil {
		return &PostgresUpdateSettingsResult{
			SettingUpdateResult: map[string]SettingUpdateResult{"newSettings": {
				Status:  BadRequest,
				Message: "newSettings parameter is empty",
			}}}, nil
	}

	conn, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	currentSettings, err := sa.getPgSettings(ctx, request.CurrentSettings)
	if err != nil {
		return nil, err
	}
	newSettings, err := sa.getPgSettings(ctx, request.NewSettings)
	if err != nil {
		return nil, err
	}

	updatedExtensions, err := sa.updateExtensions(ctx, conn, newSettings.Extensions, currentSettings.Extensions)
	if err != nil {
		return nil, err
	}
	results["pgExtensions"] = *updatedExtensions

	for _, key := range newSettings.NotSupported {
		message := fmt.Sprintf("Setting %s not supported", key)
		sa.log.Info(message)
		results[key] = SettingUpdateResult{
			Status:  Skipped,
			Message: message,
		}
	}
	return &PostgresUpdateSettingsResult{results}, nil
}

func (sa ServiceAdapter) getUserNameWithHostName(userName string) string {
	values := strings.Split(sa.ClusterAdapter.GetUser(), "@")
	if len(values) > 1 {
		return fmt.Sprintf("%s@%s", userName, values[1])
	}
	return userName
}

func (sa ServiceAdapter) reassignGrantsToNewUserForDb(ctx context.Context, dbName string, newUserName string) error {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Will reassign grants to new user %s for db %s", newUserName, dbName))

	currentUserName, err := sa.getCurrentUserForDb(ctx, dbName)
	if err != nil {
		logger.Error("", zap.Error(err))
		return err
	}

	connDb, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return err
	}
	defer connDb.Close()

	_, err = connDb.Exec(context.Background(), reassignGrants(currentUserName, newUserName))
	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't reassign grants from %s to %s", currentUserName, newUserName),
			zap.Error(err))
		return err
	}
	logger.Info(fmt.Sprintf("Reassign grants to new user %s for db %s has been completed", newUserName, dbName))
	return nil
}

func (sa ServiceAdapter) getCurrentUserForDb(ctx context.Context, dbName string) (string, error) {
	logger := util.ContextLogger(ctx)
	connDb, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return "", err
	}
	defer connDb.Close()
	rows, err := connDb.Query(context.Background(), getOwnerForMetaData)

	if err != nil {
		logger.Error(fmt.Sprintf("Couldn't get owner for meta table for db %s", dbName), zap.Error(err))
		return "", err
	}
	defer rows.Close()
	var currentUserForDb string
	for rows.Next() {
		err = rows.Scan(&currentUserForDb)
		if err != nil {
			logger.Error("Error occurred during scan databases row", zap.Error(err))
			return "", err
		}
	}
	logger.Debug(fmt.Sprintf("Current admin for database %s: %s", dbName, currentUserForDb))
	return currentUserForDb, nil
}

func (sa ServiceAdapter) getDBInfo(ctx context.Context, dbName string) (*DbInfo, error) {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Get info for database %s", dbName))
	userName, err := sa.getCurrentUserForDb(ctx, dbName)
	if err != nil {
		return nil, err
	}
	return &DbInfo{Owner: userName}, nil
}

func getBadRequestSettingsResult(extensionsArray []string) (*SettingUpdateResult, error) {
	message := fmt.Sprintf("Update settings Request contains not valid PostgreSQL extensions %s", extensionsArray)
	log.Info(message)
	return &SettingUpdateResult{
		Status:  BadRequest,
		Message: message,
	}, nil
}

func readDefaultExtFile() *ExtensionConfig {
	extensionFile := ExtensionPath + ExtensionName
	file, err := os.ReadFile(extensionFile)
	if err != nil {
		log.Info(fmt.Sprintf("Skipping extensions file, cannot read it: %s", extensionFile))
		return &ExtensionConfig{
			DefaultExt:        make([]string, 0),
			ExtUpdateRequired: false,
		}
	}
	var extensionMap map[string]interface{}
	err = json.Unmarshal(file, &extensionMap)
	if err != nil {
		log.Warn(fmt.Sprintf("Failed to parse extensions file %s", extensionFile), zap.Error(err))
		extensionMap = make(map[string]interface{})
	}

	extensionSlice := util.GetStringArrayFromInterface(extensionMap[ExtensionsKey])
	log.Info(fmt.Sprintf("Default extensions: %s", extensionSlice))
	updateRequired := extensionMap[UpdateRequiredKey].(bool)
	log.Info(fmt.Sprintf("Extensions should be updated: %t", updateRequired))

	return &ExtensionConfig{
		DefaultExt:        extensionSlice,
		ExtUpdateRequired: updateRequired,
	}
}

func CreateExtFromSlice(ctx context.Context, conn cluster.Conn, username string, extensions []string) {
	for _, extension := range extensions {
		log.Info(fmt.Sprintf("Create extension %s", extension))
		_, err := conn.Exec(ctx, createExtensionIfNotExist(extension))
		if err != nil {
			log.Warn(fmt.Sprintf("Couldn't create extension %s", extension), zap.Error(err))
		}

		switch extension {
		case OracleFdw:
			log.Debug(fmt.Sprintf("Grant %s for OracleFDW", username))
			_, err = conn.Exec(ctx, grantRightsForFdw(extension, username))
			if err != nil {
				log.Warn(fmt.Sprintf("Couldn't set grants for extension %s", extension), zap.Error(err))
			}
		case Dblink:
			log.Debug(fmt.Sprintf("Grant %s for dblink", username))
			_, err = conn.Exec(ctx, grantRightsForFdw("dblink_fdw", username))
			if err != nil {
				log.Warn(fmt.Sprintf("Couldn't set grants for extension %s", extension), zap.Error(err))
			}
			for _, function := range dblinkFuncs {
				log.Debug(fmt.Sprintf("Grant %s for dblink function %s", username, function))
				_, err = conn.Exec(ctx, setGrantOnFunction(function, username))
				if err != nil {
					log.Warn(fmt.Sprintf("Couldn't set grants for function %s", function), zap.Error(err))
				}
			}
		}
	}
}

func (sa ServiceAdapter) getConnectionLimitFromTemplate1Db(ctx context.Context) string {
	logger := util.ContextLogger(ctx)
	connDb, err := sa.GetConnection(ctx)
	if err != nil {
		return "-1"
	}
	defer connDb.Close()
	rows, err := connDb.Query(context.Background(), getConnectionLimitFromTemplate1)
	if err != nil {
		logger.Error("Couldn't get connection limit from template1", zap.Error(err))
		return "-1"
	}
	defer rows.Close()
	var connectionLimit string
	for rows.Next() {
		err = rows.Scan(&connectionLimit)
		if err != nil {
			logger.Error("Error occurred during scan databases row", zap.Error(err))
			return "-1"
		}
	}
	logger.Debug(fmt.Sprintf("Connection limit for template1: %s", connectionLimit))
	return connectionLimit
}

func (sa ServiceAdapter) GetSchemas(dbName string) ([]string, error) {
	result := []string{}

	// existing schemas
	schemasFromDB, err := sa.getSchemasFromDB(dbName)
	if err != nil {
		return result, err
	}
	result = append(result, schemasFromDB...)

	return result, err
}

func (sa ServiceAdapter) getSchemasFromDB(dbName string) ([]string, error) {
	ctx := context.Background()
	result := []string{}
	conn, err := sa.GetConnectionToDb(ctx, dbName)
	if err != nil {
		return result, nil
	}
	defer conn.Close()

	rows, err := conn.Query(ctx, getSchemasQuery())
	if err != nil {
		return result, err
	}

	for rows.Next() {
		var schema string
		err := rows.Scan(&schema)
		if err != nil {
			sa.log.Error("cant scan schema name", zap.Error(err))
			return result, err
		}
		result = append(result, schema)
	}

	sa.log.Debug(fmt.Sprintf("Schemas from DB: %s", result))
	return result, nil
}

func (sa ServiceAdapter) getPgSettings(ctx context.Context, settings map[string]interface{}) (PgSettings, error) {
	logger := util.ContextLogger(ctx)
	logger.Debug(fmt.Sprintf("Database settings for process: %s", settings))
	creationParameters := map[string]string{}
	extensions := []string{}
	notSupported := []string{}
	for key, value := range settings {
		switch key {
		case CreationParametersKey:
			creationParameters = util.GetMapStringFromMapInterface(value)
			logger.Info(fmt.Sprintf("Database will be created with the next parameters %s", creationParameters))
			err := validateCreationParameters(ctx, creationParameters)
			if err != nil {
				return PgSettings{}, err
			}
			creationParameters["connection limit"] = sa.getConnectionLimitFromTemplate1Db(ctx)
		case PGExtensionsKey:
			extensions = util.GetStringArrayFromInterface(settings[PGExtensionsKey])
		default:
			notSupported = append(notSupported, key)
		}
	}

	return PgSettings{
		CreationParameters: creationParameters,
		Extensions:         extensions,
		NotSupported:       notSupported,
	}, nil
}
