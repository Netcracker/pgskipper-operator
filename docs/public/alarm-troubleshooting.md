# Prometheus Alerts Troubleshooting

This chapter describes the troubleshooting scenarios and recommendations for the Prometheus alerts of PostgreSQL Operator.

The patroni-services chart defines the alerts in `operator/charts/patroni-services/templates/monitoring-templates/prometheus-rule.yml`. Each section below names the Prometheus alert that it covers. Per-node alerts exist in four variants, one for each of `node1` to `node4` (`First` to `Fourth` in the alert name). Every alert waits for `metricCollector.prometheusRules.alertDelay` (default `3m`) before it fires.

For deployment and operator errors, see [Troubleshooting](troubleshooting.md). For PostgreSQL cluster issues and the generic procedures referenced below, see [PostgreSQL Cluster Troubleshooting](troubleshooting-pg-cluster.md).

## Table of Contents

* [PostgreSQL is Down](#postgresql-is-down-on-hostname)
* [PostgreSQL is Degraded](#postgresql-is-degraded-on-hostname)
* [Disk space almost full](#disk-space-on-hostname-postgre-node-disk-is-almost-full)
* [High CPU usage](#cpu-on-hostname-postgresql-node-is-more-than-95-busy)
* [High memory usage](#memory-on-hostname-postgre-node-is-more-than-95-busy)
* [Long-running queries](#there-are-long-running-queries-on-hostname-node-execution-time-is-more-than-query_max_time_threshold-seconds)
* [Unable to collect metrics from backup agent](#unable-to-collect-metrics-from-postgresql-backup-agent-hostname)
* [Backup agent has problem](#postgresql-backup-agent-has-problem-on-hostname)
* [Backup space is low](#backup-space-on-hostname-is-less-than-backup_threshold-free)
* [Last backup failed](#last-backup-on-hostname-is-failed)
* [Last successful backup is too old](#last-successful-backup-on-hostname-is-too-old)
* [Too many locks](#locks-on-postgresql-node-node-more-than-pg_locks_threshold)
* [Patroni status not running](#patroni-status-on-hostname-node-is-not-running)
* [Replication lag](#postgresqlpatroni-replica-is-lagging)
* [Standby leader not connected](#patroni-standby-leader-is-not-connected)
* [Connection limit almost reached](#current-overall-connections-exceed-max_connection-percentage)
* [Connection limit reached](#current-connections-exceed-max_connection)
* [Replication slot WAL size too high](#postgresql-replication-slot-wal-size-is-too-high)
* [Threshold Configuration for Monitoring Alarms](#threshold-configuration-for-monitoring-alarms)

## PostgreSQL is Down on {HOST.NAME}

**Prometheus alert:** `PostgreSQL is Down`

### Description

The alarm is raised because of database or monitoring agent failure.

The database cluster is designed to survive internal or infrastructure failures, and recover after failures automatically.

### Possible Causes

- Out of Memory of PostgreSQL.
- Disk issue (no space left on device).
- Monitoring agent is down or unable to collect metrics from database.

### Impact

- All the applications that work with PostgreSQL are down.

### Actions for Investigation

- Check the status of Patroni pods: `kubectl get pods -n <namespace> -L pgtype -o wide`.
- Check the cluster state: `kubectl exec -n <namespace> <patroni-pod> -- patronictl list`.
- Review logs for Patroni pods for any errors or issues. See [Getting Pod's Log](troubleshooting-pg-cluster.md#getting-pods-log).
- Verify resource utilization of Patroni pods (CPU, memory): `kubectl top pods -n <namespace>`.
- Make sure that the monitoring agent (`monitoring-collector`) is up and running, and review its logs.

### Recommended Actions to Resolve Issue

- Try to analyze any errors in Patroni pods.
- If no leader can be detected, see [Patroni Cluster Has No Leader](troubleshooting-pg-cluster.md#patroni-cluster-has-no-leader) and [Patroni cannot Initialize Cluster and Wait for Leader to Bootstrap on all Nodes](troubleshooting-pg-cluster.md#patroni-cannot-initialize-cluster-and-wait-for-leader-to-bootstrap-on-all-nodes).
- Restart or redeploy Patroni pods if they are in a failed state.
- If the monitoring agent is failed, restart it.
- Investigate and address any resource constraints affecting the PostgreSQL performance.

## PostgreSQL is Degraded on {HOST.NAME}

**Prometheus alert:** `PostgreSQL is Degraded`

### Description

Replica database pod(s) goes down.

The database is able to process requests, but some replica instances are not available, and it can lead to problems in the future.

### Possible Causes

- Long outage of Patroni Replica Pod.
- Replication issues of PostgreSQL.

### Impact

- No impact on applications.
- In case of a failover, data loss is possible, because the replica is out of sync.

### Actions for Investigation

- Find the replica pods. See [Determining Leader and Replica PG Pods](troubleshooting-pg-cluster.md#determining-leader-and-replica-pg-pods).
- Try to analyze any errors in Patroni replica Pod. See [Getting Pod's Log](troubleshooting-pg-cluster.md#getting-pods-log).

### Recommended Actions to Resolve Issue

- Restart or redeploy Patroni pods if they are in a failed state.
- [Reinitialize replica of Patroni](troubleshooting-pg-cluster.md#replica-pod-reinitialization).

## Disk space on {HOST.NAME} Postgre {Node} Disk is almost full

**Prometheus alerts:** `Postgres First Node Disk is almost full`, `Postgre Second Node Disk is almost full` (the rule spells it `Postgre`), `Postgres Third Node Disk is almost full`, `Postgres Fourth Node Disk is almost full`. The alert fires when the data volume of the node is more than 90% full.

### Description

Available disk space on the database data volume is too low.

### Possible Causes

- Some logical PostgreSQL databases started to consume more disk space.
- PostgreSQL WAL files rotation issue due to stale replication connections or inactive replication slots.

### Impact

- Possible outage of PostgreSQL database due to no space left on device.

### Actions for Investigation

- Check size of `pg_wal` folder on PostgreSQL Leader disk:

  ```bash
  kubectl exec -n <namespace> <patroni-pod> -- df -h /var/lib/pgsql/data
  kubectl exec -n <namespace> <patroni-pod> -- du -sh /var/lib/pgsql/data/postgresql_node<N>/pg_wal
  ```

- Check size of logical databases in PostgreSQL:

  ```sql
  SELECT datname, pg_size_pretty(pg_database_size(datname)) FROM pg_database ORDER BY pg_database_size(datname) DESC;
  ```

- Check how much WAL each replication slot retains. See [PostgreSQL replication slot wal size is too high](#postgresql-replication-slot-wal-size-is-too-high).

### Recommended Actions to Resolve Issue

- Clean up space by dropping not needed logical PostgreSQL databases.
- Drop inactive replication slots that are no longer needed.
- Increase size of the PVCs used by Patroni pods.

## CPU on {HOST.NAME} PostgreSQL Node is more than 95% busy

**Prometheus alerts:** `CPU on Postgres First Node is more than 95% busy` to `CPU on Postgres Fourth Node is more than 95% busy`. The alert fires when CPU usage of the Patroni container is above 95% of its CPU limit.

### Description

The alarm indicates that the PostgreSQL Database is under high load.

### Possible Causes

- Incorrect CPU configuration.
- Database is under high load.

### Impact

- Slowness of query execution.
- New connections are established over a longer time.

### Actions for Investigation

- Check number of active PostgreSQL connections: `SELECT count(*) FROM pg_stat_activity WHERE state = 'active';`
- Verify whether heavy (non-optimal) queries are performed on the database as described in [Heavy Queries Detection](troubleshooting-pg-cluster.md#heavy-queries-detection).
- Make sure that CPU limits are calculated based on the [formula](installation.md#best-practices-and-recommendations).

### Recommended Actions to Resolve Issue

- Increase CPU limits for Patroni pods (`patroni.resources` in the patroni-core values). It is recommended to use the same configuration for all database nodes.

## Memory on {HOST.NAME} Postgre Node is more than 95% busy

**Prometheus alerts:** `Memory on Postgres First Node is more than 95% busy` to `Memory on Postgres Fourth Node is more than 95% busy`. The alert fires when the working set of the Patroni container is above 95% of its memory limit.

### Description

The alarm indicates that the database is under high load.

### Possible Causes

- Incorrect Memory configuration.
- Database is under high load.

### Impact

- Slowness of query execution.
- New connections are established over a longer time.
- Possible Out of Memory.

### Actions for Investigation

- Check number of active PostgreSQL connections: `SELECT count(*) FROM pg_stat_activity;`
- Verify whether heavy (non-optimal) queries are performed on the database as described in [Heavy Queries Detection](troubleshooting-pg-cluster.md#heavy-queries-detection).
- Make sure that Memory limits are calculated based on the [formula](installation.md#best-practices-and-recommendations).

### Recommended Actions to Resolve Issue

- Increase Memory Limits for Patroni pods (`patroni.resources` in the patroni-core values). It is recommended to use the same configuration for all database nodes.

## There are long-running queries on {HOST.NAME} {NODE}. Execution time is more than {$QUERY_MAX_TIME_THRESHOLD} second(s)

**Prometheus alerts:** `There are long running queries on First Node` to `There are long running queries on Fourth Node`.

### Description

Possible non-optimized long-running queries that should be investigated.

### Possible Causes

- Non-optimized queries.
- Misconfiguration of Memory and CPU limits.

### Impact

- N/A

### Actions for Investigation

- Verify whether heavy (non-optimal) queries are performed on the database as described in [Heavy Queries Detection](troubleshooting-pg-cluster.md#heavy-queries-detection).
- Verify and correct the `queryMaxTimeThreshold` threshold as described in [Threshold Configuration for Monitoring Alarms](#threshold-configuration-for-monitoring-alarms).

### Recommended Actions to Resolve Issue

- Find out the problem query.
- Check the execution plan.
- Optimize if possible.

## Unable to collect metrics from PostgreSQL backup agent {HOST.NAME}

**Prometheus alert:** `Unable to collect metrics from PostgreSQL backup agent`

### Description

PostgreSQL Backup Daemon is not working.

### Possible Causes

- PostgreSQL Backup Daemon is down.
- PostgreSQL Monitoring Agent is down.
- Database is unavailable for PostgreSQL Backup Daemon.

### Impact

- Scheduled Backups are not working.
- Not possible to request new Logical Backups.

### Actions for Investigation

- Check status of PostgreSQL Backup Daemon pod: `kubectl get pods -n <namespace> | grep postgres-backup-daemon`.
- Review logs for PostgreSQL Backup Daemon pod for any errors or issues: `kubectl logs -n <namespace> deployment/postgres-backup-daemon`.
- Verify resource utilization of PostgreSQL Backup Daemon pod (CPU, memory).

### Recommended Actions to Resolve Issue

- Restart or redeploy PostgreSQL Backup Daemon Pod if it is in a failed state.
- Investigate and address any resource constraints affecting the PostgreSQL Backup Daemon pod work.
- Check that PostgreSQL Monitoring Agent is working and collecting metrics.
- Check that there is enough space on PostgreSQL Backup Daemon PVC.

## PostgreSQL backup agent has problem on {HOST.NAME}

**Prometheus alert:** `PostgreSQL backup agent has problem`

### Description

PostgreSQL Backup Daemon scheduler is not working due to internal errors.

### Possible Causes

- The latest backups are in Failed state.

### Impact

- No possibility to do the recovery to the latest available backup.

### Actions for Investigation

- Check logs of PostgreSQL Backup Daemon during backup procedure: `kubectl logs -n <namespace> deployment/postgres-backup-daemon`.
- Try to run a [manual backup](#running-a-manual-backup) and see if it is completed successfully.

### Recommended Actions to Resolve Issue

- Fix the problem reported in the log of PostgreSQL Backup Daemon.
- Restart PostgreSQL Backup Daemon pod if the problem persists.

## Backup space on {HOST.NAME} is less than {$BACKUP_THRESHOLD}% free

**Prometheus alert:** `Space for Postgres backup is less than acceptable threshold`. Two rules have this name: one fires below `backupAlertThreshold` (default 5%) of free space, the other below `backupWarningThreshold` (default 20%).

### Description

Available disk space on the PostgreSQL Backup Daemon is too low.

### Possible Causes

- Incorrect backup schedule, backups are performed too often.
- Obsolete backups are not removed or eviction rules are incorrect.

### Impact

- No new PostgreSQL Backups in the future because of the disk space issue.

### Actions for Investigation

- Check the storage usage: `kubectl exec -n <namespace> deployment/postgres-backup-daemon -- df -h /backup-storage`.
- Check that backups are being evicted within the eviction policy (`backupDaemon.evictionPolicy`, env var `EVICTION_POLICY` of the backup daemon).
- Verify and correct the backup schedule (`backupDaemon.backupSchedule`).
- Verify and correct the `backupAlertThreshold` and `backupWarningThreshold` thresholds as described in [Threshold Configuration for Monitoring Alarms](#threshold-configuration-for-monitoring-alarms).

### Recommended Actions to Resolve Issue

- Reduce frequency of the backups via the `backupDaemon.backupSchedule` deploy parameter.
- Make the eviction policy keep fewer backups via the `backupDaemon.evictionPolicy` deploy parameter.
- Increase the backup volume (`backupDaemon.storage.size`).
- If the storage is already full, see [Backup Daemon Storage Clean Up](troubleshooting-pg-cluster.md#backup-daemon-storage-clean-up).

## Last backup on {HOST.NAME} is failed

**Prometheus alert:** `Last Postgres backup is failed`

### Description

Latest taken Backup of PostgreSQL Backup Daemon is in failed state.

### Possible Causes

- Database becomes unavailable for PostgreSQL Backup Daemon.
- Not enough disk space to store the backup archive.

### Impact

- No possibility to do the recovery to the latest available backup.

### Actions for Investigation

- Check logs of PostgreSQL Backup Daemon during backup procedure.
- Try to run a [manual backup](#running-a-manual-backup) and see if it is completed successfully.

### Recommended Actions to Resolve Issue

- Fix the problem reported in the log of PostgreSQL Backup Daemon.

## Last Successful backup on {HOST.NAME} is too old

**Prometheus alert:** `Last Successful Postgres backup is too old`. The alert fires when the last successful backup is older than `maxLastBackupAge` (default `86400` seconds).

### Description

PostgreSQL Backup Daemon has not completed a successful backup within the configured interval.

### Possible Causes

- Incorrect backup schedule or alarm threshold.
- PostgreSQL Backup Daemon is down or failed with an error.

### Impact

- No possibility to do the recovery to a recent backup.

### Actions for Investigation

- Make sure that PostgreSQL Backup Daemon is up and running, and review its logs.
- Verify the backup schedule (`backupDaemon.backupSchedule`).
- Verify and correct the `maxLastBackupAge` threshold as described in [Threshold Configuration for Monitoring Alarms](#threshold-configuration-for-monitoring-alarms).

### Recommended Actions to Resolve Issue

- Restart PostgreSQL Backup Daemon pod if it is in a failed state.
- Run a [manual backup](#running-a-manual-backup) and check that it is completed successfully.

### Running a Manual Backup

Request a backup from the backup daemon API. Add `-u <user>:<password>` if authentication is enabled for the backup daemon.

```bash
kubectl port-forward -n <namespace> deployment/postgres-backup-daemon 8080:8080
curl -X POST http://localhost:8080/backup
```

The response contains the backup ID. Check the result with `curl http://localhost:8080/backup/status/<backup-id>`.

## Locks on PostgreSQL node {NODE} more than {$PG_LOCKS_THRESHOLD}

**Prometheus alerts:** `Locks on First Node more then acceptable threshold` to `Locks on Fourth Node more then acceptable threshold`.

### Description

Number of PostgreSQL Locks in the database is more than the configured threshold (`locksThreshold`, default `500`).

### Possible Causes

- Quantity of database locks is too high.
- Incorrect alarm threshold.

### Impact

- Slowness of query execution.
- New connections are established over a longer time.

### Actions for Investigation

- The `pg_locks` view shows what locks are granted and what processes are waiting for locks to be acquired.
- Start a query looking for lock problems: `SELECT relation::regclass, * FROM pg_locks WHERE NOT granted;`
- You can figure out what processes are holding or waiting for locks using the information in the `pg_stat_activity` view. See [Heavy Queries Detection](troubleshooting-pg-cluster.md#heavy-queries-detection).
- Enable the `log_lock_waits` PostgreSQL configuration parameter (`patroni.postgreSQLParams`).
- Verify and correct the `locksThreshold` threshold as described in [Threshold Configuration for Monitoring Alarms](#threshold-configuration-for-monitoring-alarms).

### Recommended Actions to Resolve Issue

- Such a problem should be investigated by the Application team.

## Patroni status on {HOST.NAME} {NODE} is not running

**Prometheus alerts:** `Patroni first Node is not running` to `Patroni fourth Node is not running`.

### Description

Patroni can't start PostgreSQL on node.

### Possible Causes

- Invalid permissions for the PVC.
- Not enough space on Patroni PVC.

### Impact

- PostgreSQL is not accessible by the Application.

### Actions for Investigation

- Check the `State` of Patroni members using `patronictl list`.
- Check logs of the members in an unexpected `State`: a healthy leader is `running`, and a healthy replica is `streaming`.
- Review logs for Patroni pods for any errors or issues.

### Recommended Actions to Resolve Issue

- Try to analyze any errors in Patroni pods.
- Restart or redeploy Patroni pods if they are in a failed state.

## PostgreSQL/Patroni Replica Is Lagging

**Prometheus alerts:** `Patroni Replica Is Lagging`, `PostgreSQL Replica Is Lagging`. The alerts fire when the replication lag is above `replicationLagValue` (default `33554432` bytes, 32 MiB).

### Description

There is a replication lag for one of the Patroni/PostgreSQL replicas.

### Possible Causes

- Replication Lag for one of the members of Patroni/PostgreSQL cluster.

### Impact

- N/A

### Actions for Investigation

- Check the `State` of Patroni members using `patronictl list`.
- Review logs of Patroni Lagging members.

### Recommended Actions to Resolve Issue

- Try to [re-init Patroni Replica](troubleshooting-pg-cluster.md#replica-pod-reinitialization).

## Patroni Standby Leader Is Not Connected

**Prometheus alert:** `Patroni Standby Leader Is Not Connected`

### Description

Patroni Standby Leader Is Not Connected to the Primary Cluster of PostgreSQL.

### Possible Causes

- Standby Leader is unavailable.
- Cross-site connection problems.

### Impact

- Not possible to perform Disaster Recovery cases.

### Actions for Investigation

All the steps are performed on the standby side.

- Check the status of Patroni pods.
- Determine the standby leader pod and verify the database connection. See [Determining Leader and Replica PG Pods](troubleshooting-pg-cluster.md#determining-leader-and-replica-pg-pods).
- Review logs for Patroni pods for any errors or issues.
- Check cross-site connectivity between the Kubernetes clusters.

### Recommended Actions to Resolve Issue

- If the standby leader cannot be detected, see [Patroni cannot Initialize Cluster and Wait for Leader to Bootstrap on all Nodes](troubleshooting-pg-cluster.md#patroni-cannot-initialize-cluster-and-wait-for-leader-to-bootstrap-on-all-nodes).
- Restore cross-site connectivity.
- If the above steps do not help, the problem requires a detailed investigation. Contact support.

## Current overall connections exceed max_connection percentage

**Prometheus alerts:** `Current overall connections exceed max_connection percentage` (threshold `maxConnectionExceedPercentageThreshold`, default 90%) and `Current overall connections reached average max_connection percentage` (threshold `maxConnectionReachedPercentageThreshold`, default 80%).

### Description

Alarm indicates that the open connection to `max_connections` ratio increases more than 90%.

### Possible Causes

- Too many open connections to PostgreSQL.

### Impact

- Not possible to establish new connections to PostgreSQL in the future.

### Actions for Investigation

- Check number of open connections via running the following query: `SELECT count(*) FROM pg_stat_activity;`
- Verify and correct the `max_connections` PostgreSQL configuration parameter.

### Recommended Actions to Resolve Issue

- Increase the `max_connections` PostgreSQL configuration parameter. See [Not Enough Connections for Clients](troubleshooting-pg-cluster.md#not-enough-connections-for-clients).
- Reduce number of open connections to PostgreSQL. If the percentage increases to 95% or more, check for and terminate unused and obsolete connections.

## Current connections exceed max_connection

**Prometheus alert:** `Current connections exceed max_connection`

### Description

Alarm indicates that current connections have reached the `max_connections` value.

### Possible Causes

- Too many open connections to PostgreSQL.
- `max_connections` is too low for the workload.

### Impact

- New connections to PostgreSQL fail.

### Actions for Investigation

- Check for unused and obsolete connections: `SELECT datname, usename, state, count(*) FROM pg_stat_activity GROUP BY 1, 2, 3 ORDER BY 4 DESC;`

### Recommended Actions to Resolve Issue

- Terminate unused and obsolete connections.
- If the application requires all those connections, increase `max_connections`. See [Not Enough Connections for Clients](troubleshooting-pg-cluster.md#not-enough-connections-for-clients).

## PostgreSQL replication slot wal size is too high

**Prometheus alert:** `PostgreSQL replication slot wal size is too high`. The alert fires when a replication slot retains more WAL than `postgresExporter.prometheusRules.maxReplicationSlotWalSizeThreshold` (default `1024` MB).

### Description

The replication slot WAL size is more than the allowed threshold in MB.

### Possible Causes

- The application that uses the replication slot does not consume changes.
- The `wal_keep_size` parameter is too high.
- An old replication slot is too far behind.

### Impact

- WAL files can occupy all space on the disk.

### Actions for Investigation

- Check the application that uses the replication slot.
- Check the `wal_keep_size` parameter.
- Check how far behind the oldest replication slots are:

```sql
SELECT slot_name,
       pg_wal_lsn_diff(
          pg_current_wal_lsn(),
          restart_lsn
       ) AS bytes_behind,
       active,
       wal_status
FROM pg_replication_slots
WHERE wal_status <> 'lost'
ORDER BY restart_lsn;
```

### Recommended Actions to Resolve Issue

- Decrease the `wal_keep_size` parameter value.
- For old replication slots that are no longer needed, use the `pg_drop_replication_slot()` function.

## Threshold Configuration for Monitoring Alarms

It is possible to change the threshold for some alarms to tune alarm configuration according to the deployment scheme and business needs. The thresholds are Helm values of the patroni-services chart under `metricCollector.prometheusRules`. To change a threshold, set the value and upgrade the patroni-services release.

| Threshold | Default Value | Description |
|-----------|---------------|-------------|
| `backupAlertThreshold` | `5` | Specifies the percent of available backup disk space |
| `backupWarningThreshold` | `20` | Specifies the percent of available backup disk space |
| `maxLastBackupAge` | `86400` | Specifies the interval in seconds since last successful backup |
| `queryMaxTimeThreshold` | `3600` | Specifies the query execution time in seconds |
| `locksThreshold` | `500` | Specifies the number of database locks |
| `replicationLagValue` | `33554432` | Specifies the replication lag in bytes |
| `maxConnectionExceedPercentageThreshold` | `90` | Specifies the percent of `max_connections` in use |
| `maxConnectionReachedPercentageThreshold` | `80` | Specifies the percent of `max_connections` in use |
| `alertDelay` | `3m` | Specifies how long a condition must hold before the alert fires |

For example:

```yaml
metricCollector:
  prometheusRules:
    queryMaxTimeThreshold: 7200
    locksThreshold: 1000
```
