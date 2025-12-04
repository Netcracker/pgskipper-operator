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
	"fmt"
	"reflect"
	"strings"

	"github.com/Netcracker/qubership-query-exporter/selfmonitor"

	"github.com/Netcracker/qubership-query-exporter/config"
	"github.com/godror/godror"
	log "github.com/sirupsen/logrus"
)

type OracleDBService struct {
	db     *sql.DB
	dbName string
	master bool
	dsn    string
}

func (odbs *OracleDBService) Initialize(dsn string, dbName string) (err error) {
	odbs.db, err = sql.Open("godror", dsn)
	if err != nil {
		return
	}
	odbs.db.SetMaxOpenConns(*maxOpenConns)
	odbs.dsn = dsn
	return
}

func (odbs *OracleDBService) Close() {
	log.Debugf("DB connection pool stat: %+v", odbs.db.Stats())
	if odbs.db != nil {
		err := odbs.db.Close()
		if err != nil {
			log.Errorf("Error closing DB : %+v", err)
		} else {
			log.Info("DB Closed successfully")
		}
	} else {
		log.Warn("Attempt to close db failed: dbservice.db == nil, nothing to close")
	}
}

func (odbs *OracleDBService) ExecuteInitSqls(sqls []string) {
	for _, sql := range sqls {
		odbs.executeInitQuery(sql)
	}
}

func (odbs *OracleDBService) executeInitQuery(sql string) {
	timeout := getTimeout(nil)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	log.Infof("Executing init-sql '%v' with timeout %+v", sql, timeout)
	res, err := odbs.db.ExecContext(ctx, sql)

	if err != nil {
		log.Errorf("Error executing init-sql '%v' : %+v", sql, err)
	} else {
		log.Infof("Init-sql '%v' executed successfully : %+v", sql, res)
	}
	odbs.printDBStatAndUpdatePrometheusSelfMetric("Init-query")
}

func (odbs *OracleDBService) ExecuteSelect(qName string, qRand string, qCfg *config.QueryConfig) ([]map[string]string, []string, error) {
	return execWithTimeout(qName, qName, qCfg, odbs.executeSelect)
}

func (odbs *OracleDBService) executeSelect(ctx context.Context, qName string, qRand string, qCfg *config.QueryConfig) ([]map[string]string, []string, error) {
	rows, err := odbs.getRows(ctx, qName, qCfg)

	if err != nil {
		odbs.printDBStatAndUpdatePrometheusSelfMetric(qRand)
		return nil, nil, fmt.Errorf("Error executing query %v : %+v", qName, err)
	}

	defer rows.Close()
	odbs.printDBStatAndUpdatePrometheusSelfMetric(qRand)

	columnNames, err := getOracleColumnNames(rows)
	if err != nil {
		log.Errorf("[%v] Error getting Column Names: %+v", qRand, err)
	}
	log.Tracef("[%v] Query %v, columnNames : %+v", qRand, qName, columnNames)

	i := 0
	result := make([]map[string]string, 0)
	for rows.Next() {
		mapRow, err := processOracleRowToMap(rows, qRand)
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

func (odbs *OracleDBService) getRows(ctx context.Context, qName string, qCfg *config.QueryConfig) (*sql.Rows, error) {

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

	return odbs.db.QueryContext(ctx, query, parameters...)
}

func processOracleRowToMap(rows *sql.Rows, qRand string) (map[string]string, error) {
	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("Error reading ColumnTypes: %+v", err)
	}

	values := make([]interface{}, len(columns))
	object := map[string]interface{}{}
	for i, column := range columns {
		object[column.Name()] = reflect.New(column.ScanType()).Interface()
		values[i] = object[column.Name()]
	}

	err = rows.Scan(values...)
	if err != nil {
		return nil, fmt.Errorf("Error reading rows %+v", err)
	}

	result := make(map[string]string)
	for i, v := range values {
		switch t := v.(type) {
		case *string:
			result[strings.ToUpper(columns[i].Name())] = *t
		case *godror.Number:
			result[strings.ToUpper(columns[i].Name())] = string(*t)
		/*case float64:
		  result[strings.ToUpper(columns[i].Name())] = strconv.FormatFloat(t, 'E', -1, 64)*/
		default:
			log.Errorf("[%v] Error converting for %v type %+v", qRand, columns[i].Name(), reflect.TypeOf(t))
		}
	}
	return result, nil
}

/*
func (odbs *OracleDBService) ExecuteSimpleQuery(query string) ([]map[string]string, []string, error) {
    var qCfg config.QueryConfig
    qCfg.Sql = query
    return odbs.ExecuteSelect("SIMPLE SELECT", "SIMPLE SELECT", &qCfg)
}

func (odbs *OracleDBService) PrintSimpleQuery(query string) {
    res, _, err := odbs.ExecuteSimpleQuery(query)
    if err != nil {
        log.Errorf("Error executing simple query %v : %+v", query, err)
    } else {
        log.Infof("Simple query %v executed successfully, result is : %+v", query, res)
    }
}
*/

func getOracleColumnNames(rows *sql.Rows) ([]string, error) {
	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("Error reading ColumnTypes: %+v", err)
	}
	columnNames := make([]string, len(columns))
	for i, column := range columns {
		columnNames[i] = column.Name()
	}
	return columnNames, nil
}

func (odbs *OracleDBService) DBStat() string {
	return fmt.Sprintf("DB connection pool stat: %+v", odbs.db.Stats())
}

func (odbs *OracleDBService) printDBStatAndUpdatePrometheusSelfMetric(qRand string) {
	dbStat := odbs.db.Stats()
	log.Debugf("[%v] DB connection pool stat: %+v", qRand, dbStat)
	selfmonitor.UpdateOracleDbPoolStats(dbStat)
}

func (odbs *OracleDBService) GetAllDatabases() ([]string, error) {
	return []string{}, nil
}

func (odbs *OracleDBService) GetDatabaseName() string {
	return odbs.dbName
}

func (odbs *OracleDBService) IsMaster() bool {
	return odbs.master
}

func (odbs *OracleDBService) GetType() string {
	return TypeOracle
}

func (odbs *OracleDBService) GetDSN() string {
	return odbs.dsn
}

func (odbs *OracleDBService) Ping() bool {
	return true
}

func (odbs *OracleDBService) GetClassifier() config.Classifier {
	return config.Classifier{}
}
