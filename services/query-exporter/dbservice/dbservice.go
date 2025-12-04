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

package dbservice

import (
	"context"
	"flag"
	"strings"
	"time"

	"github.com/Netcracker/qubership-query-exporter/utils"

	"github.com/Netcracker/qubership-query-exporter/config"

	log "github.com/sirupsen/logrus"
)

const (
	TypeOracle   = "oracle"
	TypePostgres = "postgres"
)

var (
	maxOpenConns = flag.Int("max-open-connections", utils.GetEnvInt("MAX_OPEN_CONNECTIONS_MASTER", 10), "Set max number of DB pool connections")
	queryTimeout = flag.Int64("query-timeout", int64(utils.GetEnvInt("MAX_QUERY_TIMEOUT", 30)), "Query execution timeout in seconds")

	maxFailedTimeouts  = flag.Int64("max-failed-timeouts", int64(utils.GetEnvInt("MAX_FAILED_TIMEOUTS", 3)), "Query timeout failed before skipped list")
	timeoutsMap        = make(map[string]int64)
	longRunningQueries = []string{}
)

type DBService interface {
	Initialize(dsn string, dbName string) (err error)
	Close()
	ExecuteInitSqls(sqls []string)
	ExecuteSelect(qName string, qRand string, qCfg *config.QueryConfig) ([]map[string]string, []string, error)
	DBStat() string
	GetAllDatabases() ([]string, error)
	GetDatabaseName() string
	GetType() string
	GetDSN() string
	IsMaster() bool
	GetClassifier() config.Classifier
	Ping() bool
}

func GetDBServices(appConfig *config.Config) []DBService {
	dbServices := make([]DBService, 0)
	for dbName, database := range appConfig.Databases {
		log.Debugf("New database with name %s and type %s was added", dbName, database.Type)
		dbService := getServiceByType(database.Type, true)
		err := dbService.Initialize(database.Dsn, "")
		if err != nil {
			log.Fatalf("Cannot connect to physical database %v : %+v", dbName, err)
		}
		dbService.ExecuteInitSqls(database.InitSqls)
		dbServices = append(dbServices, dbService)
	}

	if utils.IsAutodiscoveryEnabled() {
		for _, dbService := range dbServices {
			logicalDatabases, err := getLogicalDbsServices(dbService, dbServices)
			if err != nil {
				log.Fatalf("Cannot get logical databases list : %+v", err)
			}
			dbServices = append(dbServices, logicalDatabases...)
		}
	}

	return dbServices
}

func getServiceByType(dbType string, master bool) DBService {
	var dbService DBService
	if dbType == "" || strings.ToLower(dbType) == TypeOracle {
		dbService = &OracleDBService{master: master}
	} else if strings.ToLower(dbType) == TypePostgres {
		dbService = &PostgresDBService{master: master, dbName: "postgres"}
	}
	return dbService
}

func GetNewDBServices(dbServices []DBService) []DBService {
	resultDbServices := []DBService{}
	for _, dbService := range dbServices {
		if dbService.IsMaster() {
			newDbServices, err := getLogicalDbsServices(dbService, dbServices)
			if err != nil {
				log.Fatalf("Cannot get logical databases list from %s : %+v", dbService.GetDatabaseName(), err)
			}
			resultDbServices = append(resultDbServices, newDbServices...)
		}
	}
	return resultDbServices
}

func getLogicalDbsServices(dbService DBService, existingServices []DBService) ([]DBService, error) {
	dbServices := []DBService{}
	databases, err := dbService.GetAllDatabases()
	if err != nil {
		return nil, err
	}
	for _, dbName := range databases {
		if !serviceAlreadyPresent(dbService.GetType(), dbName, existingServices) {
			newDbService := getServiceByType(dbService.GetType(), false)
			log.Debugf("New database with name %s and type %s was added", dbName, newDbService.GetType())
			newDbService.Initialize(dbService.GetDSN(), dbName)
			dbServices = append(dbServices, newDbService)
		}
	}
	return dbServices, nil
}

func serviceAlreadyPresent(dbType string, name string, existingServices []DBService) bool {
	for _, existingService := range existingServices {
		if dbType == existingService.GetType() {
			if name == existingService.GetDatabaseName() {
				return true
			}
		}
	}
	return false
}

func getTimeout(qCfg *config.QueryConfig) time.Duration {
	if qCfg == nil {
		return time.Second * time.Duration(*queryTimeout)
	}

	var timeout time.Duration
	if qCfg.Timeout != "" {
		t, parseErr := time.ParseDuration(qCfg.Timeout)
		if parseErr == nil {
			timeout = t
		}
	}
	if timeout == 0 {
		timeout = time.Second * time.Duration(*queryTimeout)
	}

	return timeout
}

func execWithTimeout(qName string, qRand string, qCfg *config.QueryConfig, executeSelect func(ctx context.Context, qName string, qRand string, qCfg *config.QueryConfig) ([]map[string]string, []string, error)) ([]map[string]string, []string, error) {
	timeout := getTimeout(qCfg)
	log.Debugf("[%v] Executing query %v with timeout %+v : %+v", qRand, qName, timeout, qCfg)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rows, columnNames, err := executeSelect(ctx, qName, qRand, qCfg)
	if err != nil {
		handleTimeoutFail(ctx, qName)
		return nil, nil, err
	}
	return rows, columnNames, nil
}

func GetLongRunningQueries() []string {
	return longRunningQueries
}

// Circuit breaker mechanism
func handleTimeoutFail(ctx context.Context, qName string) {
	select {
	default:
	case <-ctx.Done():
		timeoutCount := timeoutsMap[qName]
		if timeoutCount >= *maxFailedTimeouts {
			log.Warnf("Adding query %s to long running list", qName)
			longRunningQueries = append(longRunningQueries, qName)
		} else {
			timeoutCount++
			log.Warnf("Timeout for query %s has been execeeded %d time(s)", qName, timeoutCount)
			timeoutsMap[qName] = timeoutCount
		}
	}
}
