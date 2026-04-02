This section describes features of the Query Exporter.
* [Prerequisites](#prerequisites)
* [Custom queries](#custom-queries)
* [Exporter user](#exporter-user)
* [Exclude queries](#exclude-queries)
* [Self monitoring](#self-monitoring)
* [Parallel connections](#parallel-connections)
* [Circuit-breaker mechanism](#circuit-breaker-mechanism)

# Prerequisites

Query exporter requires the next list of extensions:

| Extension | 
---------------------------------|
| pg_stat_statements             |
| pgsentinel                     |
| pg_wait_sampling               |
| pg_buffercache                 | 
| pg_stat_kcache                 | 

For AWS RDS `pgsentinel` and `pg_wait_sampling` are not supported.

These extensions will be added to `shared_preload_libraries` and created automatically for main logical database (`postgres` by default).

However for managed databases these extensions must be enabled for database instance manually.

# Custom queries

## Migration from postgres-exporter

Please check [new queries format](/operator/charts/patroni-services/query-exporter/query-exporter-queries.yaml) for query-exporter.
For custom queries two sections must be used in config: `metrics` and `queries`.

Queries section includes map of queries. Each query now include next mandatory fields:
```yaml
    databases:
    - master # Default name for postgres database in config
    metrics:
    - pg_database_datdba # Metrics list generated for query
    sql: SELECT datname, datdba as pg_database_datdba FROM pg_database # SQL query for execution 
```

Important Note: Aliases in SQL query must correspond with metric names.

Metrics section includes map of metrics with the next format:
```yaml
  pg_database_datdba:
    type: gauge # Metric type
    labels:
    - datname  # Label names
    description: Database ID
```

## Purposes

This feature is used for dynamically update of queries for Query Exporter by config maps, placed in another namespaces.

## How to enable this feature

Custom queries' watcher for Query Exporter should be enabled in [deployment parameters](/docs/public/installation.md#query-exporter).
Namespaces list also should be defined in the deployment parameters.

There are several ways of configure necessary roles:

1. If you have privileges to create ClusterRole and ClusterRoleBinding, create next resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: query-exporter
rules:
  - apiGroups:
    - ""
    resources:
    - configmaps
    verbs:
    - get
    - watch
    - list
```

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: query-exporter-<namespace>
subjects:
  - kind: ServiceAccount
    name: postgres-sa
    namespace: <namespace>
roleRef:
  kind: ClusterRole
  name: query-exporter
  apiGroup: rbac.authorization.k8s.io
```

where `<namespace>` is the namespace with Postgres.

*Note* In this scenario RoleBindings to ClusterRole for each namespace from `queryExporter.customQueries.namespacesList` can be used instead one 
ClusterRoleBinding.

2) For the second way cluster privileges are not required:
For each namespace listed in `queryExporter.customQueries.namespacesList` create:

```yaml
kind: Role
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: query-exporter
  namespace: <namespace>
rules:
  - verbs:
      - get
      - watch
      - list
    apiGroups:
      - ''
    resources:
      - configmaps
```

```yaml
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: query-exporter
  namespace: <namespace>
subjects:
  - kind: ServiceAccount
    name: postgres-sa
    namespace: <postgres-namespace>
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: query-exporter
```

where `<namespace>` is the current namespace from `queryExporter.customQueries.namespacesList` and `<postgres-namespace>` namespace with Postgres.

## Execution scope

For custom and basic queries execution scope can be modified by setting in `queries` section:
1) `databases` parameter
```yaml
...
pg_example_query:
  logicalDatabases:
    - "database1"
    - "database2"
...
```
In this case `pg_example` query will be executed only for `database1` and `database2` databases.


2) `classifiers` parameter

```yaml
...
pg_example_query:
  classifiers:
    - microserviceName: "postgres-sa"
      namespace: "postgres-dbaas"
      tenantId: "tenantId"
...
```

In this case query `pg_example` will be executed for all databases matching at least one DBaas classifier from the list. An arbitrary set of tags can be set for each classifier.



## How it works

In postgres-operator new watchers are created for namespaces, listed in deployment parameters.
These watchers react to Create, Update, Delete events for config maps with labels from `queryExporter.customQueries.labels` parameter and mandatory label
```query-exporter: custom-queries```. Config maps should contain metrics with custom queries for Query Exporter. Metrics must correspond to the [query exporter format](/operator/charts/patroni-services/query-exporter/query-exporter-queries.yaml) and must meet [metric naming rules](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels)).
After the Create event, changes from created config map will be appended to `query-exporter-queries` config map.
After the Modify event, changes from config map will be replaced in `query-exporter-queries` config map.
After the Delete event, changes from config map will be deleted from `query-exporter-queries` config map.
When the changes committed, Query Exporter pod will be restarted for applying changes. Merged queries and metrics will have `merged_from` field with the name of namespace and name of config map these metrics/queries came from.


# Exporter user

New user is created by Postgres-Operator for Query Exporter. Parameters `queryExporter.pgUser` and `queryExporter.pgPassword` specify username and password for created user. If user with provided name already exists, the password will be changed for this user and all necessary roles, will be granted to the user.

Query Exporter user are granted with the next roles:
* pg_read_all_data
* pg_monitor

More information about predefined roles above can be found [here](https://www.postgresql.org/docs/current/predefined-roles.html).


# Exclude queries

Query Exporter has mechanism which allows to exclude queries from execution list. These queries will be skipped by Query Exporter during query invocation.
In order to set the list of the queries for exclude, you must specify the parameter `queryExporter.excludeQueries` in the next way:
```yaml
queryExporter:
  excludeQueries:
    - "pg_lock_tree_query"
    - "connection_by_role_with_limit_query"
```
Names of the queries can be found in [query-exporter-queries](/operator/charts/patroni-services/query-exporter/query-exporter-queries.yaml) configmap. All metrics for excluded query will be automatically excluded.


# Self monitoring

Query Exporter produces metrics related to queries invocation. Please find the list of metrics below:

* query_status - Metric with number of successful and failed query invocations. Metric has `status` label that shows if the query was executed successfully or not. Type is Gaugage.
* query_latency - This metric shows the distribution of query duration. Type is Histogram.

Buckets for `query_latency` metric can be configured using the parameter `queryExporter.selfMonitorBuckets`.
These metrics are enabled by default. In order to disable these metrics `queryExporter.selfMonitorDisabled` deploy parameter must be set to false.


# Parallel connections

The number of parallel connections can be configured using `queryExporter.maxMasterConnections` and `queryExporter.maxLogicalConnections` parameters.
If these parameters are specified, Query Exporter will use limited number of connections to perform queries simultaniously on different logical databases.
Before every new connection to database current number of connections will be checked for a limit and establishing of new connection only possible when connection limit is not exceeded or one of the previously opened connections has been released. After query execution connection get back to the connection pool.

# Circuit-Breaker mechanism

Query Exporter has circuit-breaker mechanism, based on query timeout. After the query execution timeout, which is set by `queryExporter.queryTimeout` parameter was exceeded for `queryExporter.maxFailedTimeouts` times, the query is put to skip list, and will be skipped in further metric scrapes until postgres porter pod restart.
