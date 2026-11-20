---
name: troubleshoot-postgres
description: Diagnose and resolve failures in pgskipper-operator PostgreSQL clusters on Kubernetes and OpenShift. Covers Patroni high availability and leader election, PatroniCore and PatroniServices custom resources, Helm install and upgrade errors, major upgrades and collation fixes, pgBackRest and backup daemon backups, the PgBouncer connection pooler, Site Manager active-standby DR, DBaaS adapter, frozen prepared transactions, application-side PostgreSQL errors, and the Prometheus alerts shipped with the patroni-services chart. Use for pgskipper-operator, Patroni, or PostgreSQL-as-a-Service incidents.
---

# PostgreSQL operator troubleshooting

Use this skill to diagnose pgskipper-operator clusters. This file holds the cluster layout, safety rules, triage steps,
and a symptom index. Detailed procedures live in three reference files. Read only the section you need.

| File | Contents |
|------|----------|
| [references/alarm-troubleshooting.md](references/alarm-troubleshooting.md) | Prometheus alerts: meaning, causes, investigation, actions, and thresholds |
| [references/troubleshooting.md](references/troubleshooting.md) | Error messages: Helm, operators, Site Manager, Patroni, scheduling, replication, monitoring, DR scenarios, upgrades, backups, PgBouncer, DBaaS, applications |
| [references/troubleshooting-pg-cluster.md](references/troubleshooting-pg-cluster.md) | Generic operations: logs, leader detection, pause mode, replica reinitialization, volumes, heavy queries, backup daemon storage, diagnostic SQL |

## Cluster layout

The names below assume the default cluster name `patroni` (Helm value `patroni.clusterName`). If the installation uses
another name, replace `patroni` in every pod, service, and ConfigMap name.

| Object | Name |
|--------|------|
| Core operator (chart `patroni-core`, manages `PatroniCore`) | Deployment `patroni-core-operator`, service account `patroni-sa` |
| Services operator (chart `patroni-services`, manages `PatroniServices`) | Deployment `postgres-operator`, service account `postgres-sa` |
| PostgreSQL nodes | StatefulSets `pg-patroni-node1`, `pg-patroni-node2`, and so on. The Patroni container has the same name as its StatefulSet and is the first container in the pod. |
| pgBackRest | Container `pgbackrest-sidecar` in each Patroni pod, present only when `pgBackRest` is configured |
| Role labels | `pgtype=primary` on the leader, `pgtype=replica` on replicas. The operator migrates the legacy `pgtype=master` label. |
| Services | `pg-patroni` (leader), `pg-patroni-ro` (replicas). With the pooler installed, `pg-patroni` points to PgBouncer and `pg-patroni-direct` points to the leader. |
| Patroni DCS | Kubernetes by default (`patroni.dcs.type: kubernetes`). Cluster state lives in ConfigMaps `patroni-leader` and `patroni-config`. |
| Patroni configuration | `/patroni/pg_node.yml`. `PATRONICTL_CONFIG_FILE` points to it, so `patronictl list` needs no `-c` flag. |
| Data directory | `/var/lib/pgsql/data/postgresql_node<N>` on the volume mounted at `/var/lib/pgsql/data` |
| Patroni REST API | Port `8008`. Credentials are in the env vars `PATRONI_REST_API_USER` and `PATRONI_REST_API_PASSWORD`. |
| Backup daemon | Deployment `postgres-backup-daemon`, storage at `/backup-storage`, HTTP API on port `8080` |
| Connection pooler | Deployment `connection-puller` (spelled this way in the operator code), ConfigMap `pooler-config`, secret `pgbouncer-secret`, port `6432` |
| DBaaS adapter | Deployment `dbaas-postgres-adapter` |

## Safety rules

Ask the user for explicit confirmation before you run any command that changes cluster state, deletes data, or
disconnects clients. This includes:

- Patroni dynamic configuration changes: `patronictl edit-config`, `PATCH /config` on the REST API, and pause or resume.
- Replica reinitialization (`patronictl reinit`, `POST /reinitialize`). It deletes the replica's data directory.
- `kubectl delete` on PVCs, StatefulSets, Services, `PatroniCore`, or `PatroniServices` resources.
- Deleting or editing the `patroni-leader` and `patroni-config` ConfigMaps, or the keys of an etcd DCS.
- Removing files from volumes (`rm -rf` on a node or in a pod), including old backups in the backup daemon storage.
- `ROLLBACK PREPARED`, `pg_cancel_backend`, and `pg_terminate_backend`.
- `ALTER SYSTEM`. The operator and Patroni manage PostgreSQL parameters, so set them through `patroni.postgreSQLParams`.
- `helm upgrade`, `helm rollback`, and scaling StatefulSets or Deployments.

Never do or propose the following. If the user asks, explain why and offer the documented alternative:

- Editing the `pgtype` pod labels by hand. Patroni manages them and overwrites manual changes, and wrong labels route
  writes to a replica.
- Setting `patroni.dockerImage` back to an older image, for example to recover from a failed upgrade. Some pod
  environment variables work only with a specific Patroni version.

Read-only commands, such as `kubectl get`, `kubectl describe`, `kubectl logs`, `patronictl list`, and `SELECT` queries,
need no confirmation.

## Initial triage

1. Get the state of pods and custom resources:

   ```bash
   kubectl get pods -n <namespace> -L pgtype -o wide
   kubectl get patronicore,patroniservices -n <namespace> -o yaml
   kubectl get events -n <namespace> --sort-by=.lastTimestamp
   ```

2. Read the logs of the failing component:

   ```bash
   kubectl logs -n <namespace> deployment/patroni-core-operator
   kubectl logs -n <namespace> deployment/postgres-operator
   kubectl logs -n <namespace> <patroni-pod> -c pg-patroni-node<N>
   kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar
   kubectl logs -n <namespace> deployment/postgres-backup-daemon
   kubectl logs -n <namespace> deployment/connection-puller
   kubectl logs -n <namespace> deployment/dbaas-postgres-adapter
   ```

   Add `--previous` to read the log of a container that restarted.

3. Get the Patroni view of the cluster:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl list
   ```

4. Find the symptom in the index below and read the matching section.

## Symptom index

| Symptom | Section |
|---------|---------|
| A Prometheus alert fires | [alarm-troubleshooting.md](references/alarm-troubleshooting.md), section named after the alert |
| `rendered manifests contain a resource that already exists` | [Helm install fails: resource already exists](references/troubleshooting.md#helm-install-fails-resource-already-exists) |
| `spec.selector: Invalid value ... field is immutable` | [Helm upgrade fails: selector is immutable](references/troubleshooting.md#helm-upgrade-fails-selector-is-immutable) |
| `spec.externalDataBase.project in body must be of type string` (older operator versions) | [External database project is not a string](references/troubleshooting.md#external-database-project-is-not-a-string) |
| `Session token expired, re-authentication.` repeats, Site Manager shows no active status | [Site Manager cannot authorize](references/troubleshooting.md#site-manager-cannot-authorize) |
| `tokenreviews.authentication.k8s.io is forbidden` in `postgres-operator` logs | [Site Manager API calls fail: TokenReview is forbidden](references/troubleshooting.md#site-manager-api-calls-fail-tokenreview-is-forbidden) |
| No member has the `Leader` role in `patronictl list` | [Cluster has no leader](references/troubleshooting.md#cluster-has-no-leader) |
| Patroni pod in `CrashLoopBackOff`, `Error`, or `Pending` | [Patroni pods do not start](references/troubleshooting.md#patroni-pods-do-not-start) |
| `Insufficient cpu` or `Insufficient memory` in pod events | [Pod is Pending: insufficient CPU or memory](references/troubleshooting.md#pod-is-pending-insufficient-cpu-or-memory) |
| `didn't match Pod's node affinity/selector` or `MatchNodeSelector` in pod events | [Pod is Pending: node selector or affinity does not match](references/troubleshooting.md#pod-is-pending-node-selector-or-affinity-does-not-match) |
| `pg_basebackup: could not create directory ... Permission denied` | [Replica bootstrap fails: permission denied](references/troubleshooting.md#replica-bootstrap-fails-permission-denied) |
| `PANIC: could not read from control file` | [Replica fails to start: could not read from control file](references/troubleshooting.md#replica-fails-to-start-could-not-read-from-control-file) |
| `record with incorrect prev-link` or `invalid resource manager ID` | [Replica fails to start: malformed WAL records](references/troubleshooting.md#replica-fails-to-start-malformed-wal-records) |
| `invalid segment number 0 in file "pg_internal.init...` on the leader | [Replica bootstrap fails: malformed pg_internal.init](references/troubleshooting.md#replica-bootstrap-fails-malformed-pg_internalinit) |
| Every member logs `waiting for leader to bootstrap` | [All members wait for the leader to bootstrap](references/troubleshooting.md#all-members-wait-for-the-leader-to-bootstrap) |
| `Error communicating with DCS`, `DCS is not accessible`, or `K8sConnectionFailed` | [Patroni cannot reach the Kubernetes API](references/troubleshooting.md#patroni-cannot-reach-the-kubernetes-api) |
| `requested timeline ... is not a child of this server's history` or `requested WAL segment ... has already been removed` | [Replication is broken](references/troubleshooting.md#replication-is-broken) |
| `number of requested standby connections exceeds max_wal_senders` | [Replica cannot connect: max_wal_senders exceeded](references/troubleshooting.md#replica-cannot-connect-max_wal_senders-exceeded) |
| Disk usage of a Patroni node missing on the dashboard | [Disk metrics are missing](references/troubleshooting.md#disk-metrics-are-missing) |
| DR scenario: `exit_code: 12 not equals zero and failed pods exists` | [DR scenario fails: failed pods exist](references/troubleshooting.md#dr-scenario-fails-failed-pods-exist) |
| DR scenario: `Patroni cluster is not ready!` and `/archive/get` returns `404` | [DR scenario fails: replica gets 404 from the WAL archive](references/troubleshooting.md#dr-scenario-fails-replica-gets-404-from-the-wal-archive) |
| `pg-upgrade` container fails: `Check cluster before upgrade - Failed.`, `shared_preload_libraries is not found in PostgreSQL config`, `Upgrade failed with exit code`, or a log that ends with `making chmod 750 to datadir` | [Major upgrade fails](references/troubleshooting.md#major-upgrade-fails) |
| `The database was created using collation version ...` or `Cannot alter locale version` | [Collation fix fails for some databases](references/troubleshooting.md#collation-fix-fails-for-some-databases) |
| pgBackRest backup or restore fails | [pgBackRest backups fail](references/troubleshooting.md#pgbackrest-backups-fail) |
| Clients cannot connect through PgBouncer | [Clients cannot connect through PgBouncer](references/troubleshooting.md#clients-cannot-connect-through-pgbouncer) |
| DBaaS aggregator: `bulk-drop": Read timed out` | [Frozen prepared transactions block DBaaS operations](references/troubleshooting.md#frozen-prepared-transactions-block-dbaas-operations) |
| `ERROR: prepared transactions are disabled` | [Prepared transactions are disabled](references/troubleshooting.md#prepared-transactions-are-disabled) |
| `This connection has been closed` | [Connection closed unexpectedly](references/troubleshooting.md#connection-closed-unexpectedly) |
| `cannot execute UPDATE in a read-only transaction` | [Write fails in a read-only transaction](references/troubleshooting.md#write-fails-in-a-read-only-transaction) |
| `remaining connection slots are reserved for non-replication superuser connections` | [Not enough connections for clients](references/troubleshooting.md#not-enough-connections-for-clients) |

## Reading a reference file

Do not load a whole reference file. List its headings first:

```bash
grep -n '^##' references/troubleshooting.md
```

Then read from the start line of the matching heading up to the next heading of the same level, with the offset and
limit of your file-reading tool or with `sed -n '<start>,<end>p' <file>`.

## Key Helm values

| Value | Chart | Notes |
|-------|-------|-------|
| `patroni.clusterName` | patroni-core | Default `patroni`. Prefix of pod, service, and ConfigMap names. |
| `patroni.dcs.type`, `patroni.dcs.hosts` | patroni-core | Default `kubernetes`. Set `hosts` (a list) only for `etcd` or `etcd3`. |
| `patroni.postgreSQLParams` | patroni-core | A list of `"name: value"` strings, not a map. The operator derives `PG_MAX_CONNECTIONS` and `PG_CONF_MAX_PREPARED_TRANSACTIONS` from it. |
| `patroni.patroniParams` | patroni-core | Patroni settings in the same list format |
| `patroni.forceCollationVersionUpgrade` | patroni-core | `true` reruns the collation fix |
| `patroni.majorUpgrade.enabled` | patroni-core | Enables the major version upgrade |
| `pgBackRest.repoType`, `repoPath`, `s3.*`, `rwx.*`, `fullRetention`, `diffRetention`, `diffSchedule`, `incrSchedule` | patroni-core, patroni-services | `repoType` is `rwx` or `s3`. Default `repoPath` is `/var/lib/pgbackrest`. |
| `backupDaemon.backupSchedule`, `backupDaemon.evictionPolicy` | patroni-services | A cron string and a policy string such as `7d/delete` |
| `connectionPooler.install`, `connectionPooler.config.databases`, `connectionPooler.config.pgbouncer` | patroni-services | `config.pgbouncer` keys are written to `pgbouncer.ini` unchanged |
| `siteManager.install`, `siteManager.httpAuth.smNamespace` | patroni-services | Namespace where Site Manager runs |
| `externalDataBase.type`, `externalDataBase.project` | patroni-services | `type` is `RDS` or `CLOUDSQL`. Write an all-digit `project` as a YAML string: `"123456789012"`. |

## When to escalate

Stop changing the cluster and escalate to the maintainers when:

- An operator crashes with a Go panic.
- You suspect data corruption, such as checksum failures or corrupted pages.
- Patroni reports more than one leader.
- The DCS state is lost.
- A pgBackRest restore fails repeatedly.
- A major upgrade failed and the database is in an inconsistent state.

Attach to the escalation:

- Logs of both operators and of every Patroni pod.
- Output of `kubectl get patronicore,patroniservices -n <namespace> -o yaml`.
- Output of `patronictl list` and the `patroni-leader` and `patroni-config` ConfigMaps.
- Output of `pgbackrest --stanza=<stanza> info` when backups are involved.

## Related documentation

- [Installation guide](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/installation.md):
  prerequisites and the full list of Helm parameters
- [Active-standby cluster](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/active-standby-cluster.md)
- [Disaster recovery](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/disaster-recovery.md):
  Site Manager API and switchover
- [pgBackRest](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/pgBackRest.md):
  backup, restore, PITR, and retention
- [Connection pooler](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/connection-pooler.md)
- [Major upgrade](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/major-upgrade.md):
  includes manual steps for an interrupted upgrade
- [TLS configuration](https://github.com/Netcracker/pgskipper-operator/blob/main/docs/public/features/tls-configuration.md)
