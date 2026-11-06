# PostgreSQL operator troubleshooting: error messages

Each section starts with the error text or the symptom you observe, followed by the cause and the fix. Prometheus alerts
are in [alarm-troubleshooting.md](alarm-troubleshooting.md). Generic procedures, such as finding the leader or
reinitializing a replica, are in [troubleshooting-pg-cluster.md](troubleshooting-pg-cluster.md).

Names assume the default cluster name `patroni`. Ask the user before you run any command that changes the cluster; the
safety rules are in `SKILL.md`.

## Contents

- [Is it the database?](#is-it-the-database)
- [Deployment and operator issues](#deployment-and-operator-issues)
  - [Helm install fails: resource already exists](#helm-install-fails-resource-already-exists)
  - [Helm upgrade fails: selector is immutable](#helm-upgrade-fails-selector-is-immutable)
  - [External database project is not a string](#external-database-project-is-not-a-string)
- [Site Manager and Vault](#site-manager-and-vault)
  - [Site Manager cannot authorize](#site-manager-cannot-authorize)
  - [Site Manager API calls fail: TokenReview is forbidden](#site-manager-api-calls-fail-tokenreview-is-forbidden)
  - [Vault login fails with 403](#vault-login-fails-with-403)
- [Patroni cluster issues](#patroni-cluster-issues)
  - [Cluster has no leader](#cluster-has-no-leader)
  - [Patroni pods do not start](#patroni-pods-do-not-start)
- [Upgrade and migration issues](#upgrade-and-migration-issues)
  - [Major upgrade fails](#major-upgrade-fails)
  - [Collation fix fails for some databases](#collation-fix-fails-for-some-databases)
- [Backup issues](#backup-issues)
  - [pgBackRest backups fail](#pgbackrest-backups-fail)
- [Connection pooler issues](#connection-pooler-issues)
  - [Clients cannot connect through PgBouncer](#clients-cannot-connect-through-pgbouncer)
- [DBaaS issues](#dbaas-issues)
  - [Frozen prepared transactions block DBaaS operations](#frozen-prepared-transactions-block-dbaas-operations)
- [Application-side errors](#application-side-errors)
  - [Prepared transactions are disabled](#prepared-transactions-are-disabled)
  - [Connection closed unexpectedly](#connection-closed-unexpectedly)
  - [Write fails in a read-only transaction](#write-fails-in-a-read-only-transaction)
  - [Not enough connections for clients](#not-enough-connections-for-clients)

## Is it the database?

Two signals point to a database problem: a Prometheus alert such as `PostgreSQL is Down`, or database errors in
application logs.

- An alert fires, but applications work: the alert may be a false positive. Check the cluster anyway.
- Applications fail, but monitoring shows a healthy cluster: start with
  [Application-side errors](#application-side-errors).
- An alert fires and applications fail: the database is the source. Start with `patronictl list` and the Patroni pod
  logs.

## Deployment and operator issues

### Helm install fails: resource already exists

**Symptom**

```text
Error: rendered manifests contain a resource that already exists. Unable to continue with install: existing resource conflict: kind: Role, namespace: <namespace>, name: postgres-operator
```

**Cause:** The namespace still holds objects from an earlier installation that no Helm release owns.

**Fix**

1. List the leftovers and the releases in the namespace:

   ```bash
   kubectl get role,rolebinding,serviceaccount -n <namespace>
   helm list -n <namespace>
   ```

2. With the user's confirmation, delete only the objects named in the error. The patroni-services chart creates Role and
   RoleBinding `postgres-operator` and service account `postgres-sa`. The patroni-core chart creates Role and
   RoleBinding `patroni-core-operator` and service account `patroni-sa`.

   ```bash
   kubectl delete role postgres-operator -n <namespace>
   kubectl delete rolebinding postgres-operator -n <namespace>
   kubectl delete serviceaccount postgres-sa -n <namespace>
   ```

3. Run the installation again.

### Helm upgrade fails: selector is immutable

**Symptom**

```text
Error: UPGRADE FAILED: cannot patch "postgres-operator" with kind Deployment: Deployment.apps "postgres-operator" is invalid: spec.selector: Invalid value: v1.LabelSelector{MatchLabels:map[string]string{"name":"postgres-operator", "operator":"pod"}, MatchExpressions:[]v1.LabelSelectorRequirement(nil)}: field is immutable
```

**Cause:** The chart copies `operator.podLabels` into the selector of the operator Deployment, or `podLabels` when
`operator.podLabels` is empty. Changing these values between releases changes `spec.selector`, and Kubernetes does not
allow changing the selector of an existing Deployment. In the example above, `operator: pod` is such a label. The
patroni-core chart builds the selector of `patroni-core-operator` the same way.

**Investigation:** Compare the selector of the running Deployment with the values of the release:

```bash
kubectl get deployment postgres-operator -n <namespace> -o jsonpath='{.spec.selector.matchLabels}'
helm get values patroni-services -n <namespace>
```

**Fix:** Pick one option with the user:

- Return `operator.podLabels` or `podLabels` to the values the Deployment was created with, and run `helm upgrade`
  again.
- Keep the new labels: delete the operator Deployment named in the error and run `helm upgrade` again. Helm creates the
  Deployment with the new selector.

  ```bash
  kubectl delete deployment postgres-operator -n <namespace>
  ```

### External database project is not a string

**Applies to:** older operator versions that used the `PostgresService` CRD. The current CRD is `PatroniServices`, and
the patroni-services chart renders the value in quotes (`project: "{{ .Values.externalDataBase.project }}"`), so the
chart from this repository does not produce this error.

**Symptom**

```text
The PostgresService "postgres-service" is invalid: spec.externalDataBase.project: Invalid value: "integer": spec.externalDataBase.project in body must be of type string: "integer"
```

**Cause:** The CRD expects `externalDataBase.project` to be a string, but the old deployment rendered an all-digit
project ID without quotes, so it reached the custom resource as a number.

**Fix:** Upgrade the operator and the patroni-services chart. If you cannot upgrade, wrap the value twice so that it
stays a string after rendering: `project: '"123456789012"'`.

With the current chart, write the ID as a plain YAML string, so that Helm does not convert it to a float such as
`1.23456789012e+11`. Do not wrap it twice: the chart would render `project: ""123456789012""`, which is invalid YAML.

```yaml
externalDataBase:
  type: RDS
  project: "123456789012"
```

## Site Manager and Vault

### Site Manager cannot authorize

**Symptom:** Site Manager shows no active status for PostgreSQL, and the `postgres-operator` log repeats:

```json
{"level":"info","timestamp":"2024-07-09T07:42:22.482Z","msg":"Session token expired, re-authentication."}
{"level":"error","timestamp":"2024-07-09T07:42:22.482Z","msg":"User <user> is unauthorized. Check site-manager configuration"}
```

**Cause:** The operator validates Site Manager requests against the Site Manager service account in the namespace set by
`siteManager.httpAuth.smNamespace` (env var `SM_NAMESPACE` of `postgres-operator`). Site Manager runs in another
namespace, or the value is wrong.

**Fix**

1. Find the namespace where Site Manager runs:

   ```bash
   kubectl get pods -A | grep site-manager
   ```

2. Compare it with the value the operator uses:

   ```bash
   kubectl get deployment postgres-operator -n <namespace> \
     -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SM_NAMESPACE")].value}'
   ```

3. Set the correct namespace in the patroni-services values. If Site Manager uses a non-default service account, also
   set `siteManager.httpAuth.smServiceAccountName`.

   ```yaml
   siteManager:
     install: true
     httpAuth:
       enabled: true
       smNamespace: <site-manager-namespace>
   ```

4. With the user's confirmation, upgrade the patroni-services release. The parameter does not exist in the patroni-core
   chart.

   ```bash
   helm upgrade patroni-services <patroni-services-chart> -n <namespace> -f <values-file>
   ```

### Site Manager API calls fail: TokenReview is forbidden

**Symptom:** Calls to the PostgreSQL Site Manager endpoints fail, and the `postgres-operator` log contains:

```json
{"level":"error","timestamp":"2022-01-31T11:52:25.143Z","msg":"There is an error during TokenReview Request","error":"tokenreviews.authentication.k8s.io is forbidden: User \"system:serviceaccount:<namespace>:postgres-sa\" cannot create resource \"tokenreviews\" in API group \"authentication.k8s.io\" at the cluster scope"}
```

**Cause:** The operator validates Site Manager tokens through the TokenReview API, and its service account lacks the
permission to create TokenReviews.

**Fix:** Bind the built-in `system:auth-delegator` ClusterRole to the operator service account. This needs cluster-admin
rights and the user's confirmation.

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

### Vault login fails with 403

**Symptom:** With Vault enabled (`VAULT_ENABLED=true` in the DBaaS adapter), login to Vault fails:

```text
URL: PUT http://<vault-url>/v1/auth/<auth-method>/login
Code 403. Errors:
* permission denied
```

The Vault log shows that Vault cannot create TokenReviews:

```json
{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"tokenreviews.authentication.k8s.io is forbidden: User \"system:serviceaccount:<vault-namespace>:<vault-service-account>\" cannot create resource \"tokenreviews\" in API group \"authentication.k8s.io\" at the cluster scope","reason":"Forbidden","details":{"group":"authentication.k8s.io","kind":"tokenreviews"},"code":403}
```

**Cause:** The Vault Kubernetes auth method validates the client's service account JWT through the TokenReview API, and
the Vault service account lacks that permission. Vault is misconfigured, not the operator.

**Fix:** Bind `system:auth-delegator` to the Vault service account, as described in the
[Vault Kubernetes auth documentation](https://developer.hashicorp.com/vault/docs/auth/kubernetes). The Vault
administrator owns this change.

## Patroni cluster issues

### Cluster has no leader

**Symptom:** `patronictl list` shows no member with the `Leader` role, no pod has the `pgtype=primary` label, and writes
fail.

**Investigation**

1. Get the member states and check whether maintenance mode is on:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl list
   ```

2. Search the Patroni logs for DCS and leader-lock errors:

   ```bash
   kubectl logs -n <namespace> <patroni-pod> -c pg-patroni-node<N> | grep -iE 'dcs|configmap|leader|lock|forbidden|timeout'
   ```

3. Inspect the leader lock:

   ```bash
   kubectl get configmap patroni-leader -n <namespace> -o jsonpath='{.metadata.annotations}'
   ```

4. Check that the Patroni service account can update its ConfigMaps:

   ```bash
   kubectl get pod <patroni-pod> -n <namespace> -o jsonpath='{.spec.serviceAccountName}'
   kubectl auth can-i update configmaps -n <namespace> --as=system:serviceaccount:<namespace>:<service-account>
   ```

5. Check network policies. Patroni must reach the Kubernetes API, and members must reach each other on ports `8008` and
   `5432`.

   ```bash
   kubectl get networkpolicy -n <namespace>
   ```

**Common causes**

- Patroni cannot reach the Kubernetes API, or RBAC denies access to the ConfigMaps.
- Every member is unhealthy, for example because of a full disk or a PostgreSQL startup error.
- Maintenance mode is on, so Patroni does not promote a replica.
- Replicas lag too far behind to be promoted, or carry the `nofailover` tag.
- The DCS type is `etcd` or `etcd3` (`patroni.dcs.hosts`) and the etcd cluster is unhealthy. Check it with
  `etcdctl endpoint health`.

**Fix:** Remove the cause. Do not edit or delete the `patroni-leader` ConfigMap. If more than one member claims the leader
role, stop and escalate.

### Patroni pods do not start

**Symptom:** A Patroni pod is in `CrashLoopBackOff`, `Error`, or `Pending`.

**Investigation**

```bash
kubectl describe pod <patroni-pod> -n <namespace>
kubectl logs <patroni-pod> -n <namespace> -c pg-patroni-node<N> --previous
kubectl get pvc -n <namespace>
```

**Common causes**

- The PVC is not bound: wrong storage class, no free PV, or a node selector that matches no node.
- Wrong permissions on the data volume. Set `patroni.fsGroup` or fix the ownership on the volume.
- No space left on the data volume.
- The node lacks the CPU or memory that the pod requests.
- The image cannot be pulled: wrong registry or missing pull secret.
- An invalid entry in `patroni.postgreSQLParams`. PostgreSQL refuses to start with an unknown parameter or with a
  library in `shared_preload_libraries` that the image does not contain.

## Upgrade and migration issues

### Major upgrade fails

The operator scales every Patroni StatefulSet to zero, then runs the upgrade in pod `pg-major-upgrade-<timestamp>`,
container `pg-upgrade`, on the leader's volume. Start with the log of that container:

```bash
kubectl get pods -n <namespace> | grep pg-major-upgrade
kubectl logs -n <namespace> <upgrade-pod> -c pg-upgrade
```

The end of the log and the exit code of `pg-upgrade` tell you what failed and what the operator did:

| End of the `pg-upgrade` log | Exit code | Operator action |
|-----------------------------|-----------|-----------------|
| `Check cluster before upgrade - Failed.` followed by the `pg_upgrade --check` output | 13 | Rolls back on its own: logs `Can't upgrade Patroni cluster. Rollback.`, starts Patroni again on the old version, and restores read-only databases |
| `shared_preload_libraries is not found in PostgreSQL config, please check PostgreSQL params, exiting...` | 1 | No rollback. The Patroni StatefulSets stay at zero replicas. |
| `Upgrade failed with exit code <N>` followed by the last 100 lines of the `pg_upgrade` dump logs | 1 | No rollback. The Patroni StatefulSets stay at zero replicas. |

Get the exit code with:

```bash
kubectl get pod <upgrade-pod> -n <namespace> \
  -o jsonpath='{.status.containerStatuses[?(@.name=="pg-upgrade")].state.terminated.exitCode}'
```

#### The pre-upgrade check fails (exit code 13)

`pg_upgrade --check` found an incompatibility between the old and the new version. The check also saves its output to
`/var/lib/pgsql/data/check_result` on the leader's volume. Fix what it reports, then run the upgrade again.

#### `shared_preload_libraries` is missing (exit code 1)

**Cause:** `patroni.postgreSQLParams` has no `shared_preload_libraries` entry, so the `postgresql.conf` of the old
cluster has none either. The script stops before `pg_upgrade` starts, so the data of the old cluster is intact.

Upgrade images built before this check existed print no message: their log ends with
`making chmod 750 to datadir`. The cause and the fix are the same.

**Fix:** The script reads the parameter from the `postgresql.conf` that Patroni writes, so the old cluster must run once
with the parameter before you retry. Ask the user before each step.

1. In the patroni-core values, set `patroni.dockerImage` back to the image of the old PostgreSQL version, and add
   `shared_preload_libraries`. Helm replaces lists instead of merging them, so keep the other entries of
   `postgreSQLParams`. Keep the libraries the cluster already uses; the chart default is shown below.

   ```yaml
   patroni:
     dockerImage: <old-patroni-image>
     postgreSQLParams:
       - "shared_preload_libraries: pg_stat_statements, pg_hint_plan, pg_cron"
       # ...the other entries of the list
   ```

2. Upgrade the patroni-core release. If the Patroni pods do not start, scale the StatefulSets `pg-patroni-node<N>`
   back to one replica.
3. Check the parameter on the leader:

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- psql -c 'SHOW shared_preload_libraries;'
   ```

4. Set the new image in `patroni.dockerImage` again to repeat the upgrade.

#### `pg_upgrade` itself fails (exit code 1)

The operator runs `pg_upgrade` with `--link`, so the old data directory may already be changed and unsafe to start.
That is why the operator does not roll back. Do not start the old version on this data. Stop, collect the `pg-upgrade`
log, and escalate. Recovery follows the manual steps in the
[major upgrade troubleshooting guide](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/major-upgrade.md#troubleshooting)
and can require restoring the data from a backup.

### Collation fix fails for some databases

**Symptom:** Clients see a warning when they connect:

```text
The database was created using collation version 2.31, but the operating system provides version 2.35.
```

The `patroni-core-operator` log contains:

```json
{"level":"warn","timestamp":"2024-09-18T16:11:02.389Z","msg":"Cannot alter locale version for db: <database>","error":"ERROR: syntax error at or near \"REFRESH\" (SQLSTATE 42601)"}
```

**Cause:** Moving from a CentOS-based PostgreSQL image to an Ubuntu-based one changes the glibc collation version. The
operator fixes this with `ALTER DATABASE ... REFRESH COLLATION VERSION`, which exists only in PostgreSQL 15 and later.

**Fix:** Upgrade PostgreSQL to version 15 or later. Then set `patroni.forceCollationVersionUpgrade: true` in the
patroni-core values and upgrade the release to run the fix again.

Older logs can also show `Cannot reindex database for db: <database>` with `syntax error at or near "-"`. Operator
versions before this repository did not quote database names that contain hyphens. Current versions quote them, so
upgrade the operator.

## Backup issues

### pgBackRest backups fail

**Symptom:** Scheduled or manual pgBackRest backups fail, or a restore times out.

**Investigation**

```bash
kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- pgbackrest info
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- ls /etc/pgbackrest
```

`pgbackrest info` without `--stanza` lists every stanza with its backups and WAL archive range. The sidecar image
contains `curl`, so you can test an S3 endpoint from it:

```bash
kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- curl -sS -o /dev/null -w '%{http_code}\n' <s3-endpoint>
```

**Relevant values** (patroni-core and patroni-services charts)

```yaml
pgBackRest:
  repoType: "s3"                 # rwx or s3
  repoPath: "/var/lib/pgbackrest"
  diffSchedule: "0 0/1 * * *"
  incrSchedule: "0 0/1 * * *"
  fullRetention: 5
  diffRetention: 3
  s3:
    bucket: "<bucket>"
    endpoint: "<s3-endpoint>"
    region: "<region>"
    key: "<access-key>"
    secret: "<secret-key>"
    verifySsl: true
```

**Common causes**

- The S3 key or secret is wrong or expired.
- The S3 endpoint is unreachable: a network policy, blocked egress, or a TLS error with `verifySsl: true`.
- For `repoType: rwx`, the backup volume is not bound or is full.
- Retention is too short for the schedule, so the backup a restore needs has already expired.

## Connection pooler issues

### Clients cannot connect through PgBouncer

**Symptom:** Clients cannot connect through the pooler, while direct connections to the leader work.

**Investigation**

1. Check that the pooler is installed (`connectionPooler.install: true`) and running:

   ```bash
   kubectl get deployment connection-puller -n <namespace>
   kubectl logs -n <namespace> deployment/connection-puller
   ```

2. Read the generated configuration:

   ```bash
   kubectl get configmap pooler-config -n <namespace> -o yaml
   ```

3. Test a direct connection to the leader, bypassing the pooler:

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- psql -U postgres -c "SELECT version();"
   ```

With the pooler installed, the operator points service `pg-patroni` at PgBouncer and creates `pg-patroni-direct` for
direct access to the leader.

**Relevant values** (patroni-services chart)

```yaml
connectionPooler:
  install: true
  replicas: 1
  config:
    databases:
      '*': "host=pg-patroni-direct port=5432"
    pgbouncer:
      listen_port: '6432'
      auth_type: 'md5'
      auth_user: 'pgbouncer'
      auth_query: 'SELECT p_user, p_password FROM public.lookup($1)'
      max_client_conn: '<limit>'
      default_pool_size: '<size>'
```

Every key under `config.pgbouncer` goes to the `[pgbouncer]` section of `pgbouncer.ini` unchanged, and keys under
`config.databases` go to `[databases]`. When the configuration changes, the operator scales the pooler down to apply it.

**Common causes**

- `config.databases` points to a wrong host.
- `auth_type` does not match the password hashes. The chart defaults are `auth_type: md5` and
  `password_encryption: md5`.
- The `pgbouncer` user or the `public.lookup` function that `auth_query` calls is missing in the target database.
- The client limit (`max_client_conn`) or the pool size (`default_pool_size`) is exhausted.

## DBaaS issues

### Frozen prepared transactions block DBaaS operations

**Symptom:** A deployment is blocked, and the DBaaS aggregator log contains:

```text
I/O error on POST request for "http://dbaas-postgres-adapter.<namespace>:8080/api/v2/dbaas/adapter/postgresql/resources/bulk-drop": Read timed out
```

**Cause:** Prepared transactions that nobody committed or rolled back hold locks. They block `REASSIGN OWNED BY` and
`DROP DATABASE`, so the adapter call times out.

**Fix**

1. On the leader pod (`pgtype=primary`), list prepared transactions:

   ```sql
   SELECT gid, database, owner, prepared FROM pg_prepared_xacts;
   ```

2. Confirm with the user and the application owner that these transactions can be discarded. A transaction manager may
   still expect to commit them.

3. Connect to the database from the `database` column and roll back each transaction. PostgreSQL finishes a prepared
   transaction only from the database where it was prepared.

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- psql -d <database> -c "ROLLBACK PREPARED '<gid>';"
   ```

4. Check that the list is empty:

   ```sql
   SELECT gid FROM pg_prepared_xacts;
   ```

5. Repeat the drop through the DBaaS API and check that it completes.

## Application-side errors

### Prepared transactions are disabled

**Symptom**

```text
org.postgresql.util.PSQLException: ERROR: prepared transactions are disabled
```

**Cause:** The application uses prepared transactions (two-phase commit), and `max_prepared_transactions` is `0`.

**Fix:** Set the parameter in the patroni-core values and upgrade the release. The operator derives
`PG_CONF_MAX_PREPARED_TRANSACTIONS` from this list, so do not edit the env var in the StatefulSet: the operator
overwrites it. PostgreSQL applies this parameter only after a restart.

```yaml
patroni:
  postgreSQLParams:
    - "max_prepared_transactions: <value>"
```

### Connection closed unexpectedly

**Symptom:** The application log repeats:

```text
org.postgresql.util.PSQLException: This connection has been closed
```

**Cause 1:** The TCP connection between the client and the leader breaks because of network problems.

**Fix:** Check connectivity between the client pod and the leader pod, then between their nodes. See
[Checking connectivity between pods](troubleshooting-pg-cluster.md#checking-connectivity-between-pods). If the network
is fine, restart the client pod.

**Cause 2:** The application uses a connection pool, and the pooled connections became invalid after an outage.

**Fix:** The application must validate pooled connections. For example, WildFly needs
`<validate-on-match>true</validate-on-match>` in the datasource configuration. If the application cannot recover after a
database outage, report it to the application developers. Restarting the client clears the pool as a workaround.

### Write fails in a read-only transaction

**Symptom**

```text
org.postgresql.util.PSQLException: ERROR: cannot execute UPDATE in a read-only transaction
```

**Cause 1:** The client writes to a replica. Either the client uses the `pg-patroni-ro` service, or the `pgtype` labels
do not match the real roles, so `pg-patroni` routes traffic to a replica.

**Investigation:** Compare the roles from Patroni with the pod labels and the service endpoints:

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
kubectl get pods -n <namespace> -L pgtype
kubectl get endpoints pg-patroni -n <namespace>
```

**Fix:** If the labels are wrong, report a bug to the operator maintainers. As a workaround, and only with the user's
confirmation, fix the labels by hand:

```bash
kubectl label pod <former-leader-pod> -n <namespace> pgtype=replica --overwrite
kubectl label pod <actual-leader-pod> -n <namespace> pgtype=primary --overwrite
```

**Cause 2:** An application bug: the application writes inside a transaction it opened as read-only.

### Not enough connections for clients

**Symptom**

```text
FATAL: remaining connection slots are reserved for non-replication superuser connections
```

**Cause:** `max_connections` is too low for the number of clients.

**Investigation**

```bash
psql -c 'SHOW max_connections;'
psql -c 'SELECT datname, count(*) FROM pg_stat_activity GROUP BY datname;'
```

**Fix:** Reduce the number of client connections, for example with the connection pooler, or raise `max_connections`.

A higher `max_connections` affects every user of the cluster. Each connection is a separate server process, and memory
settings such as `work_mem` are sized per connection. More connections leave less memory per operation, so sorts and
hashes spill to temporary files on disk more often.

To raise the limit, set it in the patroni-core values and upgrade the release. The operator derives
`PG_MAX_CONNECTIONS` from this list. PostgreSQL applies this parameter only after a restart.

```yaml
patroni:
  postgreSQLParams:
    - "max_connections: <value>"
```

Check the new value with `psql -c 'SHOW max_connections;'`. If it has not changed after a few minutes, run
`/propagate_settings.sh` in the Patroni container.
