# PostgreSQL Operator Troubleshooting Guide

This guide provides detailed troubleshooting procedures for pgskipper-operator (PostgreSQL as a Service operator managing Patroni-based PostgreSQL clusters on Kubernetes and OpenShift).

## Table of Contents

### Prometheus Alerts
* [PostgreSQL is Down](#postgresql-is-down-on-hostname)
* [PostgreSQL is Degraded](#postgresql-is-degraded-on-hostname)
* [Disk space almost full](#disk-space-on-hostname-postgre-node-disk-is-almost-full)
* [High CPU usage](#cpu-on-hostname-postgresql-node-is-more-than-95-busy)
* [High Memory usage](#memory-on-hostname-postgre-node-is-more-than-95-busy)
* [Long-running queries](#there-are-long-running-queries-on-hostname-node)
* [Backup agent issues](#unable-to-collect-metrics-from-postgresql-backup-agent-hostname)
* [Patroni status not running](#patroni-status-on-hostname-node-is-not-running)
* [Replication lag](#postgresqlpatroni-replica-is-lagging)
* [Connection limit reached](#current-overall-connections-exceed-max_connection-percentage)

### Deployment and Operator Issues
* [Deployment fails with rendered manifests contain a resource that already exists error](#deployment-fails-with-rendered-manifests-contain-a-resource-that-already-exists-error)
* [Postgres Operator failing during registration in Vault](#postgres-operator-failing-during-registration-in-vault)
* [Not possible to call PostgreSQL SiteManager endpoints](#not-possible-to-call-postgresql-sitemanager-endpoints)
* [Postgres-Service upgrade failed](#postgres-service-upgrade-failed)
* [Deployment fails with spec.externalDataBase.project Invalid value](#deployment-fails-with-the-postgresservice-is-invalid-specexternaldatabaseproject-invalid-value-integer)
* [Postgres site manager authorization issues](#postgres-site-manager-can-not-authorize-so-there-is-no-active-status-in-site-manager)

### Upgrade and Migration Issues
* [Postgres collation fix failed](#postgres-collation-fix-failed-for-several-databases)
* [Postgres major upgrade failed](#postgres-major-upgrade-failed)

### Application-Side Errors
* [Prepared Transactions are Disabled](#prepared-transactions-are-disabled)
* [Unexpectedly Closed Connection](#unexpectedly-closed-connection)
* [Read-only Transaction Error](#accessing-pg-results-in-read-only-transaction-error)
* [Not Enough Connections](#not-enough-connections-for-clients)

### Disaster Recovery Issues
* [Frozen Prepared Transactions](#deployment-fails-due-to-frozen-prepared-transactions-in-postgresql)

### Additional Common Issues
* [Patroni cluster has no leader](#patroni-cluster-has-no-leader)
* [pgBackRest backup failures](#pgbackrest-backup-failures)
* [Connection pooler (PgBouncer) issues](#connection-pooler-pgbouncer-issues)

---

## Introduction

This section provides detailed troubleshooting procedures for HA PostgreSQL cluster installations managed by pgskipper-operator. Ensure you have administrator privileges in the Kubernetes/OpenShift cluster and access to pod logs and diagnostics.

## Troubleshooting Overview

To resolve a database-related issue, determine whether the issue is caused by a database fault or failure of any other system. There are two main ways:

1. **Monitoring system reports** - Prometheus alerts like `PostgreSQL is DOWN`
2. **Application errors** - Application logs contain database-related errors

If monitoring reports issues but applications work properly, this may be a false positive. However, investigate the database to confirm health.

If applications report database failure, check the monitoring dashboard first. If dashboard shows healthy status but applications fail, troubleshoot applications (see [Typical Application-Side Error Messages](#typical-application-side-error-messages)).

---

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
- Reinitialize the replica: run `patronictl reinit <cluster-name> <member-name>` in a Patroni pod

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
- Reinitialize the lagging replica: run `patronictl reinit <cluster-name> <member-name>` in a Patroni pod

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
patronictl list
```
- Check logs of members with State not equal to `running`

**Recommended Actions:**
- Analyze errors in Patroni pods
- Restart or redeploy Patroni pods if in failed state

---

## Typical Application-Side Error Messages

### Prepared Transactions are Disabled

**Symptom:** Application log contains error:
```
org.postgresql.util.PSQLException: ERROR: prepared transactions are disabled
```

**Interpretation:** Using prepared transactions, but PostgreSQL has this feature disabled.

**Possible Reason:** Value of `max_prepared_transactions` in `postgresql.conf` is zero or absent.

**Proposed Fix:** Add `"max_prepared_transactions: <value>"` to `patroni.postgreSQLParams` in the patroni-core values and upgrade the release. The operator derives the `PG_CONF_MAX_PREPARED_TRANSACTIONS` env var from this list, so editing the env var in the StatefulSet does not last. PostgreSQL applies the parameter after a restart.

### Unexpectedly Closed Connection

**Symptom:** Application log contains recurring error:
```
org.postgresql.util.PSQLException: This connection has been closed
```

**Interpretation:** TCP connection between client and PG server closed unexpectedly.

**Possible Reason:** Permanent connectivity issues between client application pod and leader PG pod.

**Proposed Fix:** Check connectivity between client application and PG leader pods. If issues exist, check connectivity between nodes where pods run. If nodes OK, restart client application pod.

**Alternative Reason:** Connection pooling enabled, connections in pool became invalid after connectivity issues.

**Proposed Fix:** Ask application developers if application can withstand DB outage. If not, report bug. Actual fix depends on technologies used (e.g., WildFly AS: add `<validate-on-match>true</validate-on-match>` in datasource configuration).

**Workaround:** If connectivity issues temporary, restart client application to clear connection pool.

### Accessing PG Results in Read-only Transaction Error

**Symptom:** Application log contains error:
```
org.postgresql.util.PSQLException: ERROR: cannot execute UPDATE in a read-only transaction
```

**Possible Reason:** Accessing read-only PostgreSQL instance (replica pod).

**Proposed Fix:** Report bug to Netcracker PG support team.

**Workaround:** Fix the pod labels manually. Compare `patronictl list` with `kubectl get pods -L pgtype`, then relabel the pods:

```bash
kubectl label pod <former-leader-pod> -n <namespace> pgtype=replica --overwrite
kubectl label pod <actual-leader-pod> -n <namespace> pgtype=primary --overwrite
```

**Alternative Reason:** Application bug - data modification attempt in read-only transaction.

### Not Enough Connections for Clients

**Symptom:** Application log contains error:
```
FATAL: remaining connection slots are reserved for non-replication superuser connections
```

**Possible Reason:** Value of `max_connections` in `postgresql.conf` too low, or too many clients.

**Proposed Fix:**

1. Determine current `max_connections` value:
```bash
psql -c 'show max_connections'
```

2. Check connections per database:
```bash
psql -c 'select datname, count(*) from pg_stat_activity group by datname'
```

3. Estimate if sufficient, then either:
   - Reduce client connections
   - Increase `max_connections` setting

**Note:** Changing `max_connections` affects other users. Service calculates `work_mem` to fit every possible connection in memory. Increasing `max_connections` means less memory per operation, so sorts and hashes spill to temporary files on disk more often and the IO rate grows. Each connection is a new process with overhead.

To increase `max_connections`: add `"max_connections: <value>"` to `patroni.postgreSQLParams` in the patroni-core values and upgrade the release. The operator derives the `PG_MAX_CONNECTIONS` env var from this list. PostgreSQL applies the parameter after a restart.

Verify change:
```bash
psql -c 'show max_connections'
```

If value unchanged after few minutes, try:
```bash
/propagate_settings.sh
```

---

## Deployment and Operator Issues

### Deployment fails with rendered manifests contain a resource that already exists error

**Symptom:** Deployment fails with error:
```
Error: rendered manifests contain a resource that already exists. Unable to continue with install: existing resource conflict: kind: Role, namespace: <namespace>, name: postgres-operator
```

**Interpretation:** Trying to install Postgres Operator in namespace where Kubernetes objects still present from previous install.

**Possible Reason:** Kubernetes namespace not empty or not cleaned up properly.

**Proposed Fix:** Remove Kubernetes objects:
```bash
kubectl delete role postgres-operator -n <namespace>
kubectl delete sa postgres-sa -n <namespace>
kubectl delete rolebinding postgres-operator -n <namespace>
```

---

### Postgres Operator failing during registration in Vault

**Symptom:** When you use Vault as secret storage (`VAULT_ENABLED=true` in the DBaaS adapter), you may face some of these errors:

```
URL: PUT http://<vault-url>/v1/auth/<auth_method>/login 
Code 403. Errors:
* permission denied
```

One of the reasons may be incorrect Vault deployment to the cluster.
Check vault logs and if you find an error like this:

```json
{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"tokenreviews.authentication.k8s.io is forbidden: User \"system:serviceaccount:vault-operator:vault-service\" cannot create resource \"tokenreviews\" in API group \"authentication.k8s.io\" at the cluster scope","reason":"Forbidden","details":{"group":"authentication.k8s.io","kind":"tokenreviews"},"code":403}
```

**Interpretation:** Vault is unable to authorize Postgres Operator Service Account JWT token.

**Possible Reason:** Vault is misconfigured.

**Proposed Fix:** Bind the `system:auth-delegator` ClusterRole to the Vault service account. For more information, see the [Vault Kubernetes auth documentation](https://developer.hashicorp.com/vault/docs/auth/kubernetes).

---

### Not possible to call PostgreSQL SiteManager endpoints

**Symptom:** Calls to the PostgreSQL SiteManager API in an active-standby setup fail, and the `postgres-operator` log contains the following errors:

```json
{"level":"error","timestamp":"2022-01-31T11:52:25.143Z","msg":"There is an error during TokenReview Request","error":"tokenreviews.authentication.k8s.io is forbidden: User \"system:serviceaccount:postgres-service:postgres-sa\" cannot create resource \"tokenreviews\" in API group \"authentication.k8s.io\" at the cluster scope"}
```

**Interpretation:** Postgres Operator is unable to create TokenReview.

**Possible Reason:** Not all prerequisites are met.

**Proposed Fix:** Bind the built-in `system:auth-delegator` ClusterRole to the operator service account (`postgres-sa` by default):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: <namespace>-postgres-sa-auth-delegator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
  - kind: ServiceAccount
    name: postgres-sa
    namespace: <namespace>
```

---

### Postgres-Service upgrade failed

**Symptom:** The upgrade fails with the following log:

```
Error: UPGRADE FAILED: cannot patch "postgres-operator" with kind Deployment: Deployment.apps "postgres-operator" is invalid: spec.selector: Invalid value: v1.LabelSelector{MatchLabels:map[string]string{"name":"postgres-operator", "operator":"pod"}, MatchExpressions:[]v1.LabelSelectorRequirement(nil)}: field is immutable
```

**Possible Reason:** The chart copies `operator.podLabels` (or `podLabels` when `operator.podLabels` is empty) into the selector of the operator Deployment. Changing these values between releases changes `spec.selector`, which Kubernetes does not allow for an existing Deployment. In the example above, `operator: pod` is such a label. The patroni-core chart builds the selector of `patroni-core-operator` the same way.

Compare the selector of the running Deployment with the values of the release:

```bash
kubectl get deployment postgres-operator -n <namespace> -o jsonpath='{.spec.selector.matchLabels}'
helm get values patroni-services -n <namespace>
```

**Proposed Fix:** Either return `operator.podLabels` or `podLabels` to the values the Deployment was created with, or delete the operator Deployment and run `helm upgrade` again, so that Helm creates it with the new selector.

---

### Deployment fails with The PostgresService is invalid: spec.externalDataBase.project Invalid value integer

**Applies to:** older operator versions that used the `PostgresService` CRD. The current CRD is `PatroniServices`, and the patroni-services chart renders `externalDataBase.project` in quotes, so the current chart does not produce this error.

**Symptom:** A deployment with an external database (RDS) fails with the following error:

```text
The PostgresService "postgres-service" is invalid: spec.externalDataBase.project: Invalid value: "integer": spec.externalDataBase.project in body must be of type string: "integer"
```

**Possible Reason:** The old deployment rendered an all-digit project ID without quotes, so it reached the custom resource as a number, while the CRD expects a string.

**Proposed Fix:**

Upgrade the operator and the patroni-services chart. If you cannot upgrade, enclose the value in additional quotation marks so that it stays a string after rendering: `project: '"123456789012"'`.

With the current chart, write the ID as a plain YAML string, so that Helm does not convert it to a float such as `1.23456789012e+11`. Do not add the extra quotation marks: the chart would render `project: ""123456789012""`, which is invalid YAML.

```yaml
externalDataBase:
  type: RDS
  project: "123456789012"
```

---

### Postgres site manager can not authorize, so there is no active status in site-manager

**Symptom:** Site Manager shows no active status, and the `postgres-operator` log contains the following messages:

```json
{"level":"info","timestamp":"2024-07-09T07:34:53.647Z","msg":"Session token expired, re-authentication."}
{"level":"info","timestamp":"2024-07-09T07:42:22.482Z","msg":"Session token expired, re-authentication."}
{"level":"info","timestamp":"2024-07-09T07:57:22.512Z","msg":"Session token expired, re-authentication."}
{"level":"info","timestamp":"2024-07-09T07:57:53.761Z","msg":"Session token expired, re-authentication."}
```

```json
{"level":"error","timestamp":"2024-07-09T07:42:22.482Z","msg":"User <user> is unauthorized. Check site-manager configuration"}
```

**Possible Reason:** Site-manager is deployed to namespace with custom name or `siteManager.httpAuth.smNamespace` parameter is specified incorrectly.

**Proposed Fix:** Check the Site Manager namespace name, fix `siteManager.httpAuth.smNamespace` in the patroni-services values, and upgrade the patroni-services release. The parameter does not exist in the patroni-core chart.

---

## Upgrade and Migration Issues

### Postgres collation fix failed for several databases

**Symptom:** When connecting to database, warning message is displayed:

```text
The database was created using collation version 2.31, but the operating system provides version 2.35.
```

Logs show:

```json
{"level":"warn","timestamp":"2024-09-18T16:10:58.091Z","msg":"Cannot reindex database for db: cloud-ibnrm-pg-composite-streamers","error":"ERROR: syntax error at or near \"-\" (SQLSTATE 42601)"}
{"level":"warn","timestamp":"2024-09-18T16:11:00.888Z","msg":"Cannot reindex system for db: cloud-rnrm-pg-composite-streamers","error":"ERROR: syntax error at or near \"-\" (SQLSTATE 42601)"}
{"level":"warn","timestamp":"2024-09-18T16:11:02.389Z","msg":"Cannot alter locale version for db: dbaas_f630a1851b974280b5ee1af650a9e0ec","error":"ERROR: syntax error at or near \"REFRESH\" (SQLSTATE 42601)"}
```

**Possible Reason:** After upgrade from Postgres based on CentOS to Postgres on Ubuntu, collation version mismatch occurred due to different locale versions on the OS. The operator fixes collation versions with `ALTER DATABASE ... REFRESH COLLATION VERSION`, which is available only in PostgreSQL 15 and later. The `syntax error at or near "-"` errors come from older operator versions that did not quote database names containing hyphens.

**Proposed Fix:** Upgrade Postgres to version 15 or higher, upgrade the operator, and set the force flag `patroni.forceCollationVersionUpgrade: true` in the patroni-core values to run the script again.

---

### Postgres major upgrade failed

**Symptom:** The `pg-upgrade` container of the `pg-major-upgrade-<timestamp>` pod fails. Before running it, the operator scales all Patroni StatefulSets to zero. Read the log and the exit code:

```bash
kubectl logs -n <namespace> <upgrade-pod> -c pg-upgrade
kubectl get pod <upgrade-pod> -n <namespace> -o jsonpath='{.status.containerStatuses[?(@.name=="pg-upgrade")].state.terminated.exitCode}'
```

| End of the `pg-upgrade` log | Exit code | Operator action |
|-----------------------------|-----------|-----------------|
| `Check cluster before upgrade - Failed.` followed by the `pg_upgrade --check` output | 13 | Rolls back automatically: logs `Can't upgrade Patroni cluster. Rollback.`, starts Patroni again on the old version, and restores read-only databases |
| `shared_preload_libraries is not found in PostgreSQL config, please check PostgreSQL params, exiting...` | 1 | No rollback; the Patroni StatefulSets stay at zero replicas |
| `Upgrade failed with exit code <N>` followed by the last 100 lines of the `pg_upgrade` dump logs | 1 | No rollback; the Patroni StatefulSets stay at zero replicas |

**Pre-upgrade check failed (exit code 13):** Fix the incompatibility that `pg_upgrade --check` reports (the output is also saved to `/var/lib/pgsql/data/check_result` on the leader volume), then run the upgrade again.

**`shared_preload_libraries` is missing (exit code 1):** `patroni.postgreSQLParams` has no `shared_preload_libraries` entry. The script stops before `pg_upgrade` starts, so the data of the old cluster is intact. Upgrade images built before this check existed print no message, and their log ends with `making chmod 750 to datadir`.

The script reads the parameter from the `postgresql.conf` that Patroni writes, so the old cluster must run once with the parameter:

1. Set `patroni.dockerImage` back to the image of the old PostgreSQL version and add `shared_preload_libraries` to `patroni.postgreSQLParams`. The parameter list is replaced as a whole, so keep its other entries.

   ```yaml
   patroni:
     dockerImage: <old-patroni-image>
     postgreSQLParams:
       - "shared_preload_libraries: pg_stat_statements, pg_hint_plan, pg_cron"
       # ...the other entries of the list
   ```

2. Upgrade the patroni-core release. If the Patroni pods do not start, scale the `pg-<cluster-name>-node<N>` StatefulSets back to one replica.
3. Check the parameter with `SHOW shared_preload_libraries;` on the leader.
4. Set the new image again to repeat the upgrade.

**`pg_upgrade` failed (exit code 1):** The operator runs `pg_upgrade` with `--link`, so the old data directory may already be changed and unsafe to start, and the operator does not roll back. Do not start the old version on this data. Follow the manual steps in [Major Upgrade troubleshooting](features/major-upgrade.md#troubleshooting); recovery can require restoring the data from a backup.

---

## Disaster Recovery Issues

### Deployment fails due to frozen prepared transactions in PostgreSQL

**Symptom:** Deployment fails, and the following error appears in the DBaaS Aggregator logs:

```text
I/O error on POST request for "http://dbaas-postgres-adapter.postgresql:8080/api/v2/dbaas/adapter/postgresql/resources/bulk-drop": Read timed out
```

Additionally, the aggregator fails to process bulk-drop database operations, which causes the deployment to be blocked.

**Interpretation:** The issue arises when prepared transactions (`PREPARED TRANSACTION`) remain active in the database. These transactions prevent operations like `REASSIGN OWNED BY` or `bulk-drop`, causing the DBaaS Aggregator to time out.

**Possible Reason:** Frozen `PREPARED TRANSACTION` entries in the PostgreSQL database (`pg_prepared_xacts`) lock critical database operations.

**Proposed Fix:**

1. Log in to the Patroni leader pod (label `pgtype=primary`) and check for active prepared transactions:

```sql
SELECT gid, database, owner FROM pg_prepared_xacts;
```

2. Make sure with the application owner that the transactions can be discarded. Then connect to the database from the `database` column and roll back each prepared transaction by its transaction ID (gid). PostgreSQL finishes a prepared transaction only from the database where it was prepared.

```sql
ROLLBACK PREPARED '<gid>';
```

3. Verify no active transactions remain:

```sql
SELECT gid FROM pg_prepared_xacts;
```

4. Drop the database via the DBaaS API and ensure the issue no longer occurs.

---

## Additional Common Issues

### Patroni cluster has no leader

**Symptom:** `patronictl list` shows no member with the `Leader` role, and no pod has the `pgtype=primary` label.

**Diagnostic Steps:**

1. Check the member states and whether maintenance mode is on:
```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
```

2. Check the Patroni logs for DCS errors. By default, Patroni uses Kubernetes as the DCS (`patroni.dcs.type: kubernetes`) and keeps its state in the `<cluster-name>-leader` and `<cluster-name>-config` ConfigMaps. The Patroni container has the same name as its StatefulSet:
```bash
kubectl logs -n <namespace> <patroni-pod> -c pg-<cluster-name>-node<N> | grep -iE "dcs|configmap|leader|lock|forbidden|timeout"
```

**Proposed Fix:**

- Ensure Patroni pods can reach the Kubernetes API, or the etcd hosts from `patroni.dcs.hosts` when `patroni.dcs.type` is `etcd`, and that RBAC allows them to update their ConfigMaps
- Check that network policies allow communication between Patroni pods and between Patroni and the DCS
- Fix the errors that keep members from starting, such as a full disk
- Check whether Patroni is in maintenance mode; `patronictl resume` turns it off

### pgBackRest backup failures

**Symptom:** Backups fail with errors in pgBackRest logs.

**Diagnostic Steps:**

1. Check pgBackRest logs:
```bash
kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar
```

2. Check the stanzas, backups, and WAL archive:
```bash
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- pgbackrest info
```

**Proposed Fix:**

- Verify the S3 credentials (`pgBackRest.s3.key`, `pgBackRest.s3.secret`) or the RWX volume settings (`pgBackRest.rwx`)
- Check `pgBackRest.repoType`, `pgBackRest.repoPath`, and `pgBackRest.s3.endpoint`
- Ensure the retention values (`pgBackRest.fullRetention`, `pgBackRest.diffRetention`) match the backup schedule

### Connection pooler (PgBouncer) issues

**Symptom:** Applications cannot connect through PgBouncer.

**Diagnostic Steps:**

1. Check PgBouncer logs:
```bash
kubectl logs -n <namespace> deployment/connection-puller
```

2. Verify the PgBouncer configuration:
```bash
kubectl get configmap pooler-config -n <namespace> -o yaml
```

**Proposed Fix:**

- Verify `connectionPooler.install: true` in the patroni-services values
- Check `connectionPooler.config.databases` and the PgBouncer settings in `connectionPooler.config.pgbouncer`, such as `pool_mode`, `max_client_conn`, `default_pool_size`, and `auth_type`
- Ensure the database user exists and has proper privileges
