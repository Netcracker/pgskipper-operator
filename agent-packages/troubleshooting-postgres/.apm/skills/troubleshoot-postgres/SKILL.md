---
name: troubleshoot-postgres
description: Diagnose and resolve PostgreSQL operator issues including Patroni cluster failures, pgBackRest backup problems, deployment errors, Vault integration, Site Manager authentication, upgrade failures, collation mismatches, frozen transactions, PGBouncer connection issues, and active-standby DR configuration. Covers PatroniCore and PatroniServices CRD troubleshooting, DCS connectivity (etcd/Consul), leader election, pgBackRest restore/PITR, connection pooler configuration, TLS setup, and DBaaS adapter integration. Use for pgskipper-operator, PostgreSQL-as-a-Service, Patroni clusters on Kubernetes/OpenShift.
---

# PostgreSQL Operator Troubleshooting Skill

Use this skill when investigating issues related to:

- **pgskipper-operator** (PostgreSQL operator managing Patroni clusters)
- **PatroniCore** CRD failures (cluster provisioning, leader election, DCS connectivity)
- **PatroniServices** CRD issues (service layer, monitoring, backup integration)
- **Patroni** high-availability failures (split-brain, failover, DCS issues)
- **pgBackRest** backup/restore failures (S3/GCS storage, PITR, retention)
- **Deployment errors** (Helm chart issues, CRD validation, resource conflicts)
- **Vault integration** (secret storage, authentication, token issues)
- **Site Manager** (active-standby DR, API authentication, cross-cluster sync)
- **Connection pooler** (PGBouncer configuration, connection limits)
- **Upgrade failures** (major version upgrades, `pg_upgrade`, collation fixes)
- **DBaaS adapter** integration (bulk operations, prepared transactions)

## Component Overview

### Operator Architecture

**pgskipper-operator** is a Kubernetes operator providing PostgreSQL-as-a-Service with two modes:

1. **`OPERATOR_ROLE=patroni`** — Runs `PatroniCoreReconciler` (manages `PatroniCore` CRD)
2. **Default** — Runs `PostgresServiceReconciler` (manages `PatroniServices` CRD)

**Key Components:**
- **Patroni** — HA orchestration with leader election via DCS (etcd/Consul)
- **pgBackRest** — Backup/restore with S3/GCS/PV storage
- **PGBouncer** — Connection pooling (session/transaction/statement modes)
- **Site Manager** — Active-standby DR coordination across Kubernetes clusters
- **Monitoring** — postgres-exporter, pgbackrest-exporter, query-exporter
- **DBaaS Adapter** — Database lifecycle management API

### Critical Configuration Paths

**Helm Chart Locations:**
- `operator/charts/patroni-core/` — Core operator Helm chart
- `operator/charts/patroni-services/` — Services layer Helm chart

**Key Helm Values:**
- `patroni.postgreSQLParams.*` — PostgreSQL configuration (`shared_preload_libraries`, etc.)
- `patroni.dcs.*` — DCS (etcd/Consul) connection settings
- `pgBackRest.*` — Backup configuration (repoPath, retention, storage)
- `pooler.*` — PGBouncer settings (poolMode, maxClientConn)
- `siteManager.httpAuth.smNamespace` — Site Manager namespace for Active-Standby
- `externalDataBase.project` — RDS/external database project ID (must be string)

### Forbidden Actions

**Never** use these commands without explicit user confirmation:

- `patronictl edit-config` — Direct DCS modification bypasses operator reconciliation
- `kubectl delete pvc` — Data loss; use operator-managed backup/restore instead
- `etcdctl rm` / `consul kv delete` — Breaks Patroni cluster state
- `kubectl delete PatroniCore` without backup — Permanent data loss

## Common Failure Modes

### 1. Deployment Failures

#### Resource Already Exists

**Symptom:** `rendered manifests contain a resource that already exists`

**Quick Check:**
```bash
kubectl get role,sa,rolebinding -n <namespace> | grep postgres-operator
```

**Resolution:** Clean up leftover resources from previous install (see [Troubleshooting_guide.md](#deployment-fails-with-rendered-manifests-contain-a-resource-that-already-exists-error)).

#### CRD Validation Failures

**Symptom:** `The PostgresService is invalid: spec.externalDataBase.project: Invalid value`

**Fix:** Enclose numeric project IDs in extra quotes: `project: '"123456789012"'`

#### Upgrade Failures

**Symptom:** `spec.selector: Invalid value ... field is immutable`

**Cause:** Kubernetes Deployment selector labels cannot be changed.

**Resolution:** Delete and recreate deployment, or use fresh namespace.

### 2. Patroni Cluster Issues

#### No Leader / Leader Election Failures

**Symptom:** Cluster stuck without leader, logs show `Leader election failed`

**Diagnostic Steps:**

1. Check DCS connectivity:
```bash
kubectl logs -n <namespace> <patroni-pod> | grep -iE "dcs|etcd|consul|connection refused"
```

2. Verify Patroni cluster status:
```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
```

3. Check network policies:
```bash
kubectl get networkpolicy -n <namespace>
```

**Common Causes:**
- DCS (etcd/Consul) unreachable or unhealthy
- Network policy blocking Patroni ↔ DCS communication
- Misconfigured `patroni.dcs` Helm values
- Split-brain scenario (multiple leaders)

**Resolution:**
- Verify DCS health and accessibility
- Check `patroni.dcs.endpoints` in Helm values
- Ensure network policies allow Patroni → DCS traffic
- Review Patroni logs for specific DCS errors

#### Pods Not Starting (CrashLoopBackOff)

**Symptom:** Patroni pods in `CrashLoopBackOff` or `Error` state

**Quick Checks:**

1. Pod events:
```bash
kubectl describe pod -n <namespace> <patroni-pod>
```

2. Init container logs:
```bash
kubectl logs -n <namespace> <patroni-pod> -c <init-container-name>
```

3. Main container logs:
```bash
kubectl logs -n <namespace> <patroni-pod> -c postgres
```

**Common Issues:**
- PVC mount failures (storage class misconfiguration)
- Insufficient resources (CPU/memory requests vs. limits)
- Image pull errors (registry authentication)
- PostgreSQL configuration errors (`shared_preload_libraries`, etc.)

### 3. Vault Integration Failures

**Symptom:** `Code 403. Errors: * permission denied` or `tokenreviews.authentication.k8s.io is forbidden`

**Cause:** Vault unable to authorize Postgres Operator Service Account JWT token.

**Resolution:** Create RoleBinding for Vault Service Account (see [Vault documentation](https://www.vaultproject.io/docs/auth/kubernetes#configuring-kubernetes)).

**Diagnostic:**
```bash
kubectl logs -n vault-operator <vault-pod> | grep -i tokenreview
```

### 4. Site Manager (Active-Standby DR) Issues

#### Authentication Failures

**Symptom:** No active status in Site Manager, logs show `Session token expired, re-authentication` repeatedly

**Symptom Example:**
```json
{"level":"error","timestamp":"2024-07-09T07:42:22.482Z","msg":"User %s is unauthorized. Check site-manager configuration"}
```

**Cause:** `siteManager.httpAuth.smNamespace` parameter incorrect or Site Manager in non-default namespace.

**Resolution:**

1. Verify Site Manager namespace:
```bash
kubectl get pods -A | grep site-manager
```

2. Fix Helm value:
```yaml
siteManager:
  httpAuth:
    smNamespace: "<actual-site-manager-namespace>"
```

3. Update patroni-core deployment:
```bash
helm upgrade patroni-core ./operator/charts/patroni-core -f values.yaml
```

#### TokenReview Failures

**Symptom:** `tokenreviews.authentication.k8s.io is forbidden` in operator logs

**Resolution:** Create ClusterRoleBinding for Auth Delegator (see `/examples/crb-pg-sm-auth-delegator.yaml`).

### 5. pgBackRest Backup/Restore Issues

**Symptom:** Backup jobs fail or restores timeout.

**Diagnostic Steps:**

1. Check pgBackRest sidecar logs:
```bash
kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar
```

2. Verify stanza info:
```bash
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- \
  pgbackrest --stanza=<stanza-name> info
```

3. Check storage connectivity:
```bash
# For S3
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- \
  aws s3 ls s3://<bucket-name>/

# For GCS
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- \
  gsutil ls gs://<bucket-name>/
```

**Common Issues:**
- Storage credentials expired/incorrect (check secret)
- `pgBackRest.repoPath` misconfigured
- Insufficient storage quota
- Network egress blocked to S3/GCS
- Retention policy too aggressive (deleting active backups)

**Resolution:**
- Verify secret contains valid credentials
- Check `pgBackRest.repo1-*` Helm values
- Ensure retention matches backup frequency
- Review pgBackRest configuration in `patroni.yml`

### 6. Connection Pooler (PGBouncer) Issues

**Symptom:** Applications cannot connect through PGBouncer.

**Diagnostic Steps:**

1. Check PGBouncer logs:
```bash
kubectl logs -n <namespace> <pgbouncer-pod>
```

2. Verify configuration:
```bash
kubectl exec -n <namespace> <pgbouncer-pod> -- cat /etc/pgbouncer/pgbouncer.ini
```

3. Test direct PostgreSQL connection (bypass pooler):
```bash
kubectl exec -n <namespace> <patroni-pod> -- \
  psql -U postgres -c "SELECT version();"
```

**Common Issues:**
- `pooler.enabled: false` in Helm values
- Incorrect `pooler.poolMode` (session/transaction/statement)
- Database user missing or wrong privileges
- Connection limit exhausted (`max_client_conn`)
- Authentication method mismatch (md5 vs. scram-sha-256)

**Resolution:**
- Enable pooler: `pooler.enabled: true`
- Verify `pooler.poolMode` matches application needs
- Check database user exists: `SELECT * FROM pg_user WHERE usename='<user>';`
- Adjust `pooler.maxClientConn` and `pooler.defaultPoolSize`

### 7. Upgrade Failures

#### Major Version Upgrades

**Symptom:** `pg_upgrade` container fails with exit code 1, logs end with `Making chmod 750 to datadir`

**Cause:** `shared_preload_libraries` missing from `patroni.postgreSQLParams`.

**Resolution:**

1. Rollback to previous version:
```bash
helm rollback patroni-core <previous-revision>
```

2. Add required parameter:
```yaml
patroni:
  postgreSQLParams:
    shared_preload_libraries: "pg_stat_statements,auto_explain"
```

3. Retry upgrade with corrected values.

#### Collation Version Mismatch

**Symptom:** Warning `The database was created using collation version 2.31, but the operating system provides version 2.35`

**Symptom Logs:**
```json
{"level":"warn","msg":"Cannot alter locale version for db: <dbname>","error":"ERROR: syntax error at or near \"REFRESH\" (SQLSTATE 42601)"}
```

**Cause:** Collation fix script requires PostgreSQL 15+ (not available on PostgreSQL 14).

**Resolution:**

1. Upgrade to PostgreSQL 15 or higher
2. Enable force flag:
```yaml
patroni:
  forceCollationVersionUpgrade: true
```

3. Re-apply Helm chart.

### 8. Frozen Prepared Transactions

**Symptom:** DBaaS Aggregator times out on `bulk-drop` operations:
```
I/O error on POST request for "http://dbaas-postgres-adapter.postgresql:8080/api/v2/dbaas/adapter/postgresql/resources/bulk-drop": Read timed out
```

**Cause:** Prepared transactions (`PREPARED TRANSACTION`) blocking `REASSIGN OWNED BY` or database drops.

**Resolution:**

1. Identify frozen transactions:
```sql
SELECT gid, database, owner FROM pg_prepared_xacts;
```

2. Roll back each transaction:
```sql
ROLLBACK PREPARED '<gid>';
```

3. Verify cleanup:
```sql
SELECT gid FROM pg_prepared_xacts;
```
(Result should be empty)

4. Retry database drop operation.

## Diagnostic Workflow

### Initial Triage

1. **Identify the failure component:**
   - Operator logs: `kubectl logs -n <namespace> deployment/postgres-operator`
   - Patroni logs: `kubectl logs -n <namespace> <patroni-pod> -c postgres`
   - PGBackRest logs: `kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar`
   - PGBouncer logs: `kubectl logs -n <namespace> <pgbouncer-pod>`

2. **Check resource state:**
   - CRD status: `kubectl get patronicore,patroniservices -n <namespace> -o yaml`
   - Pod status: `kubectl get pods -n <namespace> -o wide`
   - PVC status: `kubectl get pvc -n <namespace>`

3. **Review events:**
   ```bash
   kubectl get events -n <namespace> --sort-by='.lastTimestamp'
   ```

### Narrowing Down Issues

Use the symptom-to-section mapping below to jump to relevant troubleshooting steps:

| **Symptom** | **Section** |
|-------------|-------------|
| `rendered manifests contain a resource` | [Deployment Failures](#1-deployment-failures) |
| `Code 403. Errors: * permission denied` | [Vault Integration](#3-vault-integration-failures) |
| `tokenreviews.authentication.k8s.io is forbidden` | [Vault Integration](#3-vault-integration-failures) or [Site Manager](#4-site-manager-active-standby-dr-issues) |
| `Leader election failed` | [Patroni Cluster Issues](#2-patroni-cluster-issues) |
| `Session token expired, re-authentication` | [Site Manager](#4-site-manager-active-standby-dr-issues) |
| pgBackRest backup fails | [pgBackRest Issues](#5-pgbackrest-backuprestore-issues) |
| PGBouncer connection refused | [Connection Pooler](#6-connection-pooler-pgbouncer-issues) |
| `collation version mismatch` | [Upgrade Failures](#7-upgrade-failures) |
| `Making chmod 750 to datadir` (upgrade fails) | [Upgrade Failures](#7-upgrade-failures) |
| `bulk-drop: Read timed out` | [Frozen Prepared Transactions](#8-frozen-prepared-transactions) |
| `spec.selector: Invalid value ... field is immutable` | [Deployment Failures](#1-deployment-failures) |

## Using the Full Troubleshooting Guide

For deeper issues not covered above, load only the relevant section from `references/Troubleshooting_guide.md` — **do NOT read the full file**.

**Steps:**

1. Grep section headers to find line numbers:
```bash
grep -n "^## " references/Troubleshooting_guide.md
```

2. Match symptom to section.

3. Read only that section using `offset` and `limit`:
   - `offset` = section start line
   - `limit` = lines until next section header

**Example:**
If grep shows:
```
25:## Deployment fails with rendered manifests contain a resource that already exists error
50:## Postgres Operator failing during registration in Vault
```

To read the "Deployment fails" section:
```
Read(file_path="references/Troubleshooting_guide.md", offset=25, limit=25)
```

## Key Helm Chart Parameters

### Critical Configuration

**Operator Mode:**
- Set via `OPERATOR_ROLE` environment variable (default runs PatroniServices reconciler)

**Patroni DCS:**
```yaml
patroni:
  dcs:
    type: etcd  # or consul
    endpoints: "etcd-cluster:2379"
```

**PostgreSQL Parameters:**
```yaml
patroni:
  postgreSQLParams:
    shared_preload_libraries: "pg_stat_statements,auto_explain"
    max_connections: "100"
    wal_level: "logical"  # Required for logical replication
```

**pgBackRest:**
```yaml
pgBackRest:
  enabled: true
  repoPath: "/pgbackrest"
  repo1-type: "s3"
  repo1-s3-bucket: "<bucket-name>"
  repo1-s3-region: "us-east-1"
  repo1-retention-full: "7"
```

**PGBouncer:**
```yaml
pooler:
  enabled: true
  poolMode: "transaction"  # session | transaction | statement
  maxClientConn: "1000"
  defaultPoolSize: "25"
```

**Site Manager (Active-Standby):**
```yaml
siteManager:
  enabled: true
  httpAuth:
    smNamespace: "site-manager-ns"
    clusterName: "primary-cluster"
```

**External Database (RDS):**
```yaml
externalDataBase:
  type: rds
  project: '"123456789012"'  # Must be string with extra quotes
  host: "database-1.c1abc2def3gh.us-east-1.rds.amazonaws.com"
  port: 5432
```

## Log Analysis Tips

### Operator Logs

**Key patterns to grep:**

```bash
# Reconciliation errors
kubectl logs deployment/postgres-operator | grep -i "error\|failed\|unable"

# CRD updates
kubectl logs deployment/postgres-operator | grep "PatroniCore\|PatroniServices"

# Leader election (operator-level)
kubectl logs deployment/postgres-operator | grep "leader"
```

### Patroni Logs

**Key patterns:**

```bash
# DCS connectivity
kubectl logs <patroni-pod> -c postgres | grep -iE "dcs|etcd|consul"

# Leader election (PostgreSQL cluster-level)
kubectl logs <patroni-pod> -c postgres | grep -i "leader\|election\|failover"

# Replication lag
kubectl logs <patroni-pod> -c postgres | grep -i "lag\|replication"

# Startup issues
kubectl logs <patroni-pod> -c postgres | grep -i "starting\|shutdown\|crash"
```

### pgBackRest Logs

**Key patterns:**

```bash
# Backup/restore operations
kubectl logs <patroni-pod> -c pgbackrest-sidecar | grep -iE "backup|restore|stanza"

# Storage errors
kubectl logs <patroni-pod> -c pgbackrest-sidecar | grep -iE "s3|gcs|storage|permission"

# Retention/expiration
kubectl logs <patroni-pod> -c pgbackrest-sidecar | grep -i "expire\|retention"
```

## Integration Points

### DCS (etcd/Consul)

**Health Check:**
```bash
# etcd
kubectl exec -n <etcd-namespace> <etcd-pod> -- etcdctl endpoint health

# Consul
kubectl exec -n <consul-namespace> <consul-pod> -- consul members
```

**Patroni Cluster State (stored in DCS):**
```bash
# etcd
kubectl exec -n <etcd-namespace> <etcd-pod> -- \
  etcdctl get --prefix /service/<cluster-name>/

# Consul
kubectl exec -n <consul-namespace> <consul-pod> -- \
  consul kv get -recurse service/<cluster-name>/
```

### Vault Secret Storage

**Verify Secret Path:**
```bash
kubectl exec -n vault-operator <vault-pod> -- \
  vault kv get secret/postgres/<cluster-name>
```

**Check Kubernetes Auth Method:**
```bash
kubectl exec -n vault-operator <vault-pod> -- \
  vault read auth/kubernetes/config
```

### Monitoring Exporters

**postgres-exporter:**
- Exposes PostgreSQL metrics on port 9187
- Scrapes `pg_stat_*` tables

**pgbackrest-exporter:**
- Exposes pgBackRest backup metrics
- Monitors backup age, size, status

**query-exporter:**
- Runs custom SQL queries for Prometheus metrics
- Circuit-breaker prevents query storms

**Health check:**
```bash
kubectl port-forward -n <namespace> <patroni-pod> 9187:9187
curl http://localhost:9187/metrics
```

## Prevention Best Practices

1. **Always verify `shared_preload_libraries` before major upgrades**
2. **Use `kubectl apply --dry-run=server` before deploying CRD changes**
3. **Test backup/restore in non-production before RDS migrations**
4. **Monitor DCS health — Patroni depends on it for HA**
5. **Set appropriate PVC storage class and size (avoid resize issues)**
6. **Use separate namespaces for multi-tenant deployments**
7. **Enable TLS for PostgreSQL and Patroni REST API in production**
8. **Regularly test failover procedures and Site Manager switchovers**
9. **Set realistic connection limits in PGBouncer (leave headroom)**
10. **Monitor prepared transactions — prevent frozen transaction buildup**

## Related Documentation

- [Installation Guide](docs/public/installation.md) — Prerequisites, Helm parameters, platform-specific setup
- [Quickstart](docs/public/quickstart.md) — Fast-path deployment
- [Active-Standby Cluster](docs/public/features/active-standby-cluster.md) — DR configuration
- [pgBackRest](docs/public/features/pgBackRest.md) — Backup/restore, PITR, retention
- [Connection Pooler](docs/public/features/connection-pooler.md) — PGBouncer limitations and notes
- [Major Upgrade](docs/public/features/major-upgrade.md) — pg_upgrade procedures
- [Disaster Recovery](docs/public/features/disaster-recovery.md) — Site Manager API and switchover
- [TLS Configuration](docs/public/features/tls-configuration.md) — Certificate setup

## When to Escalate

Escalate to component maintainers when:

- Operator crashes with Go panic (attach stack trace)
- Data corruption suspected (checksum failures, corrupt pages)
- Patroni in split-brain (multiple leaders simultaneously)
- DCS (etcd/Consul) data loss (requires cluster rebuild)
- pgBackRest restore fails repeatedly with cryptic errors
- Upgrade rollback unsuccessful (database in inconsistent state)
- Performance degradation unexplained by query patterns or load

Include in escalation:
- Full operator logs (`kubectl logs deployment/postgres-operator`)
- Patroni logs from all pods in cluster
- CRD YAML (`kubectl get patronicore,patroniservices -o yaml`)
- DCS state dump (etcdctl/consul kv export)
- pgBackRest stanza info (`pgbackrest --stanza=<name> info`)
