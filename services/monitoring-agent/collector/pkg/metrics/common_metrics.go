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

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/gauges"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/k8s"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/postgres"
	"go.uber.org/zap"
)

func (s *Scraper) CollectCommonMetrics() {
	logger.Info("Common metrics collection started")
	defer s.HandleMetricCollectorStatus()
	ctx := context.Background()
	if pgType == TypePatroni {
		logger.Debug("Pg type is Patroni")
		nodes := k8s.GetPatroniNodes(ctx, clusterName)
		for _, node := range nodes {
			logger.Debug(fmt.Sprintf("Collection of common metrics for node %s started", node.Name))
			s.collectMetricsFromNode(ctx, node)
		}
	} else {
		logger.Debug("Pg type is External")
		pc := postgres.NewConnector()
		s.collectMetrics(ctx, pc, "External")
	}
	logger.Info("Common metrics collection finished")
}

func (s *Scraper) collectMetricsFromNode(ctx context.Context, node k8s.PatroniNode) {
	pc := postgres.NewConnector()
	pc.SetHost(node.IP)
	s.collectMetrics(ctx, pc, node.Name)
}

func (s *Scraper) collectMetrics(ctx context.Context, pc *postgres.PostgresConnector, nodeName string) {
	Log.Debug("Collect Common PG Metrics")
	var pgVersion int
	metrics := getCommonMetrics()
	labels := gauges.DefaultLabels()
	if len(nodeName) > 0 {
		labels["pg_node"] = nodeName
	}

	err := pc.EstablishConn(ctx)
	if err != nil {
		return
	}
	defer pc.CloseConnection(ctx)

	pgVersion = s.pgMajorVersion

	if pgVersion >= 15 {
		delete(metrics, "pg_is_in_backup")
	}

	s.metrics = append(s.metrics, NewMetric("postgres_version").withLabels(labels).setValue(s.pgFullVersion))

	for metricName, query := range metrics {
		value, err := pc.GetValue(ctx, query)
		if err != nil {
			logger.Error(fmt.Sprintf("Can't collect metric %s", metricName), zap.Error(err))
			continue
		}
		s.metrics = append(s.metrics, NewMetric(prepareCommonMetricName(metricName)).withLabels(labels).setValue(value))
	}
	Log.Debug("Collect Common PG Metrics Completed")
}

func prepareCommonMetricName(metricName string) string {
	return fmt.Sprintf("ma_pg_metrics_%s", metricName)
}

func getCommonMetrics() map[string]string {
	return map[string]string{
		"running":                  "select 1",
		"db_count":                 "select count(*) from pg_stat_database",
		"current_connections":      "select count(*) from pg_stat_activity",
		"locks":                    "select count(*) from pg_locks",
		"locks_not_granted":        "select count(*) from pg_locks where NOT GRANTED",
		"replication_connections":  "select count(*) from pg_stat_replication",
		"replication_slots_count":  "select count(*) from pg_replication_slots",
		"xlog_location":            "SELECT pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn() ELSE pg_current_wal_lsn() END, '0/0')::bigint",
		"pg_is_in_recovery":        "SELECT case pg_is_in_recovery when 't' then 1 else 0 end pg_is_in_recovery from pg_is_in_recovery()",
		"pg_is_in_backup":          "SELECT case pg_is_in_backup when 't' then 1 else 0 end pg_is_in_backup from pg_is_in_backup()",
		"xact_sum":                 "select sum(xact_commit + xact_rollback) :: bigint from pg_stat_database",
		"postgres_max_connections": "SELECT setting::int FROM pg_settings WHERE name = 'max_connections'",
		"query_max_time":           "SELECT COALESCE(trunc(extract(epoch from (now() - min(query_start)))), 0) FROM pg_stat_activity WHERE state='active' AND usename != 'replicator' AND lower(wait_event_type ||'.'|| wait_event) != 'client.walsenderwaitforwal'",
	}
}
