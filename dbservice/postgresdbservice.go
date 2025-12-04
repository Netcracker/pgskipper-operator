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
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Netcracker/qubership-query-exporter/utils"

	"github.com/Netcracker/qubership-query-exporter/selfmonitor"

	"github.com/Netcracker/qubership-query-exporter/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

var (
	maxOpenConnsLog = flag.Int("max-open-connections-logical", utils.GetEnvInt("MAX_OPEN_CONNECTIONS_LOGICAL", 1), "Set max number of DB pool connections per not master logical db")

	pgAllDatabasesQuery = "SELECT datname FROM pg_database WHERE datistemplate = false;"
	pgPingQuery         = "SELECT version()"

	metadataTable    = "_dbaas_metadata"
	tableExistsQuery = fmt.Sprintf("SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = '%s';", metadataTable)
	classifierQuery  = fmt.Sprintf("select value#>>'{classifier}' as classifier from %s where key='metadata'", metadataTable)

	// Env variables regExp
	re = regexp.MustCompile(`\$\{(.*?)\}`)
)

type PostgresDBService struct {
	dbPool     *pgxpool.Pool
	dbName     string
	master     bool
	dsn        string
	classifier config.Classifier
}

func findMacros(dsn string) map[string]string {
	resultMap := make(map[string]string, 0)
	match := re.FindAllStringSubmatch(dsn, -1)
	for _, variable := range match {
		resultMap[variable[0]] = variable[1]
	}
	return resultMap
}

func prepareDSN(dsn string) string {
	macros := findMacros(dsn)
	for macro, envName := range macros {
		value, ok := os.LookupEnv(envName)
		if !ok {
			panic(fmt.Sprintf("Environment variable %s is not found", macro))
		}
		dsn = strings.ReplaceAll(dsn, macro, value)
	}
	return dsn
}

func (pdbs *PostgresDBService) Initialize(dsn string, dbName string) (err error) {
	if pdbs.IsMaster() {
		dsn = prepareDSN(dsn)
	}
	pgxConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Errorf("Unable to parse dsn for Postgres: %+v", err)
		return err
	}
	if pdbs.IsMaster() {
		pgxConfig.MaxConns = int32(*maxOpenConns)
	} else {
		pgxConfig.MaxConns = int32(*maxOpenConnsLog)
	}

	if dbName == "" {
		dbName = "postgres"
	}
	pgxConfig.ConnConfig.Database = dbName
	dbPool, err := pgxpool.NewWithConfig(context.Background(), pgxConfig)
	if err != nil {
		log.Errorf("Unable to connect to Postgres database: %+v", err)
		return err
	}
	pdbs.dbPool = dbPool
	pdbs.dbName = dbName
	pdbs.dsn = dsn

	// Set up db classifier
	err = pdbs.initClassifier()
	if err != nil {
		return err
	}

	return
}

func (pdbs *PostgresDBService) Close() {
	log.Debugf("DB connection pool stat: %+v", pdbs.dbPool.Stat())
	if pdbs.dbPool != nil {
		pdbs.dbPool.Close()
		log.Info("Postgres DBPool was closed")
	} else {
		log.Warn("Attempt to close db failed: dbservice.db == nil, nothing to close")
	}
}

func (pdbs *PostgresDBService) ExecuteInitSqls(sqls []string) {
	for _, sql := range sqls {
		pdbs.executeInitQuery(sql)
	}
}

func (pdbs *PostgresDBService) executeInitQuery(sql string) {
	timeout := getTimeout(nil)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	log.Infof("Executing init-sql '%v' with timeout %+v", sql, timeout)
	res, err := pdbs.dbPool.Exec(ctx, sql)
	if err != nil {
		log.Errorf("Error executing init-sql '%v' : %+v", sql, err)
	} else {
		log.Infof("Init-sql '%v' executed successfully : %+v", sql, res)
	}
	pdbs.printDBStatAndUpdatePrometheusSelfMetric("Init-query")
}

func (odbs *PostgresDBService) ExecuteSelect(qName string, qRand string, qCfg *config.QueryConfig) ([]map[string]string, []string, error) {
	return execWithTimeout(qName, qName, qCfg, odbs.executeSelect)
}

func (pdbs *PostgresDBService) executeSelect(ctx context.Context, qName string, qRand string, qCfg *config.QueryConfig) ([]map[string]string, []string, error) {
	timeout := getTimeout(qCfg)
	log.Debugf("[%v] Executing query %v with timeout %+v : %+v", qRand, qName, timeout, qCfg)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rows, err := pdbs.getRows(ctx, qName, qCfg)

	if err != nil {
		pdbs.printDBStatAndUpdatePrometheusSelfMetric(qRand)
		return nil, nil, fmt.Errorf("Error executing query %v : %+v", qName, err)
	}

	defer rows.Close()
	pdbs.printDBStatAndUpdatePrometheusSelfMetric(qRand)

	columnNames := getPostgresColumnNames(rows)
	log.Tracef("[%v] Query %v, columnNames : %+v", qRand, qName, columnNames)

	i := 0
	result := make([]map[string]string, 0)
	for rows.Next() {
		mapRow, err := processPostgresRowToMap(rows, qRand)
		i++
		log.Tracef("[%v] Query %v, row %v : %+v", qRand, qName, i, mapRow)
		if err != nil {
			log.Errorf("[%v] Error processing row to map for query %v : %+v", qRand, qName, err)
		} else {
			result = append(result, mapRow)
			//log.Tracef("qName: %v, address: %p, length: %d, capacity: %d, items: %+v\n", qName, result, len(result), cap(result), result)
		}
	}
	return result, columnNames, nil
}

func (dbservice *PostgresDBService) getRows(ctx context.Context, qName string, qCfg *config.QueryConfig) (pgx.Rows, error) {

	query := qCfg.Sql

	if query == "" {
		return nil, fmt.Errorf("Can not find a query")
	}

	var parameters = make([]interface{}, 0)
	if len(qCfg.Parameters) != 0 {
		parameters = make([]interface{}, len(qCfg.Parameters[0]))
		i := 0
		for pName, pValue := range qCfg.Parameters[0] {
			parameters[i] = sql.Named(pName, pValue)
			i++
		}
	}

	return dbservice.dbPool.Query(ctx, query, parameters...)
}

func processPostgresRowToMap(rows pgx.Rows, qRand string) (map[string]string, error) {
	fieldDescriptions := rows.FieldDescriptions()

	values, err := rows.Values()
	if err != nil {
		return nil, fmt.Errorf("Error reading rows %+v", err)
	}
	log.Tracef("[%v] Got values %+v", values)

	result := make(map[string]string)
	for i, v := range values {
		switch t := v.(type) {
		case nil:
			log.Tracef("[%v] For column %v got nil column value type", qRand, fieldDescriptions[i].Name)
		case *string:
			log.Tracef("[%v] For column %v got *string column value type", qRand, fieldDescriptions[i].Name)
			result[strings.ToUpper(fieldDescriptions[i].Name)] = *t
		case string:
			log.Tracef("[%v] For column %v got string column value type", qRand, fieldDescriptions[i].Name)
			result[strings.ToUpper(fieldDescriptions[i].Name)] = t
		case int32, int64, uint32:
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("[%v] For column %v got %+v column value type", qRand, fieldDescriptions[i].Name, reflect.TypeOf(t))
			}
			result[strings.ToUpper(fieldDescriptions[i].Name)] = fmt.Sprintf("%v", t)
		case pgtype.Numeric:
			log.Tracef("[%v] For column %v got pgtype.Numeric column value type", qRand, fieldDescriptions[i].Name)
			val, err := t.Float64Value()
			if err != nil {
				log.Errorf("[%v] for column %v got error during processing pgtype.Numeric response to Float64 : %+v", qRand, fieldDescriptions[i].Name, err)
			} else {
				result[strings.ToUpper(fieldDescriptions[i].Name)] = fmt.Sprintf("%v", val.Float64)
			}
		case bool:
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("[%v] For column %v got pgtype.Bool column value type", qRand, fieldDescriptions[i].Name)
			}
			// convert bool to int
			valInt := 0
			if t {
				valInt = 1
			}
			result[strings.ToUpper(fieldDescriptions[i].Name)] = fmt.Sprintf("%v", valInt)
		case float32, float64:
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("[%v] For column %v float column value type", qRand, fieldDescriptions[i].Name)
			}
			result[strings.ToUpper(fieldDescriptions[i].Name)] = fmt.Sprintf("%f", t)
		case time.Time:
			if log.IsLevelEnabled(log.TraceLevel) {
				log.Tracef("[%v] For column %v got %+v column value type", qRand, fieldDescriptions[i].Name, reflect.TypeOf(t))
			}
			val := float64(t.Unix())
			result[strings.ToUpper(fieldDescriptions[i].Name)] = fmt.Sprintf("%v", val)
		default:
			log.Warnf("[%v] For column %v got type %+v; This is unexpected and default logic is applied for this column", qRand, fieldDescriptions[i].Name, reflect.TypeOf(t))
			result[strings.ToUpper(fieldDescriptions[i].Name)] = fmt.Sprintf("%v", t)
		}
	}
	return result, nil
}

/* func (pdbs *PostgresDBService) ExecuteSimpleQuery(query string) ([]map[string]string, []string, error) {
    var qCfg config.QueryConfig
    qCfg.Sql = query
    return pdbs.ExecuteSelect("SIMPLE SELECT", "SIMPLE SELECT", &qCfg)
}

func (pdbs *PostgresDBService) PrintSimpleQuery(query string) {
    res, _, err := pdbs.ExecuteSimpleQuery(query)
    if err != nil {
        log.Errorf("Error executing simple query %v : %+v", query, err)
    } else {
        log.Infof("Simple query %v executed successfully, result is : %+v", query, res)
    }
} */

func getPostgresColumnNames(rows pgx.Rows) []string {
	fieldDescriptions := rows.FieldDescriptions()
	columnNames := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columnNames[i] = fd.Name
	}
	return columnNames
}

func (pdbs *PostgresDBService) DBStat() string {
	return fmt.Sprintf("DB connection pool stat: %+v", pdbs.dbPool.Stat())
}

func (pdbs *PostgresDBService) printDBStatAndUpdatePrometheusSelfMetric(qRand string) {
	dbStat := pdbs.dbPool.Stat()
	log.Debugf("[%v] Postgres DB connection pool stat: %+v", qRand, dbStat)
	selfmonitor.UpdatePostgresDbPoolStats(*dbStat)
}

func (pdbs *PostgresDBService) GetAllDatabases() ([]string, error) {
	timeout := getTimeout(nil)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	rows, err := pdbs.dbPool.Query(ctx, pgAllDatabasesQuery)
	if err != nil {
		log.Errorf("Cannot get logical databases list for %s", pdbs.dbName)
		return nil, err
	}
	defer rows.Close()

	databases := []string{}
	for rows.Next() {
		var database string
		err = rows.Scan(&database)
		if err != nil {
			log.Errorf("Cannot parse logical database value for %s", pdbs.dbName)
			return nil, err
		}
		databases = append(databases, database)
	}
	return databases, nil
}

func (pdbs *PostgresDBService) GetDatabaseName() string {
	return pdbs.dbName
}

func (pdbs *PostgresDBService) IsMaster() bool {
	return pdbs.master
}

func (pdbs *PostgresDBService) GetType() string {
	return TypePostgres
}

func (pdbs *PostgresDBService) GetDSN() string {
	return pdbs.dsn
}

func (pdbs *PostgresDBService) Ping() bool {
	timeout := getTimeout(nil)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := pdbs.dbPool.Exec(ctx, pgPingQuery)
	if err != nil {
		log.Errorf("Cannot connect to database %s", pdbs.dbName)
		return false
	}
	return true
}

func (pdbs *PostgresDBService) GetClassifier() config.Classifier {
	return pdbs.classifier
}

func (pdbs *PostgresDBService) initClassifier() error {
	timeout := getTimeout(nil)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	isMetadataExists, err := pdbs.isMetadataExists(ctx)
	if err != nil {
		return err
	}
	if !isMetadataExists {
		log.Debugf("No classifier found in database %s", pdbs.dbName)
		return nil
	}

	classifier, err := pdbs.obtainClassifierFromMetadata(ctx)
	if err != nil {
		return err
	}

	pdbs.classifier = classifier
	return nil
}

func (pdbs *PostgresDBService) isMetadataExists(ctx context.Context) (bool, error) {
	rows, err := pdbs.dbPool.Query(ctx, tableExistsQuery)
	if err != nil {
		log.Errorf("Cannot connect to database %s", pdbs.dbName)
		return false, err
	}
	defer rows.Close()

	return rows.Next(), nil
}

func (pdbs *PostgresDBService) obtainClassifierFromMetadata(ctx context.Context) (config.Classifier, error) {
	rows, err := pdbs.dbPool.Query(ctx, classifierQuery)
	if err != nil {
		log.Errorf("Cannot connect to database %s", pdbs.dbName)
		return nil, err
	}
	defer rows.Close()

	classifier := config.Classifier{}
	classifierStr := ""
	for rows.Next() {
		err = rows.Scan(&classifierStr)
		if err != nil {
			log.Warn("No Classifier found in metadata for database %s", pdbs.dbName)
			return config.Classifier{}, nil
		}

		err = json.Unmarshal([]byte(classifierStr), &classifier)
		if err != nil {
			log.Error("JSON classifier parse error for database %s", pdbs.dbName)
			return config.Classifier{}, nil
		}
		log.Debug(fmt.Sprintf("Classifier %s was found for database %s", classifier, pdbs.dbName))
	}
	return classifier, nil
}
