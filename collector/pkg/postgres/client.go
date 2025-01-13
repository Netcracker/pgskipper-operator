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
	"strconv"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

var logger = util.GetLogger()

var (
	PgHost     = util.GetEnv("POSTGRES_HOST", "pg-patroni")
	PgPort     = util.GetEnvInt("POSTGRES_PORT", 5432)
	PgUser     = util.GetEnv("MONITORING_USER", "monitoring_role")
	PgPass     = util.GetEnv("MONITORING_PASSWORD", "monitoring_password")
	PgDatabase = util.GetEnv("POSTGRES_DATABASE", "postgres")
	PgSsl      = util.GetEnv("PGSSLMODE", "prefer")
)

type Row map[string]interface{}

type PostgresConnector struct {
	host     string
	port     int
	database string
	user     string
	password string
	ssl      bool
	conn     *pgxpool.Pool
}

func NewConnector() *PostgresConnector {

	return NewConnectorForUser(PgHost, PgPort, PgUser, PgPass)
}

func NewConnectorForUser(host string, port int, user, pass string) *PostgresConnector {

	ssl := false
	if PgSsl == "require" {
		ssl = true
	}

	return &PostgresConnector{
		host:     host,
		port:     port,
		database: PgDatabase,
		user:     user,
		password: pass,
		ssl:      ssl,
	}
}

func (pc *PostgresConnector) EstablishConn(ctx context.Context) error {
	return pc.EstablishConnForDB(ctx, pc.database)
}

func (pc *PostgresConnector) EstablishConnForDB(ctx context.Context, database string) error {
	pc.CloseConnection(ctx)
	pc.database = database
	conn, err := pc.getConnectionToDb(ctx)
	if err != nil {
		return err
	}
	pc.conn = conn
	return nil
}

func (pc *PostgresConnector) CloseConnection(ctx context.Context) {
	if pc.conn != nil {
		pc.conn.Close()
	}
	pc.conn = nil
}

func (pc *PostgresConnector) GetHost() string {
	return pc.host
}

func (pc *PostgresConnector) GetDatabase() string {
	return pc.database
}

func (pc *PostgresConnector) SetHost(host string) {
	pc.host = host
}

func (pc *PostgresConnector) getConnectionToDb(ctx context.Context) (*pgxpool.Pool, error) {
	conn, err := pgxpool.New(ctx, pc.getConnectionUrl())
	if err != nil {
		logger.Error("Error occurred during connect to DB", zap.Error(err))
		return nil, err
	}

	return conn, nil
}

func (pc *PostgresConnector) getConnectionUrl() string {
	username := url.PathEscape(pc.user)
	password := url.PathEscape(pc.password)
	if pc.ssl {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?%s", username, password, pc.host, pc.port, pc.database, "sslmode=require")
	} else {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", username, password, pc.host, pc.port, pc.database)
	}
}

func (pc *PostgresConnector) GetValue(ctx context.Context, query string, args ...interface{}) (result interface{}, err error) {
	row := pc.conn.QueryRow(ctx, query, args...)
	err = row.Scan(&result)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot execute %s", query), zap.Error(err))
		return result, err
	}
	return result, nil
}

func (pc *PostgresConnector) Exec(ctx context.Context, query string, args ...interface{}) (int, error) {
	cT, err := pc.conn.Exec(ctx, query, args...)
	return int(cT.RowsAffected()), err
}

func (pc *PostgresConnector) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	if pc.conn == nil {
		logger.Error("Postgres connection is nil")
	}
	rows, err := pc.conn.Query(ctx, query, args...)
	return rows, err
}

func (pc *PostgresConnector) GetData(ctx context.Context, query string) ([]string, []Row) {
	resultRows := []Row{}
	rows, err := pc.Query(ctx, query)
	if err != nil {
		logger.Error(fmt.Sprintf("Cannot execute query %s", query), zap.Error(err))
		return nil, nil
	}
	defer rows.Close()

	fields := []string{}
	values := make([]interface{}, len(rows.FieldDescriptions()))
	valuesPoint := make([]interface{}, len(rows.FieldDescriptions()))
	for i, field := range rows.FieldDescriptions() {
		fields = append(fields, field.Name)
		valuesPoint[i] = &values[i]
	}

	for rows.Next() {
		resultMap := map[string]interface{}{}
		err = rows.Scan(valuesPoint...)
		if err != nil {
			logger.Error("Cannot scan row")
			continue
		}
		for i, fName := range fields {
			resultMap[fName] = values[i]
		}
		resultRows = append(resultRows, Row(resultMap))
	}
	return fields, resultRows
}

func GetStringValue(row Row, name, defaultValue string) string {
	if value, ok := row[name]; ok {
		return value.(string)
	}
	return defaultValue
}

func GetFloatValue(row Row, name string, defaultValue float64) float64 {
	if value, ok := row[name]; ok {
		if value, err := strconv.ParseFloat(fmt.Sprintf("%v", value), 64); err != nil {
			return value
		}
	}
	return defaultValue
}
