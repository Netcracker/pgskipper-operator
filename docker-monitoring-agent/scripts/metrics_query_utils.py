# Copyright 2024-2025 NetCracker Technology Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import logging

logger = logging.getLogger("metric-collector")

METRICS_10 = {
    "replication_data": """select 
                        usename, application_name, client_addr, state, sync_priority, sync_state,
                        pg_wal_lsn_diff(sent_lsn, replay_lsn)::bigint as sent_replay_lag,
                        (case pg_is_in_recovery() when 't' then pg_wal_lsn_diff(pg_last_wal_receive_lsn(), sent_lsn) else pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn)end)::bigint as sent_lag
                        from pg_stat_replication WHERE usename = 'replicator'
                        """,
    "replication_slots": """select 
                        slot_name,
                        pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery()
                                                        THEN pg_last_wal_replay_lsn()
                                                        ELSE pg_current_wal_lsn()
                                                   END, restart_lsn)::bigint as restart_lsn_lag,
                        pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery()
                                                        THEN pg_last_wal_replay_lsn()
                                                        ELSE pg_current_wal_lsn()
                                                   END, confirmed_flush_lsn)::bigint as confirmed_flush_lsn_lag
                        from pg_replication_slots""",
    "xlog_location":    """SELECT pg_wal_lsn_diff(CASE WHEN pg_is_in_recovery()
                                                        THEN pg_last_wal_replay_lsn()
                                                        ELSE pg_current_wal_lsn()
                                                   END, '0/0')::bigint
                                                   """,
    "pg_xlog":          "/var/lib/pgsql/data/postgresql_{}/pg_wal"
}

METRICS_9 = {
    "replication_data": """select 
                        usename, application_name, client_addr, state, sync_priority, sync_state,
                        pg_xlog_location_diff(sent_location, replay_location)::bigint as sent_replay_lag,
                        pg_xlog_location_diff(pg_current_xlog_location(), sent_location)::bigint as sent_lag
                        from pg_stat_replication WHERE usename = 'replicator'
                        """,
    "replication_slots": """select 
                        slot_name,
                        pg_xlog_location_diff(CASE WHEN pg_is_in_recovery()
                                                        THEN pg_last_xlog_replay_location()
                                                        ELSE pg_current_xlog_location()
                                                   END, restart_lsn)::bigint as restart_lsn_lag,
                        pg_xlog_location_diff(CASE WHEN pg_is_in_recovery()
                                                        THEN pg_last_xlog_replay_location()
                                                        ELSE pg_current_xlog_location()
                                                   END, confirmed_flush_lsn)::bigint as confirmed_flush_lsn_lag
                        from pg_replication_slots""",
    "xlog_location":    """SELECT pg_xlog_location_diff(CASE WHEN pg_is_in_recovery()
                                                        THEN pg_last_xlog_replay_location()
                                                        ELSE pg_current_xlog_location()
                                                   END, '0/0')::bigint""",
    "pg_xlog":          "/var/lib/pgsql/data/postgresql_{}/pg_xlog"
}


def get_common_pg_metrics_by_version(version):
    return {
        "running":                  "select 1",
        "db_count":                 "select count(*) from pg_stat_database",
        "current_connections":      "select count(*) from pg_stat_activity",
        "locks":                    "select count(*) from pg_locks",
        "locks_not_granted":        "select count(*) from pg_locks where NOT GRANTED",
        "replication_connections":  "select count(*) from pg_stat_replication",
        "replication_slots_count":  "select count(*) from pg_replication_slots",
        "xlog_location":            get_metrics_type_by_version("xlog_location", version),
        "pg_is_in_recovery":        "SELECT pg_is_in_recovery()",
        "xact_sum":                 "select sum(xact_commit + xact_rollback) :: bigint from pg_stat_database",
        "query_max_time":           "SELECT COALESCE(trunc(extract(epoch from (now() - min(query_start)))), 0) FROM pg_stat_activity WHERE state='active' AND usename != 'replicator' AND wait_event_type ||'.'|| wait_event != 'Client.WalSenderWaitForWAL'",
    }


def __is_version_below_10(version):
    return version < [10, 0]


def get_metrics_type_by_version(type, version):
    logger.debug("Getting metrics of type: {} for pgsql version: {}".format(type, version))
    version_check = __is_version_below_10(version)
    if version_check:
        return METRICS_9[type]
    else:
        return METRICS_10[type]
