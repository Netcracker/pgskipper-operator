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

package metrics

import (
	"context"
	"fmt"
	"strings"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/gauges"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/postgres"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	"go.uber.org/zap"
)

var (
	queryLength   = util.GetEnvInt("QUERY_LENGTH", 200)
	metricFields  = []string{"calls", "total_time", "min_time", "max_time", "mean_time"}
	procDatabases = []string{"template0", "template1", "postgres",
		"cloudsqladmin",
		"azure_maintenance", "azure_sys", "rdsadmin"}
)

var (
	qStatQuery = fmt.Sprintf(`
	SELECT queryid, datname, round(sum(calls)) AS calls,
	round(sum(total_exec_time) / 1000) AS total_time,
	round(min(min_exec_time) / 1000) AS min_time,
	round(max(max_exec_time) / 1000) AS max_time,
	round(sum(mean_exec_time) / 1000) AS mean_time,
	rolname,
	lower(left(query, %d)) AS short_query
	FROM pg_stat_statements as pg_stat_statements
	JOIN pg_database ON pg_stat_statements.dbid = pg_database.oid
	JOIN pg_roles ON pg_stat_statements.userid = pg_roles.oid
	WHERE calls > 50 and datname not in ('postgres', 'template0', 'template1')
	GROUP BY queryid, short_query, datname, mean_exec_time, calls, rolname;`,
		queryLength)
	dbQuery    = `select datname from pg_database`
	dbaasQuery = `select current_database() as datname,
	value#>>'{classifier,namespace}' as db_namespace,
	value#>>'{classifier,microserviceName}' as microservice,
	value#>>'{classifier,tenantId}' as tenant_id, 1 as name
	  from _dbaas_metadata where key='metadata'`
	tablesStatQuery = `
    SELECT relid, relname,
        pg_relation_size(relid) AS "t_size",
        pg_indexes_size(relid) AS "idx_size",
        pg_total_relation_size(relid) AS "tot_size",
        coalesce(nullif(n_live_tup,0),seq_tup_read) as "live_tuples"
    FROM pg_stat_all_tables
    WHERE schemaname NOT IN ('pg_catalog', 'information_schema') 
        AND schemaname !~ '^pg_toast'`
	commonPerfMetricsQueries = map[string]string{
		"count_not_idle": ` SELECT count(*) cnt, datname, usename FROM pg_stat_activity
				 WHERE state <> 'idle' and datname <> '' GROUP BY datname, usename, state
				 ORDER BY cnt desc;
			 `,
		"connection_by_database": ` SELECT count(*) cnt, datname FROM pg_stat_activity
				WHERE datname <> '' GROUP BY datname ORDER BY cnt DESC;
			`,
		"connection_by_role": ` SELECT count(*) cnt, usename, rolconnlimit as limit 
				FROM pg_stat_activity as pg_stat_activity
				JOIN pg_roles ON pg_stat_activity.usename = pg_roles.rolname
				WHERE datname <> '' GROUP BY usename, rolconnlimit ORDER BY cnt DESC;
			`,
		"connection_by_role_with_limit": ` SELECT role_cnt, t1.usename, CASE
                WHEN rolconnlimit = -1 THEN 'No data'
                ELSE rolconnlimit::text
                END AS limit, not_idle_cnt
                FROM(SELECT count(*) role_cnt, usename, rolconnlimit
                FROM pg_stat_activity as pg_stat_activity
                JOIN pg_roles ON pg_stat_activity.usename = pg_roles.rolname
                WHERE datname <> '' GROUP BY usename, rolconnlimit ORDER BY role_cnt DESC) t1
                FULL OUTER JOIN  (SELECT count(*) not_idle_cnt, usename FROM pg_stat_activity
                WHERE state <> 'idle' and datname <> '' GROUP BY datname, usename, state
                ORDER BY not_idle_cnt desc) t2 on (t1.usename = t2.usename) order by role_cnt desc;
			`,
		"size_by_database": ` SELECT pg_database_size(datname) as value, 
				datname from pg_database order by pg_database_size(datname) desc;
			`,
	}

	largeObjectQuery = `SELECT count(distinct loid) AS object_count,
    count(*) AS total_chunks, sum(octet_length(data)) AS total_size_bytes
	FROM pg_largeobject;`

	countStatQuery = `SELECT max_connections, used, res_for_super as reserved_for_superuser,
	(max_connections - used-res_for_super) reserved_for_reg_users
	FROM (SELECT count(*) used FROM pg_stat_activity) t1,
	(SELECT setting::int res_for_super FROM pg_settings
	WHERE name=$$superuser_reserved_connections$$) t2,
	(SELECT setting::int max_connections FROM pg_settings
	WHERE name=$$max_connections$$) t3;
`
)

func (s *Scraper) CollectPerformanceMetrics() {
	logger.Info("Performance metrics collection started")
	defer s.HandleMetricCollectorStatus()
	ctx := context.Background()
	pc := postgres.NewConnector()

	err := pc.EstablishConn(ctx)
	if err != nil {
		return
	}
	defer pc.CloseConnection(ctx)

	s.collectQueriesMetrics(ctx, pc)
	s.collectMetricsPerDB(ctx, pc)
	s.collectTempFileMetrics(ctx, pc)
	s.collectLargeObjectMetrics(ctx, pc)
	s.collectCommonPerfMetrics(ctx, pc)
	s.collectCountStatMetrics(ctx, pc)
	logger.Info("Performance metrics collection finished")
}

func (s *Scraper) collectLargeObjectMetrics(ctx context.Context, pg *postgres.PostgresConnector) {
	logger.Info("Large object metrics per DB collection started")

	databases, err := getDatabasesList(ctx, pg)
	if err != nil {
		logger.Error("Failed to retrieve databases list for large object metrics")
		return
	}

	defer func() {
		_ = pg.EstablishConnForDB(ctx, postgres.PgDatabase)
	}()

	logger.Debug(fmt.Sprintf("Will Collect large object Stats for next dbs %v", databases))
	for _, db := range databases {
		logger.Debug(fmt.Sprintf("Collecting large object metrics for DB: %s", db))
		err = pg.EstablishConnForDB(ctx, db)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping DB %s due to connection issue", db))
			continue
		}

		columns, rows := getData(ctx, pg, largeObjectQuery)
		if len(rows) == 0 {
			logger.Info(fmt.Sprintf("No large objects found in DB: %s", db))
			continue
		}

		for _, row := range rows {
			labels := gauges.DefaultLabels()
			labels["datname"] = db

			for _, column := range columns {
				value := fmt.Sprintf("%v", row[column])
				metricName := fmt.Sprintf("ma_pg_large_object_%s", column)
				s.metrics = append(s.metrics, NewMetric(metricName).withLabels(labels).setValue(value))
			}
		}
	}

	logger.Info("Large object metrics per DB collection finished")
}

func (s *Scraper) collectTempFileMetrics(ctx context.Context, pg *postgres.PostgresConnector) {
	logger.Info("Temp file metrics collection started")

	tempMetricsQueries := map[string]string{
		"temp_file_size_by_db": ` SELECT now() AS time, datname, 
		temp_bytes / 1024 / 1024 AS temp_size_mb FROM pg_stat_database
		WHERE temp_bytes > 0 AND datname IS NOT NULL ORDER BY temp_size_mb DESC; 
		`,
		"temp_file_count_by_db": ` SELECT datname, temp_files AS temp_file_count
		FROM pg_stat_database WHERE temp_files > 0 AND datname IS NOT NULL ORDER BY temp_file_count DESC;
		`,
		"temp_file_size_by_query": ` SELECT query, calls AS total_calls,
		(temp_blks_read + temp_blks_written) * current_setting('block_size')::int / 1024 / 1024 AS temp_size_mb
		FROM pg_stat_statements WHERE (temp_blks_read > 0 OR temp_blks_written > 0)
		ORDER BY temp_size_mb DESC LIMIT 10;
		`,
		"active_temp_writes": ` SELECT pid, usename, datname, query, state FROM pg_stat_activity
		WHERE state = 'active' AND datname IS NOT NULL;
		`,
	}

	for metricName, query := range tempMetricsQueries {
		columns, rows := getData(ctx, pg, query)
		for _, row := range rows {
			labels := gauges.DefaultLabels()
			var value string

			if metricName == "active_temp_writes" {
				value = fmt.Sprintf("%d", len(rows))
				for _, column := range columns {
					labels[column] = fmt.Sprintf("%v", row[column])
				}
			} else {
				for _, column := range columns {
					rValue := fmt.Sprintf("%v", row[column])

					if column == "temp_size_mb" || column == "temp_file_count" || column == "total_calls" {
						value = rValue
					} else {
						labels[column] = rValue
					}
				}
			}

			if value != "" {
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s", metricName)).withLabels(labels).setValue(value))
			} else {
				logger.Warn(fmt.Sprintf("No numeric value found for metric '%s'", metricName))
			}
		}
	}

	logger.Info("Temp file metrics collection finished")
}

func (s *Scraper) collectQueriesMetrics(ctx context.Context, pc *postgres.PostgresConnector) {
	columns, rows := getData(ctx, pc, qStatQuery)
	for _, row := range rows {
		for _, metric := range columns {
			if util.Contains(metricFields, metric) {
				mValue := row[metric]
				labels := gauges.DefaultLabels()
				labels["metric_name"] = metric
				for _, column := range columns {
					if !util.Contains(metricFields, column) {
						rValue := row[column]
						if column == "short_query" || column == "query" {
							escaper := strings.NewReplacer(
								`\`, `\\`,
								`"`, `\"`,
								"\n", `\n`,
								"\r", `\n`,
							)
							rValue = escaper.Replace(fmt.Sprintf("%s", rValue))
						}
						labels[column] = fmt.Sprintf("%v", rValue)
					}
				}
				s.metrics = append(s.metrics, NewMetric("ma_pg_queries_stat").withLabels(labels).setValue(mValue))
			}
		}
	}
}

func (s *Scraper) collectMetricsPerDB(ctx context.Context, pg *postgres.PostgresConnector) {
	logger.Info("Tables statistic metrics collection started")
	databases, err := getDatabasesList(ctx, pg)
	if err != nil {
		return
	}
	defer func() {
		_ = pg.EstablishConnForDB(ctx, postgres.PgDatabase)
	}()
	logger.Debug(fmt.Sprintf("Will Collect Table Stats for next dbs %v", databases))
	for _, db := range databases {
		logger.Debug(fmt.Sprintf("Start table stat collection for db: %s", db))

		// select database
		err = pg.EstablishConnForDB(ctx, db)
		if err != nil {
			logger.Error(fmt.Sprintf("Can't establish connection with %s, skipping", db))
			continue
		}
		s.collectTableMetrics(ctx, pg)
		s.collectDBaaSData(ctx, pg)
	}
	logger.Info("Tables statistic metrics collection finished")
}

func (s *Scraper) collectDBaaSData(ctx context.Context, pg *postgres.PostgresConnector) {
	columns, rows := getData(ctx, pg, dbaasQuery)

	for _, row := range rows {
		labels := gauges.DefaultLabels()
		labels["datname"] = pg.GetDatabase()
		value := ""
		for _, column := range columns {
			rValue := row[column]
			if column == "name" { //todo why name is value ???
				value = fmt.Sprintf("%v", rValue)
			} else {
				labels[column] = fmt.Sprintf("%v", rValue)
			}
		}
		s.metrics = append(s.metrics, NewMetric("ma_pg_dbaas").withLabels(labels).setValue(value))
	}
}

func (s *Scraper) collectTableMetrics(ctx context.Context, pg *postgres.PostgresConnector) {
	columns, rows := getData(ctx, pg, tablesStatQuery)

	for _, row := range rows {
		labels := gauges.DefaultLabels()
		labels["datname"] = pg.GetDatabase()
		var tableSizeValue, liveTuplesValue, totSizeValue, idxSizeValue string
		for _, column := range columns {
			rValue := fmt.Sprintf("%v", row[column])
			switch column {
			case "t_size":
				tableSizeValue = rValue
			case "live_tuples":
				liveTuplesValue = rValue
			case "tot_size":
				totSizeValue = rValue
			case "idx_size":
				idxSizeValue = rValue
			default:
				labels[column] = rValue
			}
		}
		s.metrics = append(s.metrics, NewMetric("ma_pg_table_size_by_db").withLabels(labels).setValue(tableSizeValue))
		s.metrics = append(s.metrics, NewMetric("ma_pg_tuples_by_db").withLabels(labels).setValue(liveTuplesValue))
		s.metrics = append(s.metrics, NewMetric("ma_pg_tot_size_by_db").withLabels(labels).setValue(totSizeValue))
		s.metrics = append(s.metrics, NewMetric("ma_pg_idx_size_by_db").withLabels(labels).setValue(idxSizeValue))
	}
}

func (s *Scraper) collectCommonPerfMetrics(ctx context.Context, pg *postgres.PostgresConnector) {
	logger.Info("Common additional performance metrics collection started")
	for metric, query := range commonPerfMetricsQueries {
		columns, rows := getData(ctx, pg, query)
		for _, row := range rows {
			labels := gauges.DefaultLabels()
			value := ""
			for i, column := range columns {
				if i == 0 {
					value = fmt.Sprintf("%v", row[column])
				} else {
					labels[column] = fmt.Sprintf("%v", row[column])
				}
			}
			s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s", metric)).withLabels(labels).setValue(value))
		}
	}

	logger.Debug("Common additional performance metrics collection finished")
}

func (s *Scraper) collectCountStatMetrics(ctx context.Context, pc *postgres.PostgresConnector) {
	columns, rows := getData(ctx, pc, countStatQuery)
	for _, row := range rows {
		for _, metric := range columns {
			mValue := row[metric]
			s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_connection_count_stat_%s", metric)).withLabels(gauges.DefaultLabels()).setValue(mValue))
		}
	}
}

func getDatabasesList(ctx context.Context, pg *postgres.PostgresConnector) ([]string, error) {
	databases := []string{}
	rows, err := pg.Query(ctx, dbQuery)
	if err != nil {
		logger.Error("Can't get databases list")
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		datname := new(string)
		err = rows.Scan(datname)
		if err != nil {
			logger.Error("Can't scan database name")
			return nil, err
		}

		if !util.Contains(procDatabases, *datname) {
			databases = append(databases, *datname)
		}
	}
	return databases, nil
}

func getData(ctx context.Context, pg *postgres.PostgresConnector, query string) ([]string, []Row) {
	resultRows := []Row{}
	rows, err := pg.Query(ctx, query)
	if err != nil {
		// Failing DBaaS query for non-DBaas databases is valid
		if query != dbaasQuery {
			logger.Error(fmt.Sprintf("Cannot execute query %s", query), zap.Error(err))
		}
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
