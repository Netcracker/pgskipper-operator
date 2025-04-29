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
	"fmt"
	"strconv"
	"strings"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
)

const (
	DbPrefixPattern      = `^(_|[a-z])[\w\d_]{0,29}$`
	DbIdentifiersPattern = `^(_|[a-z])[\w\d_-]{0,62}$`
	OracleFdw            = "oracle_fdw"
	Dblink               = "dblink"
)

var protectedRoles = []string{"'rdsadmin'", "'cloudsqladmin'", "'azuresu'"}
var dblinkFuncs = []string{"dblink_connect_u(text)", "dblink_connect_u(text, text)"}

var (
	crParamsKeys = []string{"OWNER", "TEMPLATE", "ENCODING", "LOCALE", "LC_COLLATE",
		"LC_CTYPE", "TABLESPACE", "ALLOW_CONNECTIONS", "CONNECTION LIMIT", "IS_TEMPLATE"}
	getDatabases                      = "SELECT DATNAME FROM pg_database;"
	deleteMetaData                    = "DELETE FROM _DBAAS_METADATA"
	insertIntoMetaTable               = "INSERT INTO _DBAAS_METADATA VALUES ($1, $2)"
	selectPostgresAvailableExtensions = "SELECT NAME FROM PG_AVAILABLE_EXTENSIONS"
	createMetaTable                   = "CREATE TABLE IF NOT EXISTS public._DBAAS_METADATA (key varchar(256) PRIMARY KEY, value json)"
	getUser                           = "SELECT 1 FROM pg_roles WHERE rolname = $1;"
	getDatabase                       = "SELECT 1 FROM pg_database WHERE datname = $1;"
	dropConnectionsToDb               = "SELECT pg_terminate_backend(pg_stat_activity.pid) FROM pg_stat_activity WHERE datname = $1;"
	prohibitConnectionsToDb           = "UPDATE pg_database SET datallowconn = false WHERE datname = $1;"
	prohibitConnectionsToDbExt        = "ALTER DATABASE \"%s\" WITH ALLOW_CONNECTIONS false;"
	getDependenceDbForRole            = "SELECT distinct datname FROM pg_database WHERE has_database_privilege($1, datname, 'CREATE') or oid in (select dbid from pg_shdepend join pg_roles on pg_shdepend.refobjid = pg_roles.oid where rolname =$2);"
	dropUserConnectionsToDb           = "SELECT pg_terminate_backend(pg_stat_activity.pid) FROM pg_stat_activity WHERE usename = $1;"
	getOwnerForMetaData               = "select tableowner from pg_tables where tablename = '_dbaas_metadata';"
	getMetadata                       = "SELECT value FROM _DBAAS_METADATA where key = $1;"
	getAllMetadata                    = "SELECT * FROM _DBAAS_METADATA;"
	getConnectionLimitFromTemplate1   = "select datconnlimit::text from pg_database where datname = 'template1';"
	existsRollbackTransactions        = "SELECT EXISTS (SELECT 1 FROM pg_prepared_xacts WHERE database = $1);"
	selectPreparedTransactions        = "SELECT gid FROM pg_prepared_xacts WHERE database = $1;"
)

func createDatabase(dbName string, parameters map[string]string) string {
	createStr := fmt.Sprintf("CREATE DATABASE \"%s\"", escapeInputValue(dbName))
	if len(parameters) > 0 {
		createStr += " WITH"
		for k, v := range parameters {
			if k == "connection limit" {
				intVal, _ := strconv.Atoi(v)
				createStr += fmt.Sprintf(" %s=%d", k, intVal)
			} else {
				createStr += fmt.Sprintf(" %s='%s'", k, escapeInputValue(v))
			}
		}
	}
	createStr += ";"
	return createStr
}

func createUser(userName string, password string) string {
	return fmt.Sprintf("CREATE USER \"%s\" WITH PASSWORD '%s'", userName, password)
}

func createSchema(schema string) string {
	return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS \"%s\"", escapeInputValue(schema))
}

func grantAllRightsOnDatabase(dbName string, userName string) string {
	return fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE \"%s\" TO \"%s\";", dbName, userName)
}

func grantSchemaCreate(schema, userName string) string {
	return fmt.Sprintf("GRANT CREATE ON SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func grantSchemaUsage(schema, userName string) string {
	return fmt.Sprintf("GRANT USAGE ON SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func revokeRights() string {
	return "REVOKE CREATE ON SCHEMA public FROM public;"
}

func dropDatabase(dbName string) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", dbName)
}

func dropUser(userName string) string {
	return fmt.Sprintf("DROP USER IF EXISTS \"%s\";", userName)
}

func alterOwnerMetaTable(userName string) string {
	return fmt.Sprintf("ALTER TABLE _DBAAS_METADATA OWNER TO \"%s\"", userName)
}

func alterOwnerForTable(schema, tableName, userName string) string {
	return fmt.Sprintf("ALTER TABLE \"%s\".\"%s\" OWNER TO \"%s\"", schema, tableName, userName)
}

func alterOwnerForSequence(schema, sequenceName, newOwner string) string {
	return fmt.Sprintf(`ALTER SEQUENCE "%s"."%s" OWNER TO "%s"`, schema, sequenceName, newOwner)
}

func alterOwnerForLargeObject(schema, oid, userName string) string {
	return fmt.Sprintf("ALTER LARGE OBJECT %s OWNER TO \"%s\";", oid, userName) //TODO:
}

func alterOwnerForSchema(schema, userName string) string {
	return fmt.Sprintf("ALTER SCHEMA \"%s\" OWNER TO \"%s\"", schema, userName)
}

func alterDatabaseOwnerQuery(dbName, userName string) string {
	return fmt.Sprintf("ALTER DATABASE \"%s\" OWNER TO \"%s\";", dbName, userName)
}

func alterProcedureOwnerQuery(schema, procedure, userName string) string {
	return fmt.Sprintf("ALTER PROCEDURE %s OWNER TO \"%s\"", procedure, userName)
}

func alterFunctionOwnerQuery(schema, procedure, userName string) string {
	return fmt.Sprintf("ALTER FUNCTION %s OWNER TO \"%s\"", procedure, userName)
}

func alterViewOwnerQuery(schema, view, userName string) string {
	return fmt.Sprintf("ALTER VIEW \"%s\".\"%s\" OWNER TO \"%s\"", schema, view, userName)
}

func alterTypeOwnerQuery(schema, typeForAlter, userName string) string {
	return fmt.Sprintf("ALTER TYPE \"%s\".\"%s\" OWNER TO \"%s\"", schema, typeForAlter, userName)
}

func createExtensionIfNotExist(extension string) string {
	return fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS \"%s\" CASCADE;", extension)
}

func grantRightsForFdw(fdwname string, userName string) string {
	return fmt.Sprintf("GRANT ALL ON FOREIGN DATA WRAPPER \"%s\" to \"%s\";", fdwname, userName)
}

func changeUserPassword(userName string, password string) string {
	return fmt.Sprintf("ALTER USER \"%s\" WITH PASSWORD '%s' ;", userName, password)
}

func deleteExtension(extension string) string {
	return fmt.Sprintf("DROP EXTENSION IF EXISTS \"%s\";", extension)
}

func allowConnectionsToDb(dbName string) string {
	return fmt.Sprintf("UPDATE pg_database SET datallowconn = true WHERE datname = '%s';", dbName)
}

func allowConnectionsToDbExt(dbName string) string {
	return fmt.Sprintf("ALTER DATABASE \"%s\" WITH ALLOW_CONNECTIONS true;", dbName)
}

func dropOldDatabase(dbName string) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", strings.Replace(dbName, "\"", "\\\"", -1))
}

func reassignGrants(userName string, tempRole string) string {
	return fmt.Sprintf("REASSIGN OWNED BY \"%s\" TO \"%s\";", userName, tempRole)
}

func dropOwnedObjects(userName string) string {
	return fmt.Sprintf("DROP OWNED BY \"%s\";", userName)
}

func grantUserToAdmin(userName string, adminName string) string {
	return fmt.Sprintf("GRANT \"%s\" TO \"%s\";", userName, adminName)
}

func escapeInputValue(value string) string {
	result := strings.ReplaceAll(value, `"`, `""`)
	return strings.ReplaceAll(result, "'", "''")
}

func setReadOnlyRoleGrants(schema string, userName string) string {
	return fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func setAdminRoleSequencesGrants(schema string, userName string) string {
	return fmt.Sprintf("GRANT ALL ON ALL SEQUENCES IN SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func setReadWriteRoleGrants(schema string, userName string) string {
	return fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func setReadWriteRoleSequencesGrants(schema string, userName string) string {
	return fmt.Sprintf("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func setReadOnlyRoleSequencesGrants(schema string, userName string) string {
	return fmt.Sprintf("GRANT SELECT ON ALL SEQUENCES IN SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func setAdminGrants(schema string, userName string) string {
	return fmt.Sprintf("GRANT ALL ON ALL TABLES IN SCHEMA \"%s\" TO \"%s\";", schema, userName)
}

func setReadOnlyRoleDefaultGrants(owner, userName string) string {
	return fmt.Sprintf("ALTER DEFAULT PRIVILEGES for role \"%s\" GRANT SELECT ON TABLES TO \"%s\";", owner, userName)
}

func setReadWriteRoleDefaultGrants(owner, userName string) string {
	return fmt.Sprintf("ALTER DEFAULT PRIVILEGES for role \"%s\" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO \"%s\";", owner, userName)
}

func setSchemasDefaultGrants(adminUser, userName string) string {
	return fmt.Sprintf("ALTER DEFAULT PRIVILEGES for role \"%s\" GRANT USAGE ON SCHEMAS TO \"%s\";", adminUser, userName)
}

func setSequencesRODefaultGrants(adminUser, userName string) string {
	return fmt.Sprintf("ALTER DEFAULT PRIVILEGES for role \"%s\" GRANT SELECT ON SEQUENCES TO \"%s\";", adminUser, userName)
}

func setSequencesRWDefaultGrants(adminUser, userName string) string {
	return fmt.Sprintf("ALTER DEFAULT PRIVILEGES for role \"%s\" GRANT USAGE, SELECT ON SEQUENCES TO \"%s\";", adminUser, userName)
}

func setGrantAsForRole(owner, userName string) string {
	return fmt.Sprintf("GRANT \"%s\" TO \"%s\";", owner, userName)
}

func grantConnectionToDB(dbName, userName string) string {
	return fmt.Sprintf("GRANT CONNECT ON DATABASE \"%s\" TO \"%s\";", dbName, userName)
}

func AllowReplicationForUser(userName string) string {
	if strings.ToUpper(util.ExternalPostgreSQLType) == "RDS" {
		return fmt.Sprintf("GRANT rds_replication TO \"%s\";", userName)
	}
	return fmt.Sprintf("ALTER USER \"%s\" WITH REPLICATION;", userName)
}

func getTablesListQuery(schema string, external bool) string {
	baseQuery := fmt.Sprintf("SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = '%s' and tablename != '_dbaas_metadata'", schema)
	return getExtPreparedQuery(baseQuery, fmt.Sprintf("tableowner not in (%s)", strings.Join(protectedRoles, ",")), external)
}

func getSequenceListQuery(schema string, external bool) string {
	baseQuery := fmt.Sprintf("SELECT sequencename FROM pg_sequences WHERE schemaname = '%s'", schema)
	return getExtPreparedQuery(baseQuery, fmt.Sprintf("sequenceowner not in (%s)", strings.Join(protectedRoles, ",")), external)
}

func getLargeObjectsListQuery(external bool) string {
	baseQuery := "SELECT oid::TEXT FROM pg_catalog.pg_largeobject_metadata"
	if external {
		baseQuery += " where lomowner != 10"
	}
	return fmt.Sprintf("%s;", baseQuery)
}

func getViewsListQuery(schema string, external bool) string {
	baseQuery := fmt.Sprintf("select viewname from pg_catalog.pg_views where schemaname='%s'", schema)
	return getExtPreparedQuery(baseQuery, fmt.Sprintf("viewowner not in (%s)", strings.Join(protectedRoles, ",")), external)
}

func getProceduresListQuery(schema string, external bool) string {
	baseQuery := fmt.Sprintf("SELECT p.oid::regprocedure FROM pg_catalog.pg_namespace n JOIN pg_catalog.pg_proc p ON p.pronamespace = n.oid WHERE p.prokind = 'p' AND n.nspname = '%s'", schema)
	return getExtPreparedQuery(baseQuery, "p.proowner != 10", external)
}

func getFunctionsListQuery(schema string, external bool) string {
	baseQuery := fmt.Sprintf("SELECT p.oid::regprocedure FROM pg_catalog.pg_namespace n JOIN pg_catalog.pg_proc p ON p.pronamespace = n.oid WHERE p.prokind <> 'p' AND n.nspname = '%s' and not (n.nspname = 'public' and p.oid::regprocedure::text='lookup(name)')", schema)
	return getExtPreparedQuery(baseQuery, "p.proowner != 10", external)
}

func getCustomTypesListQuery(schema string, external bool) string {
	baseQuery := fmt.Sprintf("SELECT t.typname FROM pg_type t LEFT JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace WHERE (t.typrelid = 0 OR (SELECT c.relkind = 'c' FROM pg_catalog.pg_class c WHERE c.oid = t.typrelid)) AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_type el WHERE el.oid = t.typelem AND el.typarray = t.oid) AND n.nspname = '%s'", schema)
	return getExtPreparedQuery(baseQuery, "t.typowner != 10", external)
}

func getMetadataGrantQuery(grantee, privilege string) string {
	return fmt.Sprintf("select 1 from information_schema.role_table_grants where table_name='_dbaas_metadata' and grantee='%s' and privilege_type='%s';", escapeInputValue(grantee), privilege)
}

func getSchemasQuery() string {
	return "SELECT schema_name from information_schema.schemata WHERE NOT schema_name LIKE 'pg_%' AND schema_name <> 'information_schema';"
}

func getExtPreparedQuery(originalQuery, additionalCond string, external bool) string {
	baseQuery := originalQuery
	if external {
		baseQuery += fmt.Sprintf(" and %s", additionalCond) // exclude external superuser owned functions
	}
	return fmt.Sprintf("%s;", baseQuery)
}

func setGrantOnFunction(funcName, user string) string {
	return fmt.Sprintf("GRANT EXECUTE ON FUNCTION %s TO \"%s\";", funcName, escapeInputValue(user))
}

func rollbackPreparedByGid(idTrans string) string {
	return fmt.Sprintf("ROLLBACK PREPARED '%s';", idTrans)
}
