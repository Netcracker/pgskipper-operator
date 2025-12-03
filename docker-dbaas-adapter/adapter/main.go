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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/backup"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/basic"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/initial"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dbaas"
	fiber2 "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/impl/fiber"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/service"
	coreUtils "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	fiber "github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

const (
	appName = "postgresql"
	appPath = "/" + appName
)

var (
	logger = util.GetLogger()

	apiVersion = "v1"

	pgHost     = flag.String("pg_host", util.GetEnv("POSTGRES_HOST", "127.0.0.1"), "Host of PostgreSQL cluster, env: POSTGRES_HOST")
	pgPort     = flag.Int("pg_port", util.GetEnvInt("POSTGRES_PORT", 5432), "Port of PostgreSQL cluster, env: POSTGRES_PORT")
	pgUser     = flag.String("pg_user", util.GetEnv("POSTGRES_ADMIN_USER", "postgres"), "Username of dbaas user in PostgreSQL, env: POSTGRES_ADMIN_USER")
	pgPass     = flag.String("pg_pass", util.GetEnv("POSTGRES_ADMIN_PASSWORD", ""), "Password of dbaas user in PostgreSQL, env: POSTGRES_ADMIN_PASSWORD")
	pgDatabase = flag.String("pg_database", util.GetEnv("POSTGRES_DATABASE", "postgres"), "PostgreSQL database, env: POSTGRES_DATABASE")
	pgSsl      = flag.String("pg_ssl", util.GetEnv("PG_SSL", "off"), "Enable ssl connection to postgreSQL, env: PG_SSL")

	pgBackupAuth          = flag.Bool("backup_auth", util.GetEnvBool("AUTH", false), "Backup auth, env: AUTH")
	pgBackupDaemonAddress = flag.String("daemon_address", util.GetEnv("POSTGRES_BACKUP_DAEMON_ADDRESS", "http://postgres-backup-daemon:9000"), "Host of PostgreSQL cluster, env: POSTGRES_BACKUP_DAEMON_ADDRESS")
	pgBackupKeep          = flag.String("backup_keep", util.GetEnv("POSTGRES_BACKUP_KEEP", "1 week"), "Host of PostgreSQL cluster, env: POSTGRES_BACKUP_KEEP")

	readOnlyHost   = flag.String("readonly_host", util.GetEnv("READONLY_HOST", "127.0.0.1"), " ReadOnly Host of PostgreSQL cluster, env: READONLY_HOST")
	vaultEnabled   = flag.Bool("vault_enabled", util.GetEnvBool("VAULT_ENABLED", false), "Enable vault, env: VAULT_ENABLED")
	vaultAddress   = flag.String("vault_address", util.GetEnv("VAULT_ADDR", ""), "Vault address, env: VAULT_ADDR")
	vaultRole      = flag.String("vault_role", util.GetEnv("VAULT_ROLE", "postgres-sa"), "Vault role, env: VAULT_ROLE")
	vaultRotPeriod = flag.String("vault_rotation_period", util.GetEnv("VAULT_ROTATION_PERIOD", "3600"), "Vault rotation period, env: VAULT_ROTATION_PERIOD")
	vaultDBName    = flag.String("vault_db_engine_name", util.GetEnv("VAULT_DB_ENGINE_NAME", "postgresql"), "Vault db engine name, env: VAULT_DB_ENGINE_NAME")
	namespace      = flag.String("namespace", util.GetEnv("CLOUD_NAMESPACE", ""), "Namespace name, env: CLOUD_NAMESPACE")

	isMultiUsersEnabled = flag.Bool("multi_users_enabled", util.GetEnvBool("MULTI_USERS_ENABLED", false), "Is multi Users functionality enabled, env: MULTI_USERS_ENABLED")

	servePort = flag.Int("serve_port", 8080, "Port to serve requests incoming to adapter")
	serveUser = flag.String(
		"serve_user",
		util.GetEnv("DBAAS_ADAPTER_API_USER", "dbaas-aggregator"),
		"Username to authorize incoming requests, env: DBAAS_ADAPTER_API_USER",
	)
	servePass = flag.String(
		"serve_pass",
		util.GetEnv("DBAAS_ADAPTER_API_PASSWORD", "dbaas-aggregator"),
		"Password to authorize incoming requests, env: DBAAS_ADAPTER_API_PASSWORD",
	)

	phydbid = flag.String(
		"phydbid",
		util.GetEnv("DBAAS_AGGREGATOR_PHYSICAL_DATABASE_IDENTIFIER", ""),
		"Identifier to register physical database in dbaas aggregator, env DBAAS_AGGREGATOR_PHYSICAL_DATABASE_IDENTIFIER",
	)

	selfAddress = flag.String(
		"self_address",
		util.GetEnv("DBAAS_ADAPTER_ADDRESS", ""),
		"Address in the form <scheme>://<host>:<port> how adapter could be reached from aggregator, env DBAAS_ADAPTER_ADDRESS",
	)

	dbaasAggregatorRegistrationAddress = flag.String(
		"registration_address",
		util.GetEnv("DBAAS_AGGREGATOR_REGISTRATION_ADDRESS", "http://dbaas-aggregator.dbaas:8080"),
		"Address in the form <scheme>://<host>:<port> to reach aggregator for registration, env DBAAS_AGGREGATOR_REGISTRATION_ADDRESS",
	)

	dbaasAggregatorRegistrationUsername = flag.String(
		"registration_username",
		util.GetEnv("DBAAS_AGGREGATOR_REGISTRATION_USERNAME", "cluster-dba"),
		"Username of basic auth to reach aggregator for registration, env DBAAS_AGGREGATOR_REGISTRATION_USERNAME ",
	)

	dbaasAggregatorRegistrationPassword = flag.String(
		"registration_password",
		util.GetEnv("DBAAS_AGGREGATOR_REGISTRATION_PASSWORD", ""),
		"Username of basic auth to reach aggregator for registration, env DBAAS_AGGREGATOR_REGISTRATION_PASSWORD ",
	)

	labelsFileName = flag.String(
		"labels_file_location_name",
		"dbaas.physical_databases.registration.labels.json",
		"File name where labels are located in json key-value format",
	)

	labelsLocationDir = flag.String(
		"labels_file_location_dir",
		"/app/config/",
		"Directory with file where labels are located in json key-value format",
	)

	registrationFixedDelay = flag.Int(
		"registration_fixed_delay",
		util.GetEnvInt("DBAAS_AGGREGATOR_REGISTRATION_FIXED_DELAY_MS", 150000), // default 2.5 min
		"Scheduled physical database registration fixed delay in milliseconds")

	registrationRetryTime = flag.Int(
		"registration_retry_time",
		util.GetEnvInt("DBAAS_AGGREGATOR_REGISTRATION_RETRY_TIME_MS", 60000), // default 1 min
		"Force physical database registration retry time in milliseconds")

	registrationRetryDelay = flag.Int(
		"registration_retry_delay",
		util.GetEnvInt("DBAAS_AGGREGATOR_REGISTRATION_RETRY_DELAY_MS", 5000), // default 5 sec
		"Force physical database registration retry delay between attempts in milliseconds")

	supports = dao.SupportsBase{
		Users:             true,
		Settings:          true,
		DescribeDatabases: false,
		AdditionalKeys:    map[string]bool{"vault": true},
	}
)

func checkInitMode() bool {
	for _, arg := range os.Args {
		if arg == "init" {
			return true
		}
	}
	return false
}

func main() {
	flag.Parse()
	logger.Debug("Adapter started")
	clusterAdapter := cluster.NewAdapter(*pgHost, *pgPort, *pgUser, *pgPass, *pgDatabase, *pgSsl)

	basicRegistrationAuth := dao.BasicAuth{
		Username: *dbaasAggregatorRegistrationUsername,
		Password: *dbaasAggregatorRegistrationPassword,
	}

	dbaasClient, err := dbaas.NewDbaasClient(*dbaasAggregatorRegistrationAddress, &basicRegistrationAuth, nil)

	if dbaasClient == nil {
		panic(fmt.Errorf("failed to establish connection to DBaaS aggregator, err: %v", err))
	}

	if err != nil {
		logger.Error(fmt.Sprintf("Failed to get DBaaS aggregator version, err %v. Setting default API version", err))
	}

	//Getting dbaas-adpater api version
	version, _ := dbaasClient.GetVersion()
	if version == "v3" {
		apiVersion = "v2"
	} else {
		apiVersion = "v1"
	}
	logger.Info(fmt.Sprintf("API version obtained: %s", apiVersion))

	var dbAdminImpl service.DbAdministration = basic.NewServiceAdapter(clusterAdapter, dao.ApiVersion(apiVersion), getRoles(), getFeatures())

	if checkInitMode() {
		initial.UpdateExtensions(dbAdminImpl)
		if initial.IsRoleUpdateRequired() {
			initial.PerformMigration(dbAdminImpl)
		} else {
			logger.Info("Roles update is disabled, skip migration...")
		}
		return
	}

	var vaultClient *coreUtils.VaultClient

	if *vaultEnabled {
		logger.Info("Vault Integration is Enabled")
		vaultConfig := coreUtils.VaultConfig{
			IsVaultEnabled:  *vaultEnabled,
			Address:         *vaultAddress,
			VaultRole:       *vaultRole,
			VaultRotPeriod:  *vaultRotPeriod,
			VaultDBName:     *vaultDBName,
			VaultAuthMethod: coreUtils.NCPrefix + util.GetEnv("CLOUD_PUBLIC_HOST", "") + "_" + *namespace,
		}
		vaultClient = coreUtils.NewVaultClient(vaultConfig)
	} else {
		vaultClient = &coreUtils.VaultClient{}
	}

	var backupAdminServiceImpl service.BackupAdministrationService
	if *pgSsl == "on" {
		logger.Debug("SSL is enabled")
		if !strings.Contains(*pgBackupDaemonAddress, "https") {
			logger.Info(fmt.Sprintf("replacing backup-daemon address with https, %s", *pgBackupDaemonAddress))
			*pgBackupDaemonAddress = strings.ReplaceAll(*pgBackupDaemonAddress, "http", "https")
		}
	}
	backupAdminServiceImpl = backup.NewServiceAdapter(clusterAdapter, *pgBackupDaemonAddress, *pgBackupKeep, *pgBackupAuth, *pgUser, *pgPass, *pgSsl)

	administrationService := service.NewCoreAdministrationService(
		*namespace,
		*servePort,
		dbAdminImpl,
		logger,
		*vaultEnabled,
		vaultClient,
		*readOnlyHost,
	)

	if strings.Contains(*dbaasAggregatorRegistrationAddress, "https") {
		logger.Info("tls is enabled, will check if https presented in adapter url")
		if !strings.Contains(*selfAddress, "https") {
			*selfAddress = strings.ReplaceAll(*selfAddress, "http", "https")
		}
		*selfAddress = strings.ReplaceAll(*selfAddress, "8080", "8443")
		logger.Info(fmt.Sprintf("replacing self address with https, %s", *selfAddress))
	}
	logger.Info(fmt.Sprintf("self address set for registring in aggregator, %s", *selfAddress))
	log.Fatal(fiber2.RunFiberServer(*servePort, func(app *fiber.App, ctx context.Context) error {
		fiber2.BuildFiberDBaaSAdapterHandlers(
			app,
			*serveUser,
			*servePass,
			appPath,
			administrationService,
			service.NewPhysicalRegistrationService(
				appName,
				logger,
				*phydbid,
				*selfAddress,
				dao.BasicAuth{
					Username: *serveUser,
					Password: *servePass,
				},
				ReadLabelsFile(), //labels
				dbaasClient,
				*registrationFixedDelay,
				*registrationRetryTime,
				*registrationRetryDelay,
				administrationService,
				ctx,
			),
			backupAdminServiceImpl,
			supports.ToMap(),
			logger,
			false,
			"")

		prefix := fmt.Sprintf("/api/%s/dbaas/adapter/postgresql/databases/", apiVersion)
		app.Get(prefix+":dbName/info", dbAdminImpl.(*basic.ServiceAdapter).GetDatabaseOwnerHandler())
		app.Put(prefix+":dbName/settings", dbAdminImpl.(*basic.ServiceAdapter).UpdatePostgreSQLSettingsHandler())

		app.Get("/api/version", func(c *fiber.Ctx) error {
			return c.SendString(apiVersion)
		})

		return nil
	}))

}

func ReadLabelsFile() map[string]string {
	file, err := os.ReadFile(*labelsLocationDir + *labelsFileName)
	if err != nil {
		logger.Info(fmt.Sprintf("Skipping labels file, cannot read it: %s", *labelsLocationDir+*labelsFileName))
		return make(map[string]string)
	}
	var labels map[string]string
	err = json.Unmarshal(file, &labels)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to parse labels file %s", *labelsLocationDir+*labelsFileName), zap.Error(err))
		labels = make(map[string]string)
	}
	logger.Info(fmt.Sprintf("Labels: %v", labels))
	return labels
}

func getFeatures() map[string]bool {
	IsTls := *pgSsl == "on"
	return map[string]bool{
		basic.FeatureMultiUsers:   *isMultiUsersEnabled && apiVersion != "v1",
		basic.FeatureTls:          IsTls,
		basic.FeatureNotStrictTLS: IsTls && util.IsExternalPostgreSQl(),
	}
}

func getRoles() []string {
	return []string{"admin", "streaming", "rw", "ro"}
}
