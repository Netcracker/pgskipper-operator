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

//nolint

package basic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

const (
	usernamePrefix1 = "dbaas"
	apiVersion      = dao.ApiVersion("v2")
)

const (
	AdminRole = "admin"
	RORole    = "ro"
	RWRole    = "rw"

	cKey  = "classifier"
	nsKey = "namespace"
	msKey = "microserviceName"
)

var roles = []string{AdminRole, RWRole, RORole}

var (
	user     = "test_user"
	password = "password"
	host     = "test_host"
	port     = 54321
	features = map[string]bool{}
	Database = "test_db"
	SSL      = "off"
)

type PostgresClusterAdapterMock struct {
	mock.Mock
	cluster.ClusterAdapter
}

type GeneratorMock struct {
	mock.Mock
	Generator
}

func (m *GeneratorMock) Generate() string {
	args := m.Called()
	return args.String(0)
}

func (ca *PostgresClusterAdapterMock) GetHost() string {
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetPort() int {
	return ca.Called().Int(0)
}

func (ca *PostgresClusterAdapterMock) GetUser() string {
	ca.Called()
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetPassword() string {
	ca.Called()
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetDatabase() string {
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetConnection(ctx context.Context) (cluster.Conn, error) {
	args := ca.Called()
	return args.Get(0).(cluster.Conn), args.Error(1)
}

func (ca *PostgresClusterAdapterMock) GetConnectionToDb(ctx context.Context, database string) (cluster.Conn, error) {
	args := ca.Called(database)
	return args.Get(0).(cluster.Conn), args.Error(1)
}

func (ca *PostgresClusterAdapterMock) GetConnectionToDbWithUser(ctx context.Context, database string, username string, password string) (cluster.Conn, error) {
	args := ca.Called(database, username, password)
	return args.Get(0).(cluster.Conn), args.Error(1)
}

type MockRow struct {
	mock.Mock
	pgx.Row
}

func (m *MockRow) Scan(dest ...interface{}) error {
	args := m.Called(dest)
	return args.Error(0)
}

type MockRows struct {
	mock.Mock
	pgx.Rows
}

func (m *MockRows) Next() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockRows) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockRows) Scan(dest ...interface{}) error {
	args := m.Called(dest)
	return args.Error(0)
}

func (m *MockRows) Close() {
	m.Called()
}

type MockConn struct {
	mock.Mock
	cluster.Conn
}

func (m *MockConn) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	args = append([]interface{}{ctx, query}, args...)
	ret := m.Called(args...)
	return ret.Get(0).(pgx.Rows), ret.Error(1)
}

func (m *MockConn) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	args = append([]interface{}{ctx, query}, args...)
	ret := m.Called(args...)
	return ret.Get(0).(pgx.Row)
}

func (m *MockConn) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	arguments = append([]interface{}{ctx, sql}, arguments...)
	ret := m.Called(arguments...)
	return ret.Get(0).(pgconn.CommandTag), ret.Error(1)
}

func (m *MockConn) Databases(ctx context.Context) ([]sql.DB, error) {
	args := m.Called(ctx)
	databases := args.Get(0)
	if databases != nil {
		return args.Get(0).([]sql.DB), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConn) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestGetDatabases(t *testing.T) {
	expectedDBNames := []string{"test_db_1", "test_db_2"}

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	//var err error

	ca.On("GetConnection").Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)

	conn.On("Query", context.Background(), getDatabases).Return(rows, nil)
	conn.On("Close", context.Background()).Return(nil)

	rows.On("Next").Return(true).Times(2)
	rows.On("Next").Return(false).Once()
	rows.On("Close").Return(nil)

	for _, roles := range expectedDBNames {
		roles := roles
		rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			arg := args.Get(0).([]interface{})
			strArg := arg[0].(*string)
			*strArg = roles
		}).Once()
	}

	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	databases := sa.GetDatabases(context.Background())
	assert.ElementsMatch(t, expectedDBNames, databases)
}

func TestDescribeDatabases(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	databaseDesc := sa.DescribeDatabases(context.Background(), []string{Database}, false, false)
	fmt.Printf("databaseDesc: %v\n", databaseDesc)
	assert.NotNil(t, databaseDesc)

}

func (m *MockConn) Database(ctx context.Context) ([]sql.DB, error) {
	args := m.Called(ctx)
	databases := args.Get(0)
	if databases != nil {
		return args.Get(0).([]sql.DB), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConn) DatabaseExists(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

func (m *MockConn) Remove(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestDropResources_Database(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	row := new(MockRow)
	connDb := new(MockConn)
	testId := "foobar"

	ca.On("GetConnection").Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)
	dbName := "db1"
	conn.On("GetDatabase").Return(dbName)
	conn.On("Query", context.Background(), getDatabase, dbName).Return(rows, nil)
	conn.On("QueryRow", context.Background(), existsRollbackTransactions, dbName).Return(row)
	conn.On("Exec", context.Background(), dropDatabase(dbName)).Return(pgconn.CommandTag("DELETED"), nil)
	conn.On("Exec", context.Background(), prohibitConnectionsToDb, dbName).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), dropConnectionsToDb, dbName).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Close", context.Background()).Return(nil)
	row.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]interface{})
		answ := arg[0].(*bool)
		*answ = true
	})
	rows.On("Next").Return(true).Times(2)
	rows.On("Next").Return(false).Once()
	rows.On("Close").Return(nil)

	ca.On("GetConnectionToDb", dbName).Return(connDb, nil)
	connDb.On("Query", context.Background(), selectPreparedTransactions, dbName).Return(rows, nil)
	connDb.On("Exec", context.Background(), rollbackPreparedByGid(testId)).Return(pgconn.CommandTag("DELETED"), nil)
	connDb.On("Close", context.Background()).Return(nil)
	rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]interface{})
		answ := arg[0].(*string)
		*answ = testId
	})

	resourceForDrop := []dao.DbResource{{Kind: DbKind, Name: dbName}}
	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	droppedRes := sa.DropResources(context.Background(), resourceForDrop)
	fmt.Printf("my dropped resources: %v\n", droppedRes)
	assert.Equal(t, dao.DELETED, droppedRes[0].Status)

}

func TestDropResources_DatabaseFailedCase(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	row := new(MockRow)
	connDb := new(MockConn)
	testId := "foobar"

	ca.On("GetConnection").Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)
	dbName := "db1"
	expectedErrorC := errors.New("error while execute drop database")
	conn.On("GetDatabase").Return(dbName)
	conn.On("Query", context.Background(), getDatabase, dbName).Return(rows, nil)
	conn.On("QueryRow", context.Background(), existsRollbackTransactions, dbName).Return(row)
	conn.On("Exec", context.Background(), dropDatabase(dbName)).Return(pgconn.CommandTag("DELETED"), expectedErrorC)
	conn.On("Exec", context.Background(), prohibitConnectionsToDb, dbName).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), dropConnectionsToDb, dbName).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Close", context.Background()).Return(nil)
	row.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]interface{})
		answ := arg[0].(*bool)
		*answ = true
	})
	rows.On("Next").Return(true).Times(2)
	rows.On("Next").Return(false).Once()
	rows.On("Close").Return(nil)

	ca.On("GetConnectionToDb", dbName).Return(connDb, nil)
	connDb.On("Query", context.Background(), selectPreparedTransactions, dbName).Return(rows, nil)
	connDb.On("Exec", context.Background(), rollbackPreparedByGid(testId)).Return(pgconn.CommandTag("DELETED"), nil)
	connDb.On("Close", context.Background()).Return(nil)
	rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]interface{})
		answ := arg[0].(*string)
		*answ = testId
	})

	resourceForDrop := []dao.DbResource{{Kind: DbKind, Name: dbName}}
	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	droppedRes := sa.DropResources(context.Background(), resourceForDrop)
	fmt.Printf("dropResource : %v\n", droppedRes)
	assert.Equal(t, dao.DELETE_FAILED, droppedRes[0].Status)
	assert.Equal(t, "error while execute drop database", droppedRes[0].ErrorMessage)

}

type UserMock struct {
	mock.Mock
	dao.CreatedUser
}

func (m *MockConn) User(ctx context.Context, name string) (string, error) {
	args := m.Called(ctx, name)
	user := args.Get(0)
	if user != nil {
		return args.Get(0).(string), args.Error(1)
	}
	return name, args.Error(1)
}

func TesDropResources__UserFailedCase(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)

	ca.On("GetConnection").Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)
	expectedErrorC := errors.New("error while execute drop database")

	user := new(UserMock)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)

	userName := "user1"
	resourceForDrop := []dao.DbResource{{Kind: UserKind, Name: userName}}
	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(user, nil).Once()
	conn.On("Exec", context.Background(), dropUser(userName)).Return(pgconn.CommandTag("DELETED"), expectedErrorC)
	droppedRes := sa.DropResources(context.Background(), resourceForDrop)
	fmt.Printf("my dropped resources from user: %v\n", droppedRes)
	fmt.Printf("dropResource : %v\n", droppedRes)
	assert.Equal(t, dao.DELETE_FAILED, droppedRes[0].Status)
	assert.Equal(t, "error while execute drop user", droppedRes[0].ErrorMessage)

}

func TesDropResources_User(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)

	ca.On("GetConnection").Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)

	user := new(UserMock)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)

	userName := "user1"
	resourceForDrop := []dao.DbResource{{Kind: UserKind, Name: userName}}
	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(user, nil).Once()
	conn.On("Exec", context.Background(), dropUser(userName)).Return(pgconn.CommandTag("DELETED"), nil)
	droppedRes := sa.DropResources(context.Background(), resourceForDrop)
	fmt.Printf("my dropped resources from user: %v\n", droppedRes)
	assert.Equal(t, resourceForDrop, droppedRes)
	assert.ElementsMatch(t, resourceForDrop, droppedRes)

}

func TestCreateUser_UserExist(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	dbName := "db1"

	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)

	conn.On("Close", context.Background()).Return(nil)

	userName := "user1"
	userPassword := "password1"
	userRole := AdminRole
	var err error
	schema := "public"
	emptyUser := ""

	featuresWithMult := map[string]bool{FeatureMultiUsers: true}
	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"user1": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}
	metadataJson, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(err))
		return
	}
	userCreateRequest := dao.UserCreateRequest{DbName: dbName, Role: userRole, Password: userPassword}

	ca.On("GetUser").Return(userName)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("Query", context.Background(), getUser, userName).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createUser(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleSequencesGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleSequencesGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminRoleSequencesGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setSequencesRWDefaultGrants(userName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setSequencesRODefaultGrants(userName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(userName, nil)
	conn.On("DatabaseExists", context.Background(), mock.AnythingOfType("string")).Return(true, nil)

	rows.On("Name").Return(userName)

	rows.On("Next").Return(true).Times(3)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(6)

	sa := NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)
	createdUser, err := sa.CreateUser(context.Background(), userName, userCreateRequest)
	assert.Empty(t, err)
	fmt.Printf("my created user: %v\n", createdUser.Name)
	assert.Equal(t, dbName, createdUser.Name)
	assert.Equal(t, []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: userName}}, createdUser.Resources)
	assert.Equal(t, dao.ConnectionProperties{
		"name":     dbName,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": userName,
		"password": userPassword,
		"role":     userRole,
	}, createdUser.ConnectionProperties)

	//RW USer
	featuresWithMult = map[string]bool{FeatureMultiUsers: true}
	metadata = map[string]interface{}{
		RolesKey: map[string]string{
			"user1": "rw",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}
	metadataJson, errParse = json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(err))
		return
	}
	userCreateRequest = dao.UserCreateRequest{DbName: dbName, Role: "rw", Password: userPassword}

	ca.On("GetUser").Return(userName)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("Query", context.Background(), getUser, userName).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Exec", context.Background(), setReadWriteRoleGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setSchemasDefaultGrants(userName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadWriteRoleDefaultGrants(userName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createUser(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(userName, nil)
	conn.On("DatabaseExists", context.Background(), mock.AnythingOfType("string")).Return(true, nil)

	rows.On("Name").Return(userName)

	rows.On("Next").Return(true).Times(3)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(6)

	fmt.Printf("my connection from create user: %v\n", conn)

	sa = NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)
	createdUser, err = sa.CreateUser(context.Background(), userName, userCreateRequest)
	assert.Empty(t, err)
	fmt.Printf("my created user: %v\n", createdUser.Name)
	assert.Equal(t, dbName, createdUser.Name)
	assert.Equal(t, []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: userName}}, createdUser.Resources)
	assert.Equal(t, dao.ConnectionProperties{
		"name":     dbName,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": userName,
		"password": userPassword,
		"role":     "rw",
	}, createdUser.ConnectionProperties)

	//ROUser
	featuresWithMult = map[string]bool{FeatureMultiUsers: true}
	metadata = map[string]interface{}{
		RolesKey: map[string]string{
			"user1": "ro",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}
	metadataJson, errParse = json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(err))
		return
	}
	userCreateRequest = dao.UserCreateRequest{DbName: dbName, Role: "ro", Password: userPassword}

	ca.On("GetUser").Return(userName)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("Query", context.Background(), getUser, userName).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleDefaultGrants(userName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createUser(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(userName, nil)
	conn.On("DatabaseExists", context.Background(), mock.AnythingOfType("string")).Return(true, nil)

	rows.On("Name").Return(userName)

	rows.On("Next").Return(true).Times(3)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(6)

	fmt.Printf("my connection from create user: %v\n", conn)

	sa = NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)
	createdUser, err = sa.CreateUser(context.Background(), userName, userCreateRequest)
	assert.Empty(t, err)
	fmt.Printf("my created user: %v\n", createdUser.Name)
	assert.Equal(t, dbName, createdUser.Name)
	assert.Equal(t, []dao.DbResource{{Kind: DbKind, Name: dbName}, {Kind: UserKind, Name: userName}}, createdUser.Resources)
	assert.Equal(t, dao.ConnectionProperties{
		"name":     dbName,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbName),
		"host":     host,
		"port":     port,
		"username": userName,
		"password": userPassword,
		"role":     "ro",
	}, createdUser.ConnectionProperties)

	//InvalidRole - Here Error will not be empty
	featuresWithMult = map[string]bool{FeatureMultiUsers: true}
	metadata = map[string]interface{}{
		"roles": map[string]string{
			"user1": "ro",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}
	metadataJson, errParse = json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(err))
		return
	}
	userCreateRequest = dao.UserCreateRequest{DbName: dbName, Role: "Invalid Role", Password: userPassword}

	ca.On("GetUser").Return(userName)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("Query", context.Background(), getUser, userName).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setReadOnlyRoleDefaultGrants(userName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), allowConnectionsToDb(dbName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createUser(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), changeUserPassword(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantAllRightsOnDatabase(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)

	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(userName, nil)
	conn.On("DatabaseExists", context.Background(), mock.AnythingOfType("string")).Return(true, nil)

	rows.On("Name").Return(userName)

	rows.On("Next").Return(true).Times(3)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(6)

	sa = NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)
	createdUser, err = sa.CreateUser(context.Background(), userName, userCreateRequest)
	fmt.Printf("my created user with error: %v\n", createdUser)
	assert.NotNil(t, err)
}

func TestCreateDatabase(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	generator := new(GeneratorMock)
	var err error

	userName := "user1"
	userPassword := "test_password"
	namePrefix := "test_prefix"
	dbName := "test_database_name"
	schema := "public"
	emptyUser := ""
	grantUserSQL := fmt.Sprintf("GRANT ALL ON SCHEMA %s TO %s", schema, userName)

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"user1": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}

	metadataJson, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(err))
		return
	}

	user := new(UserMock)
	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	ca.On("GetConnectionToDbWithUser", dbName, userName, userPassword).Return(conn, nil)
	ca.On("GetUser").Return(userName)
	fmt.Printf("my ac from connect func connection: %v\n", conn)

	conn.On("Close", context.Background()).Return(nil)

	ca.On("GetUser").Return(userName)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)

	generator.On("Generate").Return(dbName).Once()
	for i := range roles {
		generator.On("Generate").Return(fmt.Sprintf("user%d", i)).Once()
		generator.On("Generate").Return(fmt.Sprintf("password%d", i)).Once()
	}

	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(user, nil)

	conn.On("Name").Return(dbName, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Exec", context.Background(), createUser(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminRoleSequencesGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantUserSQL).Return(pgconn.CommandTag("UPDATED"), nil)

	// multiusers disabled
	featuresWithMult := map[string]bool{FeatureMultiUsers: false}

	request := dao.DbCreateRequest{Metadata: map[string]interface{}{}, NamePrefix: &namePrefix, Password: userPassword, DbName: dbName, Settings: map[string]interface{}{}, Username: userName}

	rows.On("Next").Return(true).Times(4)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(8)

	sa := NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)
	sa.Generator = generator

	setting, err := sa.getPgSettings(context.Background(), request.Settings)
	assert.Nil(t, err)

	conn.On("Exec", context.Background(), createDatabase(dbName, setting.CreationParameters)).Return(pgconn.CommandTag("UPDATED"), nil)

	dbNameExp := dbName

	dbNameRes, dbDesc, err := sa.CreateDatabase(context.Background(), request)
	assert.Nil(t, err)

	conn.On("Exec", context.Background(), createDatabase(dbName, setting.CreationParameters)).Return(pgconn.CommandTag("UPDATED"), nil)

	assert.Equal(t, dbNameExp, dbNameRes)
	expCp := dao.ConnectionProperties{
		"host":     host,
		"name":     dbNameExp,
		"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, dbNameExp),
		"port":     port,
		"username": "user1",
		"password": "test_password",
		"role":     AdminRole,
	}
	assert.Equal(t, expCp, dbDesc.ConnectionProperties[0])
}

func TestCreateDatabaseMultiUser(t *testing.T) {
	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	generator := new(GeneratorMock)
	var err error

	userName := "user"
	userPassword := "password"
	namePrefix := "test_prefix"
	dbName := "test_database_name"
	schema := "public"
	emptyUser := ""

	namespace := "test-namespace"
	msName := "test-microservice"
	classifier := map[string]interface{}{nsKey: namespace, msKey: msName}

	// initialize database part with matcher
	genDBNameWithoutTS := fmt.Sprintf("%s_", namePrefix)
	dbArgMatcher := mock.MatchedBy(func(dbName string) bool { return strings.Contains(dbName, genDBNameWithoutTS) })

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"dbaas_user0": "admin",
			"dbaas_user1": "rw",
			"dbaas_user2": "ro",
		},
		RolesVersionKey: util.GetRolesVersion(),
		cKey:            classifier,
	}

	metadataJson, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(err))
		return
	}

	user := new(UserMock)
	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", dbArgMatcher).Return(conn, nil)
	ca.On("GetConnectionToDbWithUser", dbArgMatcher, userName, userPassword).Return(conn, nil)

	fmt.Printf("my ac from connect func connection: %v\n", conn)

	conn.On("Close", context.Background()).Return(nil)

	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)

	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(user, nil)

	for i := range roles {
		userName = fmt.Sprintf("dbaas_user%d", i)
		generator.On("Generate").Return(fmt.Sprintf("user%d", i)).Once()
		generator.On("Generate").Return(fmt.Sprintf("password%d", i)).Once()

		grantUserSQL := fmt.Sprintf("GRANT ALL ON SCHEMA %s TO %s", schema, (fmt.Sprintf("dbaas_user%d", i)))

		ca.On("GetConnectionToDbWithUser", dbArgMatcher, (fmt.Sprintf("dbaas_user%d", i)), (fmt.Sprintf("password%d", i))).Return(conn, nil)
		conn.On("Name").Return(dbName, nil)
		ca.On("GetUser").Return(userName)
		conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
		conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
		conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
		conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
		conn.On("Exec", context.Background(), setReadOnlyRoleSequencesGrants(schema, "dbaas_user2")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setReadWriteRoleSequencesGrants(schema, "dbaas_user1")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setAdminRoleSequencesGrants(schema, "dbaas_user0")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setSequencesRWDefaultGrants("dbaas_user0", "dbaas_user1")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setSequencesRODefaultGrants("dbaas_user0", "dbaas_user2")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setSchemasDefaultGrants("dbaas_user0", "dbaas_user1")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setReadWriteRoleDefaultGrants("dbaas_user0", "dbaas_user1")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setReadOnlyRoleDefaultGrants("dbaas_user0", "dbaas_user2")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setSchemasDefaultGrants("dbaas_user0", "dbaas_user2")).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), grantSchemaCreate(schema, (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setReadOnlyRoleGrants(schema, (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setReadWriteRoleGrants(schema, (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), mock.MatchedBy(getMatcherFuncWithUser(genDBNameWithoutTS, i))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), createUser((fmt.Sprintf("dbaas_user%d", i)), (fmt.Sprintf("password%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), alterOwnerMetaTable((fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), grantSchemaCreate(schema, (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), grantConnectionToDB((fmt.Sprintf("dbaas_user%d", i)), (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), grantSchemaUsage(schema, (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), setAdminGrants(schema, (fmt.Sprintf("dbaas_user%d", i)))).Return(pgconn.CommandTag("UPDATED"), nil)
		conn.On("Exec", context.Background(), grantUserSQL).Return(pgconn.CommandTag("UPDATED"), nil)
	}

	// multiusers enabled
	featuresWithMult := map[string]bool{FeatureMultiUsers: true}

	request := dao.DbCreateRequest{Metadata: metadata, NamePrefix: &namePrefix, Password: "", DbName: "", Settings: map[string]interface{}{}, Username: ""}

	rows.On("Next").Return(true).Times(4)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]interface{})
		metadataForObtain := arg[0].(*map[string]interface{})
		*metadataForObtain = metadata
	}).Return(nil)
	rows.On("Next").Return(false).Times(8)

	sa := NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)
	sa.Generator = generator

	setting, err := sa.getPgSettings(context.Background(), request.Settings)
	fmt.Printf("my settings for pg db: %v\n", setting)
	assert.Nil(t, err)

	conn.On("Exec", context.Background(), mock.MatchedBy(
		func(query string) bool {
			genEmpty := createDatabase("", setting.CreationParameters)
			queryArr := strings.Split(query, "\"")
			emptyQueryArr := strings.Split(genEmpty, "\"")
			return queryArr[0] == emptyQueryArr[0] && strings.Contains(queryArr[1], genDBNameWithoutTS)
		})).Return(pgconn.CommandTag("UPDATED"), nil)

	dbNameRes, dbDesc, err := sa.CreateDatabase(context.Background(), request)
	assert.Nil(t, err)
	assert.Contains(t, dbNameRes, genDBNameWithoutTS)

	for i, r := range roles {
		found := false
		if dbDesc != nil {
			for _, cp := range dbDesc.ConnectionProperties {
				if cp["role"] == r {
					found = true
					expCp := dao.ConnectionProperties{
						"host":     host,
						"name":     cp["name"],
						"url":      fmt.Sprintf("jdbc:postgresql://%s:%d/%s", host, port, cp["name"]),
						"port":     port,
						"username": fmt.Sprintf("dbaas_user%d", i),
						"password": fmt.Sprintf("password%d", i),
						"role":     r,
					}
					assert.Equal(t, expCp, cp)
					assert.Contains(t, cp["name"], genDBNameWithoutTS)
				}

			}
		}
		assert.True(t, found)
	}
}

func getMatcherFuncWithUser(dbName string, i int) func(query string) bool {
	return func(query string) bool {
		genEmpty := grantConnectionToDB("", fmt.Sprintf("dbaas_user%d", i))
		queryArr := strings.Split(query, "\"")
		emptyQueryArr := strings.Split(genEmpty, "\"")
		return queryArr[0] == emptyQueryArr[0] && strings.Contains(queryArr[1], dbName) && queryArr[2] == emptyQueryArr[2]
	}
}

func TestCreateDatabasePanics(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)
	var err error

	userName := "user1"
	userPassword := "test_password"
	namePrefix := "test_prefix"
	dbName := "test_database_name"
	schema := "public"
	emptyUser := ""
	grantUserSQL := fmt.Sprintf("GRANT ALL ON SCHEMA %s TO %s", schema, userName)

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"user1": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}
	metadataJson, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata", zap.Error(errParse))
		return
	}

	user := new(UserMock)
	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", dbName).Return(conn, nil)
	ca.On("GetConnectionToDbWithUser", dbName, userName, userPassword).Return(conn, nil)
	ca.On("GetUser").Return(userName)
	fmt.Printf("my ac from connect func connection: %v\n", conn)

	conn.On("Close", context.Background()).Return(nil)

	ca.On("GetUser").Return(userName)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)

	conn.On("isUserExist", context.Background(), mock.Anything, mock.AnythingOfType("string")).Return(true, nil)
	conn.On("User", context.Background(), mock.AnythingOfType("string")).Return(user, nil)

	conn.On("Name").Return(dbName, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getOwnerForMetaData).Return(rows, nil)
	conn.On("Exec", context.Background(), createUser(userName, userPassword)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaCreate(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createSchema(schema)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), alterOwnerMetaTable(emptyUser)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), revokeRights()).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantConnectionToDB(dbName, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantSchemaUsage(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), setAdminGrants(schema, userName)).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), grantUserSQL).Return(pgconn.CommandTag("UPDATED"), nil)

	// multiusers disabled
	featuresWithMult := map[string]bool{FeatureMultiUsers: false}
	request := dao.DbCreateRequest{Metadata: map[string]interface{}{
		"roles": map[string]string{
			"user1": "admin",
		}}, NamePrefix: &namePrefix, Password: userPassword, DbName: dbName, Settings: map[string]interface{}{}, Username: userName}

	rows.On("Next").Return(true).Times(4)
	rows.On("Close").Return(nil)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(8)

	sa := NewServiceAdapter(ca, apiVersion, roles, featuresWithMult)

	setting, err := sa.getPgSettings(context.Background(), request.Settings)
	assert.Nil(t, err)

	conn.On("Exec", context.Background(), createDatabase(dbName, setting.CreationParameters)).Return(pgconn.CommandTag("UPDATED"), errors.New("Error while create database"))

	dbNameExp := dbName

	assert.PanicsWithError(t, "Error while create database", func() {
		_, _, err := sa.CreateDatabase(context.Background(), request)
		assert.Error(t, err)
	})

	fmt.Printf("data : %v\n", dbNameExp)

}

func Test_UpdateMetadata(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	ca.On("GetDatabase").Return(Database)

	conn := new(MockConn)
	rows := new(MockRows)

	ca.On("GetConnectionToDb", Database).Return(conn, nil)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)

	// New metadata
	metadata := map[string]interface{}{
		cKey: map[string]string{msKey: "test", nsKey: "test-ns"},
	}

	// Old metadata
	oldMetadata := map[string]interface{}{
		RolesKey: map[string]interface{}{
			"test_user_name": "admin",
		},
		cKey: map[string]string{msKey: "testOld", nsKey: "test-ns-old"},
	}

	// Merged metadata to save
	resultMetadata := map[string]interface{}{
		cKey: map[string]string{msKey: "test", nsKey: "test-ns"},
		RolesKey: map[string]string{
			"test_user_name": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}

	metadataJson, errParse := json.Marshal(resultMetadata)
	if errParse != nil {
		log.Error("Error during marshal metadata")
		return
	}

	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil).Once()
	rows.On("Next").Return(true).Once()
	rows.On("Scan", mock.Anything).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]interface{})
		strArg1 := arg[0].(*map[string]interface{})
		*strArg1 = oldMetadata
	}).Return(nil).Once()

	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), deleteMetaData).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), insertIntoMetaTable, "metadata", metadataJson).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Exec", context.Background(), createMetaTable).Return(pgconn.CommandTag("UPDATED"), nil)
	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Close", context.Background()).Return(nil)
	rows.On("Close").Return(nil)

	rows.On("Next").Return(true).Times(2)
	rows.On("Scan", mock.Anything).Return(nil)
	rows.On("Next").Return(false).Times(2)

	sa.UpdateMetadata(context.Background(), metadata, Database)
}

func Test_GetMetadataInternal(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	ca.On("GetDatabase").Return(Database)

	conn := new(MockConn)
	rows := new(MockRows)

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"test_user_name": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}

	ca.On("GetConnectionToDb", Database).Return(conn, nil)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)

	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Close", context.Background()).Return(nil)
	rows.On("Close").Return(nil)

	rows.On("Next").Return(true).Times(2)

	rolesMap := metadata[RolesKey].(map[string]string)

	for userName, role := range rolesMap {
		rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			arg := args.Get(0).([]interface{})
			strArg1 := arg[0].(*map[string]interface{})

			if *strArg1 == nil {
				*strArg1 = make(map[string]interface{})
			}

			rolesMap := (*strArg1)[RolesKey]
			if rolesMap == nil {
				rolesMap = make(map[string]string)
				(*strArg1)[RolesKey] = rolesMap
			}

			rolesMap.(map[string]string)[userName] = role
			(*strArg1)[RolesVersionKey] = util.GetRolesVersion()
		}).Times(2)
	}

	rows.On("Next").Return(false).Times(2)

	_, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata")
		return
	}

	fmt.Printf("metadata : %v\n", metadata)
	fmt.Printf("metadatajsonerror : %v\n", errParse)

	data, _ := sa.GetMetadataInternal(context.Background(), Database)
	fmt.Printf("data : %v\n", data)

	assert.Equal(t, metadata, data)

}

func TestGetSchema(t *testing.T) {

	expectedSchemaNames := []string{"test_schema_1", "test_schema_2"}

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)
	rows := new(MockRows)

	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetConnectionToDb", Database).Return(conn, nil)
	fmt.Printf("my ac from connect func connection: %v\n", conn)

	conn.On("Query", context.Background(), getDatabases).Return(rows, nil)
	conn.On("Query", context.Background(), getSchemasQuery()).Return(rows, nil)
	conn.On("Close", context.Background()).Return(nil)

	rows.On("Next").Return(true).Times(2)
	rows.On("Next").Return(false).Once()
	rows.On("Close").Return(nil)

	for _, schemas := range expectedSchemaNames {
		schemas := schemas
		rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			arg := args.Get(0).([]interface{})
			strArg := arg[0].(*string)
			*strArg = schemas
		}).Once()
	}

	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	desiredSchema, err := sa.GetSchemas(Database)

	fmt.Printf("my schemas from connect func connection: %v\n", desiredSchema)
	if err != nil {
		panic(errors.New("This is an explicit error"))
	}
	assert.ElementsMatch(t, expectedSchemaNames, desiredSchema)
}

func TestGetDefaultCreateRequest(t *testing.T) {
	ca := new(PostgresClusterAdapterMock)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	request := sa.GetDefaultCreateRequest()
	assert.Equal(t, dao.DbCreateRequest{}, request)

}

func TestGetDefaultUserCreateRequest(t *testing.T) {
	ca := new(PostgresClusterAdapterMock)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)
	request := sa.GetDefaultUserCreateRequest()
	assert.Equal(t, dao.UserCreateRequest{}, request)

}

func Test_GetMetadata(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	ca.On("GetDatabase").Return(Database)

	conn := new(MockConn)
	rows := new(MockRows)

	metadata := map[string]interface{}{
		RolesKey: map[string]string{
			"test_user_name": "admin",
		},
		RolesVersionKey: util.GetRolesVersion(),
	}

	ca.On("GetConnectionToDb", Database).Return(conn, nil)

	sa := NewServiceAdapter(ca, apiVersion, roles, features)

	conn.On("Query", context.Background(), getAllMetadata).Return(rows, nil)
	conn.On("Query", context.Background(), getMetadata, "metadata").Return(rows, nil)
	conn.On("Close", context.Background()).Return(nil)
	rows.On("Close").Return(nil)

	rows.On("Next").Return(true).Times(2)

	rolesMap := metadata["roles"].(map[string]string)

	for userName, role := range rolesMap {
		rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			arg := args.Get(0).([]interface{})
			strArg1 := arg[0].(*map[string]interface{})

			if *strArg1 == nil {
				*strArg1 = make(map[string]interface{})
			}

			rolesMap := (*strArg1)["roles"]
			if rolesMap == nil {
				rolesMap = make(map[string]string)
				(*strArg1)["roles"] = rolesMap
			}

			rolesMap.(map[string]string)[userName] = role
			(*strArg1)[RolesVersionKey] = util.GetRolesVersion()
		}).Times(2)
	}

	rows.On("Next").Return(false).Times(2)

	_, errParse := json.Marshal(metadata)
	if errParse != nil {
		log.Error("Error during marshal metadata")
		return
	}

	fmt.Printf("metadata : %v\n", metadata)
	fmt.Printf("metadatajsonerror : %v\n", errParse)

	data := sa.GetMetadata(context.Background(), Database)
	fmt.Printf("data : %v\n", data)

	assert.Equal(t, metadata, data)

}

func TestValidatePostgresAvailableExtensions(t *testing.T) {
	t.Run("Returns true if all extensions are available", func(t *testing.T) {

		expectedExtensionNames := []string{"ext1", "ext2"}
		ca := new(PostgresClusterAdapterMock)
		ca.On("GetHost").Return(host)
		ca.On("GetPort").Return(port)
		ca.On("GetDatabase").Return(Database)

		conn := new(MockConn)
		rows := new(MockRows)

		ca.On("GetConnection").Return(conn, nil)
		conn.On("Close", context.Background()).Return(nil)
		conn.On("Query", context.Background(), selectPostgresAvailableExtensions).Return(rows, nil)

		rows.On("Next").Return(true).Twice()

		for _, exts := range expectedExtensionNames {
			exts := exts
			rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
				arg := args.Get(0).([]interface{})
				strArg := arg[0].(*string)
				*strArg = exts
			}).Once()
		}

		rows.On("Next").Return(false).Once()

		sa := NewServiceAdapter(ca, apiVersion, roles, features)

		result, err := sa.ValidatePostgresAvailableExtensions(context.Background(), []string{"ext1", "ext2"})
		fmt.Printf("available extensions : %v\n", result)
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("Returns false if an extension is not available", func(t *testing.T) {
		expectedExtensionNames := []string{"ext1", "ext2"}
		ca := new(PostgresClusterAdapterMock)
		ca.On("GetHost").Return(host)
		ca.On("GetPort").Return(port)
		ca.On("GetDatabase").Return(Database)

		conn := new(MockConn)
		rows := new(MockRows)

		ca.On("GetConnection").Return(conn, nil)
		conn.On("Close", context.Background()).Return(nil)
		conn.On("Query", context.Background(), selectPostgresAvailableExtensions).Return(rows, nil)

		rows.On("Next").Return(true).Once()

		for _, exts := range expectedExtensionNames {
			exts := exts
			rows.On("Scan", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
				arg := args.Get(0).([]interface{})
				strArg := arg[0].(*string)
				*strArg = exts
			}).Once()
		}

		rows.On("Next").Return(false).Once()

		sa := NewServiceAdapter(ca, apiVersion, roles, features)

		result, err := sa.ValidatePostgresAvailableExtensions(context.Background(), []string{"ext1", "ext2"})
		fmt.Printf("available extensions : %v\n", result)

		assert.NoError(t, err)
		assert.False(t, result)
	})
}

func TestCreateExtFromSlice(t *testing.T) {
	t.Run("Creates extensions successfully", func(t *testing.T) {

		ca := new(PostgresClusterAdapterMock)
		ca.On("GetHost").Return(host)
		ca.On("GetPort").Return(port)
		ca.On("GetDatabase").Return(Database)

		conn := new(MockConn)

		ca.On("GetConnection").Return(conn, nil)

		conn.On("Exec", mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag(""), nil).Twice()
		CreateExtFromSlice(context.Background(), conn, "", []string{"ext1", "ext2"})

		conn.AssertCalled(t, "Exec", context.Background(), createExtensionIfNotExist("ext1"), mock.Anything)
		conn.AssertCalled(t, "Exec", context.Background(), createExtensionIfNotExist("ext2"), mock.Anything)
	})

	t.Run("Creates extensions successfully when username is nil", func(t *testing.T) {

		ca := new(PostgresClusterAdapterMock)
		ca.On("GetHost").Return(host)
		ca.On("GetPort").Return(port)
		ca.On("GetDatabase").Return(Database)

		conn := new(MockConn)
		var OracleFdw = "oracle_fdw"
		userName := "user1"

		ca.On("GetConnection").Return(conn, nil)

		conn.On("Exec", mock.Anything, mock.Anything, mock.Anything).Return(pgconn.CommandTag(""), nil).Times(4)
		CreateExtFromSlice(context.Background(), conn, userName, []string{"ext1", "ext2", OracleFdw})

		conn.AssertCalled(t, "Exec", context.Background(), createExtensionIfNotExist("ext1"), mock.Anything)
		conn.AssertCalled(t, "Exec", context.Background(), createExtensionIfNotExist("ext2"), mock.Anything)
		conn.AssertCalled(t, "Exec", context.Background(), createExtensionIfNotExist(OracleFdw), mock.Anything)

		conn.AssertCalled(t, "Exec", context.Background(), grantRightsForFdw(OracleFdw, userName))

	})

}
