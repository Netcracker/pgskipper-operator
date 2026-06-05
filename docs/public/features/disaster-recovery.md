This chapter describes how to deploy and use PostgreSQL in Disaster Recovery scheme.

# Overview

Postgres Service can be deployed in the Disaster Recovery (DR) scheme with clusters in `active` and `standby` modes using the configuration described in the section Active-Standby PostgreSQL Cluster Deployment Scheme in the _Postgres Operator Maintenance_ chapter. 

For more information about the DR scheme, refer to the [PostgreSQL Service Installation Procedure](../installation.md#active-standby-deployment-in-two-kubernetes-clusters-prerequisites).

![Postgres Service DR Scheme](../images/arch/pg-arch-on-prem-dr.png)

In case of maintenance, switchover, or failover, promote the standby cluster to active by changing the configuration. 

Previously, this action was fully manual. Now there is a high level Site Manager service which allows switching of the `active` and `standby` modes using the Postgres Service Site Manager.

**Note** In case of deployment in Active-Standby scheme it's possible to reach out PostgreSQL Leader instance via `pg-patroni-external.<POSTGRESQL_NAMESPACE>` service. Such service available on both sites in PostgreSQL Namespace.

**Note** For applications on both sites (Active and Standby) it's possible to connect to Active PostgreSQL by specifying the next PostgreSQL host: `pg-patroni-external.<POSTGRESQL_NAMESPACE>`. This service will always point to Active PostgreSQL.

**Note** Also, in case of using PostgreSQL DBaaS Adapter, you have to specify next parameter: `dbaas.pgHost` with value `pg-patroni-external.<POSTGRESQL_NAMESPACE>`.

# Prerequisites

* In case of two separate Postgres services already installed on two Kubernetes or OpenShift clusters (also can be deployed on different namespaces of the one cloud)
* Configuration below can be considered as additional part for [Installation Guide](../installation.md)
* **Openshift 4.X** Postgres Operator limits should be set to `limits.cpu=100m`  and `limits.memory=100Mi`.
* In case if `siteManager.httpAuth.enabled` is set to `true`, TokenReview rights should be granted to `postgres-sa` ServiceAccount in PostgreSQL Operator namespace.
* `siteManager.httpAuth.smNamespace` should be specified if custom name for site-manager NS is used.

# Active Postgres Service on cluster-1

PostgresService custom resource has to contain the siteManager configuration and `empty` standbyScluster configuration:

```yaml
# Patroni standby cluster configuration is empty
...
patroni:
  ...
#  standbyCluster:
#    host:
#    port: 
...

# Site Manager configuration
siteManager:
  install: true
  activeClusterHost: "pg-patroni.postgres-service.svc.cluster-2.local"
  activeClusterPort: 5432
...
```
 **Note** `activeClusterHost` and `activeClusterPort` have params of opposite cluster to set `active host` when current patroni cluster is in `standby` mode.  

# Standby Postgres Service on cluster-2

PostgresService custom resource has to contain the siteManager configuration and standbyScluster configuration:

```yaml
# Patroni standby cluster configuration
...
patroni:
  ...
  standbyCluster:
    host: pg-patroni.postgres-service.svc.cluster-1.local
    port: 5432
...

# Site Manager configuration
siteManager:
  install: true
  httpAuth:
    enabled: true
    smNamespace: "site-manager"
    smServiceAccountName: "sm-auth-sa"
  activeClusterHost: "pg-patroni.postgres-service.svc.cluster-1.local"
  activeClusterPort: 5432
...
```
**Note** `activeClusterHost` and `activeClusterPort` have params of opposite cluster to set `active host` when current patroni cluster is in `standby` mode.


## Rest API

All the PostgreSQL SM endpoints are secured via Kubernetes JWT Service Account Tokens. Token should be specified in Request Header.

### Get Status

Request:

```bash
curl -GET -H "Authorization: Bearer <TOKEN>" http://postgres-operator.{NAMESPACE}:8080/sitemanager
```

* "Authorization: Bearer" - header should be specified only in case `siteManager.httpAuth.enabled=true`.

`GET` `sitemanager` endpoint returns json with `mode` and it's `status` of current cluster

Response:

```json

{"mode": "active", "status": "done"}
```

* `mode`
    * `active` - Cluster is in the Active mode.
    * `standby` - Cluster is in the Standby mode.
    * `disabled` - Patroni Cluster disabled.
* `status`
    * `in progress` - Switch mode in progress.
    * `done` - Mode switched successfully.
    * `failed` - Switch mode failed.  


### Health

Request:

```bash
curl -GET -H "Authorization: Bearer <TOKEN>" http://postgres-operator.{NAMESPACE}:8080/health
```

* "Authorization: Bearer " - header should be specified only in case `siteManager.httpAuth.enabled=true`.

`GET` `sitemanager` endpoint returns json with `status` of current cluster.

Response:

```json
{"status": "up"}
```

* `status`
    * `up` - Postgres Service cluster is ready.
    * `down` - Almost all Postgres Service clusters are ready.
    * `degraded` - Postgres Service cluster is not ready.

### Switch Mode

Request:

```bash
curl -XPOST -d '{"mode": "active"}' -H "Authorization: Bearer <TOKEN>" -H "Content-Type: application/json" \
http://postgres-operator.{NAMESPACE}:8080/sitemanager
```

* "Authorization: Bearer " - header should be specified only in case `siteManager.httpAuth.enabled=true`.
* `mode`
  * `active` - Switch on Active mode.
  * `standby` - Switch on Standby mode.
  * `disabled` - Disable Patroni cluster.

Response:

```text
Successfully changed on active mode
```
