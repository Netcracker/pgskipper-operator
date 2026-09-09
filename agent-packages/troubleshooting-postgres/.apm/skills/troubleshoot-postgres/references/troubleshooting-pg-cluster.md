# PostgreSQL Cluster Operational Troubleshooting Guide

This section provides detailed troubleshooting procedures for HA PG cluster installations. It provides instructions on how to detect and fix generic issues. Ensure you have administrator privileges in the Kubernetes/OpenShift cluster and SSH access to cluster nodes where necessary.

## Table of Contents

- [Introduction](#introduction)
- [Troubleshooting Overview](#troubleshooting-overview)
- [Generic Operations How To](#generic-operations-how-to)
  - [Finding the Deployment or the Pod](#finding-the-deployment-or-the-pod)
  - [Getting Pod's Log](#getting-pods-log)
  - [Scaling the Deployment](#scaling-the-deployment)
  - [Checking Connectivity between Pods](#checking-connectivity-between-pods)
  - [Checking Connectivity between Nodes](#checking-connectivity-between-nodes)
  - [Determining on which Node a Pod Runs](#determining-on-which-node-a-pod-runs)
  - [Determining PG Cluster Name](#determining-pg-cluster-name)
  - [Determining Leader and Replica PG Pods](#determining-leader-and-replica-pg-pods)
  - [Determining which Pod is Considered to be Leader by Patroni](#determining-which-pod-is-considered-to-be-leader-by-patroni)
  - [Determining which Pod Receives Traffic from Postgres Service](#determining-which-pod-receives-traffic-from-postgres-service)
  - [Determining a Host Path where Volume Data is Stored](#determining-a-host-path-where-volume-data-is-stored)
  - [Cleaning Volume with Pod's Data](#cleaning-volume-with-pods-data)
  - [Checking the Database Health via Monitoring Dashboard](#checking-the-database-health-via-monitoring-dashboard)
  - [Making Updates with Custom Frontend Service Type](#making-updates-with-custom-frontend-service-type)
  - [Switch on/off Pause Mode in Patroni](#switch-onoff-pause-mode-in-patroni)
  - [Replica Pod Reinitialization](#replica-pod-reinitialization)
  - [Heavy Queries Detection](#heavy-queries-detection)
  - [Backup Daemon Storage Clean Up](#backup-daemon-storage-clean-up)

---

# Introduction

This section provides detailed troubleshooting procedures for HA PG cluster installations. It provides instructions on how to detect and fix generic issues. Ensure you have administrator privileges in the Kubernetes/OpenShift cluster and SSH access to cluster nodes. The ability to use `sudo` with any command is granted. You should also have at least member access to the cloud tenant where the cluster is deployed.

# Troubleshooting Overview

To resolve a database-related issue is to determine whether the issue is caused by a database fault or a failure of any other system. There are two main ways of determining the database failure:

* The monitoring system reports that the database is down and a corresponding alarm `PostgreSQL is DOWN` is sent to the system administrator.
* The application is not working properly, and the application troubleshooting process has revealed that the application log contains database-related errors.

If the monitoring alarm `PostgreSQL is DOWN` is sent to the system administrator, but the applications work properly, this is probably a bug in the monitoring system (false positive). However, it is recommended to troubleshoot the database to make sure that the database works.

If one or more applications report database failure, check the database health via monitoring dashboard first. If the dashboard reports that the database is healthy, troubleshoot the applications. This may be an application-side error. If this does not help, the problem is probably in the database.

**Note**: If the monitoring alarm `PostgreSQL is DOWN` is sent to the system administrator, and one or more applications report about a database failure, this is **certainly** a database failure.

After you have determined that the source of the problem is the database, check if the database pods are running and ready by finding all of the database pods. Then check the `Status:` field on the corresponding pages is `Active`. If any of the pods is not ready, troubleshoot all of the cloud PG solution pods.

Database health and performance status can be tracked using the Monitoring Dashboard.

PostgreSQL includes built-in instruments for monitoring database activity and analyzing performance. For more information about monitoring tools, refer to the _Official PostgreSQL documentation_ at https://www.postgresql.org/docs/current/monitoring.html

If this does not help, the problem is probably not generic, and requires a detailed investigation. Contact Netcracker support.

---

# Generic Operations How To

This chapter explains how to perform generic actions during the HA PG cluster troubleshooting process. Some of them are common Kubernetes/OpenShift troubleshooting actions, and others are specific for PG cluster diagnosis.

## Finding the Deployment or the Pod

Navigate to the Kubernetes/OpenShift console.

### Finding the Deployment

Navigate to **Applications > Deployments**, and find the row where the value of the first column is the name of the deployment you are looking for. Click the link in the second column. You will be redirected to the deployment's page.

### Finding the Pod

Navigate to **Applications > Pods**, and find the row, where the value of the first column is the name of the pod you are looking for. Click the link in the first column of this row. You will be redirected to the pod's page.

**Note**: The names of the PG pods and deployments start with the `pg-` prefix and have the word `node` in the middle.

## Getting Pod's Log

Navigate to the Kubernetes/OpenShift console, and find the desired pod. Select the `Logs` tab. The pod's log appears on the page.

Using kubectl:

```bash
kubectl logs -n <namespace> <pod-name>

# For specific container in pod
kubectl logs -n <namespace> <pod-name> -c <container-name>

# Follow logs in real-time
kubectl logs -n <namespace> <pod-name> -f

# Get logs from previous pod instance (after restart)
kubectl logs -n <namespace> <pod-name> --previous
```

## Scaling the Deployment

Navigate to the Kubernetes/OpenShift console, and find the desired deployment. There is a colored circle along with a pair of arrows: one up and one down. To scale the deployment up, click the up arrow. To scale the deployment down, click the down arrow. If you are scaling the deployment down to zero, you may be asked for confirmation.

Using kubectl:

```bash
# Scale deployment
kubectl scale deployment <deployment-name> -n <namespace> --replicas=<count>

# Scale statefulset
kubectl scale statefulset <statefulset-name> -n <namespace> --replicas=<count>
```

## Checking Connectivity Between Pods

Navigate to the Kubernetes/OpenShift Console, find one of the desired pods, and go into its **Details** tab. Its IP is shown in the `IP:` field. Then navigate to another pod's **Terminal** tab and run the following command:

```bash
ping $IP
```

where, `$IP` is the IP of the first pod. Wait for 10 seconds, then press `Ctrl-C`. The following output should be produced:

```
sh-4.2# ping 10.1.4.24
PING 10.1.4.24 (10.1.4.24) 56(84) bytes of data.
64 bytes from 10.1.4.24: icmp_seq=1 ttl=64 time=0.193 ms
64 bytes from 10.1.4.24: icmp_seq=2 ttl=64 time=0.214 ms
64 bytes from 10.1.4.24: icmp_seq=3 ttl=64 time=0.239 ms
^C
--- 10.1.4.24 ping statistics ---
10 packets transmitted, 10 received, 0% packet loss, time 9003ms
rtt min/avg/max/mdev = 0.157/0.210/0.239/0.029 ms
```

If the value before the words "packet loss" differs from `0%`, then the network between the pods is unstable; if this value has reached the `100%` level, there is no connectivity between the pods at all.

## Checking Connectivity Between Nodes

**Note**: This action requires access to the Kubernetes/OpenShift nodes.

Assuming that you know the internal IPs of the needed nodes, you may SSH to one node and run the following command:

```bash
ping $IP
```

where, `$IP` is the IP of another node. Wait for 10 seconds, then press `Ctrl-C`.

If the value before the words "packet loss" differs from `0%`, then the network between the nodes is unstable; if this value has reached the `100%` level, there is no connectivity between the nodes at all.

## Determining on which Node a Pod Runs

Navigate to the Kubernetes/OpenShift Console, find the desired pod, and go into its `Details` tab. The node name is shown in the `Node:` field.

Using kubectl:

```bash
kubectl get pod <pod-name> -n <namespace> -o wide
```

The `NODE` column shows which node the pod is running on.

## Determining PG Cluster Name

For PostgreSQL Operator deployment, PG cluster name equals to `patroni` by default.

To verify, check the Patroni configuration:

```bash
kubectl exec -n <namespace> <patroni-pod> -- cat /patroni/pg_node.yml | grep scope
```

## Determining Leader and Replica PG Pods

### Method 1: Using Patroni

Navigate to the Kubernetes/OpenShift Console, find any of one of the PG pods, go into its `Terminal` tab, and run the following command:

```bash
patronictl -c /patroni/pg_node.yml list
```

This will show the cluster state with leader and replica roles clearly marked.

### Method 2: Using psql

In the pod's terminal, run:

```bash
psql
```

The command prompt should appear:

```
psql (12.3)
Type "help" for help.

postgres=#
```

Run the following command:

```sql
SELECT pg_is_in_recovery();
```

If this query returns `t` (true), this pod is a replica; if this query returns `f`, this pod is the leader; any other output is an error.

### Method 3: Using kubectl labels

```bash
kubectl get pods -n <namespace> -L pgtype
```

Look for `pgtype=leader` and `pgtype=replica` labels.

## Determining which Pod is Considered to be Leader by Patroni

Patroni stores metadata about the PG cluster in Kubernetes DCS and is available in the Kubernetes/OpenShift console via configuration map. This includes PG cluster state information, such as which PG instance is currently the leader. Normally, metadata in DCS matches the actual PG cluster state.

Navigate to the Kubernetes/OpenShift Console and PG project, then navigate to **Resources > Config Maps > patroni-leader** and click the `Show Annotations` link. The annotation `leader` contains the name of the leader pod.

Using kubectl:

```bash
kubectl get configmap patroni-leader -n <namespace> -o jsonpath='{.metadata.annotations.leader}'
```

## Determining which Pod Receives Traffic from Postgres Service

HA PG installation has a few pods, but only one (leader) receives traffic at a time.

Check via service endpoints:

```bash
kubectl get endpoints postgres-service -n <namespace>
```

Or check pod labels and service selector:

```bash
kubectl get svc postgres-service -n <namespace> -o yaml | grep selector -A 5
kubectl get pods -n <namespace> --show-labels
```

## Determining a Host Path where Volume Data is Stored

### Find a PVC Name

First, find the name of the PVC that is used for the desired volume mount point.

Navigate to the Kubernetes/OpenShift Console, find the desired pod and go to the `Details` tab. At the right side of the page, find a line that begins with `Mount:` and contains the desired volume mount point at the right side of an arrow.

For instance, if you look for the name of the PVC used for mount point `/var/lib/pgsql/data`, you have to find a line like this:

```
Mount: pg-patroni-node2-data-mp → /var/lib/pgsql/data
```

The mount name is `pg-patroni-node2-data-mp`.

The name of the PVC associated with this mount may be found below, under the `Volumes` header in the `Claim name:` row:

```
Claim name: pg-patroni-node2-claim
```

Using kubectl:

```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}'
```

### Find a PV name

Once you have found PVC, run the following command:

```bash
kubectl get pvc <pvc-name> -n <namespace>
```

Remember the value from the `VOLUME` column - this is the name of the PV that is bound to the PVC.

Example:

```bash
$ kubectl get pvc pg-patroni-node2-claim -n postgres
NAME                     STATUS   VOLUME                              CAPACITY   ACCESS MODES   AGE
pg-patroni-node2-claim   Bound    pv-postgresql-test2-mano-infra-1   2Gi        RWO            2d
```

Here, `pv-postgresql-test2-mano-infra-1` is the name of PV.

### Find a Host Path where Volume Data is Stored

Run the following command:

```bash
kubectl get pv <pv-name> -o jsonpath='{.spec.hostPath.path}'
```

Example:

```bash
$ kubectl get pv pv-postgresql-test2-mano-infra-1 -o jsonpath='{.spec.hostPath.path}'
/var/lib/origin/openshift.local.volumes/pv-postgresql-test2-mano-infra-1
```

If this command returned `null` or empty, the PV is not of the `hostPath` type.

## Cleaning Volume with Pod's Data

**Warning**:
* This action requires access to Kubernetes/OpenShift nodes.
* You may lose important data. Do not perform steps from this section until you understand them.
* Always create a backup before cleaning volume data.

### Find a Host Path where Volume Data is Stored

Use the instructions under [Determining a Host Path where Volume Data is Stored](#determining-a-host-path-where-volume-data-is-stored) to find a host path.

### Remove Volume Data

Find the node on which a pod runs, then SSH to the node. Stop the pod (scale its deployment down to zero). Then, in your SSH session, run the following command:

```bash
sudo rm -rf $VOLUME_PATH/*
```

where, `$VOLUME_PATH` is the path you found in the previous step. This will clean all the data at the volume used by the pod. The expected output of this command is empty. After the data is removed, scale the deployment up to 1.

## Checking the Database Health via Monitoring Dashboard

Navigate to the monitoring dashboard, and go to the **PostgreSQL cluster** dashboard. Select the name of your cluster from the `Cluster` drop-down list box. Select the name of your project where the PG cluster is deployed from the `Project` drop-down list box.

The database may be considered healthy if all of the following conditions are met:

* The `Cluster Status` value is `UP`.
* The `Running Nodes` equals the number of the nodes in the PG cluster.
* The `Smoke Tests` value is `100%`.

## Making Updates with Custom Frontend Service Type

Only the **ClusterIP** service type is supported. If you use a different type of service and want to update with standard tools, then you can do this as follows:

1. Export your custom service:
```bash
kubectl get svc pg-patroni -n <namespace> -o yaml > pg-patroni.my.yml
```

2. Delete custom service:
```bash
kubectl delete svc pg-patroni -n <namespace>
```

3. Follow the standard update procedure

4. Delete new created service:
```bash
kubectl delete svc pg-patroni -n <namespace>
```

5. Import your custom service:
```bash
kubectl apply -f pg-patroni.my.yml
```

## Switch on/off Pause Mode in Patroni

Pause mode allows temporarily stepping down Patroni from managing the PG cluster. To turn on/off Patroni pause mode, navigate to any PG pod's terminal.

### Pause Patroni (stop automatic failover)

**REST API:**
```bash
curl -i -XPATCH -u ${NC_PATRONI_REST_API_USER}:${NC_PATRONI_REST_API_PASSWORD} \
  http://$(hostname -i):8008/config -d '{"pause": true}'
```

**patronictl:**
```bash
patronictl -c /patroni/pg_node.yml pause
```

### Resume Patroni (enable automatic failover)

**REST API:**
```bash
curl -i -XPATCH -u ${NC_PATRONI_REST_API_USER}:${NC_PATRONI_REST_API_PASSWORD} \
  http://$(hostname -i):8008/config -d '{"pause": false}'
```

**patronictl:**
```bash
patronictl -c /patroni/pg_node.yml resume
```

**Note**: `${NC_PATRONI_REST_API_USER}` and `${NC_PATRONI_REST_API_PASSWORD}` are required only if authentication is enabled.

## Replica Pod Reinitialization

Replica reinitialization is performed to remove all data from the replica node and make full data synchronization with the leader node from scratch.

To perform replica reinitialization, navigate to the desired replica pod terminal, and run one of the following commands:

### REST API Method

```bash
curl -i -XPOST -u ${NC_PATRONI_REST_API_USER}:${NC_PATRONI_REST_API_PASSWORD} \
  http://$(hostname -i):8008/reinitialize
```

**Note**: `${NC_PATRONI_REST_API_USER}` and `${NC_PATRONI_REST_API_PASSWORD}` are required only if authentication enabled.

### Patronictl Method

```bash
patronictl -c /patroni/pg_node.yml reinit patroni <member-name>
```

To get the member name, run:
```bash
patronictl -c /patroni/pg_node.yml list
```

### When Reinitialization Doesn't Work

For some cases, reinitialization does not work. You need to put Patroni on pause before starting replica reinitialization, and turn off pause mode after reinitialization.

**Steps:**

1. Pause Patroni:
```bash
patronictl -c /patroni/pg_node.yml pause
```

2. Reinitialize replica:
```bash
patronictl -c /patroni/pg_node.yml reinit patroni <member-name>
```

3. Resume Patroni:
```bash
patronictl -c /patroni/pg_node.yml resume
```

## Heavy Queries Detection

PostgreSQL includes built-in views for monitoring active queries. To detect heavy (long-running) queries:

### Check Long-Running Queries

```sql
SELECT 
  pid,
  now() - pg_stat_activity.query_start AS duration,
  query,
  state,
  wait_event_type,
  wait_event
FROM pg_stat_activity
WHERE (now() - pg_stat_activity.query_start) > interval '5 minutes'
  AND state = 'active'
ORDER BY duration DESC;
```

### Check Queries by Duration

```sql
SELECT 
  pid,
  usename,
  datname,
  state,
  query_start,
  now() - query_start AS duration,
  substring(query, 1, 100) AS query_snippet
FROM pg_stat_activity
WHERE state != 'idle'
  AND query NOT LIKE '%pg_stat_activity%'
ORDER BY duration DESC;
```

### Terminate a Specific Query

If you need to terminate a heavy query:

```sql
-- Cancel query (gentle)
SELECT pg_cancel_backend(<pid>);

-- Terminate connection (forceful)
SELECT pg_terminate_backend(<pid>);
```

### Check Blocking Queries

To see queries that are blocking other queries:

```sql
SELECT 
  blocked_locks.pid AS blocked_pid,
  blocked_activity.usename AS blocked_user,
  blocking_locks.pid AS blocking_pid,
  blocking_activity.usename AS blocking_user,
  blocked_activity.query AS blocked_statement,
  blocking_activity.query AS blocking_statement
FROM pg_catalog.pg_locks blocked_locks
JOIN pg_catalog.pg_stat_activity blocked_activity ON blocked_activity.pid = blocked_locks.pid
JOIN pg_catalog.pg_locks blocking_locks 
  ON blocking_locks.locktype = blocked_locks.locktype
  AND blocking_locks.database IS NOT DISTINCT FROM blocked_locks.database
  AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
  AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page
  AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple
  AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid
  AND blocking_locks.transactionid IS NOT DISTINCT FROM blocked_locks.transactionid
  AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid
  AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid
  AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid
  AND blocking_locks.pid != blocked_locks.pid
JOIN pg_catalog.pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid
WHERE NOT blocked_locks.granted;
```

### Enable Query Logging

To log slow queries, set in PostgreSQL configuration:

```sql
-- Log queries taking longer than 1 second
ALTER SYSTEM SET log_min_duration_statement = 1000;
SELECT pg_reload_conf();
```

Or via Helm values:

```yaml
patroni:
  postgreSQLParams:
    log_min_duration_statement: "1000"  # milliseconds
    log_statement: "all"  # or "ddl", "mod"
```

## Backup Daemon Storage Clean Up

If Backup Daemon storage is full and backups are not being evicted properly:

### Check Current Backups

```bash
kubectl exec -n <namespace> <backup-daemon-pod> -- ls -lh /backup
```

### Check Disk Usage

```bash
kubectl exec -n <namespace> <backup-daemon-pod> -- df -h /backup
```

### Review Eviction Policy

Check the deployment configuration for eviction policy settings:

```bash
kubectl get deployment backup-daemon -n <namespace> -o yaml | grep -A 10 evictionPolicy
```

### Manually Remove Old Backups (with caution)

**Warning**: Only remove backups if you have verified they are old and not needed.

```bash
kubectl exec -n <namespace> <backup-daemon-pod> -- rm -rf /backup/<old-backup-directory>
```

### Adjust Eviction Policy

Update Helm values to adjust retention:

```yaml
backupDaemon:
  evictionPolicy:
    maxAge: "7d"  # Keep backups for 7 days
    maxCount: 10  # Keep maximum 10 backups
  backupSchedule:
    full: "0 2 * * 0"  # Weekly full backup at 2 AM Sunday
    incremental: "0 2 * * 1-6"  # Daily incremental at 2 AM Mon-Sat
```

### Force Eviction Run

If eviction is not running automatically:

```bash
kubectl exec -n <namespace> <backup-daemon-pod> -- /scripts/evict-old-backups.sh
```

---

## Additional Diagnostic Commands

### Check PostgreSQL Version

```bash
kubectl exec -n <namespace> <patroni-pod> -- psql -c "SELECT version();"
```

### Check PostgreSQL Configuration

```bash
kubectl exec -n <namespace> <patroni-pod> -- psql -c "SHOW all;"
```

### Check Table Sizes

```sql
SELECT
  schemaname,
  tablename,
  pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC
LIMIT 20;
```

### Check Database Sizes

```sql
SELECT
  datname,
  pg_size_pretty(pg_database_size(datname)) AS size
FROM pg_database
ORDER BY pg_database_size(datname) DESC;
```

### Check Replication Slot Status

```sql
SELECT
  slot_name,
  slot_type,
  database,
  active,
  pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained_wal
FROM pg_replication_slots;
```

### Check WAL Files

```bash
kubectl exec -n <namespace> <patroni-pod> -- du -sh /var/lib/pgsql/data/pg_wal
kubectl exec -n <namespace> <patroni-pod> -- ls -lh /var/lib/pgsql/data/pg_wal | head -20
```
