# PostgreSQL cluster operations

Generic procedures for diagnosing a pgskipper-operator cluster: finding pods and the leader, reading logs, checking
connectivity, managing Patroni, and running diagnostic queries. Error-specific procedures are in
[troubleshooting.md](troubleshooting.md), and alert procedures are in [alarm-troubleshooting.md](alarm-troubleshooting.md).

You need namespace administrator rights in Kubernetes or OpenShift. Some procedures also need SSH access to the cluster
nodes. Names assume the default cluster name `patroni`. Ask the user before you run any command that changes the
cluster; the safety rules are in `SKILL.md`.

## Contents

- [Finding pods and StatefulSets](#finding-pods-and-statefulsets)
- [Reading pod logs](#reading-pod-logs)
- [Scaling a StatefulSet or Deployment](#scaling-a-statefulset-or-deployment)
- [Checking connectivity between pods](#checking-connectivity-between-pods)
- [Checking connectivity between nodes](#checking-connectivity-between-nodes)
- [Finding the node a pod runs on](#finding-the-node-a-pod-runs-on)
- [Finding the cluster name](#finding-the-cluster-name)
- [Finding the leader and the replicas](#finding-the-leader-and-the-replicas)
- [Finding the leader recorded in the DCS](#finding-the-leader-recorded-in-the-dcs)
- [Finding the pod that receives traffic](#finding-the-pod-that-receives-traffic)
- [Finding the host path of a volume](#finding-the-host-path-of-a-volume)
- [Cleaning a pod's volume](#cleaning-a-pods-volume)
- [Checking health on the monitoring dashboard](#checking-health-on-the-monitoring-dashboard)
- [Updating with a custom frontend service type](#updating-with-a-custom-frontend-service-type)
- [Pausing and resuming Patroni](#pausing-and-resuming-patroni)
- [Replica pod reinitialization](#replica-pod-reinitialization)
- [Heavy queries detection](#heavy-queries-detection)
- [Backup daemon storage cleanup](#backup-daemon-storage-cleanup)
- [Additional diagnostic commands](#additional-diagnostic-commands)

## Finding pods and StatefulSets

Each PostgreSQL node runs in its own StatefulSet named `pg-patroni-node<N>`, with one pod each.

```bash
kubectl get statefulsets -n <namespace>
kubectl get pods -n <namespace> -L pgtype -o wide
```

In the OpenShift web console, the same objects are under **Workloads > StatefulSets** and **Workloads > Pods**.

## Reading pod logs

The Patroni container has the same name as its StatefulSet, for example `pg-patroni-node1`.

```bash
kubectl logs -n <namespace> <pod-name> -c <container-name>

# Follow the log
kubectl logs -n <namespace> <pod-name> -c <container-name> -f

# Log of the previous container instance, after a restart
kubectl logs -n <namespace> <pod-name> -c <container-name> --previous
```

## Scaling a StatefulSet or Deployment

Scaling a Patroni StatefulSet to zero stops that PostgreSQL node. Ask the user before you scale anything.

```bash
kubectl scale statefulset pg-patroni-node<N> -n <namespace> --replicas=<count>
kubectl scale deployment <deployment-name> -n <namespace> --replicas=<count>
```

## Checking connectivity between pods

1. Get the IP of the first pod:

   ```bash
   kubectl get pod <first-pod> -n <namespace> -o jsonpath='{.status.podIP}'
   ```

2. From the second pod, ping the first one if the image contains `ping`:

   ```bash
   kubectl exec -n <namespace> <second-pod> -- ping -c 10 <first-pod-ip>
   ```

   Any packet loss above `0%` means the network between the pods is unstable. `100%` loss means there is no
   connectivity.

3. To test the PostgreSQL port itself, run `pg_isready` from a Patroni pod:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- pg_isready -h <first-pod-ip> -p 5432
   ```

## Checking connectivity between nodes

This needs SSH access to the nodes. Get the internal IPs with `kubectl get nodes -o wide`, connect to one node, and
ping the other:

```bash
ping -c 10 <other-node-ip>
```

Read the packet loss the same way as for pods.

## Finding the node a pod runs on

```bash
kubectl get pod <pod-name> -n <namespace> -o wide
```

The `NODE` column shows the node.

## Finding the cluster name

The cluster name is `patroni` by default and comes from the Helm value `patroni.clusterName`. To check it on a running
cluster:

```bash
kubectl get patronicore -n <namespace> -o jsonpath='{.items[0].spec.patroni.clusterName}'
kubectl exec -n <namespace> <patroni-pod> -- grep scope /patroni/pg_node.yml
```

## Finding the leader and the replicas

### With Patroni

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl list
```

The `Role` column marks the leader and the replicas. `PATRONICTL_CONFIG_FILE` points to `/patroni/pg_node.yml`, so you
do not need the `-c` flag.

### With psql

```bash
kubectl exec -n <namespace> <patroni-pod> -- psql -tAc 'SELECT pg_is_in_recovery();'
```

`t` means the pod is a replica, and `f` means it is the leader. Any other output is an error.

### With pod labels

```bash
kubectl get pods -n <namespace> -L pgtype
```

The leader has `pgtype=primary`, and replicas have `pgtype=replica`. Older installations used `pgtype=master`; the
operator replaces it.

## Finding the leader recorded in the DCS

With the default Kubernetes DCS, Patroni stores the cluster state in ConfigMaps. The `leader` annotation of
`patroni-leader` holds the name of the leader pod. Normally it matches `patronictl list`.

```bash
kubectl get configmap patroni-leader -n <namespace> -o jsonpath='{.metadata.annotations.leader}'
```

Do not edit or delete this ConfigMap.

## Finding the pod that receives traffic

Only the leader receives read-write traffic through service `pg-patroni`. Replicas serve `pg-patroni-ro`. With the
connection pooler installed, `pg-patroni` points to PgBouncer, and `pg-patroni-direct` points to the leader.

```bash
kubectl get endpoints pg-patroni pg-patroni-ro -n <namespace>
kubectl get service pg-patroni -n <namespace> -o jsonpath='{.spec.selector}'
kubectl get pods -n <namespace> --show-labels
```

## Finding the host path of a volume

1. Find the PVC of the pod:

   ```bash
   kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}'
   ```

2. Find the PV bound to the PVC. The `VOLUME` column holds the PV name.

   ```bash
   kubectl get pvc <pvc-name> -n <namespace>
   ```

3. Find the path on the node:

   ```bash
   kubectl get pv <pv-name> -o jsonpath='{.spec.hostPath.path}{.spec.local.path}'
   ```

   Empty output means the PV is neither `hostPath` nor `local`. Find the data location in the documentation of its
   storage provider.

## Cleaning a pod's volume

This deletes all data of the node. Ask the user first, and make sure a recent backup exists. To rebuild a replica,
prefer [Replica pod reinitialization](#replica-pod-reinitialization): it achieves the same result without node access.

1. Find the host path. See [Finding the host path of a volume](#finding-the-host-path-of-a-volume).
2. Find the node of the pod. See [Finding the node a pod runs on](#finding-the-node-a-pod-runs-on).
3. Stop the pod:

   ```bash
   kubectl scale statefulset pg-patroni-node<N> -n <namespace> --replicas=0
   ```

4. Connect to the node over SSH and remove the data:

   ```bash
   sudo rm -rf <volume-path>/*
   ```

5. Start the pod again:

   ```bash
   kubectl scale statefulset pg-patroni-node<N> -n <namespace> --replicas=1
   ```

## Checking health on the monitoring dashboard

Open the PostgreSQL Grafana dashboard that the patroni-services chart ships (`monitoring/grafana-dashboard.json`), and
select the cluster and namespace. The cluster is healthy when:

- **PostgreSQL Cluster Status** is up.
- **Running Nodes** equals the number of Patroni nodes.
- **Smoke Test** and **Master Smoke Test** pass.

## Updating with a custom frontend service type

The charts support only the `ClusterIP` type for service `pg-patroni`. If you changed the type, keep your version
during an update:

1. Export the custom service:

   ```bash
   kubectl get service pg-patroni -n <namespace> -o yaml > pg-patroni.custom.yaml
   ```

2. With the user's confirmation, delete it. Clients lose connectivity until step 5.

   ```bash
   kubectl delete service pg-patroni -n <namespace>
   ```

3. Run the standard update procedure.
4. Delete the service that the update created:

   ```bash
   kubectl delete service pg-patroni -n <namespace>
   ```

5. Restore the custom service. Remove `metadata.resourceVersion`, `metadata.uid`, and `spec.clusterIP` from the file
   first if `kubectl apply` rejects them.

   ```bash
   kubectl apply -f pg-patroni.custom.yaml
   ```

## Pausing and resuming Patroni

In maintenance mode (pause), Patroni stops managing PostgreSQL: it does not fail over and does not restart PostgreSQL.
Pausing and resuming change the cluster behavior, so ask the user first. Run the commands in any Patroni pod.

### Pause

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl pause
```

### Resume

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl resume
```

### Through the REST API

The Patroni container has the REST API credentials in the env vars `PATRONI_REST_API_USER` and
`PATRONI_REST_API_PASSWORD`. Use `-u` only if REST API authentication is on.

```bash
curl -i -X PATCH -u "${PATRONI_REST_API_USER}:${PATRONI_REST_API_PASSWORD}" \
  "http://$(hostname -i):8008/config" -d '{"pause": true}'
```

Send `{"pause": false}` to resume.

## Replica pod reinitialization

Reinitialization deletes all data on a replica and copies it from the leader again. Ask the user first. Never run it
on the leader.

1. Get the member names:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl list
   ```

2. Reinitialize the replica. `--force` skips the interactive prompt, which `kubectl exec` cannot answer.

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl reinit patroni <member-name> --force
   ```

   The REST API alternative runs in the replica pod itself:

   ```bash
   curl -i -X POST -u "${PATRONI_REST_API_USER}:${PATRONI_REST_API_PASSWORD}" \
     "http://$(hostname -i):8008/reinitialize"
   ```

3. Watch the replica until its state is `streaming` or `running` again:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl list
   ```

If reinitialization does not start or stops halfway, pause Patroni, reinitialize the replica, and resume Patroni
afterward. See [Pausing and resuming Patroni](#pausing-and-resuming-patroni).

## Heavy queries detection

Run these queries on the pod where the alert fired, connected as a superuser.

### Long-running queries

```sql
SELECT
  pid,
  now() - query_start AS duration,
  usename,
  datname,
  state,
  wait_event_type,
  wait_event,
  left(query, 200) AS query
FROM pg_stat_activity
WHERE state <> 'idle'
  AND pid <> pg_backend_pid()
ORDER BY duration DESC
LIMIT 20;
```

### Most expensive statements

`pg_stat_statements` is in the default `shared_preload_libraries`. If the extension exists in the database, list the
statements with the highest total time:

```sql
SELECT
  calls,
  round(total_exec_time) AS total_ms,
  round(mean_exec_time) AS mean_ms,
  left(query, 200) AS query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 20;
```

### Blocking queries

```sql
SELECT
  blocked.pid AS blocked_pid,
  blocked.usename AS blocked_user,
  blocking.pid AS blocking_pid,
  blocking.usename AS blocking_user,
  left(blocked.query, 200) AS blocked_query,
  left(blocking.query, 200) AS blocking_query
FROM pg_stat_activity AS blocked
JOIN LATERAL unnest(pg_blocking_pids(blocked.pid)) AS b(pid) ON true
JOIN pg_stat_activity AS blocking ON blocking.pid = b.pid;
```

### Stopping a query

Ask the user before you stop a query.

```sql
-- Cancel the current query; the connection stays open
SELECT pg_cancel_backend(<pid>);

-- Close the connection
SELECT pg_terminate_backend(<pid>);
```

### Logging slow queries

Set the threshold through the patroni-core values, not with `ALTER SYSTEM`:

```yaml
patroni:
  postgreSQLParams:
    - "log_min_duration_statement: 1000"   # milliseconds
```

Avoid `log_statement: all` in production: it logs every statement and can flood the log volume.

## Backup daemon storage cleanup

Use this procedure when the backup daemon storage is full or old backups are not evicted.

1. Check the storage usage:

   ```bash
   kubectl exec -n <namespace> deployment/postgres-backup-daemon -- df -h /backup-storage
   ```

2. Check the eviction settings. `backupDaemon.evictionPolicy` (default `7d/delete`) applies to full backups,
   `evictionBinaryPolicy` to granular backups, and `archiveEvictionPolicy` to WAL archives.

   ```bash
   kubectl get patroniservices -n <namespace> -o yaml | grep -i eviction
   ```

3. List the backups through the backup daemon API. If authentication is on, use the `postgres` user credentials.

   ```bash
   kubectl port-forward -n <namespace> deployment/postgres-backup-daemon 8080:8080
   curl -u postgres:<password> http://localhost:8080/backups/list
   ```

4. With the user's confirmation, evict a backup that is no longer needed. Use the API rather than deleting files, so the
   daemon keeps its index consistent.

   ```bash
   curl -u postgres:<password> -X DELETE "http://localhost:8080/evict?id=<backup-id>"
   ```

5. To keep fewer backups, change the policy or the schedule in the patroni-services values and upgrade the release:

   ```yaml
   backupDaemon:
     backupSchedule: "0 0/7 * * *"   # cron
     evictionPolicy: "7d/delete"
   ```

   If the storage is too small for the policy, increase `backupDaemon.storage.size`.

## Additional diagnostic commands

### PostgreSQL version and settings

```bash
kubectl exec -n <namespace> <patroni-pod> -- psql -c 'SELECT version();'
kubectl exec -n <namespace> <patroni-pod> -- psql -c 'SHOW all;'
```

### Table sizes

```sql
SELECT
  schemaname,
  tablename,
  pg_size_pretty(pg_total_relation_size(format('%I.%I', schemaname, tablename))) AS size
FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY pg_total_relation_size(format('%I.%I', schemaname, tablename)) DESC
LIMIT 20;
```

### Database sizes

```sql
SELECT
  datname,
  pg_size_pretty(pg_database_size(datname)) AS size
FROM pg_database
ORDER BY pg_database_size(datname) DESC;
```

### Replication slots and retained WAL

Run on the leader. An inactive slot with a large `retained_wal` value keeps WAL files on disk.

```sql
SELECT
  slot_name,
  slot_type,
  database,
  active,
  pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained_wal
FROM pg_replication_slots;
```

### WAL directory size

```bash
kubectl exec -n <namespace> <patroni-pod> -- du -sh /var/lib/pgsql/data/postgresql_node<N>/pg_wal
kubectl exec -n <namespace> <patroni-pod> -- sh -c 'ls -lt /var/lib/pgsql/data/postgresql_node<N>/pg_wal | head -20'
```
