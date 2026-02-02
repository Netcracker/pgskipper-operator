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

package postgres

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Netcracker/pgskipper-replication-controller/pkg/utils"
	"github.com/jackc/pgx/v4"
	"go.uber.org/zap"

	"github.com/jackc/pgconn"
)

const (
	HealthUP  = "UP"
	HealthOOS = "OUT_OF_SERVICE"

	healthQuery = "SELECT 1 FROM pg_catalog.pg_tables"
)

var (
	log         = utils.GetLogger()
	connTimeout = time.Duration(utils.GetEnvInt("PG_CONN_TIMEOUT_SEC", 20))
)

type Conn interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Close(ctx context.Context) error
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

type Client struct {
	Host      string
	Port      int
	SSl       string
	User      string
	Password  string
	DefaultDB string
	Health    string
}

func NewClient(host string, port int, username, password string, database string, ssl string) *Client {
	username = url.PathEscape(username)
	password = url.PathEscape(password)

	c := &Client{
		Host:      host,
		Port:      port,
		User:      username,
		Password:  password,
		SSl:       ssl,
		Health:    HealthUP,
		DefaultDB: database,
	}
	log.Debug(fmt.Sprintf("Checking connection for host=%s port=%d with database %s", host, port, database))
	c.RequestHealth()
	log.Info("PG client has been initialized")
	return c
}

func (ca Client) RequestHealth() string {
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

func (ca Client) GetPort() int {
	return ca.Port
}

func (ca Client) GetUser() string {
	return ca.User
}

func (ca Client) GetPassword() string {
	return ca.Password
}

func (ca Client) GetHost() string {
	return ca.Host
}

func (ca Client) GetConnection(ctx context.Context) (Conn, error) {
	return ca.GetConnectionToDb(ctx, ca.DefaultDB)
}

func (ca Client) GetConnectionToDb(ctx context.Context, database string) (Conn, error) {
	if database == "" {
		database = ca.DefaultDB
	}
	return ca.GetConnectionToDbWithUser(ctx, database, ca.GetUser(), ca.GetPassword())
}

func (ca Client) GetConnectionToDbWithUser(ctx context.Context, database string, username string, password string) (Conn, error) {
	return ca.getConnectionToDbWithUser(ctx, database, username, password)
}

func (ca Client) getConnectionToDbWithUser(ctx context.Context, database string, username string, password string) (Conn, error) {
	conn, err := pgx.Connect(ctx, ca.getConnectionUrl(username, password, database))
	if err != nil {
		log.Error("Error occurred during connect to DB", zap.Error(err))
		return nil, err
	}
	return conn, nil
}

func (ca Client) getConnectionUrl(username string, password string, database string) string {
	if ca.SSl == "on" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?%s", username, password, ca.Host, ca.GetPort(), database, "sslmode=require")
	} else {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", username, password, ca.Host, ca.GetPort(), database)
	}
}

func (ca Client) getHealth() string {
	err := ca.executeHealthQuery()
	if err != nil {
		log.Error("Postgres is unavailable", zap.Error(err))
		return HealthOOS
	} else {
		return HealthUP
	}
}

func (ca Client) executeHealthQuery() error {
	ctx, cancel := context.WithTimeout(context.Background(), connTimeout*time.Second)
	defer cancel()

	conn, err := ca.getConnectionToDbWithUser(ctx, ca.DefaultDB, ca.User, ca.Password)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, healthQuery)
	return err
}

func EscapeInputValue(value string) string {
	singleQuote := strings.ReplaceAll(value, "'", "''")
	return strings.ReplaceAll(singleQuote, "\"", "\"\"")
}
