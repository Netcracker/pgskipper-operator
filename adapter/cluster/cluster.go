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

package cluster

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"go.uber.org/zap"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/jackc/pgconn"
)

const (
	HealthUP  = "UP"
	HealthOOS = "OUT_OF_SERVICE"
)

var (
	log         = util.GetLogger()
	connTimeout = time.Duration(util.GetEnvInt("PG_CONN_TIMEOUT_SEC", 20))
)

type Conn interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Close()
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

type ClusterAdapter interface {
	GetConnection(ctx context.Context) (Conn, error)
	GetConnectionToDb(ctx context.Context, database string) (Conn, error)
	GetConnectionToDbWithUser(ctx context.Context, database string, username string, password string) (Conn, error)
	GetUser() string
	GetPassword() string
	GetHost() string
	GetPort() int
}

type ClusterAdapterImpl struct {
	Pool     *pgxpool.Pool
	Host     string
	Port     int
	SSl      string
	User     string
	Password string
	Database string
	Health   string
	log      *zap.Logger
}

func NewAdapter(host string, port int, username, password string, database string, ssl string) *ClusterAdapterImpl {
	username = url.PathEscape(username)
	password = url.PathEscape(password)

	c := &ClusterAdapterImpl{
		Host:     host,
		Port:     port,
		User:     username,
		Password: password,
		SSl:      ssl,
		Health:   HealthUP,
		Database: database,
		log:      util.GetLogger(),
	}
	log.Debug(fmt.Sprintf("Checking connection for host=%s port=%d with database %s", host, port, database))
	c.RequestHealth()
	return c
}

func (ca ClusterAdapterImpl) RequestHealth() string {
	ch := make(chan string, 1)
	go func() {
		ch <- ca.getHealth()
	}()

	select {
	case healthStatus := <-ch:
		if healthStatus == HealthOOS {
			panic(fmt.Errorf("postgres is unavailable"))
		}
		ca.Health = healthStatus
	case <-time.After(connTimeout * time.Second):
		panic("postgres connection timeout expired")
	}

	return ca.Health
}

func (ca ClusterAdapterImpl) GetPort() int {
	return ca.Port
}

func (ca ClusterAdapterImpl) GetUser() string {
	return ca.User
}

func (ca ClusterAdapterImpl) GetPassword() string {
	return ca.Password
}

func (ca ClusterAdapterImpl) GetHost() string {
	return ca.Host
}

func (ca ClusterAdapterImpl) GetConnection(ctx context.Context) (Conn, error) {
	return ca.GetConnectionToDb(ctx, ca.Database)
}

func (ca ClusterAdapterImpl) GetConnectionToDb(ctx context.Context, database string) (Conn, error) {
	if database == "" {
		database = ca.Database
	}
	return ca.GetConnectionToDbWithUser(ctx, database, ca.GetUser(), ca.GetPassword())
}

func (ca ClusterAdapterImpl) GetConnectionToDbWithUser(ctx context.Context, database string, username string, password string) (Conn, error) {
	return ca.getConnectionToDbWithUser(ctx, database, username, password)
}

func (ca ClusterAdapterImpl) getConnectionToDbWithUser(ctx context.Context, database string, username string, password string) (Conn, error) {
	conn, err := pgxpool.Connect(ctx, ca.getConnectionUrl(username, password, database))
	if err != nil {
		log.Error("Error occurred during connect to DB", zap.Error(err))
		return nil, err
	}
	return conn, nil
}

func (ca ClusterAdapterImpl) getConnectionUrl(username string, password string, database string) string {
	if ca.SSl == "on" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?%s", username, password, ca.Host, ca.GetPort(), database, "sslmode=require")
	} else {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", username, password, ca.Host, ca.GetPort(), database)
	}
}

func (ca ClusterAdapterImpl) getHealth() string {
	err := ca.executeHealthQuery()
	if err != nil {
		log.Error("Postgres is unavailable", zap.Error(err))
		return HealthOOS
	} else {
		return HealthUP
	}
}

func (ca ClusterAdapterImpl) executeHealthQuery() error {
	ctx, cancel := context.WithTimeout(context.Background(), connTimeout*time.Second)
	defer cancel()

	conn, err := ca.getConnectionToDbWithUser(ctx, ca.Database, ca.User, ca.Password)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Exec(ctx, "SELECT * FROM pg_catalog.pg_tables")
	return err
}
