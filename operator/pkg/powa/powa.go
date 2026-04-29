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

package powa

import (
	"context"
	"fmt"

	v1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
)

const (
	powaUser   = "powa"
	powaDBName = "powa"
)

var (
	logger           = util.GetLogger()
	extPOWATable     = []string{"pg_stat_statements", "btree_gist", "powa", "pg_qualstats", "pg_stat_kcache", "hypopg", "pg_track_settings", "pg_wait_sampling"}
	preloadLibraries = []string{"powa", "pg_stat_kcache", "pg_qualstats", "pg_wait_sampling"}
	pgSettings       = []string{"track_io_timing=on", "pg_wait_sampling.profile_queries=true"}
	commonExt        = []string{"hypopg"}
	secretName       = "powa-secret"
)

func UpdatePreloadLibraries(cr *v1.PatroniCore) {
	helper.UpdatePreloadLibraries(cr, preloadLibraries)
}

func UpdatePgSettings(cr *v1.PatroniCore) {
	cr.Spec.Patroni.PostgreSQLParams = append(cr.Spec.Patroni.PostgreSQLParams, pgSettings...)
}

func SetUpPOWA(pgHost string) error {
	logger.Info("Setting up POWA")
	pg := pgClient.GetPostgresClient(pgHost)
	password, err := getPowaPassword()
	if err != nil {
		return err
	}

	err = helper.WaitUntilRecoveryIsDone(pg)
	if err != nil {
		logger.Error("recovery check failed, during POWA setup", zap.Error(err))
		return err
	}

	isPowaDBExist, err := isPowaDatabaseExist(pg)
	if err != nil {
		return err
	}
	isPowaUserExist, err := isPowaUserExist(pg)
	if err != nil {
		return err
	}

	if isPowaUserExist {
		logger.Info("Powa is already configured")
		if err := helper.AlterUserPassword(pg, powaUser, password); err != nil {
			return err
		}
	} else {
		if err := createPOWAUser(password, pg); err != nil {
			return err
		}
	}

	if !isPowaDBExist {
		createPOWATableWithExtension(pg)
		createPOWAExtensions(pg)
	}

	return nil
}

func isPowaDatabaseExist(pg *pgClient.PostgresClient) (bool, error) {
	conn, err := pg.GetConnection()
	if err != nil {
		return false, err
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), fmt.Sprintf("select * from pg_database where datname = '%s';", powaDBName))
	if err != nil {
		logger.Error("error during fetching info about Powa database")
		return false, err
	}
	if rows.Next() {
		rows.Close()
		return true, nil
	}
	return false, nil
}

func isPowaUserExist(pg *pgClient.PostgresClient) (bool, error) {
	conn, err := pg.GetConnection()
	if err != nil {
		return false, err
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), fmt.Sprintf("select * from pg_roles where rolname='%s';", powaUser))
	if err != nil {
		logger.Error("error during fetching info about Powa user")
		return false, err
	}
	if rows.Next() {
		rows.Close()
		return true, nil
	}
	return false, nil
}

func createPOWAUser(password string, pg *pgClient.PostgresClient) error {
	password = pgClient.EscapeString(password)
	if err := pg.Execute(fmt.Sprintf("CREATE ROLE %s SUPERUSER LOGIN PASSWORD '%s' ;", powaUser, password)); err != nil {
		logger.Error("cannot create POWA user", zap.Error(err))
	}
	return nil
}

func getPowaPassword() (string, error) {
	foundSecret, err := helper.GetHelper().GetSecret(secretName)
	if err != nil {
		logger.Error(fmt.Sprintf("can't find the secret %s", secretName), zap.Error(err))
		return "", err
	}
	return string(foundSecret.Data["password"]), nil
}

func createPOWATableWithExtension(pg *pgClient.PostgresClient) {
	if err := pg.Execute("CREATE DATABASE powa;"); err != nil {
		logger.Error("cannot create powa database", zap.Error(err))
		return
	}
	helper.CreateExtensionsForDB(pg, powaDBName, extPOWATable)
}

func createPOWAExtensions(pg *pgClient.PostgresClient) {
	databases, _ := helper.GetAllDatabases(pg)
	helper.CreateExtensionsForDBs(pg, databases, commonExt)
}
