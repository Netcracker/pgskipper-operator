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

package client

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	pgx "github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"go.uber.org/zap"

	"github.com/Netcracker/pgskipper-operator/pkg/util"
)

var (
	instance *PostgresClient
	logger   = util.GetLogger()
	pgUser   = flag.String("pg_user", getEnv("PG_ADMIN_USER", "postgres"), "Username of admin user in PostgreSQL, env: PG_ADMIN_USER")
	pgPass   = flag.String("pg_pass", getEnv("PG_ADMIN_PASSWORD", ""), "Password of admin user in PostgreSQL, env: PG_ADMIN_PASSWORD")
	dbName   = "postgres"
	ssl      = "off"
)

type PostgresClient struct {
	adapter *postgresAdapter
}

type postgresAdapter struct {
	Pool     *pgxpool.Pool
	Host     string
	Port     int
	SSl      string
	User     string
	Password string
	Database string
	Health   string
}

func GetPostgresClient(pgHost string) *PostgresClient {
	if instance == nil {
		adapter := newAdapter(pgHost, 5432, *pgUser, *pgPass, dbName, ssl)
		if adapter != nil {
			instance = &PostgresClient{adapter: adapter}
		} else {
			return nil
		}
	}
	return instance
}

func GetPostgresClientForHost(pgHost_ string) *PostgresClient {
	return &PostgresClient{adapter: newAdapter(pgHost_, 5432, *pgUser, *pgPass, dbName, ssl)}
}

func UpdatePostgresClientPassword(pass string) {
	pgPass = &pass
	instance = nil
}

func (c *PostgresClient) GetConnection() (*pgxpool.Conn, error) {
	return c.adapter.GetConnection()
}

func (c *PostgresClient) GetConnectionToDb(dbName string) (*pgx.Conn, error) {
	return c.adapter.GetConnectionToDb(dbName)
}

func (c *PostgresClient) Execute(query string) error {
	conn, err := c.adapter.GetConnection()
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(context.Background(), query); err != nil {
		return err
	}
	return nil
}

func (c *PostgresClient) GetUser() string {
	return c.adapter.User
}

func (c *PostgresClient) GetDatabase() string {
	return c.adapter.Database
}

func (c *PostgresClient) ExecuteForDB(dbName, query string) error {
	conn, err := c.adapter.GetConnectionToDb(dbName)
	if err != nil {
		return err
	}
	defer func() {
		err := conn.Close(context.Background())
		if err != nil {
			logger.Error("Error during closing connection", zap.Error(err))
		}
	}()
	if _, err := conn.Exec(context.Background(), query); err == nil {
		return nil
	} else {
		return err
	}
}

func newAdapter(host string, port int, username string, password string, database string, ssl string) *postgresAdapter {

	connectionString := getConnectionUrl(username, password, database, host, port, ssl)
	var pool = &pgxpool.Pool{}
	conf, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		logger.Error("Postgres connection string cannot be parsed")
		return nil
	}
	pollErr := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		conf.ConnConfig.DialFunc = (&net.Dialer{
			KeepAlive: 30 * time.Second,
			Timeout:   10 * time.Second,
		}).DialContext
		pool, err = pgxpool.ConnectConfig(context.Background(), conf)
		if err != nil {
			logger.Error("Error during creation of cluster adapter, retrying", zap.Error(err))
			return false, nil
		}
		return true, nil
	})
	if pollErr != nil {
		return nil
	}
	adapter := &postgresAdapter{
		Pool:     pool,
		Host:     host,
		Port:     port,
		User:     username,
		Password: password,
		SSl:      ssl,
		Health:   "UP",
	}
	adapter.RequestHealth()
	return adapter
}

func (adapter *postgresAdapter) RequestHealth() string {
	adapter.Health = adapter.getHealth()
	return adapter.Health
}

func (adapter postgresAdapter) GetConnection() (*pgxpool.Conn, error) {
	conn, err := adapter.Pool.Acquire(context.Background())

	if err != nil {
		logger.Error("Error occurred during connect to DB", zap.Error(err))
		return nil, err
	}

	return conn, nil
}

func (adapter postgresAdapter) GetConnectionToDb(database string) (*pgx.Conn, error) {

	if database == "" {
		database = adapter.Database
	}

	conn, err := pgx.Connect(context.Background(), getConnectionUrl(adapter.User, adapter.Password, database, adapter.Host, adapter.Port, adapter.SSl))
	if err != nil {
		logger.Error("Error occurred during connect to DB", zap.Error(err))
		return nil, err
	}

	return conn, nil
}

func (adapter postgresAdapter) GetConnectionToDbWithUser(database string, username string, password string) (*pgx.Conn, error) {

	conn, err := pgx.Connect(context.Background(), getConnectionUrl(username, password, database, adapter.Host, adapter.Port, adapter.SSl))
	if err != nil {
		logger.Error("Error occurred during connect to DB", zap.Error(err))
		return nil, err
	}

	return conn, nil
}

func getConnectionUrl(username string, password string, database string, host string, port int, ssl string) string {
	username = url.PathEscape(username)
	password = url.PathEscape(password)
	database = url.PathEscape(database)
	if ssl == "on" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?%s", username, password, host, port, database, "sslmode=require")
	} else {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", username, password, host, port, database)
	}
}

func (adapter postgresAdapter) getHealth() string {
	err := adapter.executeHealthQuery()
	if err != nil {
		logger.Error("Postgres is unavailable", zap.Error(err))
		return "OUT_OF_SERVICE"
	} else {
		return "UP"
	}
}

func (adapter postgresAdapter) executeHealthQuery() error {
	conn, err := adapter.GetConnection()
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Exec(context.Background(), "SELECT 1")
	return err
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func EscapeString(str string) string {
	return strings.ReplaceAll(str, "'", "''")
}
