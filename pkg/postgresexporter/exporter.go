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

package postgresexporter

import (
	"fmt"
	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"

	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"go.uber.org/zap"
	"strings"
)

const (
	mainDatabase = "postgres"
	expSec       = "postgres-exporter-user-credentials"
)

type PostgresExporterCreds struct {
	username string
	password string
}

var (
	exporterExtensions = []string{"pg_stat_statements", "pgsentinel", "pg_wait_sampling", "pg_buffercache", "pg_stat_kcache"}
)

func createPostgresExporterExtensions(pgHost, pgDatabase string) {
	client := pgClient.GetPostgresClient(pgHost)
	helper.CreateExtensionsForDB(client, pgDatabase, exporterExtensions)
}

func SetUpExporter(expSpec *v1.PostgresExporter) error {
	logger.Info("Setting up Postgres Exporter")
	pgHost := getHostFromURI(expSpec.Uri)
	pgDatabase := getDatabaseFromURI(expSpec.Uri)
	createPostgresExporterExtensions(pgHost, pgDatabase)
	err := ensurePostgresExporterUser(pgHost)
	return err
}

func getHostFromURI(uri string) string {
	return strings.Split(uri, ":")[0]
}

func getDatabaseFromURI(uri string) string {
	start := strings.Index(uri, "/")
	if start == -1 {
		return mainDatabase
	}
	end := strings.Index(uri, "?")
	if end == -1 {
		end = len(uri)
	}
	return uri[start+1 : end]
}

func ensurePostgresExporterUser(pgHost string) error {
	creds, err := getExporterCreds()
	if err != nil {
		return err
	}
	client := pgClient.GetPostgresClient(pgHost)
	if helper.IsUserExist(creds.username, client) {
		if err = helper.AlterUserPassword(client, creds.username, creds.password); err != nil {
			return err
		}
	} else {
		if err = createExporterUser(creds, client); err != nil {
			return err
		}
	}
	if err = grantExporterUser(creds, client); err != nil {
		return err
	}
	return nil
}

func createExporterUser(creds PostgresExporterCreds, client *pgClient.PostgresClient) error {
	logger.Info(fmt.Sprintf("Creation of \"%s\" user for postgres-exporter", creds.username))
	if err := client.Execute(fmt.Sprintf("CREATE ROLE \"%s\" LOGIN PASSWORD '%s';", creds.username, creds.password)); err != nil {
		logger.Error("cannot create Exporter user", zap.Error(err))
		return err
	}
	return nil
}

func grantExporterUser(creds PostgresExporterCreds, pg *pgClient.PostgresClient) error {
	if err := pg.Execute(fmt.Sprintf("GRANT pg_read_all_data, pg_monitor TO \"%s\";", creds.username)); err != nil {
		logger.Error("cannot modify Exporter user", zap.Error(err))
		return err
	}
	return nil
}

func getExporterCreds() (PostgresExporterCreds, error) {
	foundSecret, err := helper.GetHelper().GetSecret(expSec)
	if err != nil {
		return PostgresExporterCreds{}, err
	}
	username := pgClient.EscapeString(string(foundSecret.Data["username"]))
	password := pgClient.EscapeString(string(foundSecret.Data["password"]))
	return PostgresExporterCreds{username: username, password: password}, nil
}
