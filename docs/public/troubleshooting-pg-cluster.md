# PostgreSQL Cluster Troubleshooting

This guide describes how to detect and fix issues of HA PostgreSQL clusters managed by pgskipper-operator, and the generic operations used during troubleshooting.

For Prometheus alerts, see [Prometheus Alerts Troubleshooting](alarm-troubleshooting.md). For deployment and operator errors, see [Troubleshooting](troubleshooting.md).

## Table of Contents

- [Introduction](#introduction)
- [Troubleshooting Overview](#troubleshooting-overview)
- [Typical Application-Side Error Messages](#typical-application-side-error-messages)
  - [Prepared Transactions are Disabled](#prepared-transactions-are-disabled)
  - [Unexpectedly Closed Connection](#unexpectedly-closed-connection)
  - [Accessing PG Results in Read-only Transaction Error](#accessing-pg-results-in-read-only-transaction-error)
  - [Not Enough Connections for Clients](#not-enough-connections-for-clients)
- [PaaS Infrastructure Issues](#paas-infrastructure-issues)
  - [Insufficient Cluster Resources](#insufficient-cluster-resources)
  - [MatchNodeSelector Failed](#matchnodeselector-failed)
  - [Patroni cannot Bootstrap Replica due to Wrong Permissions](#patroni-cannot-bootstrap-replica-due-to-wrong-permissions)
- [HA Clustering Issues](#ha-clustering-issues)
  - [Patroni Cluster Has No Leader](#patroni-cluster-has-no-leader)
  - [Replica Instance is Unable to Start](#replica-instance-is-unable-to-start)
  - [Replica Instance is Unable to Start Because of Malformed WALs](#replica-instance-is-unable-to-start-because-of-malformed-wals)
  - [Replica Instance is Unable to Start Because of Malformed pg_internal.init File](#replica-instance-is-unable-to-start-because-of-malformed-pg_internalinit-file)
  - [Patroni cannot Initialize Cluster and Wait for Leader to Bootstrap on all Nodes](#patroni-cannot-initialize-cluster-and-wait-for-leader-to-bootstrap-on-all-nodes)
  - [Patroni retries requests to Kubernetes API](#patroni-retries-requests-to-kubernetes-api)
- [Replication Issues](#replication-issues)
  - [Replication is Broken](#replication-is-broken)
- [Backup Issues](#backup-issues)
  - [pgBackRest Backup Failures](#pgbackrest-backup-failures)
- [Connection Pooler Issues](#connection-pooler-issues)
  - [Connection Pooler (PgBouncer) Issues](#connection-pooler-pgbouncer-issues)
- [Monitoring Issues](#monitoring-issues)
  - [Monitoring cannot Collect du/df Metrics](#monitoring-cannot-collect-dudf-metrics)
- [DR Scenario Issues](#dr-scenario-issues)
  - [Failed Pods Exist](#failed-pods-exist)
  - [Replication is Broken For Some of the Replicas](#replication-is-broken-for-some-of-the-replicas)
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
- [Monitoring Alarms Description](#monitoring-alarms-description)

## Introduction

This section provides detailed troubleshooting procedures for HA PostgreSQL cluster installations. It provides instructions on how to detect and fix generic issues. Ensure you have administrator privileges in the Kubernetes or OpenShift namespace where the cluster runs. Some procedures also require SSH access to cluster nodes and the ability to use `sudo`; the sections state explicitly when such escalated privileges are needed.

The names in this guide assume the default cluster name `patroni` (Helm value `patroni.clusterName`): StatefulSets `pg-patroni-node1`, `pg-patroni-node2`, and so on, and ConfigMaps `patroni-leader` and `patroni-config`. The Patroni container has the same name as its StatefulSet.

## Troubleshooting Overview

To resolve a database-related issue, first determine whether the issue is caused by a database fault or a failure of any other system. There are two main ways of determining the database failure:

* The monitoring system reports that the database is down, and the `PostgreSQL is Down` alert fires.
* The application is not working properly, and the application troubleshooting process has revealed that the [application log](#getting-pods-log) contains database-related errors.

If the `PostgreSQL is Down` alert fires, but the applications work properly, this is probably a false positive of the monitoring system. However, it is recommended to troubleshoot the database to make sure that the database works. The alerts and their resolution steps are described in [Prometheus Alerts Troubleshooting](alarm-troubleshooting.md).

If one or more applications report database failure, see [Checking the Database Health via Monitoring Dashboard](#checking-the-database-health-via-monitoring-dashboard) first. If the dashboard reports that the database is healthy, troubleshoot the applications according to [Typical Application-Side Error Messages](#typical-application-side-error-messages). If this does not help, the problem is probably in the database.

**Note**: If the `PostgreSQL is Down` alert fires, and one or more applications report a database failure, this is **certainly** a database failure.

After you have determined that the source of the problem is the database, check that the database pods are running and ready. See [Finding the Deployment or the Pod](#finding-the-deployment-or-the-pod). If any of the pods is not ready, troubleshoot the pods according to the following sections:

* [PaaS Infrastructure Issues](#paas-infrastructure-issues)
* [HA Clustering Issues](#ha-clustering-issues)
* [Replication Issues](#replication-issues)

PostgreSQL includes built-in instruments for monitoring database activity and analyzing performance. For more information, refer to the _Official PostgreSQL documentation_ at [https://www.postgresql.org/docs/current/monitoring.html](https://www.postgresql.org/docs/current/monitoring.html). The `pgCenter` administrative tool presents database statistics in a convenient format; see [https://github.com/lesovsky/pgcenter](https://github.com/lesovsky/pgcenter).

If this does not help, the problem is probably not generic, and requires a detailed investigation. Contact support.

## Typical Application-Side Error Messages

This section describes the application-side errors, the probable cause, and the solution.

### Prepared Transactions are Disabled

**Symptom**: The [application log](#getting-pods-log) contains an error. For example:

```text
org.postgresql.util.PSQLException: ERROR: prepared transactions are disabled
```

**Interpretation**: You are using prepared transactions, but PostgreSQL has this feature disabled.

**Possible Reason**: The value of `max_prepared_transactions` in `postgresql.conf` is zero (or this variable is absent).

**Proposed Fix**: Add `"max_prepared_transactions: <value>"` to `patroni.postgreSQLParams` in the patroni-core values and upgrade the release. The operator derives the `PG_CONF_MAX_PREPARED_TRANSACTIONS` env var from this list, so editing the env var in the StatefulSet does not last. PostgreSQL applies the parameter after a restart.

### Unexpectedly Closed Connection

**Symptom**: The [application log](#getting-pods-log) contains an error, for example, one that recurs in the logs for an extended period:

```text
org.postgresql.util.PSQLException: This connection has been closed
```

**Interpretation**: The TCP connection between the client and PostgreSQL server has been closed unexpectedly.

**Possible Reason**: Permanent connectivity issues between the client application pod and the leader PostgreSQL pod.

**Proposed Fix**: [Check the connectivity between the client application and PostgreSQL leader pods](#checking-connectivity-between-pods). If there are any issues, see [Checking Connectivity between Nodes](#checking-connectivity-between-nodes) for the nodes on which these pods run. If there are any issues, the problem is out of this guide's scope. If connectivity between nodes is OK, try to restart the client application pod.

**Possible Reason**: If the client application has connection pooling enabled, and there were connectivity issues, connections in the application's connection pool could become invalid.

**Proposed Fix**: _Ask the developers of the client application_ whether their application is able to withstand a database outage. If not, _report a bug to them_. The actual fix depends on the technologies used by the client application. For example, in WildFly AS you may add `<validate-on-match>true</validate-on-match>` in the datasource configuration.

**Proposed Workaround**: If the connectivity issues were temporary (the connectivity check between the client application and the PostgreSQL leader passes successfully), restart the client application. This clears the connection pool. If not, you must restore connectivity first. This process is out of this guide's scope.

### Accessing PG Results in Read-only Transaction Error

**Symptom**: The [application log](#getting-pods-log) contains an error like the following one:

```text
org.postgresql.util.PSQLException: ERROR: cannot execute UPDATE in a read-only transaction
```

**Possible Reason**: The application connects to a read-only instance of PostgreSQL, that is, a replica pod, for example, through the `pg-patroni-ro` service or a pod IP.

**Proposed Fix**: Make sure the application connects to the `pg-patroni` service (or to the connection pooler) for writes. See [Determining which Pod Receives Traffic from Postgres Service](#determining-which-pod-receives-traffic-from-postgres-service).

**Possible Reason**: The `pg-patroni` service points to a replica because the `pgtype` labels do not match the roles in `patronictl list`.

**Proposed Fix**: Do not edit the `pgtype` labels manually. Patroni manages them (`role_label: pgtype` in the Patroni configuration) and sets the label of its own pod on every role change, so manual changes are overwritten and can route writes to the wrong pod. A mismatch means that Patroni could not update its pod: check the Patroni logs of the affected pods for Kubernetes API errors, and check that the Patroni service account can patch pods (`kubectl auth can-i patch pods -n <namespace> --as=system:serviceaccount:<namespace>:<service-account>`). If you cannot find the cause, _report a bug to the PostgreSQL support team_ with the output of `patronictl list` and `kubectl get pods -n <namespace> -L pgtype`.

**Possible Reason**: Application bug - data modification attempt in a read-only transaction.

**Proposed Fix**: _Report a bug to the application support team_.

### Not Enough Connections for Clients

**Symptom**: The [application log](#getting-pods-log) contains an error like the following:

```text
FATAL: remaining connection slots are reserved for non-replication superuser connections
```

**Possible Reason**: The value of `max_connections` in `postgresql.conf` is too low, or there are too many clients.

**Proposed Fix**:

First of all, determine the current `max_connections` value and the number of clients. Run the following command in the [leader pod](#determining-leader-and-replica-pg-pods):

```bash
kubectl exec -n <namespace> <leader-pod> -- psql -c 'show max_connections'
```

```text
 max_connections
-----------------
 500
(1 row)
```

Get the list of databases and the number of connections:

```bash
kubectl exec -n <namespace> <leader-pod> -- psql -c 'select datname, count(*) from pg_stat_activity group by datname'
```

```text
                 datname                  | count
------------------------------------------+-------
 dbaas_71b47eed91b0411e82768fc2c400ac56   |     1
                                          |     5
 mistral_396a0fea185747429d73b99174dd8ee7 |     3
 dbaas_e3458cbf6ab245a9888cec8ece3b5a2a   |    10
 ...
```

Estimate whether the number of connections is sufficient for the application, and consider one of the following solutions:

* Reduce the number of client connections.
* Increase the value of the `max_connections` setting.

**Note**: Changing the `max_connections` setting can affect other users, because by default, the service calculates `work_mem` to fit every possible connection in memory. If you increase `max_connections`, each operation receives less memory, so sorts and hashes spill to temporary files on disk more often and the IO rate grows. Also, each connection is a new process, which carries some cost.

To increase `max_connections`, add `"max_connections: <value>"` to `patroni.postgreSQLParams` in the patroni-core values and upgrade the release. The operator derives the `PG_MAX_CONNECTIONS` env var from this list. PostgreSQL applies the parameter after a restart.

Check that the setting was changed: run `psql -c 'show max_connections'` in the leader pod again. If the value remains the same after a few minutes, run `/propagate_settings.sh` in the leader pod.

## PaaS Infrastructure Issues

The infrastructure issues are described in this section.

### Insufficient Cluster Resources

**Symptom**: One or more Patroni pods hang in the `Pending` state. The events of the pod (`kubectl describe pod <pod-name> -n <namespace>`) contain messages like this:

```text
0/7 nodes are available: 7 Insufficient memory.
```

and/or

```text
0/7 nodes are available: 7 Insufficient cpu.
```

Older Kubernetes and OpenShift versions report the same condition per node, for example `fit failure on node (compute-node-1): Insufficient Memory`.

**Interpretation**: Your cluster has insufficient resources. The pod cannot be scheduled to any of the nodes.

**Possible Reason**: Your cluster does not match the requested hardware estimates.

**Proposed Fix**: Check the allocatable and requested resources of the nodes with `kubectl describe nodes`, and add capacity to the cluster.

**Possible Reason**: Your pod has requested excessive resources.

**Proposed Fix (not recommended!)**: Reduce the resource requests in `patroni.resources` of the patroni-core values and upgrade the release. Note that this solution is **not recommended**, as you may set insufficient resources for pod operation. Use this solution only if you fully understand the consequences.

### MatchNodeSelector Failed

**Symptom**: One or more Patroni pods hang in the `Pending` state. The events of the pod contain messages like this:

```text
0/7 nodes are available: 7 node(s) didn't match Pod's node affinity/selector.
```

Older versions report `fit failure on node (compute-node-1): MatchNodeSelector`.

**Interpretation**: There is a discrepancy between the actual labels on nodes and the pod's `nodeSelector` or node affinity.

**Possible Reason (for an existing installation)**: Labels on nodes and/or the pod's `nodeSelector` were changed after deployment, and now the pod's `nodeSelector` contains a non-existent label or value.

**Proposed Fix**: Compare the pod's selector with the node labels, and restore the labels on the nodes or fix the selector in the values:

```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.nodeSelector}{"\n"}{.spec.affinity.nodeAffinity}'
kubectl get nodes --show-labels
```

**Possible Reason (for initial rollout)**: You have specified an invalid `patroni.storage.nodes` parameter (used with `patroni.storage.type: pv`) or an invalid `patroni.affinity`.

**Proposed Fix**: Correct the parameter and reinstall the cluster.

### Patroni cannot Bootstrap Replica due to Wrong Permissions

**Symptom**: Patroni cannot bootstrap the replica from the leader, and the logs of the replica are filled with messages like:

```text
pg_basebackup: could not create directory "/var/lib/pgsql/data/postgresql_node1": Permission denied
2017-10-18 14:15:43,726 ERROR: Error when fetching backup: pg_basebackup exited with code=1
2017-10-18 14:15:43,729 ERROR: failed to bootstrap from leader 'node2'
2017-10-18 14:15:43,729 INFO: Removing data directory: /var/lib/pgsql/data/postgresql_node1
```

**Interpretation**: The directory `/var/lib/pgsql/data/` has the wrong permissions.

**Possible Reason**: The volume was mounted with an owner that the PostgreSQL user cannot write to, for example, because the pod obtained a host path volume before the storage was mounted to that host path on the node. To check, run:

```bash
kubectl exec -n <namespace> <pod-name> -- ls -ld /var/lib/pgsql/data
```

A result such as `drwxr-xr-x. 2 root root 6 Oct 18 13:34 data` means that the mount has the wrong permissions.

**Proposed Fix**:

* For dynamically provisioned volumes, set `patroni.fsGroup` in the patroni-core values so that Kubernetes sets the group ownership of the volume, and upgrade the release.
* For host path volumes, find the host path of the pod's volume as described in [Determining a Host Path where Volume Data is Stored](#determining-a-host-path-where-volume-data-is-stored), [find the node on which the pod runs](#determining-on-which-node-a-pod-runs), and check the directory on the node:

  ```bash
  sudo ls -l <volume-path>
  ```

  If the storage is mounted correctly now (the directory contains `postgresql_node<N>` owned by the PostgreSQL user), restart the pod. If the permissions are still not valid, contact the Kubernetes or OpenShift administrator.

## HA Clustering Issues

The HA clustering issues are described in this section.

### Patroni Cluster Has No Leader

**Symptom**: `patronictl list` shows no member with the `Leader` role, and no pod has the `pgtype=primary` label.

**Diagnostic Steps**:

1. Check the member states and whether maintenance mode is on:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl list
   ```

2. Check the Patroni logs for DCS errors. By default, Patroni uses Kubernetes as the DCS (`patroni.dcs.type: kubernetes`) and keeps its state in the `<cluster-name>-leader` and `<cluster-name>-config` ConfigMaps:

   ```bash
   kubectl logs -n <namespace> <patroni-pod> -c pg-<cluster-name>-node<N> | grep -iE "dcs|configmap|leader|lock|forbidden|timeout"
   ```

**Proposed Fix**:

- Ensure Patroni pods can reach the Kubernetes API, or the etcd hosts from `patroni.dcs.hosts` when `patroni.dcs.type` is `etcd`, and that RBAC allows them to update their ConfigMaps. See [Patroni retries requests to Kubernetes API](#patroni-retries-requests-to-kubernetes-api).
- Check that network policies allow communication between Patroni pods and between Patroni and the DCS.
- Fix the errors that keep members from starting, such as a full disk.
- Check whether Patroni is in maintenance mode; `patronictl resume` turns it off.
- If all members wait for the leader to bootstrap, see [Patroni cannot Initialize Cluster and Wait for Leader to Bootstrap on all Nodes](#patroni-cannot-initialize-cluster-and-wait-for-leader-to-bootstrap-on-all-nodes).

### Replica Instance is Unable to Start

**Symptom**: The PostgreSQL replica is not able to start, and the [pod log](#getting-pods-log) contains the following message:

```text
PANIC:  could not read from control file: Success
```

**Interpretation**: The PostgreSQL replica instance cannot start due to failed access to the `pg_control` file.

**Possible Reason**: There are a couple of possible reasons: the database is not vacuumed on time, or lack of disk space.

**Proposed Fix**: [Replica Pod Reinitialization](#replica-pod-reinitialization).

### Replica Instance is Unable to Start Because of Malformed WALs

**Symptom**: The PostgreSQL leader is started successfully, but the replica is not able to start, and the [pod log](#getting-pods-log) contains messages like:

```text
LOG: record with incorrect prev-link 4000800/0 at 0/305BAF8
```

or

```text
LOG:  invalid resource manager ID 32 at 0/1F8C1450
```

**Interpretation**: The PostgreSQL replica instance cannot start because it encounters malformed WALs.

**Possible Reason**: The replica or leader instance was rebooted forcefully, or the replication connection was lost.

**Proposed Fix**: [Replica Pod Reinitialization](#replica-pod-reinitialization).

### Replica Instance is Unable to Start Because of Malformed pg_internal.init File

**Symptom**: The PostgreSQL leader is started successfully, but the replica is not able to start, and the [pod log](#getting-pods-log) contains messages like the following:

```text
[ERROR][source=patroni] Error when fetching backup: pg_basebackup exited with code=1
[WARNING][source=patroni] Trying again in 5 seconds
    pg_basebackup: COPY stream ended before last file was finished
    pg_basebackup: removing data directory "/var/lib/pgsql/data/postgresql_node2"
```

Also, the leader [pod log](#getting-pods-log) contains messages like the following:

```text
ERROR:  invalid segment number 0 in file "pg_internal.init.7348"
```

**Interpretation**: The replica cannot start because `pg_basebackup` encounters a malformed `pg_internal.init` file on the leader.

**Possible Reason**: The replica or leader instance was rebooted forcefully, or the replication connection was lost.

**Proposed Fix**: Restart the leader PostgreSQL instance, or delete the malformed `pg_internal.init` file named in the error from the leader's data directory. PostgreSQL rebuilds the file on startup.

### Patroni cannot Initialize Cluster and Wait for Leader to Bootstrap on all Nodes

**Symptom**: The [Patroni pod logs](#getting-pods-log) of all members contain constantly repeating lines like the following ones:

```text
2017-09-20 14:07:38,427 INFO: waiting for leader to bootstrap
2017-09-20 14:07:39,707 INFO: waiting for leader to bootstrap
2017-09-20 14:07:58,526 INFO: waiting for leader to bootstrap
```

**Interpretation**: The Patroni pods cannot initialize the cluster because none of them can start the initialization process.

**Possible Reason**: One of the pods created the `initialize` key, but did not perform the initialization and did not clean up the key. To check whether this is the actual reason, look for an `initialize` annotation with an empty value on the `patroni-config` ConfigMap:

```bash
kubectl get configmap patroni-config -n <namespace> -o jsonpath='{.metadata.annotations}'
```

Double-check that the logs of **ALL** Patroni pods contain constantly repeating lines like `INFO: waiting for leader to bootstrap`.

**Proposed Fix**: Remove the `initialize` annotation from the `patroni-config` ConfigMap. Do not change other annotations.

```bash
kubectl annotate configmap patroni-config -n <namespace> initialize-
```

### Patroni retries requests to Kubernetes API

**Symptom**: The [logs](#getting-pods-log) of all Patroni pods contain constantly repeating lines like the following ones:

```text
2022-04-07 11:35:03,164 WARNING: Retrying (Retry(total=2, connect=None, read=None, redirect=None, status=None)) after connection broken by 'ConnectTimeoutError(<urllib3.connection.HTTPSConnection object at 0x7fcba0284c50>, 'Connection to 172.30.0.1 timed out. (connect timeout=1)')': /api/v1/namespaces/postgres-main/pods?labelSelector=app%3Dpatroni
...
urllib3.exceptions.MaxRetryError: HTTPSConnectionPool(host='172.30.0.1', port=443): Max retries exceeded with url: /api/v1/namespaces/postgres-main/pods?labelSelector=app%3Dpatroni (Caused by ConnectTimeoutError(<urllib3.connection.HTTPSConnection object at 0x7fcba0284a20>, 'Connection to 172.30.0.1 timed out. (connect timeout=1)'))
...
patroni.utils.RetryFailedError: 'Exceeded retry deadline'
2022-04-07 11:35:02,159 ERROR: Error communicating with DCS
2022-04-07 11:35:02,160 INFO: DCS is not accessible
```

or

```text
2023-02-21 07:17:44,203 ERROR: Request to server https://172.30.0.1:443 failed: MaxRetryError("HTTPSConnectionPool(host='172.30.0.1', port=443): Max retries exceeded with url: /api/v1/namespaces/dmsh-pg/pods?labelSelector=app%3Dpatroni (Caused by NewConnectionError('<urllib3.connection.HTTPSConnection object at 0x7f866c109b38>: Failed to establish a new connection: [Errno 111] Connection refused',))",)
2023-02-21 07:17:44,204 ERROR: Request to server https://172.30.0.1:443 failed: ClosedPoolError("HTTPSConnectionPool(host='172.30.0.1', port=443): Pool is closed.",)
2023-02-21 07:17:45,204 ERROR: ObjectCache.run K8sConnectionFailed('No more API server nodes in the cluster',)
```

Another symptom is a short outage of PostgreSQL.

**Interpretation**: Patroni was not able to reach the Kubernetes API, and after some time demoted PostgreSQL.

**Possible Reason**: Unavailability of the Kubernetes API.

**Proposed Fixes**:

1. Tune the following Patroni parameters:

   * `retry_timeout` - timeout for DCS and PostgreSQL operation retries (in seconds). DCS or network issues shorter than this do not cause Patroni to demote the leader.
   * `ttl` - the TTL to acquire the leader lock (in seconds). Think of it as the length of time before the automatic failover process starts.
   * `loop_wait` - the number of seconds the loop sleeps.

   With an unstable Kubernetes API, increase these values with `patronictl edit-config`, and add them to `patroni.patroniParams` in the patroni-core values so that the next upgrade keeps them.

2. Make sure `failsafe_mode` is enabled. The patroni-core chart enables it by default (`"failsafe_mode: true"` in `patroni.patroniParams`). With failsafe mode, the leader keeps running while it can reach all members, even if the DCS is unavailable. For more information, see [https://patroni.readthedocs.io/en/master/dcs_failsafe_mode.html](https://patroni.readthedocs.io/en/master/dcs_failsafe_mode.html).


## Replication Issues

The replication issues are explained in this section.

### Replication is Broken

**Symptom**: A replica pod starts but fails to start PostgreSQL, or PostgreSQL does not replicate data. Its [log](#getting-pods-log) contains lines like the following ones:

```text
2017-05-18 18:12:52.002 UTC > LOG:  invalid record length at 0/58F09F0: wanted 24, got 0
```

or

```text
requested timeline 144 is not a child of this server's history
```

or

```text
< 2019-01-01 12:02:55.525 UTC > FATAL:  could not receive data from WAL stream: ERROR:  requested WAL segment 0000002B000000C100000036 has already been removed
```

Another symptom is the replication graph on the monitoring dashboard: if PostgreSQL cannot start replication or replication is broken, but PostgreSQL itself is running, the WAL location line of the replica is flat.

**Interpretation**: The replica cannot recover data from the leader because of broken replication.

**Proposed Fix**: Make sure that the leader pod is running. *Do not continue if the leader is broken - this may lead to data loss, which may be recovered only by using the backups.* If the leader is running, [reinitialize the replica pod](#replica-pod-reinitialization).

**Symptom**: A replica pod starts but fails to start PostgreSQL, or PostgreSQL does not replicate data. Its [log](#getting-pods-log) contains lines like the following ones:

```text
FATAL: number of requested standby connections exceeds max_wal_senders (currently 10)
FATAL: could not connect to the primary server: connection to server at "10.129.180.205", port 5432 failed: FATAL: number of requested standby connections exceeds max_wal_senders (currently 10)
```

**Interpretation**: There are already too many replication connections to the database.

**Proposed Fix**: Terminate some replication connections, or increase `max_wal_senders`.

1. Determine the current `max_wal_senders` value in the [leader pod](#determining-leader-and-replica-pg-pods) (the default is `10`):

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- psql -c 'show max_wal_senders'
   ```

2. List the replication slots:

   ```sql
   SELECT slot_name, plugin, slot_type, active, active_pid FROM pg_replication_slots;
   ```

3. Terminate connections that are no longer needed with `pg_terminate_backend(<pid>)`, or increase `max_wal_senders` and `max_replication_slots`:

   ```bash
   kubectl exec -n <namespace> <leader-pod> -- patronictl edit-config -s 'postgresql.parameters.max_wal_senders=20' -q --force
   kubectl exec -n <namespace> <leader-pod> -- patronictl edit-config -s 'postgresql.parameters.max_replication_slots=20' -q --force
   kubectl exec -n <namespace> <leader-pod> -- patronictl show-config
   ```

4. Restart PostgreSQL with `patronictl restart patroni`, so that the new values take effect.

Add the same values to `patroni.postgreSQLParams` in the patroni-core values, so that the next upgrade keeps them.

## Backup Issues

### pgBackRest Backup Failures

**Symptom**: Backups fail with errors in pgBackRest logs.

**Diagnostic Steps**:

1. Check pgBackRest logs:

   ```bash
   kubectl logs -n <namespace> <patroni-pod> -c pgbackrest-sidecar
   ```

2. Check the stanzas, backups, and WAL archive:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -c pgbackrest-sidecar -- pgbackrest info
   ```

**Proposed Fix**:

- Verify the S3 credentials (`pgBackRest.s3.key`, `pgBackRest.s3.secret`) or the RWX volume settings (`pgBackRest.rwx`).
- Check `pgBackRest.repoType`, `pgBackRest.repoPath`, and `pgBackRest.s3.endpoint`.
- Ensure the retention values (`pgBackRest.fullRetention`, `pgBackRest.diffRetention`) match the backup schedule.

For more information, see [pgBackRest](features/pgBackRest.md).

## Connection Pooler Issues

### Connection Pooler (PgBouncer) Issues

**Symptom**: Applications cannot connect through PgBouncer.

**Diagnostic Steps**:

1. Check PgBouncer logs:

   ```bash
   kubectl logs -n <namespace> deployment/connection-puller
   ```

2. Verify the PgBouncer configuration:

   ```bash
   kubectl get configmap pooler-config -n <namespace> -o yaml
   ```

**Proposed Fix**:

- Verify `connectionPooler.install: true` in the patroni-services values.
- Check `connectionPooler.config.databases` and the PgBouncer settings in `connectionPooler.config.pgbouncer`, such as `pool_mode`, `max_client_conn`, `default_pool_size`, and `auth_type`.
- Ensure the database user exists and has proper privileges.

For more information, see [Connection Pooler](features/connection-pooler.md).

## Monitoring Issues

The monitoring issues are explained in this section.

### Monitoring cannot Collect du/df Metrics

**Symptom**: Disk usage metrics of the Patroni nodes are missing on the monitoring dashboard, and the `Disk is almost full` alerts do not fire.

**Interpretation**: The monitoring agent (`monitoring-collector`) collects the disk metrics by running the following commands in the Patroni container through `kubectl exec`. If a command fails or does not return in time, the agent skips the disk metrics of that pod.

```text
du /var/lib/pgsql/data/ --max-depth=2 --exclude=/var/lib/pgsql/data/lost+found
df /var/lib/pgsql/data
```

**Diagnostic Steps**:

1. Check the monitoring agent logs:

   ```bash
   kubectl logs -n <namespace> deployment/monitoring-collector
   ```

2. Run the commands manually and check how long they take:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- sh -c 'time du /var/lib/pgsql/data/ --max-depth=2 --exclude=/var/lib/pgsql/data/lost+found'
   ```

**Proposed Fix**: If `du` takes too long, the data volume is slow or contains a very large number of files; check the storage performance. If the command fails with a permissions error, check that the monitoring agent's service account can create `pods/exec` in the namespace.

**Note**: The legacy Python-based collector had a `SHELL_METRICS_COLLECTOR_EXECUTORS` environment variable for this case. The current monitoring agent does not use it.

## DR Scenario Issues

This section describes potential issues that could happen during DR scenarios.

### Failed Pods Exist

**Symptom**: One of the DR scenarios fails on the PostgreSQL step with the following errors:

```text
Exiting because exit_code: 12 not equals zero and failed pods exists:  pg-patroni-node3-41-1j73n pg-patroni-node4-40-058gm
```

`patronictl list` shows that the `State` of some members is `start failed`, but PostgreSQL itself is up and running. In addition, the logs of the Patroni members do not contain any errors.

**Proposed Fix**: Restart the failed members with `patronictl restart patroni <member-name>`. Proceed with the DR scenario once again.

Also, in such cases, collect the following information for debugging:

* `ps aux | cat` output in all of the failed Patroni members.
* Logs of all of the failed Patroni members (`kubectl logs`).

Contact the PostgreSQL support team with this information.

### Replication is Broken For Some of the Replicas

**Symptom**: One of the DR scenarios fails on the PostgreSQL step with the following error:

```text
Patroni cluster is not ready! 2 out of 4
```

`patronictl list` shows that the `State` of some members is `starting`, and the logs of a `starting` pod contain errors like:

```text
* Connected to postgres-backup-daemon (10.126.47.132) port 8082 (#0)
* Server auth using Basic with user 'postgres'
> GET /archive/get?filename=000000F100000196000000DB HTTP/1.1
> Host: postgres-backup-daemon:8082
> User-Agent: curl/7.51.0
> Accept: */*
* The requested URL returned error: 404 NOT FOUND
```

**Interpretation**: The replica requests a WAL file that is not in the backup daemon WAL archive.

**Proposed Fix**: Reinitialize the failed Patroni member. See [Replica Pod Reinitialization](#replica-pod-reinitialization).

Also, in such cases, collect the logs of all of the `starting` Patroni members and of PostgreSQL Backup Daemon for debugging.

## Generic Operations How To

This section explains how to perform generic actions during the HA PostgreSQL cluster troubleshooting process. Some of them are common Kubernetes troubleshooting actions, and others are specific to PostgreSQL cluster diagnosis.

### Finding the Deployment or the Pod

Each PostgreSQL node runs in its own StatefulSet named `pg-patroni-node<N>`, with one pod each. The other components, such as `postgres-operator`, `postgres-backup-daemon`, and `monitoring-collector`, are Deployments.

#### Finding the Deployment

```bash
kubectl get statefulsets,deployments -n <namespace>
```

#### Finding the Pod

```bash
kubectl get pods -n <namespace> -L pgtype -o wide
```

In the OpenShift web console, the same objects are under **Workloads > StatefulSets**, **Workloads > Deployments**, and **Workloads > Pods**.

**Note**: The names of the PostgreSQL pods start with the `pg-` prefix and contain the word `node`.

### Getting Pod's Log

The Patroni container has the same name as its StatefulSet, for example, `pg-patroni-node1`.

```bash
kubectl logs -n <namespace> <pod-name> -c <container-name>

# Follow the log
kubectl logs -n <namespace> <pod-name> -c <container-name> -f

# Log of the previous container instance, after a restart
kubectl logs -n <namespace> <pod-name> -c <container-name> --previous
```

### Scaling the Deployment

Scaling a Patroni StatefulSet to zero stops that PostgreSQL node.

```bash
kubectl scale statefulset pg-patroni-node<N> -n <namespace> --replicas=<count>
kubectl scale deployment <deployment-name> -n <namespace> --replicas=<count>
```

### Checking Connectivity between Pods

1. Get the IP of the first pod:

   ```bash
   kubectl get pod <first-pod> -n <namespace> -o jsonpath='{.status.podIP}'
   ```

The Patroni images do not contain the `ping` command, so test the TCP ports from a Patroni pod instead.

2. To test the PostgreSQL port, run `pg_isready`. The output `accepting connections` means the port is reachable; `no response` means it is not.

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- pg_isready -h <first-pod-ip> -p 5432
   ```

3. To test any other TCP port, for example, the port of a client application pod, use the `/dev/tcp` feature of `bash`:

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- bash -c 'timeout 5 bash -c "</dev/tcp/<first-pod-ip>/<port>" && echo reachable || echo unreachable'
   ```

Repeat the check several times. If it fails only sometimes, the network between the pods is unstable; if it always fails, there is no connectivity between the pods.

### Checking Connectivity between Nodes

**Note**: This action requires SSH access to the nodes.

Get the internal IPs of the nodes with `kubectl get nodes -o wide`, connect to one node, and ping the other:

```bash
ping -c 10 <other-node-ip>
```

If the packet loss differs from `0%`, the network between the nodes is unstable. If it reaches `100%`, there is no connectivity between the nodes at all.

### Determining on which Node a Pod Runs

```bash
kubectl get pod <pod-name> -n <namespace> -o wide
```

The `NODE` column shows the node.

### Determining PG Cluster Name

The cluster name is `patroni` by default and comes from the Helm value `patroni.clusterName`. To check it on a running cluster:

```bash
kubectl get patronicore -n <namespace> -o jsonpath='{.items[0].spec.patroni.clusterName}'
kubectl exec -n <namespace> <patroni-pod> -- grep scope /patroni/pg_node.yml
```

### Determining Leader and Replica PG Pods

Use one of the following methods:

* Patroni: `kubectl exec -n <namespace> <patroni-pod> -- patronictl list`. The `Role` column marks the leader and the replicas.
* psql: `kubectl exec -n <namespace> <patroni-pod> -- psql -tAc 'SELECT pg_is_in_recovery();'`. If the query returns `t` (true), the pod is a replica; if it returns `f`, the pod is the leader; any other output is an error.
* Pod labels: `kubectl get pods -n <namespace> -L pgtype`. The leader has `pgtype=primary`, and replicas have `pgtype=replica`. Older installations used `pgtype=master`; the operator replaces it.

### Determining which Pod is Considered to be Leader by Patroni

Patroni stores metadata about the PostgreSQL cluster in the DCS. With the default Kubernetes DCS, this is a set of ConfigMaps. The `leader` annotation of the `patroni-leader` ConfigMap contains the name of the leader pod. Normally, it matches the actual cluster state. If you are unsure, check it manually:

```bash
kubectl get configmap patroni-leader -n <namespace> -o jsonpath='{.metadata.annotations.leader}'
```

Do not edit or delete this ConfigMap.

### Determining which Pod Receives Traffic from Postgres Service

An HA PostgreSQL installation has a few pods, but only one (the leader) receives read-write traffic through the `pg-patroni` service. Replicas serve the `pg-patroni-ro` service. With the connection pooler installed, `pg-patroni` points to PgBouncer, and `pg-patroni-direct` points to the leader.

```bash
kubectl get endpoints pg-patroni pg-patroni-ro -n <namespace>
kubectl get service pg-patroni -n <namespace> -o jsonpath='{.spec.selector}'
kubectl get pods -n <namespace> -o wide --show-labels
```

The pod whose IP is in the `pg-patroni` endpoints receives the traffic. If that pod is not the leader reported by `patronictl list`, **the cluster state is invalid**. See [Accessing PG Results in Read-only Transaction Error](#accessing-pg-results-in-read-only-transaction-error).

### Determining a Host Path where Volume Data is Stored

1. Find the PVC of the pod:

   ```bash
   kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}'
   ```

2. Find the PV bound to the PVC. The `VOLUME` column contains the PV name.

   ```bash
   kubectl get pvc <pvc-name> -n <namespace>
   ```

3. Find the path on the node:

   ```bash
   kubectl get pv <pv-name> -o jsonpath='{.spec.hostPath.path}{.spec.local.path}'
   ```

   Empty output means that the PV is neither of the `hostPath` nor of the `local` type. Find the data location in the documentation of its storage provider.

### Cleaning Volume with Pod's Data

**Note**:

* This action requires SSH access to the nodes.
* You may lose important data. Do not perform these steps until you understand them, and make sure a recent backup exists. To rebuild a replica, prefer [Replica Pod Reinitialization](#replica-pod-reinitialization): it achieves the same result without node access.

1. Find the host path. See [Determining a Host Path where Volume Data is Stored](#determining-a-host-path-where-volume-data-is-stored).
2. [Find the node on which the pod runs](#determining-on-which-node-a-pod-runs).
3. Stop the pod:

   ```bash
   kubectl scale statefulset pg-patroni-node<N> -n <namespace> --replicas=0
   ```

4. Connect to the node over SSH and remove the data. The expected output of this command is empty.

   ```bash
   sudo rm -rf <volume-path>/*
   ```

5. Start the pod again:

   ```bash
   kubectl scale statefulset pg-patroni-node<N> -n <namespace> --replicas=1
   ```

### Checking the Database Health via Monitoring Dashboard

Open the PostgreSQL Grafana dashboard that the patroni-services chart ships (`monitoring/grafana-dashboard.json`), and select the cluster and namespace. The database may be considered healthy if all of the following conditions are met:

* **PostgreSQL Cluster Status** is up.
* **Running Nodes** equals the number of Patroni nodes.
* **Smoke Test** and **Master Smoke Test** pass.

### Making Updates with Custom Frontend Service Type

Only the `ClusterIP` service type is supported for the `pg-patroni` service. If you use a different type of service and want to update with standard tools, proceed as follows:

1. Export your custom service:

   ```bash
   kubectl get service pg-patroni -n <namespace> -o yaml > pg-patroni.custom.yaml
   ```

2. Delete the custom service. Clients lose connectivity until step 5.

   ```bash
   kubectl delete service pg-patroni -n <namespace>
   ```

3. Follow the standard update procedure.
4. Delete the newly created service:

   ```bash
   kubectl delete service pg-patroni -n <namespace>
   ```

5. Import your custom service. Remove `metadata.resourceVersion`, `metadata.uid`, and `spec.clusterIP` from the file first if `kubectl apply` rejects them.

   ```bash
   kubectl apply -f pg-patroni.custom.yaml
   ```

### Switch on/off Pause Mode in Patroni

Pause mode allows temporarily stepping down Patroni from managing the PostgreSQL cluster: Patroni does not fail over and does not restart PostgreSQL. Run the commands in any Patroni pod.

Pause Patroni:

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl pause
```

Resume Patroni:

```bash
kubectl exec -n <namespace> <patroni-pod> -- patronictl resume
```

Alternatively, use the Patroni REST API from inside a Patroni pod. REST API authentication is always enabled and cannot be disabled, so the `-u` argument is required. The credentials are in the `PATRONI_REST_API_USER` and `PATRONI_REST_API_PASSWORD` environment variables of the Patroni container. Send `{"pause": false}` to resume.

```bash
curl -i -X PATCH -u "${PATRONI_REST_API_USER}:${PATRONI_REST_API_PASSWORD}" \
  "http://$(hostname -i):8008/config" -d '{"pause": true}'
```

### Replica Pod Reinitialization

Replica reinitialization removes all data from the replica node and synchronizes the data with the leader node from scratch. Never run it on the leader.

1. Get the member names with `patronictl list`. See [Determining Leader and Replica PG Pods](#determining-leader-and-replica-pg-pods).
2. Reinitialize the replica. `--force` skips the interactive prompt, which `kubectl exec` cannot answer.

   ```bash
   kubectl exec -n <namespace> <patroni-pod> -- patronictl reinit patroni <member-name> --force
   ```

   Alternatively, run the REST API call in the replica pod itself. The `-u` argument is required, because REST API authentication cannot be disabled.

   ```bash
   curl -i -X POST -u "${PATRONI_REST_API_USER}:${PATRONI_REST_API_PASSWORD}" \
     "http://$(hostname -i):8008/reinitialize"
   ```

3. Watch the replica with `patronictl list` until its state is `streaming` again. A replica in any other state, including `running`, is not replicating yet.

In some cases, reinitialization does not work. Then put [Patroni on pause](#switch-onoff-pause-mode-in-patroni) before starting replica reinitialization, and turn off pause mode after reinitialization.

### Heavy Queries Detection

PostgreSQL includes a statistics collector that supports the collection and reporting of information about server activity. For more information, refer to the _PostgreSQL Documentation_ at [https://www.postgresql.org/docs/current/monitoring-stats.html](https://www.postgresql.org/docs/current/monitoring-stats.html).

There are two views to detect heavy queries:

* `pg_stat_activity` contains information about the currently executed requests. For more information, refer to [https://www.postgresql.org/docs/current/monitoring-stats.html#MONITORING-PG-STAT-ACTIVITY-VIEW](https://www.postgresql.org/docs/current/monitoring-stats.html#MONITORING-PG-STAT-ACTIVITY-VIEW).
* `pg_stat_statements` provides execution statistics of SQL queries. The extension is in the default `shared_preload_libraries`. For more information, refer to [https://www.postgresql.org/docs/current/pgstatstatements.html](https://www.postgresql.org/docs/current/pgstatstatements.html).

To detect a heavy (long-running) query, connect to the database on the pod where the alert fired and select rows from these views:

```sql
-- Currently running queries, longest first
SELECT pid, now() - query_start AS duration, usename, datname, state,
       wait_event_type, wait_event, left(query, 200) AS query
FROM pg_stat_activity
WHERE state <> 'idle' AND pid <> pg_backend_pid()
ORDER BY duration DESC
LIMIT 20;

-- Statements with the highest total execution time
SELECT calls, round(total_exec_time) AS total_ms, round(mean_exec_time) AS mean_ms, left(query, 200) AS query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 20;

-- Blocked and blocking sessions
SELECT blocked.pid AS blocked_pid, blocking.pid AS blocking_pid,
       left(blocked.query, 200) AS blocked_query, left(blocking.query, 200) AS blocking_query
FROM pg_stat_activity AS blocked
JOIN LATERAL unnest(pg_blocking_pids(blocked.pid)) AS b(pid) ON true
JOIN pg_stat_activity AS blocking ON blocking.pid = b.pid;
```

`SELECT pg_cancel_backend(<pid>);` cancels a query, and `SELECT pg_terminate_backend(<pid>);` closes its connection. Once the heavy query is detected, contact the application team or support for further assistance.

### Backup Daemon Storage Clean Up

In some cases, Backup Daemon storage can be full and eviction policies do not work, because eviction happens only upon a successful full backup. In this case, clean up the Backup Daemon storage by deleting old backups or WAL archives.

1. Check the storage usage:

   ```bash
   kubectl exec -n <namespace> deployment/postgres-backup-daemon -- df -h /backup-storage
   ```

2. List the backups through the backup daemon API, and evict the backups that are no longer needed. Use the API rather than deleting backup folders, so the daemon keeps its index consistent. Add `-u <user>:<password>` if authentication is enabled.

   ```bash
   kubectl port-forward -n <namespace> deployment/postgres-backup-daemon 8080:8080
   curl http://localhost:8080/backups/list
   curl -X DELETE "http://localhost:8080/evict?id=<backup-id>"
   ```

3. If WAL archiving is enabled, remove old archived WAL files from the backup daemon pod. `<major-version>` is the PostgreSQL major version, for example `pg15`, and `30` removes files older than 30 days, which is one of the safest options:

   ```bash
   kubectl exec -n <namespace> deployment/postgres-backup-daemon -- \
     find /backup-storage/<major-version>/archive -type f -mtime +30 -delete
   ```

Also, if such a situation happens, proceed with the following steps:

1. Turn off WAL archiving on development environments.
2. Schedule full backups more often (`backupDaemon.backupSchedule`; eviction happens only after a successful full backup), and tune the eviction policy (`backupDaemon.evictionPolicy`) as per your requirements.
3. Tune the WAL settings, such as `archive_cleanup_command` and `wal_keep_size`.
4. If the storage is too small for the policy, increase `backupDaemon.storage.size`.

## Monitoring Alarms Description

The Prometheus alerts, their possible reasons, and resolution steps are described in [Prometheus Alerts Troubleshooting](alarm-troubleshooting.md). The alert thresholds are described in [Threshold Configuration for Monitoring Alarms](alarm-troubleshooting.md#threshold-configuration-for-monitoring-alarms).
