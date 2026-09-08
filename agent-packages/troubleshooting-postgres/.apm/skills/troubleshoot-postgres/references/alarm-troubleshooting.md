# PostgreSQL Operator Prometheus Alerts Troubleshooting

This guide covers troubleshooting for Prometheus alerts related to pgskipper-operator PostgreSQL clusters.

## Prometheus Alerts

### PostgreSQL is Down on {HOST.NAME}

**Description:** Alarm raised due to database or monitoring agent failure. The database cluster is designed to survive failures and recover automatically.

**Possible Causes:**
- Out of Memory for PostgreSQL
- Disk issue (no space left on device)
- Monitoring agent down or unable to collect metrics

**Impact:** All applications using PostgreSQL are down.

**Actions for Investigation:**
- Check status of Patroni pods
- Review logs for Patroni pods for errors
- Verify resource utilization (CPU, memory)

**Recommended Actions:**
- Analyze errors in Patroni pods
- Restart or redeploy Patroni pods if in failed state
- Address resource constraints

### PostgreSQL is Degraded on {HOST.NAME}

**Description:** Replica database pod(s) down. Database processes requests but some replicas unavailable.

**Possible Causes:**
- Long outage of Patroni Replica Pod
- PostgreSQL replication issues

**Impact:**
- No impact on applications currently
- Data-loss possible during failover if replica out of sync

**Actions for Investigation:**
- Analyze errors in Patroni replica Pod
- Restart or redeploy Patroni pods if failed

**Recommended Actions:**
- Reinitialize replica (see Replica Pod Reinitialization)

### Disk space on {HOST.NAME} Postgre {Node} Disk is almost full

**Description:** Available disk space on database data volume too low.

**Possible Causes:**
- Logical PostgreSQL databases consuming more space
- WAL files rotation issue due to stale replication connections

**Impact:** Possible outage due to no space left on device.

**Actions for Investigation:**
- Check size of `pg_wal` folder on PostgreSQL Leader disk
- Check size of logical databases in PostgreSQL

**Recommended Actions:**
- Clean up space by dropping unneeded logical databases
- Increase size of PVCs used by Patroni pods

### CPU on {HOST.NAME} PostgreSQL Node is more than 95% busy

**Description:** PostgreSQL Database under high load.

**Possible Causes:**
- Incorrect CPU configuration
- Database under high load

**Impact:**
- Slowness of query execution
- New connections established over longer time

**Actions for Investigation:**
- Check number of active PostgreSQL connections
- Verify CPU limits calculated correctly

**Recommended Actions:**
- Increase CPU limits for Patroni pods

### Memory on {HOST.NAME} Postgre Node is more than 95% busy

**Description:** Database under high load.

**Possible Causes:**
- Incorrect Memory configuration
- Database under high load

**Impact:**
- Slowness of query execution
- New connections established over longer time
- Possible Out of Memory

**Actions for Investigation:**
- Check number of active PostgreSQL connections
- Verify Memory limits calculated correctly

**Recommended Actions:**
- Increase Memory Limits for Patroni pods

### There are long-running queries on {HOST.NAME} {NODE}

**Description:** Non-optimized long-running queries detected.

**Possible Causes:**
- Non-optimized queries
- Misconfiguration of Memory and CPU limits

**Actions for Investigation:**
- Verify heavy (non-optimal) queries performed on database
- Verify threshold configuration

**Recommended Actions:**
- Find problematic query
- Check execution plan
- Optimize if possible

### Unable to collect metrics from PostgreSQL backup agent {HOST.NAME}

**Description:** PostgreSQL Backup Daemon not working.

**Possible Causes:**
- Backup Daemon down
- Monitoring Agent down
- Database unavailable for Backup Daemon

**Impact:**
- Scheduled backups not working
- Cannot request new logical backups

**Actions for Investigation:**
- Check status of Backup Daemon pod
- Review logs for errors
- Verify resource utilization

**Recommended Actions:**
- Restart or redeploy Backup Daemon pod
- Check monitoring agent working
- Verify enough space on Backup Daemon PVC

### PostgreSQL/Patroni Replica Is Lagging

**Description:** Replication lag for one of the Patroni/PostgreSQL replicas.

**Actions for Investigation:**
- Check state of Patroni members: `patronictl list`
- Review logs of lagging members

**Recommended Actions:**
- Re-init Patroni Replica (see Replica Pod Reinitialization in troubleshooting.md)

### Current overall connections exceed max_connection percentage

**Description:** Open connections to `max_connections` ratio exceeds 90%.

**Possible Causes:** Too many open connections to PostgreSQL.

**Impact:** Not possible to establish new connections in future.

**Actions for Investigation:**
```sql
SELECT count(*) FROM pg_stat_activity;
```
- Verify `max_connections` configuration parameter

**Recommended Actions:**
- Increase `max_connections` parameter
- Reduce number of open connections

### Patroni status on {HOST.NAME} {NODE} is not running

**Description:** Patroni cannot start PostgreSQL on node.

**Possible Causes:**
- Invalid permissions for PVC
- Not enough space on Patroni PVC

**Impact:** PostgreSQL not accessible by applications.

**Actions for Investigation:**
```bash
patronictl -c /patroni/pg_node list ${PG_CLUST_NAME:-patroni}
```
- Check logs of members with State not equal to `running`

**Recommended Actions:**
- Analyze errors in Patroni pods
- Restart or redeploy Patroni pods if in failed state
