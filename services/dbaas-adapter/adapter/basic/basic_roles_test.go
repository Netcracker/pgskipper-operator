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
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/jackc/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func Test_SupportedRoles(t *testing.T) {
	cl := new(PostgresClusterAdapterMock)

	// check multiusers
	feature := map[string]bool{FeatureMultiUsers: true}
	sa := NewServiceAdapter(cl, apiVersion, roles, feature)

	expectedRoles := roles
	supportedRoles := sa.GetSupportedRoles()

	assert.ElementsMatch(t, expectedRoles, supportedRoles)

	// check singleuser
	sa = NewServiceAdapter(cl, apiVersion, roles, features)

	expectedRoles = []string{"admin"}
	supportedRoles = sa.GetSupportedRoles()

	assert.ElementsMatch(t, expectedRoles, supportedRoles)
}

func Test_GetFeatures(t *testing.T) {
	cl := new(PostgresClusterAdapterMock)

	// check multiusers
	features := map[string]bool{FeatureMultiUsers: true}
	sa := NewServiceAdapter(cl, apiVersion, roles, features)

	expectedFeatures := features
	supportedFeatures := sa.GetFeatures()
	fmt.Printf("supportedFeatures: %v\n", supportedFeatures)

	if !reflect.DeepEqual(expectedFeatures, supportedFeatures) {
		t.Errorf("Expected features %v, but got %v", expectedFeatures, supportedFeatures)
	}

}

func Test_GetVersion(t *testing.T) {
	cl := new(PostgresClusterAdapterMock)

	// check multiusers
	feature := map[string]bool{FeatureMultiUsers: true}
	sa := NewServiceAdapter(cl, apiVersion, roles, feature)

	expectedversion := dao.ApiVersion("v2")
	version := sa.GetVersion()

	assert.Equal(t, expectedversion, version)

}

func TestCreateRoles(t *testing.T) {
	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	userName := "user1"
	userPassword := "password"
	dbName := "test_db1"
	emptyUser := ""
	schema := "public"
	prefixedUser := "dbaas_test_user"

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"dbaas_test_user": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}

	metadataJson, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(errParse))
		return
	}

	ca.On("GetUser").Return(user)
	ca.On("GetPassword").Return(userPassword)

	generator := new(GeneratorMock)
	generator.On("Generate").Return(user)
	generator.On("Generate").Return(userPassword)

	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	ca.On("GetConnectionToDbWithUser", dbName, user, userPassword).Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	fmt.Printf("my ac from connect func connection: %v\n", conn)

	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)

	conn.On("Query", context.Background(), getUser, prefixedUser).Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Exec", context.Background(), createUser(prefixedUser, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), AllowReplicationForUser(prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleSequencesGrants(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleSequencesGrants(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminRoleSequencesGrants(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(prefixedUser, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	rows.On("Next").Return(true).Times(2)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Close").Return(nil).Times(15)
	rows.On("Next").Return(false).Times(11)

	sa := NewServiceAdapter(ca, apiVersion, roles, map[string]bool{FeatureMultiUsers: false})
	sa.Generator = generator
	rolesReq := []dao.AdditionalRole{{Id: "123", DbName: dbName, ConnectionProperties: []dao.ConnectionProperties{{
		"name":     dbName,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": user,
		"password": password,
		"role":     RORole,
	}},
		Resources: []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: user}},
	}}
	succ, failed := sa.CreateRoles(context.Background(), rolesReq)
	fmt.Printf("my succ: %v\n", succ)
	fmt.Printf("my failed: %v\n", failed)
	fmt.Printf("my succ resources: %v\n", succ[0].Resources)
	fmt.Printf("my succ id: %v\n", succ[0].ConnectionProperties)

	assert.Equal(t, 1, len(succ[0].ConnectionProperties))
	assert.Equal(t, 2, len(succ[0].Resources))

	assert.Nil(t, failed)
}

func Test_CreateRoles_MultiUsers(t *testing.T) {

	var roles = []string{AdminRole, RWRole}
	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	userex := "dbaas_test_user"
	passwordex := "test_user"
	dbName := "test_db1"
	schema := "public"

	for _, role := range roles {
		metadata := map[string]interface{}{
			RolesKey: map[string]string{
				"dbaas_test_user": role,
			},
			RolesVersionKey: util.GetRolesVersion(),
		}

		metadataJson, errParse := json.Marshal(metadata)
		if errParse != nil {
			log.Error("Error during marshal metadata", zap.Error(errParse))
			return
		}
		conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	}

	ca.On("GetConnection").Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	ca.On("GetUser").Return(user)
	ca.On("GetPassword").Return(password)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	ca.On("GetConnectionToDbWithUser", dbName, user, password).Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	generator := new(GeneratorMock)
	generator.On("Generate").Return(user)

	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getUser, userex).Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Exec", context.Background(), setSchemasDefaultGrants(user, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleDefaultGrants(user, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createUser(userex, passwordex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable("")).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), AllowReplicationForUser(userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleSequencesGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminRoleSequencesGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setSequencesRWDefaultGrants(user, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(userex, passwordex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)

	rows.On("Next").Return(true).Times(7)
	rows.On("Close").Return(nil)

	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(15)

	sa := NewServiceAdapter(ca, apiVersion, roles, map[string]bool{FeatureMultiUsers: true})
	sa.Generator = generator
	rolesReq := []dao.AdditionalRole{{Id: "123", DbName: dbName, ConnectionProperties: []dao.ConnectionProperties{{
		"name":     dbName,
		"url":      fmt.Sprintf("clickhouse://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": user,
		"password": password,
		"role":     RORole,
	}},
		Resources: []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: user}},
	}}
	succ, failure := sa.CreateRoles(context.Background(), rolesReq)
	fmt.Printf("CreateRoles succ: %v\n", succ)
	fmt.Printf("CreateRoles failure: %v\n", failure)
	assert.NotEmpty(t, succ)
	assert.Nil(t, failure)
	assert.Equal(t, 2, len(succ[0].ConnectionProperties))
	assert.Equal(t, 4, len(succ[0].Resources))

}

func Test_CreateRoles_MultiUsersFailedCase(t *testing.T) {

	var roles = []string{RWRole, AdminRole}
	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	userex := "dbaas_test_user"
	passwordex := "test_user"
	dbName := "test_db1"
	dbName2 := "test_db2"
	schema := "public"
	expectedErrorC := errors.New("can't set database grants")

	for _, role := range roles {
		metadata := map[string]interface{}{
			RolesKey: map[string]string{
				"dbaas_test_user": role,
			},
			RolesVersionKey: util.GetRolesVersion(),
		}

		metadataJson, errParse := json.Marshal(metadata)
		if errParse != nil {
			log.Error("Error during marshal metadata", zap.Error(errParse))
			return
		}
		conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	}

	ca.On("GetConnection").Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	ca.On("GetUser").Return(user)
	ca.On("GetPassword").Return(password)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	ca.On("GetConnectionToDbWithUser", dbName, user, password).Return(conn, nil)
	ca.On("GetConnectionToDbWithUser", dbName2, user, password).Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	ca.On("GetConnectionToDb", dbName2).Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	generator := new(GeneratorMock)
	generator.On("Generate").Return(user)

	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getUser, userex).Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Exec", context.Background(), dropUser(userex)).Return(pgconn.CommandTag("DELETED"), nil)
	conn.On("Exec", context.Background(), dropUser(user)).Return(pgconn.CommandTag("DELETED"), nil)
	conn.On("Exec", context.Background(), setSchemasDefaultGrants(user, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleDefaultGrants(user, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createUser(userex, passwordex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName2)).Return(pgconn.CommandTag("UPDATED"), expectedErrorC)
	conn.On("Exec", context.Background(), alterOwnerMetaTable("")).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), AllowReplicationForUser(userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName2, userex)).Return(pgconn.CommandTag("UPDATED"), expectedErrorC)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminRoleSequencesGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleSequencesGrants(schema, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setSequencesRWDefaultGrants(user, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName2, userex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(userex, passwordex)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)

	rows.On("Next").Return(true).Times(7)
	rows.On("Close").Return(nil)

	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(29)

	sa := NewServiceAdapter(ca, apiVersion, roles, map[string]bool{FeatureMultiUsers: true})
	sa.Generator = generator
	rolesReq := []dao.AdditionalRole{{Id: "123", DbName: dbName, ConnectionProperties: []dao.ConnectionProperties{{
		"name":     dbName,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": user,
		"password": password,
		"role":     RORole,
	}},
		Resources: []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: user}},
	}}

	rolesReq = append(rolesReq, dao.AdditionalRole{
		Id:     "124",
		DbName: dbName2,
		ConnectionProperties: []dao.ConnectionProperties{{
			"name":     dbName2,
			"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
			"host":     host,
			"port":     port,
			"username": user,
			"password": password,
			"role":     RORole,
		}},
		Resources: []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: user}},
	})

	succ, failure := sa.CreateRoles(context.Background(), rolesReq)
	fmt.Printf("CreateRoles succ from failed case: %v\n", succ)
	fmt.Printf("CreateRoles failure: %v\n", failure)
	assert.NotEmpty(t, succ)
	assert.NotNil(t, failure)
	assert.NotEqual(t, 4, len(succ[0].ConnectionProperties))
	assert.NotEqual(t, 6, len(succ[0].Resources))
	assert.Equal(t, "can't set database grants", failure.Message)
}

func TestCreateRolesWithErrorCase(t *testing.T) {
	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	userName := "user1"
	userPassword := "password"
	dbName := "test_db1"
	emptyUser := ""
	schema := "public"
	prefixedUser := "dbaas_test_user"
	expectedErrorC := errors.New("can't set database grants")

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"dbaas_test_user": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}

	metadataJson, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(errParse))
		return
	}

	ca.On("GetUser").Return(user)
	ca.On("GetPassword").Return(userPassword)

	generator := new(GeneratorMock)
	generator.On("Generate").Return(user)
	generator.On("Generate").Return(userPassword)

	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	ca.On("GetConnectionToDbWithUser", dbName, user, userPassword).Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	fmt.Printf("my ac from connect func connection: %v\n", conn)

	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)

	conn.On("Query", context.Background(), getUser, prefixedUser).Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, expectedErrorC)
	conn.On("Exec", context.Background(), createUser(prefixedUser, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), AllowReplicationForUser(prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleSequencesGrants(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(prefixedUser, user)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, prefixedUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	rows.On("Next").Return(true).Times(2)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Close").Return(nil).Times(15)
	rows.On("Next").Return(false).Times(11)

	sa := NewServiceAdapter(ca, apiVersion, roles, map[string]bool{FeatureMultiUsers: false})
	sa.Generator = generator
	rolesReq := []dao.AdditionalRole{{Id: "123", DbName: dbName, ConnectionProperties: []dao.ConnectionProperties{{
		"name":     dbName,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": user,
		"password": password,
		"role":     RORole,
	}},
		Resources: []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: user}},
	}}
	succ, failed := sa.CreateRoles(context.Background(), rolesReq)
	fmt.Printf("my succ: %v\n", succ)
	fmt.Printf("my failed: %v\n", failed)
	assert.Equal(t, 0, len(succ))
	assert.NotNil(t, failed)
	assert.Equal(t, "can't set database grants", failed.Message)
}

func TestValidateRole(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	username := "valid_user"

	ca.On("GetConnection").Return(conn, nil)
	conn.On("Close", context.Background()).Return(nil)

	feature := map[string]bool{FeatureMultiUsers: true}
	sa := NewServiceAdapter(ca, apiVersion, roles, feature)

	t.Run("Valid user with 'ro' role", func(t *testing.T) {

		rows.On("Next").Return(true).Once()
		rows.On("Close").Return(nil)
		conn.On("Query", context.Background(), getMetadataGrantQuery(username, "SELECT")).Return(rows, nil)

		isValid, err := sa.validateRole(context.Background(), conn, "valid_user", "ro")

		assert.NoError(t, err)
		assert.True(t, isValid)
		rows.AssertExpectations(t)
	})

	t.Run("Valid user with 'rw' role", func(t *testing.T) {

		rows.On("Next").Return(true).Once()
		rows.On("Close").Return(nil)
		conn.On("Query", context.Background(), getMetadataGrantQuery(username, "UPDATE")).Return(rows, nil)

		isValid, err := sa.validateRole(context.Background(), conn, "valid_user", "rw")

		assert.NoError(t, err)
		assert.True(t, isValid)
		rows.AssertExpectations(t)
	})
}
