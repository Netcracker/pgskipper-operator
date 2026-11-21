# Prometheus alerts

The patroni-services chart defines these alerts in
`operator/charts/patroni-services/templates/monitoring-templates/prometheus-rule.yml`. Each section uses the alert name
as defined there. Per-node alerts exist in four variants, one for each of `node1` to `node4`.

Thresholds are Helm values under `metricCollector.prometheusRules`. Every alert waits for `alertDelay` (default `3m`)
before it fires.

Generic procedures referenced below, such as replica reinitialization, are in
[troubleshooting-pg-cluster.md](troubleshooting-pg-cluster.md). Ask the user before you restart pods, reinitialize
replicas, or change resources.

## PostgreSQL is Down

**Condition:** `ma_pg_patroni_cluster_status` is `10` or negative. Severity: `disaster`.

**Meaning:** The cluster serves no requests, or the monitoring agent cannot collect the cluster status. Patroni is
designed to recover from member failures automatically, so this alert means recovery did not happen.

**Possible causes**

- PostgreSQL ran out of memory.
- The data volume is full.
- The monitoring agent is down or cannot collect metrics.

**Impact:** Every application that uses this cluster is down.

**Investigation**

```bash
kubectl get pods -n <namespace> -L pgtype -o wide
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
kubectl logs -n <namespace> <patroni-pod> -c pg-patroni-node<N> --previous
kubectl top pods -n <namespace>
```

**Actions**

- Fix the errors found in the Patroni logs. See
  [Cluster has no leader](troubleshooting.md#cluster-has-no-leader) and
  [Patroni pods do not start](troubleshooting.md#patroni-pods-do-not-start).
- With the user's confirmation, restart Patroni pods that stay in a failed state.
- Raise resource limits if the pods are out of memory or CPU.

## PostgreSQL is Degraded

**Condition:** `ma_pg_patroni_cluster_status` is `6`. Severity: `high`.

**Meaning:** The leader serves requests, but one or more replicas are down.

**Possible causes**

- A replica pod has been down for a long time.
- Replication from the leader to a replica is broken.

**Impact:** Applications keep working. If the leader fails while a replica is out of sync, a failover can lose data.

**Investigation**

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
kubectl logs -n <namespace> <replica-pod> -c pg-patroni-node<N>
```

**Actions**

- Fix the errors in the replica logs, and restart the replica pod if it stays failed.
- If the replica cannot catch up, reinitialize it with the user's confirmation. See
  [Replica pod reinitialization](troubleshooting-pg-cluster.md#replica-pod-reinitialization).

## Postgres First Node Disk is almost full

**Variants:** `Postgres First Node Disk is almost full`, `Postgre Second Node Disk is almost full` (the rule spells it
`Postgre`), `Postgres Third Node Disk is almost full`, `Postgres Fourth Node Disk is almost full`.

**Condition:** `ma_pg_patroni_metrics_df_pcent` for the node is above `90`. Severity: `high`.

**Possible causes**

- The databases have grown.
- WAL files accumulate because an inactive replication slot or a stale replica holds them.

**Impact:** When the volume fills up, PostgreSQL stops.

**Investigation**

```bash
kubectl exec -n <namespace> <patroni-pod> -- df -h /var/lib/pgsql/data
kubectl exec -n <namespace> <patroni-pod> -- du -sh /var/lib/pgsql/data/postgresql_node<N>/pg_wal
```

Check how much WAL each replication slot retains and the size of each database with the queries in
[Additional diagnostic commands](troubleshooting-pg-cluster.md#additional-diagnostic-commands).

**Actions**

- Remove inactive replication slots that nobody needs, with the user's confirmation.
- Drop databases that are no longer needed, with the owner's confirmation.
- Increase the size of the PVCs that the Patroni pods use.

## CPU on Postgres First Node is more than 95% busy

**Variants:** one alert per node, `First` to `Fourth`.

**Condition:** CPU usage of the Patroni container is above 95% of its CPU limit. Severity: `average`.

**Possible causes**

- The CPU limit is too low for the workload.
- The workload has grown, or heavy queries run on the node.

**Impact:** Queries run slower, and new connections take longer to establish.

**Investigation**

- Count active connections: `SELECT count(*) FROM pg_stat_activity WHERE state = 'active';`
- Find heavy queries. See [Heavy queries detection](troubleshooting-pg-cluster.md#heavy-queries-detection).
- Compare the CPU limit in `patroni.resources` with the real load and with the sizing formula in the
  [installation guide](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/installation.md#best-practices-and-recommendations).

**Actions:** Optimize the heavy queries, or raise the CPU limit in `patroni.resources`.

## Memory on Postgres First Node is more than 95% busy

**Variants:** one alert per node, `First` to `Fourth`.

**Condition:** The working set of the Patroni container is above 95% of its memory limit. Severity: `average`.

**Possible causes**

- The memory limit is too low for the configured PostgreSQL memory settings.
- The workload has grown.

**Impact:** Queries run slower, new connections take longer, and the container can be killed for running out of memory.

**Investigation**

- Count connections: `SELECT count(*) FROM pg_stat_activity;`
- Compare the memory limit in `patroni.resources` with `shared_buffers`, `work_mem`, and `max_connections`, and with
  the sizing formula in the
  [installation guide](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/installation.md#best-practices-and-recommendations).

**Actions:** Raise the memory limit in `patroni.resources`, or lower the memory settings and the number of connections.

## There are long running queries on First Node

**Variants:** one alert per node, `First` to `Fourth`.

**Condition:** `ma_pg_metrics_query_max_time` for the node is above `queryMaxTimeThreshold` (default `3600` seconds).
Severity: `average`.

**Possible causes**

- A query is not optimized, or it waits on a lock.
- CPU or memory limits are too low.

**Investigation**

- Find the query and what it waits on. See
  [Heavy queries detection](troubleshooting-pg-cluster.md#heavy-queries-detection).
- Check whether `queryMaxTimeThreshold` suits the workload; batch jobs can run longer by design.

**Actions:** Check the execution plan with the application owner and optimize the query. Cancel or terminate it only
with the user's confirmation.

## Unable to collect metrics from PostgreSQL backup agent

**Condition:** `ma_status` of `postgres-backup-daemon` is negative or absent. Severity: `high`.

**Possible causes**

- The backup daemon is down.
- The monitoring agent is down.
- The backup daemon cannot reach the database.

**Impact:** Scheduled backups do not run, and you cannot request new backups.

**Investigation**

```bash
kubectl get pods -n <namespace> | grep -E 'postgres-backup-daemon|monitoring'
kubectl logs -n <namespace> deployment/postgres-backup-daemon
kubectl exec -n <namespace> deployment/postgres-backup-daemon -- df -h /backup-storage
```

**Actions**

- With the user's confirmation, restart the backup daemon pod.
- Check that the monitoring agent works.
- If the backup storage is full, see
  [Backup daemon storage cleanup](troubleshooting-pg-cluster.md#backup-daemon-storage-cleanup).

## Patroni Replica Is Lagging

**Related alert:** `PostgreSQL Replica Is Lagging` has the same meaning and actions.

**Condition:** `ma_pg_patroni_replication_lag` (for `PostgreSQL Replica Is Lagging`,
`ma_pg_patroni_replication_state_sent_replay_lag`) is above `replicationLagValue` (default `33554432` bytes, 32 MiB).
Severity: `high`.

**Investigation**

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
kubectl logs -n <namespace> <replica-pod> -c pg-patroni-node<N>
```

`patronictl list` shows how far each replica is behind the leader.

**Actions**

- Check the replica for disk, CPU, or network problems.
- If the lag keeps growing, reinitialize the replica with the user's confirmation. See
  [Replica pod reinitialization](troubleshooting-pg-cluster.md#replica-pod-reinitialization).

## Current overall connections exceed max_connection percentage

**Related alerts:** `Current overall connections reached average max_connection percentage` fires at a lower threshold
(default 80%, severity `average`). `Current connections exceed max_connection` fires when every connection slot is used.

**Condition:** Open connections exceed `maxConnectionExceedPercentageThreshold` (default 90%) of `max_connections`.
Severity: `high`.

**Impact:** New connections soon fail with `remaining connection slots are reserved`.

**Investigation**

```sql
SELECT count(*) FROM pg_stat_activity;
SELECT datname, usename, state, count(*) FROM pg_stat_activity GROUP BY 1, 2, 3 ORDER BY 4 DESC;
SHOW max_connections;
```

**Actions**

- Reduce the number of open connections, for example with the connection pooler, or fix clients that leak connections.
- Raise `max_connections`. See
  [Not enough connections for clients](troubleshooting.md#not-enough-connections-for-clients).

## Patroni first Node is not running

**Variants:** `Patroni first Node is not running` to `Patroni fourth Node is not running`.

**Condition:** `ma_pg_patroni_patroni_status` for the node is `0`. Severity: `warning`.

**Meaning:** Patroni cannot start PostgreSQL on the node.

**Possible causes**

- Wrong permissions on the PVC.
- No space left on the PVC.

**Impact:** PostgreSQL on that node is not available to applications. If the node was the leader, Patroni fails over to
a replica.

**Investigation**

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
```

A healthy leader is `running`, and a healthy replica is `streaming`. Read the logs of every member in any other state,
including a replica that shows `running`.

**Actions**

- Fix the errors found in the Patroni logs. See
  [Patroni pods do not start](troubleshooting.md#patroni-pods-do-not-start).
- With the user's confirmation, restart Patroni pods that stay in a failed state.

## PostgreSQL backup agent has problem

**Condition:** `ma_status` of `postgres-backup-daemon` is `6`. Severity: `average`.

**Meaning:** The backup daemon scheduler does not work because of internal errors. Usually the latest backups failed.

**Impact:** You cannot restore to the latest backup.

**Investigation**

```bash
kubectl logs -n <namespace> deployment/postgres-backup-daemon
```

Run a manual backup and check whether it completes. See [Running a manual backup](#running-a-manual-backup).

**Actions:** Fix the error from the backup daemon log. With the user's confirmation, restart the backup daemon pod if
the problem persists.

## Space for Postgres backup is less than acceptable threshold

**Variants:** two rules share this name. One fires when free space on the backup storage is below
`backupAlertThreshold` (default 5%, severity `average`), the other below `backupWarningThreshold` (default 20%,
severity `warning`).

**Possible causes**

- Backups run too often for the storage size (`backupDaemon.backupSchedule`).
- Old backups are not evicted, or the eviction policy keeps too many (`backupDaemon.evictionPolicy`, env var
  `EVICTION_POLICY` of the backup daemon).

**Impact:** When the storage fills up, new backups fail.

**Investigation**

```bash
kubectl exec -n <namespace> deployment/postgres-backup-daemon -- df -h /backup-storage
kubectl get patroniservices -n <namespace> -o yaml | grep -iE 'schedule|eviction'
```

**Actions**

- Reduce the backup frequency with `backupDaemon.backupSchedule`, or keep fewer backups with
  `backupDaemon.evictionPolicy`, then upgrade the patroni-services release with the user's confirmation.
- Increase `backupDaemon.storage.size`.
- If the storage is already full, see
  [Backup daemon storage cleanup](troubleshooting-pg-cluster.md#backup-daemon-storage-cleanup).

## Last Postgres backup is failed

**Condition:** `ma_storage_last_failed` of `postgres-backup-daemon` is above `0`. Severity: `average`.

**Possible causes**

- The database was unavailable for the backup daemon.
- The backup storage has no space for the backup archive.

**Impact:** You cannot restore to the latest backup.

**Investigation:** Read the backup daemon log around the time of the backup, and run a manual backup. See
[Running a manual backup](#running-a-manual-backup).

**Actions:** Fix the error from the backup daemon log.

## Last Successful Postgres backup is too old

**Condition:** The last successful backup is older than `maxLastBackupAge` (default `86400` seconds). Severity:
`average`.

**Possible causes**

- The backup schedule or the alert threshold is wrong.
- The backup daemon is down, or its backups fail.

**Impact:** You can restore only to an old backup.

**Investigation**

```bash
kubectl get pods -n <namespace> | grep postgres-backup-daemon
kubectl logs -n <namespace> deployment/postgres-backup-daemon
kubectl get patroniservices -n <namespace> -o yaml | grep -i schedule
```

**Actions**

- With the user's confirmation, restart the backup daemon pod if it stays failed.
- Run a manual backup and check that it completes. See [Running a manual backup](#running-a-manual-backup).
- Fix `backupDaemon.backupSchedule` or `maxLastBackupAge` if they do not match.

### Running a manual backup

A manual backup adds load to the leader, so ask the user first. Add `-u <user>:<password>` if authentication is on.

```bash
kubectl port-forward -n <namespace> deployment/postgres-backup-daemon 8080:8080
curl -X POST http://localhost:8080/backup
curl http://localhost:8080/backup/status/<backup-id>
```

The `POST` response contains the backup ID.

## Locks on First Node more then acceptable threshold

**Variants:** one alert per node, `First` to `Fourth`.

**Condition:** `ma_pg_metrics_locks` for the node is above `locksThreshold` (default `500`). Severity: `average`.

**Possible causes**

- The application holds or waits for too many locks.
- The threshold is too low for the workload.

**Impact:** Queries run slower, and new connections take longer.

**Investigation**

```sql
SELECT relation::regclass, * FROM pg_locks WHERE NOT granted;
```

Find which sessions hold or wait for the locks with `pg_stat_activity`. See
[Blocking queries](troubleshooting-pg-cluster.md#blocking-queries). To log lock waits, add `"log_lock_waits: on"` to
`patroni.postgreSQLParams`.

**Actions:** Hand the blocking sessions and queries to the application team. Adjust `locksThreshold` if the workload
needs more locks by design. Terminate sessions only with the user's confirmation.

## Patroni Standby Leader Is Not Connected

**Condition:** `ma_pg_patroni_replication_state_sm_replication_state` is `0`. Severity: `high`.

**Meaning:** In an active-standby setup, the standby leader is not connected to the primary cluster. Investigate on the
standby side.

**Possible causes**

- The standby leader is down.
- Cross-site connectivity is broken.

**Impact:** Disaster recovery operations, such as a switchover, are not possible.

**Investigation**

```bash
kubectl get pods -n <namespace> -L pgtype -o wide
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
kubectl logs -n <namespace> <standby-leader-pod> -c pg-patroni-node<N>
```

Check that the standby cluster can reach the primary cluster's PostgreSQL service across sites.

**Actions**

- If no standby leader exists, see [Cluster has no leader](troubleshooting.md#cluster-has-no-leader) and
  [All members wait for the leader to bootstrap](troubleshooting.md#all-members-wait-for-the-leader-to-bootstrap).
- Restore cross-site connectivity. Network changes belong to the infrastructure owner.
- If the cause is unclear, escalate.

## Current connections exceed max_connection

**Condition:** `ma_pg_metrics_current_connections` is greater than or equal to `ma_pg_metrics_postgres_max_connections`.
Severity: `high`.

**Impact:** New connections fail with `remaining connection slots are reserved`.

**Investigation and actions:** The same as for
[Current overall connections exceed max_connection percentage](#current-overall-connections-exceed-max_connection-percentage).
Terminate unused connections only with the user's confirmation.

## PostgreSQL replication slot wal size is too high

**Condition:** `pg_replicaiton_slots_monitoring_retained_wal` (the metric name is misspelled in the rule) is above
`postgresExporter.prometheusRules.maxReplicationSlotWalSizeThreshold` (default `1024` MB). Severity: `high`.

**Related alert:** `PostgreSQL replication slot is not active for a long time` fires for inactive slots; investigate it
the same way.

**Possible causes**

- The client of the slot, such as a logical replication subscriber or a CDC tool, stopped consuming changes.
- `wal_keep_size` is too high.
- An old slot is no longer used.

**Impact:** Retained WAL can fill the data volume and stop PostgreSQL.

**Investigation:** On the leader:

```sql
SELECT slot_name,
       pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS bytes_behind,
       active,
       wal_status
FROM pg_replication_slots
WHERE wal_status <> 'lost'
ORDER BY restart_lsn;
SHOW wal_keep_size;
```

**Actions**

- Ask the owner of the slot's client to fix it.
- Lower `wal_keep_size` through `patroni.postgreSQLParams`.
- With the owner's confirmation, drop a slot that is no longer needed: `SELECT pg_drop_replication_slot('<slot-name>');`

## Thresholds

The thresholds are Helm values of the patroni-services chart under `metricCollector.prometheusRules`. Changing one needs
a `helm upgrade` of the patroni-services release, so ask the user first.

| Value | Default | Alerts |
|-------|---------|--------|
| `alertDelay` | `3m` | All |
| `backupAlertThreshold` | `5` (% free) | `Space for Postgres backup is less than acceptable threshold` |
| `backupWarningThreshold` | `20` (% free) | `Space for Postgres backup is less than acceptable threshold` |
| `maxLastBackupAge` | `86400` (s) | `Last Successful Postgres backup is too old` |
| `queryMaxTimeThreshold` | `3600` (s) | `There are long running queries on <N> Node` |
| `locksThreshold` | `500` | `Locks on <N> Node more then acceptable threshold` |
| `replicationLagValue` | `33554432` (bytes) | `Patroni Replica Is Lagging`, `PostgreSQL Replica Is Lagging` |
| `maxConnectionExceedPercentageThreshold` | `90` (%) | `Current overall connections exceed max_connection percentage` |
| `maxConnectionReachedPercentageThreshold` | `80` (%) | `Current overall connections reached average max_connection percentage` |

`maxReplicationSlotWalSizeThreshold` (default `1024` MB) is under `postgresExporter.prometheusRules` instead.

## Alerts without a procedure

This file does not cover the alerts below yet. Read the alert expression in `prometheus-rule.yml` and investigate the
component it names.

- `PostgreSQL metrics are absent`
- `PostgreSql Large Object Size High`
- `DB Connection exceeding more than specified limit`
- `PostgreSQL the percentage of transaction ID space used out`
- `Wait event warning threshold for SubtransBuffer or SubtransSLRU`
- `Wait event critical threshold for SubtransBuffer or SubtransSLRU`
- `Pct used warning threshold`
- `Standby cluster is not streaming`
- `Standby cluster has big lag in bytes`
- `Standby cluster has big lag in milliseconds`
- `PostgreSQL default partition size high`
