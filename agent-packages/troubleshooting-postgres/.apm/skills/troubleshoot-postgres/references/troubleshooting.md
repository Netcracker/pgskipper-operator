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
* [Postgres upgrade failed without error in logs](#postgres-upgrade-failed-without-an-error-in-logs)

### Application-Side Errors
* [Prepared Transactions are Disabled](#prepared-transactions-are-disabled)
* [Unexpectedly Closed Connection](#unexpectedly-closed-connection)
* [Read-only Transaction Error](#accessing-pg-results-in-read-only-transaction-error)
* [Not Enough Connections](#not-enough-connections-for-clients)

### PaaS Infrastructure Issues
* [Insufficient Cluster Resources](#insufficient-cluster-resources)
* [MatchNodeSelector Failed](#matchnodeselector-failed)
* [Wrong Permissions on Replica Bootstrap](#patroni-cannot-bootstrap-replica-due-to-wrong-permissions)

### HA Clustering Issues
* [Replica Unable to Start](#replica-instance-is-unable-to-start)
* [Malformed WALs](#replica-instance-is-unable-to-start-because-of-malformed-wals)
* [Malformed pg_internal.init](#replica-instance-is-unable-to-start-because-of-malformed-pg_internalinit-file)
* [Patroni cannot Initialize Cluster](#patroni-cannot-initialize-cluster-and-wait-for-leader-to-bootstrap-on-all-nodes)
* [Patroni API retries](#patroni-retries-requests-to-kubernetes-api)

### Replication Issues
* [Replication is Broken](#replication-is-broken)
* [Too many replication connections](#too-many-replication-connections)

### Monitoring Issues
* [du/df Metrics Timeout](#monitoring-cannot-collect-dudf-metrics-because-of-timeout)

### Disaster Recovery Issues
* [Failed Pods Exist](#failed-pods-exist)
* [Frozen Prepared Transactions](#deployment-fails-due-to-frozen-prepared-transactions-in-postgresql)

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
- Reinitialize replica (see [Replica Pod Reinitialization](#replica-pod-reinitialization))

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
- Re-init Patroni Replica (see [Replica Pod Reinitialization](#replica-pod-reinitialization))

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

---

## Typical Application-Side Error Messages

### Prepared Transactions are Disabled

**Symptom:** Application log contains error:
```
org.postgresql.util.PSQLException: ERROR: prepared transactions are disabled
```

**Interpretation:** Using prepared transactions, but PostgreSQL has this feature disabled.

**Possible Reason:** Value of `max_prepared_transactions` in `postgresql.conf` is zero or absent.

**Proposed Fix:** Change value of `PG_CONF_MAX_PREPARED_TRANSACTIONS` parameter in deployment. Navigate to PG deployment, **Actions > Edit YAML**, find `PG_CONF_MAX_PREPARED_TRANSACTIONS`, change value. Wait for pod restart.

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

**Workaround:** Fix labels on pods manually. Go to PG pod with `pgtype: leader` label, **Actions > Edit YAML**, change `pgtype: leader` to `pgtype: replica`. Go to actual leader pod, add `pgtype: leader` label under `metadata/labels`.

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

**Note:** Changing `max_connections` affects other users. Service calculates `work_mem` to fit every possible connection in memory. Increasing `max_connections` means less memory per operation, leading to disk usage (swap) and higher IO rate. Each connection is a new process with overhead.

To increase `max_connections`: Change `PG_MAX_CONNECTIONS` parameter in deployment. **Actions > Edit YAML**, find `PG_MAX_CONNECTIONS`, change value. Wait for restart.

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

## Postgres Operator failing during registration in Vault

**Symptom:** When you are using Vault as secret storage, you may face some of these errors:

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

**Proposed Fix:** You need to create Role Binding for Vault Service Account. For more info, see [Official Vault Documentation](https://www.vaultproject.io/docs/auth/kubernetes#configuring-kubernetes).

---

## Not possible to call PostgreSQL SiteManager endpoints

**Symptom:** Not possible to call PostgreSQL SiteManager API in Active Standby, with next errors in Postgres Operator:

```json
{"level":"error","timestamp":"2022-01-31T11:52:25.143Z","msg":"There is an error during TokenReview Request","error":"tokenreviews.authentication.k8s.io is forbidden: User \"system:serviceaccount:postgres-service:postgres-sa\" cannot create resource \"tokenreviews\" in API group \"authentication.k8s.io\" at the cluster scope"}
```

**Interpretation:** Postgres Operator is unable to create TokenReview.

**Possible Reason:** Not all prerequisites are met.

**Proposed Fix:** You have to create Cluster Role Binding for Auth Delegator. See `/examples/crb-pg-sm-auth-delegator.yaml` for reference.

---

## Postgres-Service upgrade failed

**Symptom:** Upgrade failed with next logs:

```
Error: UPGRADE FAILED: cannot patch "postgres-operator" with kind Deployment: Deployment.apps "postgres-operator" is invalid: spec.selector: Invalid value: v1.LabelSelector{MatchLabels:map[string]string{"name":"postgres-operator", "operator":"pod"}, MatchExpressions:[]v1.LabelSelectorRequirement(nil)}: field is immutable
```

**Possible Reason:** Deployment labels cannot be updated during upgrade. This is not supported by Kubernetes.

**Proposed Fix:** Deployment selector labels are immutable. You must delete the deployment and recreate it, or use a fresh namespace.

---

## Deployment fails with The PostgresService is invalid: spec.externalDataBase.project Invalid value integer

**Symptom:** When using `groovy.deploy.v3` and deployment with external Postgres RDS, error happens:

```
The PostgresService "postgres-service" is invalid: spec.externalDataBase.project: Invalid value: "integer": spec.externalDataBase.project in body must be of type string: "integer"
```

**Proposed Fix:**

Enclose the `externalDataBase.project` parameter in additional quotation marks:

```yaml
externalDataBase:
  type: rds
  project: '"123456789012"'
```

---

## Postgres site manager can not authorize, so there is no active status in site-manager

**Symptom:** There is no active status in site-manager and in patroni-core-operator next logs are present:

```json
{"level":"info","timestamp":"2024-07-09T07:34:53.647Z","msg":"Session token expired, re-authentication."}
{"level":"info","timestamp":"2024-07-09T07:42:22.482Z","msg":"Session token expired, re-authentication."}
{"level":"info","timestamp":"2024-07-09T07:57:22.512Z","msg":"Session token expired, re-authentication."}
{"level":"info","timestamp":"2024-07-09T07:57:53.761Z","msg":"Session token expired, re-authentication."}
```

```json
{"level":"error","timestamp":"2024-07-09T07:42:22.482Z","msg":"User %s is unauthorized. Check site-manager configuration"}
```

**Possible Reason:** Site-manager is deployed to namespace with custom name or `siteManager.httpAuth.smNamespace` parameter is specified incorrectly.

**Proposed Fix:** Check site-manager namespace name, fix `siteManager.httpAuth.smNamespace` and update patroni-core deployment.

---

## Postgres collation fix failed for several databases

**Symptom:** When connecting to database, warning message is displayed:

```
The database was created using collation version 2.31, but the operating system provides version 2.35.
```

Logs show:

```json
{"level":"warn","timestamp":"2024-09-18T16:10:58.091Z","msg":"Cannot reindex database for db: cloud-ibnrm-pg-composite-streamers","error":"ERROR: syntax error at or near \"-\" (SQLSTATE 42601)"}
{"level":"warn","timestamp":"2024-09-18T16:11:00.888Z","msg":"Cannot reindex system for db: cloud-rnrm-pg-composite-streamers","error":"ERROR: syntax error at or near \"-\" (SQLSTATE 42601)"}
{"level":"warn","timestamp":"2024-09-18T16:11:02.389Z","msg":"Cannot alter locale version for db: dbaas_f630a1851b974280b5ee1af650a9e0ec","error":"ERROR: syntax error at or near \"REFRESH\" (SQLSTATE 42601)"}
```

**Possible Reason:** After upgrade from Postgres based on CentOS to Postgres on Ubuntu, collation version mismatch occurred due to different locale versions on the OS. A fix script was added to update collation versions, but some procedures are not available on Postgres 14.

**Proposed Fix:** Upgrade Postgres version to 15 or higher and use the force flag `patroni.forceCollationVersionUpgrade: true` to run the script again.

---

## Deployment fails due to frozen prepared transactions in PostgreSQL

**Symptom:** Deployment fails, and the following error appears in the DBaaS Aggregator logs:

```
I/O error on POST request for "http://dbaas-postgres-adapter.postgresql:8080/api/v2/dbaas/adapter/postgresql/resources/bulk-drop": Read timed out
```

Additionally, the aggregator fails to process bulk-drop database operations, which causes the deployment to be blocked.

**Interpretation:** The issue arises when prepared transactions (`PREPARED TRANSACTION`) remain active in the database. These transactions prevent operations like `REASSIGN OWNED BY` or `bulk-drop`, causing the DBaaS Aggregator to timeout.

**Possible Reason:** Frozen `PREPARED TRANSACTION` entries in the PostgreSQL database (`pg_prepared_xacts`) lock critical database operations.

**Proposed Fix:**

1. Log in to the patroni master pod and check for active prepared transactions:

```sql
SELECT gid, database, owner FROM pg_prepared_xacts;
```

2. Manually roll back prepared transactions by their transaction ID (gid):

```sql
ROLLBACK PREPARED '<gid>';
```

3. Verify no active transactions remain:

```sql
SELECT gid FROM pg_prepared_xacts;
```

4. Drop the database via the DBaaS API and ensure the issue no longer occurs.

---

## Postgres upgrade failed without an error in logs

**Symptom:** Postgres major upgrade container failed with exit code 1. Last line in logs is:

```
Making chmod 750 to datadir
```

**Possible Reason:** `shared_preload_libraries` is absent in `patroni.postgreSQLParams` section.

**Proposed Fix:** 

Rollback to the previous Postgres version with parameter `patroni.postgreSQLParams` which must contain `shared_preload_libraries`.

Example:

```yaml
patroni:
  postgreSQLParams:
    shared_preload_libraries: "pg_stat_statements,auto_explain"
```

---

## Additional Common Issues

### Patroni Leader Election Failures

**Symptom:** Patroni cluster has no leader, logs show:

```
Leader election failed
Failover failed, manual intervention might be required
```

**Diagnostic Steps:**

1. Check DCS (etcd/Consul) connectivity:
```bash
kubectl logs -n <namespace> <patroni-pod> | grep -i "dcs\|etcd\|consul"
```

2. Check Patroni configuration:
```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
```

**Proposed Fix:**

- Ensure DCS (etcd/Consul) is healthy and accessible
- Check network policies allow communication between Patroni pods and DCS
- Verify `patroni.dcs` configuration in Helm values

### pgBackRest Backup Failures

**Symptom:** Backups fail with errors in pgBackRest logs.

**Diagnostic Steps:**

1. Check pgBackRest configuration:
```bash
kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar
```

2. Verify storage connectivity (S3, GCS, or PV):
```bash
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- pgbackrest --stanza=<stanza-name> info
```

**Proposed Fix:**

- Verify storage credentials in secret
- Check `pgBackRest.repoPath` and storage class configuration
- Ensure retention policies are correctly configured

### Connection Pooler (PGBouncer) Issues

**Symptom:** Applications cannot connect through PGBouncer.

**Diagnostic Steps:**

1. Check PGBouncer logs:
```bash
kubectl logs -n <namespace> <pgbouncer-pod>
```

2. Verify PGBouncer configuration:
```bash
kubectl exec -n <namespace> <pgbouncer-pod> -- cat /etc/pgbouncer/pgbouncer.ini
```

**Proposed Fix:**

- Verify `pooler.enabled: true` in Helm values
- Check `pooler.poolMode` (session/transaction/statement)
- Ensure database user exists and has proper privileges
