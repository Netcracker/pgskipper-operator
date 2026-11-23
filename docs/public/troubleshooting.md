# PostgreSQL Operator Troubleshooting Guide

This chapter describes the troubleshooting scenarios and recommendations for deployment, upgrade, and operator issues of pgskipper-operator (PostgreSQL as a Service operator managing Patroni-based PostgreSQL clusters on Kubernetes and OpenShift).

Other troubleshooting guides:

* [Prometheus Alerts Troubleshooting](alarm-troubleshooting.md) - descriptions and resolution steps for the monitoring alerts.
* [PostgreSQL Cluster Troubleshooting](troubleshooting-pg-cluster.md) - troubleshooting overview, application-side errors, infrastructure, HA, replication, backup, connection pooler, and DR issues, and generic operations.

The following topics are covered in this section:

* [Deployment fails with rendered manifests contain a resource that already exists error](#deployment-fails-with-rendered-manifests-contain-a-resource-that-already-exists-error)
* [Not possible to call PostgreSQL SiteManager endpoints](#not-possible-to-call-postgresql-sitemanager-endpoints)
* [Postgres-Service upgrade failed](#postgres-service-upgrade-failed)
* [Deployment fails with The PostgresService is invalid: spec.externalDataBase.project Invalid value integer](#deployment-fails-with-the-postgresservice-is-invalid-specexternaldatabaseproject-invalid-value-integer)
* [Postgres site manager can not authorize, so there is no active status in site-manager](#postgres-site-manager-can-not-authorize-so-there-is-no-active-status-in-site-manager)
* [Postgres collation fix failed for several databases](#postgres-collation-fix-failed-for-several-databases)
* [Postgres major upgrade failed](#postgres-major-upgrade-failed) (including [Postgres upgrade failed without an error in logs](#postgres-upgrade-failed-without-an-error-in-logs))
* [Deployment fails due to frozen prepared transactions in PostgreSQL](#deployment-fails-due-to-frozen-prepared-transactions-in-postgresql)

For the application-side errors, Patroni cluster, pgBackRest, and PgBouncer issues that were previously described in this guide, see [PostgreSQL Cluster Troubleshooting](troubleshooting-pg-cluster.md).

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

<a id="postgres-upgrade-failed-without-an-error-in-logs"></a>**`shared_preload_libraries` is missing (exit code 1), or the upgrade failed without an error in logs:** `patroni.postgreSQLParams` has no `shared_preload_libraries` entry, so the `postgresql.conf` that Patroni writes for the old cluster has none either. The current operator checks this on the leader before it stops the cluster: the `patroni-core-operator` log contains `shared_preload_libraries is not found in PostgreSQL config, please check PostgreSQL params, failing major upgrade`, and the cluster keeps running on the old version. Older operator versions stop the cluster first, and the `pg-upgrade` container fails with exit code 1. In both cases the script stops before `pg_upgrade` starts, so the data of the old cluster is intact. Upgrade images built before this check existed print no message, and their log ends with `making chmod 750 to datadir`.

Do not set `patroni.dockerImage` back to the old image: some pod environment variables work only with a specific Patroni version. The operator keeps the old image in the Patroni StatefulSets until the upgrade succeeds, so the old cluster starts without changing the image. Add the parameter to the running cluster, then repeat the upgrade:

1. Scale the `patroni-core-operator` Deployment to zero, so that it does not start the upgrade again before the parameter is in place:

   ```bash
   kubectl scale deployment patroni-core-operator -n <namespace> --replicas=0
   ```

2. If the Patroni StatefulSets are at zero replicas, check that they still use the old image, scale each of them back to one replica, and wait until `patronictl list` shows a leader:

   ```bash
   kubectl get statefulsets -n <namespace> -o custom-columns='NAME:.metadata.name,REPLICAS:.spec.replicas,IMAGE:.spec.template.spec.containers[0].image'
   kubectl scale statefulset pg-<cluster-name>-node<N> -n <namespace> --replicas=1
   ```

3. Set the parameter in the Patroni configuration of the running cluster, and restart PostgreSQL so that it takes effect. Use the libraries that the cluster needs; the chart default is shown below.

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- patronictl edit-config -s 'postgresql.parameters.shared_preload_libraries=pg_stat_statements, pg_hint_plan, pg_cron' -q --force
   kubectl exec -n <namespace> <leader-pod> -- patronictl restart <cluster-name> --force
   ```

4. Check the parameter on the leader the same way as the operator does, and with `SHOW`:

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- sh -c 'grep shared_preload_libraries /var/lib/pgsql/data/postgresql_${POD_IDENTITY}/postgresql.conf'
   kubectl exec -n <namespace> <leader-pod> -- psql -c 'SHOW shared_preload_libraries;'
   ```

5. Add the same value to `patroni.postgreSQLParams` in the patroni-core values, keeping the new `patroni.dockerImage` and `patroni.majorUpgrade.enabled: true`. The parameter list is replaced as a whole, so keep its other entries. Upgrade the patroni-core release. The release upgrade restores the `patroni-core-operator` Deployment, and the operator repeats the major upgrade.

   ```yaml
   patroni:
     postgreSQLParams:
       - "shared_preload_libraries: pg_stat_statements, pg_hint_plan, pg_cron"
       # ...the other entries of the list
   ```

**`pg_upgrade` failed (exit code 1):** The operator runs `pg_upgrade` with `--link`, so the old data directory may already be changed and unsafe to start, and the operator does not roll back. Do not start the old version on this data. Follow the manual steps in [Major Upgrade troubleshooting](features/major-upgrade.md#troubleshooting); recovery can require restoring the data from a backup.

---

## DBaaS Issues

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
