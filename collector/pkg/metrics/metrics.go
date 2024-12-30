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
	"fmt"
	"sort"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
)

var (
	Metrics9 = map[string]string{
		"replication_data": "select usename, application_name, client_addr, state, sync_priority, sync_state, " +
			"pg_xlog_location_diff(sent_location, replay_location)::bigint as sent_replay_lag, " +
			"pg_xlog_location_diff(pg_current_xlog_location(), sent_location)::bigint as sent_lag " +
			"from pg_stat_replication WHERE usename = 'replicator'",
		"replication_slots": "select slot_name, pg_xlog_location_diff(CASE WHEN pg_is_in_recovery() " +
			"THEN pg_last_xlog_replay_location() ELSE pg_current_xlog_location() END, restart_lsn)::bigint as restart_lsn_lag, " +
			"pg_xlog_location_diff(CASE WHEN pg_is_in_recovery() THEN pg_last_xlog_replay_location() " +
			"ELSE pg_current_xlog_location() END, confirmed_flush_lsn)::bigint as confirmed_flush_lsn_lag " +
			"from pg_replication_slots",
		"xlog_location": "SELECT pg_xlog_location_diff(CASE WHEN pg_is_in_recovery() " +
			"THEN pg_last_xlog_replay_location() ELSE pg_current_xlog_location() END, '0/0')::bigint",
		"pg_xlog": "/var/lib/pgsql/data/postgresql_%s/pg_xlog",
	}

	Metrics10 = map[string]string{
		"replication_data": "select usename, application_name, client_addr, state, sync_priority, sync_state, " +
			"pg_wal_lsn_diff(sent_lsn, replay_lsn)::bigint as sent_replay_lag, (case pg_is_in_recovery() when 't' " +
			"then pg_wal_lsn_diff(pg_last_wal_receive_lsn(), sent_lsn) else pg_wal_lsn_diff(pg_current_wal_lsn(), " +
			"sent_lsn)end)::bigint as sent_lag from pg_stat_replication WHERE usename = 'replicator' ",
		"replication_slots": "select slot_name, pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn() " +
			"ELSE pg_current_wal_lsn() END, restart_lsn)::bigint as restart_lsn_lag, pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery() " +
			"THEN pg_last_wal_replay_lsn() ELSE pg_current_wal_lsn() END, confirmed_flush_lsn)::bigint as confirmed_flush_lsn_lag " +
			"from pg_replication_slots",
		"xlog_location": "SELECT pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn() " +
			"ELSE pg_current_wal_lsn() END, '0/0')::bigint",
		"pg_xlog": "/var/lib/pgsql/data/postgresql_%s/pg_wal",
	}

	ArchiveDataQuery = "SELECT (select setting from pg_settings where name='archive_mode'), " +
		"EXTRACT(EPOCH FROM (now()-last_archived_time)), archived_count, failed_count FROM pg_stat_archiver"
)

func GetMetricsTypeByVersion(name string, version int) string {
	logger.Debug(fmt.Sprintf("Getting metrics of type: %s for pgsql version: %v", name, version))
	if version < 10 {
		return Metrics9[name]
	} else {
		return Metrics10[name]
	}
}

type Metric struct {
	name   string
	value  float64
	labels map[string]string
}

func NewMetric(name string) *Metric {
	return &Metric{name: name, labels: map[string]string{}}
}

func (m *Metric) getValue() float64 {
	return m.value
}

func (m *Metric) setValue(value any) *Metric {

	if val, err := util.GetFloatValue(value, float64(-1)); err != nil {
		Log.Warn(fmt.Sprintf("Can't set value '%v' for metric '%s'", value, m.name))
	} else {
		m.value = val
	}
	return m
}

func (m *Metric) withLabels(labels map[string]string) *Metric {
	m.labels = labels
	return m
}

type labels map[string]string

func (l labels) sort() (keys []string) {
	keys = make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return
}
