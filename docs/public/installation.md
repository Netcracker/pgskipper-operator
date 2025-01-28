* [Prerequisites](#prerequisites)
  * [Common](#common)
    * [Disaster Recovery](#disaster-recovery)
    * [Vault](#vault)
    * [Kubernetes](#kubernetes)
    * [Openshift](#openshift)
    * [AWS](#aws)
    * [Azure](#azure)
    * [Google Cloud](#google-cloud)
* [Best practices and recommendations](#best-practices-and-recommendations)
* [Parameters](#parameters)
  * [Postgres Parameters](#general-postgres-parameters)
  * [Patroni Services Parameters](#patroni-services-helm-chart-parameters-section)
* [Installation](#installation)
* [Upgrade](#upgrade)
  * [Postgres Operator Version Upgrade](#postgres-operator-version-upgrade)
  * [Major Upgrade](#major-upgrade-of-postgresql)
  * [HA to DR Upgrade](#ha-to-dr-upgrade)

# Prerequisites

## General Information  

This section provides information about the steps to install and configure a Postgres Service cluster on OpenShift/Kubernetes using Helm.
Postgres Service consists of two charts:

* `Postgres` contains Patroni, PGUpgrade and its own operator. Associated CRD is `PatroniCore`

* `Postgres Services` contains service operator and other supplementary services like Dbaas Adapter, Backup Daemon, Metric Collector, Postgres Exporter, Powa UI, PGBouncer. Associated CRD is `PostgresService`

Both charts can be installed separately, but `Postgres` should be always initially installed before `Postgres Services` ([excluding external managed Postgres scheme](#externaldatabase), where Postgres is not installed at all).

## Common
The prerequisites to deploy the Postgres Service are as follows:

* The project or namespace should be created.
* The Custom Resource Definition (CRD) should be created by the cloud administrator, if deploy user do not have rights for CRD creation role.
* If Dynamic Volume Provisioning is not available, all the persistence volumes for Patroni and Backup Daemon should be created manually, and NodeSelectors (that selects particular nodes) should be specified explicitly (or NodeAffinity are set on PVs).
* In case of using `cinder` as Dynamic Volume Provisioning, you have to set `patroni.securityContext.fsGroupChangePolicy` as `OnRootMismatch` in deploy parameter.
* In case of using `cinder` as Dynamic Volume Provisioning, you have to set both `backupDaemon.securityContext.runAsUser` and `backupDaemon.securityContext.fsGroup` in deploy parameter.

Postgres Service allows to generate certificates for connections with TLS/SSL protocol using integration with cert-manager.

In this case deployment user should have the rights for creating the `cert-manager.io/v1` objects.

In case of Prometheus Monitoring stack deployment, deploy user should have the rights for creating the `integreatly.org/v1alpha1` and `monitoring.coreos.com/v1` objects.

### Disaster Recovery

For more information about prerequisites for PostgreSQL in Disaster Recovery Scheme, please, follow:
[Disaster Recovery](/docs/public/features/disaster-recovery.md#prerequisites) section.

## Kubernetes

* If the Restricted Pod Security Policy as given in [https://kubernetes.io/docs/concepts/security/pod-security-standards/](https://kubernetes.io/docs/concepts/security/pod-security-standards/) is enabled on the Kubernetes (K8s) cluster, it is mandatory to set the `backupDaemon.securityContext.runAsUser` and `backupDaemon.securityContext.fsGroup` parameters.

## OpenShift

* The project must be created in advance.
* The deployment user must have the `admin` role in this project.
* In case of Custom PVs (Host Path PVs) as a storage, the project should be annotated with next command by administrator:

```
oc annotate --overwrite namespace postgres-service openshift.io/sa.scc.uid-range='100600/100600'
```

* Also, in case of Host Path PVs In case it necessary to set folder permission according to uid-range:

```
chown -R 100600.100600 /var/lib/origin/openshift.local.volumes/pv-postgres-{1,2};
chown -R 100600.100600 /var/lib/origin/openshift.local.volumes/pv-postgres-backup
```
* And change SELinux security context:

```
chcon -R unconfined_u:object_r:container_file_t:s0 /var/lib/origin/openshift.local.volumes/<PV-NAME>
```

**Note** If you are planning to set PostgreSQL `max_connections` parameter to higher then `1000` value, you have to create additional
`ContainerRuntimeConfig` object, with `pidsLimit` configuration.

Because default limits for pids for CRI-O is set to `1024`, please, see [GitHub Issue](https://github.com/cri-o/cri-o/issues/1921).

* Avoid using `securityContext` deployment parameters. It's not supported for Openshift 4.x.

**Configuring and Enabling default seccomp profile for all pods**

For Openshift version 4.8 and above OpenShift Container Platform ships with a default seccomp profile that is referenced as runtime/default. You can enable the default seccomp profile for a pod or container workload by creating a custom Security Context Constraint (SCC).

Follow these steps to enable the default seccomp profile for all pods:

1) Export the available restricted SCC to a yaml file:

```
$ oc get scc restricted -o yaml > restricted-seccomp.yaml
```

2) Edit the created restricted SCC yaml file:

```
$ vi restricted-seccomp.yaml
```

3) Update as shown in this example:

```
kind: SecurityContextConstraints
metadata:
  name: restricted  
.....
seccompProfiles:    
- runtime/default
```

**Note:** Do not edit the default SCCs. Editing the default SCCs can lead to issues when some of the platform pods deploy or OpenShift Container Platform is upgraded.

# Best practices and recommendations

For On-Premise PostgreSQL Pods CPU/RAM **roughly** calculated by next formula:

```
RAM_LIMIT = max_connections * 4MB

max_connections = #cores * 4
```

Please, note, that number of max_connections is also limited by Number of Cores.

In general, we do not recommend to set max_connections more than 1000.

In case if you are planning to have more than 1000 max_connections, please, consider using `pgbouncer` or use multiple PostgreSQL Clusters.

## HWE

### Small

Small means PostgreSQL `max_connections` equals to 250.

| Module            | CPU       | RAM, Gi | Storage, Gb |
|-------------------|-----------|---------|-------------|
| Patroni Cluster   | 0.5       | 1       | 50          |
| Backup Daemon     | 0.5       | 0.5     | 25          |
| DBaaS Adapter     | 0.2       | 0.06    | 0           |
| Monitoring Agent  | 0.25      | 0.5     | 0           |
| Postgres Exporter | 0.3       | 0.125   | 0           |
| **Total**         | **2.25**  | **3.5** | **125**     |

### Medium

Medium means PostgreSQL `max_connections` equals to 1000.

| Module            | CPU      | RAM, Gi | Storage, Gb |
|-------------------|----------|---------|-------------|
| Patroni Cluster   | 2        | 4       | 100         |
| Backup Daemon     | 1        | 1       | 50          |
| DBaaS Adapter     | 0.2      | 0.06    | 0           |
| Monitoring Agent  | 0.25     | 0.5     | 0           |
| Postgres Exporter | 0.3      | 0.125   | 0           |
| **Total**         | **6**    | **10**  | **250**     |

### Large

Large means PostgreSQL `max_connections` equals to 2000.

| Module            | CPU    | RAM, Gi  | Storage, Gb |
|-------------------|--------|----------|-------------|
| Patroni Cluster   | 4      | 8        | 200         |
| Backup Daemon     | 3      | 3        | 50          |
| DBaaS Adapter     | 0.2    | 0.06     | 0           |
| Monitoring Agent  | 0.25   | 0.5      | 0           |
| Postgres Exporter | 0.5    | 0.250    | 0           |
| **Total**         | **12** | **20**   | **450**     |

### Storage Recommendations

In general, it's not recommended to run PostgreSQL on any type of NFS.

Local SSD disks (or Local StorageClass) are the preferred option for Production Load.

To validate disk performance execute the next command in the Leader Patroni pod:

```bash
dd if=/dev/zero of=/var/lib/pgsql/data/postgresql_${POD_IDENTITY}/test bs=512 count=10000 oflag=direct
10000+0 records in
10000+0 records out
5120000 bytes (5.1 MB) copied, 6.14565 s, 797 kB/s
```

The value should be more than 500 kB/s.

# Parameters

For each Helm Chart parameters should be provided separately.  
This sections describes all possible deploy parameters per Helm Chart per component.

# Patroni-Core Helm chart parameters section

## General Postgres Parameters

The general parameters used for the configurations are specified below.

| Parameter             | Type   | Mandatory | Default value | Description                                                                            |
|-----------------------|--------|-----------|---------------|----------------------------------------------------------------------------------------|
| postgresUser          | string | no        | postgres      | Specifies the name of the database superuser.                                          |
| postgresPassword      | string | yes       | p@ssWOrD1      | Specifies the password for the database superuser.                                     |
| replicatorPassword    | string | no        | replicator      | Specifies the password for the database replicator.                                    |
| serviceAccount.create | bool   | no        | true          | Specifies whether a service account needs to be created.                               |
| serviceAccount.name   | string | no        | postgres-sa   | Specifies name of the Service Account under which Postgres Operator will work.         |
| runTestsOnly          | bool   | no        | false         | Indicates whether to run Integration Tests (skipping deploy step) only or not.         |
| affinity              | json   | no        | n/a           | Defines affinity scheduling rules for all components. Can be overridden per component. |
| podLabels             | yaml   | no        | n/a           | Specifies custom pod labels for all the components. Can be overridden per component.   |

**Note**: `postgresUser` is not the user which will be created during deployment. You should mention here the user which is already present with superuser role. If you need to use some other user instead of postgres, you should create the desired user manually with superuser role.

## operator

This sections describes all possible deploy parameters for PostgreSQL Operator.

| Parameter                                       | Type   | Mandatory | Default value | Description                                                                            |
|-------------------------------------------------|--------|-----------|---------------|----------------------------------------------------------------------------------------|
| operator.resources.requests.memory              | string | no        | 50Mi          | Specifies memory requests for Postgres Operator.                                       |
| operator.resources.requests.cpu                 | string | no        | 50m           | Specifies cpu requests for Postgres Operator.                                          |
| operator.resources.limits.memory                | string | no        | 50Mi          | Specifies memory limits for Postgres Operator.                                         |
| operator.resources.limits.cpu                   | string | no        | 50m           | Specifies cpu limits for Postgres Operator.                                            |
| operator.affinity                               | json   | no        | n/a           | Specifies the affinity scheduling rules.                                               |
| operator.podLabels                              | yaml   | no        | n/a           | Specifies custom pod labels for Postgres Operator.                                     |
| operator.waitTimeout                            | string | no        | 10            | Specifies the timeouts in minutes for Postgres Operator to wait for successful checks. |
| operator.reconcileRetries                       | string | no        | 3             | Specifies the number of retries in single reconcile loop for Postgres Operator.        |

## patroni

This sections describes all possible deploy parameters for Patroni component.

| Parameter                             | Type                                                                            | Mandatory | Default value                                                   | Description                                                                                                                 |
|---------------------------------------|---------------------------------------------------------------------------------|-----------|-----------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------|
| patroni.install                       | bool                                                                            | no        | true                                                            | Indicates whether to install Patroni component or not. Should be set to `no` in case of Managed DBs.                        |
| patroni.clusterName                   | string                                                                          | no        | patroni                                                         | Specifies Patroni cluster name..                                                                                            |
| patroni.resources.requests.memory     | string                                                                          | no        | 250Mi                                                           | Specifies memory requests.                                                                                                  |
| patroni.resources.requests.cpu        | string                                                                          | no        | 125m                                                            | Specifies cpu requests.                                                                                                     |
| patroni.resources.limits.memory       | string                                                                          | no        | 500Mi                                                           | Specifies memory limits.                                                                                                    |
| patroni.resources.limits.cpu          | string                                                                          | no        | 250m                                                            | Specifies cpu limits.                                                                                                       |
| patroni.resources.unlimited           | bool                                                                            | no        | false                                                           | Specifies if we should skip setting limits for Patroni.                                                                     |
| patroni.postgreSQLParams              | []string                                                                        | no        | [Default PostgreSQL parameters](#default-postgresql-parameters) | Specifies PostgreSQL parameters. Values should be specified as a string list of `key: value` parameters.                    |
| patroni.patroniParams                 | []string                                                                        | no        | n/a                                                             | Specifies Patroni configuration parameters. Values should be specified as a string list of `key: value` parameters.         |
| patroni.securityContext               | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a                                                             | Specifies pod level security attributes and common container settings.                                                      |
| patroni.standbyCluster.host           | string                                                                          | no        | n/a                                                             | Specifies host of active Postgresql cluster for Patroni standby cluster configuration.                                      |
| patroni.standbyCluster.port           | string                                                                          | no        | n/a                                                             | Specifies port of active Postgresql cluster for Patroni standby cluster configuration.                                      |
| patroni.enableShmVolume               | bool                                                                            | no        | true                                                            | Specifies should tmpfs mount for /dev/shm be used in Patroni pods.                                                          |
| patroni.powa.install                  | bool                                                                            | no        | true                                                            | Indicates whether to configure POWA for PostgreSQL or not.                                                                  |
| patroni.powa.password                 | string                                                                          | no        | Pow@pASsWORD                                                  | Specifies password for POWA user.                                                                                           |
| patroni.pgHba                         | []string                                                                        | no        | n/a                                                             | Specifies additional configuration in pg_hba.conf.                                                                          |
| patroni.ignoreSlots                   | bool                                                                            | no        | true                                                            | Indicates whether Patroni should ignore custom Replication Slots or not.                                                    |
| patroni.ignoreSlots.ignoreSlotsPrefix | string                                                                          | no        | "nc_"                                                           | Specifies prefix for ignore Replications slots.                                                                             |
| patroni.storage.type                  | string                                                                          | yes       | n/a                                                             | Specifies the storage type. The possible values are `pv` and `provisioned`.                                                 |
| patroni.storage.size                  | string                                                                          | yes       | n/a                                                             | Specifies size of Patroni PVCs.                                                                                             |
| patroni.storage.storageClass          | string                                                                          | no        | n/a                                                             | Specifies storageClass that will be used for Patroni PVCs. Should be specified only in case of `provisioned` storageClass.  |
| patroni.storage.nodes                 | []string                                                                        | no        | n/a                                                             | Specifies list of nodes to which Patroni pods will be scheduled.                                                            |
| patroni.storage.selectors             | []string                                                                        | no        | n/a                                                             | Specifies list of selector to choose PVCs.                                                                                  |
| patroni.storage.volumes               | []string                                                                        | no        | n/a                                                             | Specifies list of Persistence Volumes that will be used for PVCs.  Should be specified only in case of `pv` storageClass.   |
| patroni.pgWalStorage                  | Storage Group                                                                   | no        | n/a                                                             | Specifies set of storage parameters for separater volume for `pg_wal` directory. Parameters are the same as for `storage`.  |
| patroni.pgWalStorageAutoManage        | bool                                                                            | no        | n/a                                                             | Specifies is pg_wal files have to be moved to separate volume `pg_wal` directory automatically.                             |
| patroni.priorityClassName             | string                                                                          | no        | n/a                                                             | Specifies [Priority Class](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass). |
| patroni.affinity                      | json                                                                            | no        | n/a                                                             | Specifies the affinity scheduling rules.                                                                                    |
| patroni.podLabels                     | yaml                                                                            | no        | n/a                                                             | Specifies custom pod labels.                                                                                                |
| patroni.external.pvc                  | yaml                                                                            | no        | n/a                                                             | Specifies list of pvcs to mount them to patroni pods.                                                                       |
| patroni.external.pvc.name             | yaml                                                                            | no        | n/a                                                             | Specifies name of pvc to mount it to patroni pods.                                                                          |
| patroni.external.pvc.mountPath        | yaml                                                                            | no        | n/a                                                             | Specifies path on patroni pod for mounted pvc.                                                                              |

## majorUpgrade

Patroni Core Operator includes majorUpgrade procedure.

| Parameter                             | Type                                                                            | Mandatory | Default value                                                   | Description                                                                                                                 |
|---------------------------------------|---------------------------------------------------------------------------------|-----------|-----------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------|
| patroni.majorUpgrade.enabled          | bool                                                                            | no        | false                                                           | Indicates whether to run majorUpgrade procedure or not.                                                                     |
| patroni.majorUpgrade.initDbParams     | string                                                                          | no        | n/a                                                             | Specifies flags for [initdb command](https://www.postgresql.org/docs/current/app-initdb.html).                              |



## ldap

Patroni Core Operator allows integration of ldap with PostgreSQL. By default, registration disabled.

| Parameter                 | Type   | Mandatory | Default value              | Description                                                                                       |
|---------------------------|--------|-----------|----------------------------|---------------------------------------------------------------------------------------------------|
| ldap.enabled              | bool   | no        | false                      | Indicates that LDAP should be enabled or not.                                                     |
| ldap.server               | string | no        | ldap.example.com           | The hostname or IP address of your LDAP server (e.g., ldap.example.com).                          |
| ldap.port                 | int    | no        | 389                        | The port of your LDAP server. Default is 389.                                                     |
| ldap.basedn               | string | no        | dc=example,dc=com          | The base DN (Distinguished Name) under which the user accounts reside.                            |
| ldap.binddn               | string | no        | cn=admin,dc=example,dc=com | Specifies the bind DN used for querying LDAP.                                                     |
| ldap.bindpasswd           | string | no        | adminpassword              | Specifies the password for the bind DN.                                                           |
| ldap.ldapsearchattribute  | string | no        | sAMAccountName             | Specifies the LDAP attribute used to search for the user (commonly uid or sAMAccountName for AD). |

## tls

Patroni Core Operator allows configuration of TLS for PostgreSQL. By default, registration disabled.

| Parameter                                                      | Type     | Mandatory | Default value | Description                                                                                                                                                                                                                                                                                                                   |
|----------------------------------------------------------------|----------|-----------|---------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| tls.enabled                                                    | bool     | no        | false         | Indicates that TLS should be enabled or not.                                                                                                                                                                                                                                                                                  |
| tls.certificateSecretName                                      | string   | no        | pg-cert       | Specifies the name of secret with certificate in PostgreSQL namespace. See [TLS Configuration](/docs/public/features/tls-configuration.md)                                                                                                                  |
| tls.generateCerts.enabled                                      | bool     | yes       | false         | Specifies whether to generate SSL certificates by cert-manager or not. If `false` specified, follow [manual certificate configuration guid](/docs/public/features/tls-configuration.md#manual). |
| tls.generateCerts.duration                                     | int      | no        | 365           | Specifies SSL certificate validity duration in days. The default value is 365.                                                                                                                                                                                                                                                |
| tls.generateCerts.subjectAlternativeName.additionalDnsNames    | []string | no        | n/a           | Specifies the list of additional DNS names to be added to the "Subject Alternative Name" field of SSL certificate. If access to Postgres Service for external clients is enabled, DNS names from externalHostNames parameter must be specified in here.                                                                       |
| tls.generateCerts.subjectAlternativeName.additionalIpAddresses | []string | no        | n/a           | Specifies the list of additional IP addresses to be added to the "Subject Alternative Name" field of SSL certificate. If access to Postgres Service for external clients is enabled, IP addresses from externalHostNames parameter must be specified in here.                                                                 |
| tls.generateCerts.clusterIssuerName                            | string   | yes       | n/a           | Specifies name of `ClusterIssuer` resource. If the parameter is not set or empty, `Issuer` resource in current Kubernetes namespace will be used.                                                                                                                                                                             |
| tls.certificates.tls_crt                                       | string   | no        | ""            | Specifies the certificate in BASE64 format. It is required if tls.enabled is true and tls.generateCerts.enabled is false. This allows user to specify their own certificate.                                                                                                                                                  |
| tls.certificates.tls_key                                       | string   | no        | ""            | Specifies the private key in BASE64 format. It is required if tls.enabled is true and tls.generateCerts.enabled is false. This allows user to specify their own key.                                                                                                                                                          |
| tls.certificates.ca_crt                                        | string   | no        | ""            | Specifies base 64 encoded CA certificate. It is required if tls.enabled is true and tls.generateCerts.enabled is false. This allows user to specify their own ca certificate.                                                                                                                                                 |                                                                                                                                                                                                                                                                


## pgBackRest

Patroni Core Operator allows configuration of TLS for PostgreSQL. By default, registration disabled.

| Parameter                    | Type     | Mandatory | Default value       | Description                                                                                                                   |
|------------------------------|----------|-----------|---------------------|-------------------------------------------------------------------------------------------------------------------------------|
| pgBackRest.repoType          | string   | yes       | rwx                 | Specifies type of the storage for backups. Allowed options are `s3` or `rwx`.                                                 |
| pgBackRest.repoPath          | string   | yes       | /var/lib/pgbackrest | Specifies folder to store backups.                                                                                            |
| pgBackRest.diffSchedule      | string   | yes       | 30 0/1 * * *        | Specifies schedule of the differential backups in cron format.                                                                |
| pgBackRest.incrSchedule      | string   | yes       | 30 0/1 * * *        | Specifies schedule of the incremental backups in cron format.                                                                 |
| pgBackRest.rwx.type          | string   | no        | n/a                 | Specifies the storage type. The possible values are `pv` and `provisioned`.                                                   |
| pgBackRest.rwx.size          | string   | no        | n/a                 | Specifies size of pgBackRest PVCs.                                                                                            |
| pgBackRest.rwx.storageClass  | string   | no        | n/a                 | Specifies storageClass that will be used for pgBackRest PVCs. Should be specified only in case of `provisioned` storageClass. |
| pgBackRest.rwx.volumes       | []string | no        | n/a                 | Specifies list of Persistence Volumes that will be used for PVCs.  Should be specified only in case of `pv` storageClass.     |
| pgBackRest.s3.bucket         | string   | no        | n/a                 | Specifies name of the bucket in s3.                                                                                           |
| pgBackRest.s3.endpoint       | string   | no        | n/a                 | Specifies link to the s3 server.                                                                                              |
| pgBackRest.s3.key            | string   | no        | n/a                 | Specifies key of the s3 storage to login.                                                                                     |
| pgBackRest.s3.secret         | string   | no        | n/a                 | Specifies secret of the s3 storage to login.                                                                                  |
| pgBackRest.s3.region         | string   | no        | n/a                 | Specifies region of the s3 storage.                                                                                           |
| pgBackRest.s3.verifySsl      | bool     | no        | n/a                 | Specifies do the pgBackRest verify secure connection to the s3, or not. Possible value true or false.                         |




# Patroni-Services Helm chart parameters section

## General Postgres Parameters

The general parameters used for the configurations are specified below.

| Parameter              | Type   | Mandatory | Default value | Description                                                                            |
|------------------------|--------|-----------|---------------|----------------------------------------------------------------------------------------|
| postgresUser           | string | no        | postgres      | Specifies the name of the database superuser.                                          |
| postgresPassword       | string | yes       | p@ssWOrD1      | Specifies the password for the database superuser.                                     |
| replicatorPassword     | string | no        | replicator      | Specifies the password for the database replicator.                                    |
| serviceAccount.create  | bool   | no        | true          | Specifies whether a service account needs to be created.                               |
| serviceAccount.name    | string | no        | postgres-sa   | Specifies name of the Service Account under which Postgres Operator will work.         |
| runTestsOnly           | bool   | no        | false         | Indicates whether to run Integration Tests (skipping deploy step) only or not.         |
| affinity               | json   | no        | n/a           | Defines affinity scheduling rules for all components. Can be overridden per component. |
| podLabels              | yaml   | no        | n/a           | Specifies custom pod labels for all the components. Can be overridden per component.   |

**Note**: `postgresUser` is not the user which will be created during deployment. You should mention here the user which is already present with superuser role. If you need to use some other user instead of postgres, you should create the desired user manually with superuser role.

## operator

This sections describes all possible deploy parameters for PostgreSQL Operator.

| Parameter                                       | Type   | Mandatory | Default value | Description                                                                            |
|-------------------------------------------------|--------|-----------|---------------|----------------------------------------------------------------------------------------|
| operator.resources.requests.memory              | string | no        | 50Mi          | Specifies memory requests for Postgres Operator.                                       |
| operator.resources.requests.cpu                 | string | no        | 50m           | Specifies cpu requests for Postgres Operator.                                          |
| operator.resources.limits.memory                | string | no        | 50Mi          | Specifies memory limits for Postgres Operator.                                         |
| operator.resources.limits.cpu                   | string | no        | 50m           | Specifies cpu limits for Postgres Operator.                                            |
| operator.affinity                               | json   | no        | n/a           | Specifies the affinity scheduling rules.                                               |
| operator.podLabels                              | yaml   | no        | n/a           | Specifies custom pod labels for Postgres Operator.                                     |
| operator.waitTimeout                            | string | no        | 10            | Specifies the timeouts in minutes for Postgres Operator to wait for successful checks. |
| operator.reconcileRetries                       | string | no        | 3             | Specifies the number of retries in single reconcile loop for Postgres Operator.        |

## patroni

This sections describes all possible deploy parameters for Patroni component.

| Parameter                             | Type                                                                            | Mandatory | Default value                                                   | Description                                                                                                                 |
|---------------------------------------|---------------------------------------------------------------------------------|-----------|-----------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------|
| patroni.clusterName                   | string                                                                          | no        | patroni                                                         | Specifies Patroni cluster name..                                                                                            |

## backupDaemon

This sections describes all possible deploy parameters for PostgreSQL Backup Daemon component.

| Parameter                              | Type                                                                            | Mandatory | Default value | Description                                                                                                                                                                                                                                                                                            |
|----------------------------------------|---------------------------------------------------------------------------------|-----------|---------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| backupDaemon.install                   | bool                                                                            | no        | true          | Indicates whether to install PostgreSQL Backup Daemon component or not.                                                                                                                                                                                                                                |
| backupDaemon.resources.requests.memory | string                                                                          | no        | 256Mi         | Specifies memory requests.                                                                                                                                                                                                                                                                             |
| backupDaemon.resources.requests.cpu    | string                                                                          | no        | 100m          | Specifies cpu requests.                                                                                                                                                                                                                                                                                |
| backupDaemon.resources.limits.memory   | string                                                                          | no        | 512Mi         | Specifies memory limits.                                                                                                                                                                                                                                                                               |
| backupDaemon.resources.limits.cpu      | string                                                                          | no        | 250m          | Specifies cpu limits.                                                                                                                                                                                                                                                                                  |
| backupDaemon.securityContext           | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a           | Specifies pod level security attributes and common container settings.                                                                                                                                                                                                                                 |
| backupDaemon.pgHost                    | string                                                                          | no        | pg-patroni    | Specifies PostgreSQL host.                                                                                                                                                                                                                                                                             |
| backupDaemon.walArchiving              | bool                                                                            | no        | false         | Indicates whether to save WALs files in PostgreSQL Backup Daemon. This setting can cause major disk usage impact, because each postgres WAL file size is 16MB. Also, please, note, that in case of enabled `walArchiving` memory limits for PostgreSQL Backup Daemon should be set as `1 Gib` minimal. |
| backupDaemon.backupSchedule            | string                                                                          | no        | `0 0/7 * * *` | Specifies schedule for full backups in cron format. It's also possible to set schedule as `None`, in such case scheduling will be turned off.                                                                                                                                                          |
| backupDaemon.granularBackupSchedule    | string                                                                          | no        | n/a           | Specifies Schedule for granular backups in cron format.                                                                                                                                                                                                                                                |
| backupDaemon.databasesToSchedule       | string                                                                          | no        | n/a           | Specifies database list for scheduled granular backup. By default backup is performed for all databases, except protected.                                                                                                                                                                             |
| backupDaemon.granularEviction          | string                                                                          | no        | "3600"        | Specifies scheduling  the sweeping process for granular backups in seconds.                                                                                                                                                                                                                            |
| backupDaemon.jobFlag                   | string                                                                          | no        | "1"           | Specifies ability to enable parallel backup and restore via postgres-backup-daemon. This feature is only applicable for granular backups.                                                                                                                                                              |
| backupDaemon.allowPrefix               | bool                                                                            | no        | false         | Specifies ability to add prefix to the backup names of granular backups. Prefix to be provided from user's end via backup request. If no prefix is provided and this flag 'allowPrefix' is enabled, then by default namespace would be added as prefix to the backup names.                            |
| backupDaemon.evictionPolicy            | string                                                                          | no        | `7d/delete`   | The eviction policy for full backups: period and action.                                                                                                                                                                                                                                               |
| backupDaemon.evictionBinaryPolicy      | string                                                                          | no        | `7d/delete`   | The eviction policy for granular backups: period and action.                                                                                                                                                                                                                                           |
| backupDaemon.archiveEvictionPolicy     | string                                                                          | no        | `7d`          | The eviction policy for wal files in case of Point In Time Recovery. Can be set as a time interval (min\h\d) or size (MB\GB). The limit is "soft," so the archive can keep files a little longer or take a little more space.                                                                          |
| backupDaemon.storage.type              | string                                                                          | yes       | n/a           | Specifies the storage type. The possible values are `pv` and `provisioned` and `s3`.                                                                                                                                                                                                                   |
| backupDaemon.storage.size              | string                                                                          | yes       | n/a           | Specifies size of PostgreSQL Backup Daemon PVC.                                                                                                                                                                                                                                                        |
| backupDaemon.storage.storageClass      | string                                                                          | no        | n/a           | Specifies storageClass that will be used for PostgreSQL Backup Daemon PVC. Should be specified only in case of `provisioned` storageClass.                                                                                                                                                             |
| backupDaemon.storage.nodes             | []string                                                                        | no        | n/a           | Specifies list of nodes to which Patroni pods will be scheduled.                                                                                                                                                                                                                                       |
| backupDaemon.storage.selectors         | []string                                                                        | no        | n/a           | Specifies list of selector to choose PVCs.                                                                                                                                                                                                                                                             |
| backupDaemon.storage.volumes           | []string                                                                        | no        | n/a           | Specifies list of Persistence Volumes that will be used for PVCs. Should be specified only in case of `pv` storageClass.                                                                                                                                                                               |
| backupDaemon.storage.accessMode        | []string                                                                        | no        | n/a           | Specifies list of [Access Modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes) that will be used for PVCs.                                                                                                                                                             |
| backupDaemon.s3Storage.url             | string                                                                          | no        | n/a           | Specifies url address to S3 storage.                                                                                                                                                                                                                                                                   |
| backupDaemon.s3Storage.accessKeyId     | string                                                                          | no        | n/a           | Specifies S3 accessKeyId credential.                                                                                                                                                                                                                                                                   |
| backupDaemon.s3Storage.secretAccessKey | string                                                                          | no        | n/a           | Specifies S3 secretAccessKey credential.                                                                                                                                                                                                                                                               |
| backupDaemon.s3Storage.bucket          | string                                                                          | no        | n/a           | Specifies name of S3 Bucket.                                                                                                                                                                                                                                                                           |
| backupDaemon.s3Storage.prefix          | string                                                                          | no        | postgres      | Specifies name of sub-directory before common backup path inside bucket.                                                                                                                                                                                                                               |
| backupDaemon.s3Storage.untrustedCert   | string                                                                          | no        | true          | Specifies whether or not to verify SSL certificates. By default SSL certificates are verified.                                                                                                                                                                                                         |
| backupDaemon.s3Storage.region          | string                                                                          | no        | n/a           | Specifies the name of the region associated with the client.                                                                                                                                                                                                                                           |
| backupDaemon.externalPv.name           | string                                                                          | no        | n/a           | Specifies the name of External PV.                                                                                                                                                                                                                                                                     |
| backupDaemon.externalPv.capacity       | string                                                                          | no        | n/a           | Specifies capacity of External PV.                                                                                                                                                                                                                                                                     |
| backupDaemon.externalPv.storageClass   | string                                                                          | no        | n/a           | Specifies StorageClass of External PV.                                                                                                                                                                                                                                                                 |
| backupDaemon.priorityClassName         | string                                                                          | no        | n/a           | Specifies [Priority Class](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass).                                                                                                                                                                            |
| backupDaemon.affinity                  | json                                                                            | no        | n/a           | Specifies the affinity scheduling rules.                                                                                                                                                                                                                                                               |
| backupDaemon.podLabels                 | yaml                                                                            | no        | n/a           | Specifies custom pod labels.                                                                                                                                                                                                                                                                           |

## metricCollector

This sections describes all possible deploy parameters for PostgreSQL Metric Collector component.

| Parameter                                                                  | Type                                                                            | Mandatory | Default value | Description                                                                                                                                                                                |
|----------------------------------------------------------------------------|---------------------------------------------------------------------------------|-----------|---------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| metricCollector.install                                                    | bool                                                                            | no        | true          | Indicates whether to install PostgreSQL Metric Collector component or not.                                                                                                                 |
| metricCollector.resources.requests.memory                                  | string                                                                          | no        | 128Mi         | Specifies memory requests.                                                                                                                                                                 |
| metricCollector.resources.requests.cpu                                     | string                                                                          | no        | 150m          | Specifies cpu requests.                                                                                                                                                                    |
| metricCollector.resources.limits.memory                                    | string                                                                          | no        | 256Mi         | Specifies memory limits.                                                                                                                                                                   |
| metricCollector.resources.limits.cpu                                       | string                                                                          | no        | 300m          | Specifies cpu limits.                                                                                                                                                                      |
| metricCollector.securityContext                                            | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a           | Specifies pod level security attributes and common container settings.                                                                                                                     |
| metricCollector.collectionInterval                                         | int                                                                             | no        | 60            | Specifies interval in seconds to execute Telegraf's plugins.                                                                                                                               |
| metricCollector.scrapeTimeout                                              | int                                                                             | no        | 20            | Specifies timeout in seconds to wait metric be gathered.                                                                                                                                   |
| metricCollector.telegrafPluginTimeout                                      | int                                                                             | no        | 60            | Specifies timeout in seconds to execute Telegraf's plugins.                                                                                                                                |
| metricCollector.userPassword                                                  | yaml                                                                            | no        | n/a           | Specifies the password for metric collector user.                                                                                                                                                               |
| metricCollector.ocExecTimeout                                              | int                                                                             | no        | 10            | Specifies timeout in seconds to execute `exec` commands.                                                                                                                                   |
| metricCollector.devMetricsInterval                                         | int                                                                             | no        | 10            | Specifies interval in minutes to execute Telegraf's plugins for additional metrics.                                                                                                        |
| metricCollector.devMetricsTimeout                                          | int                                                                             | no        | 10            | Timeout in minutes to execute command for additional metrics.                                                                                                                              |
| metricCollector.metricsProfile                                             | string                                                                          | no        | prod          | Specifies profile for the metrics collection. The possible values are `prod` and `dev`. For a `dev` profile, additional performance metrics such as queries stat, tables stat are collected. |
| metricCollector.prometheusMonitoring                                       | bool                                                                            | no        | false         | Indicates whether to apply Prometheus Monitoring or not.                                                                                                                                   |
| metricCollector.applyGrafanaDashboard                                      | bool                                                                            | no        | false         | Indicates whether to apply Grafana Dashboards or not.                                                                                                                                      |
| metricCollector.prometheusRules.backupAlertThreshold                       | int                                                                             | no        | 5             | Specifies threshold in % for backup storage size for alerting.                                                                                                                             |
| metricCollector.prometheusRules.backupWarningThreshold                     | int                                                                             | no        | 20            | Specifies threshold for backup storage size for warning.                                                                                                                                   |
| metricCollector.prometheusRules.alertDelay                                 | string                                                                          | no        | 3m            | Specifies alert delay to decrease false positive cases.                                                                                                                                    |
| metricCollector.prometheusRules.maxLastBackupAge                           | int                                                                             | no        | 86400         | Specifies maximum possible postgres backup age.                                                                                                                                            |
| metricCollector.prometheusRules.locksThreshold                             | int                                                                             | no        | 500           | Specifies PostgreSQL locks limit after which alert is triggered.                                                                                                                           |
| metricCollector.prometheusRules.queryMaxTimeThreshold                      | int                                                                             | no        | 3600          | Specifies PostgreSQL query execution time limit after which alert is triggered.                                                                                                            |
| metricCollector.prometheusRules.replicationLagValue                        | int                                                                             | no        | 33554432      | Specifies the value of replication lag in bytes after which alert should be triggered.                                                                                                     |
| metricCollector.prometheusRules.maxConnectionExceedPercentageThreshold     | int                                                                             | no        | 90            | Specifies the value of exceed max_connection percentage threshold. Value can be set from 0 to 100.                                                                                         |
| metricCollector.prometheusRules.maxConnectionReachedPercentageThreshold    | int                                                                             | no        | 80            | Specifies the value of reached max_connection percentage threshold. Value can be set from 0 to 100.                                                                                        |
| metricCollector.priorityClassName                                          | string                                                                          | no        | n/a           | Specifies [Priority Class](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass).                                                                |
| metricCollector.affinity                                                   | json                                                                            | no        | n/a           | Specifies the affinity scheduling rules.                                                                                                                                                   |
| metricCollector.podLabels                                                  | yaml                                                                            | no        | n/a           | Specifies custom pod labels.                                                                                                                                                               |

## dbaas

This sections describes all possible deploy parameters for PostgreSQL DBaaS Adapter component.

| Parameter                                   | Type              | Mandatory | Default value                           | Description                                                                                                                                                          |
|---------------------------------------------|-------------------|-----------|-----------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| dbaas.install                               | bool              | no        | true                                    | Indicates whether to install PostgreSQL DBaaS Adapter component or not.                                                                                              |
| dbaas.resources.requests.memory             | string            | no        | 64Mi                                    | Specifies memory requests.                                                                                                                                           |
| dbaas.resources.requests.cpu                | string            | no        | 200m                                    | Specifies cpu requests.                                                                                                                                              |
| dbaas.resources.limits.memory               | string            | no        | 64Mi                                    | Specifies memory limits.                                                                                                                                             |
| dbaas.resources.limits.cpu                  | string            | no        | 200m                                    | Specifies cpu limits.                                                                                                                                                |
| dbaas.pgHost                                | string            | no        | pg-patroni.<postgres-ns>                | Specifies PostgreSQL host..                                                                                                                                          |
| dbaas.pgPort                                | string            | no        | 5432                                    | Specifies port for connection to PostgreSQL.                                                                                                                         |
| dbaas.dbName                                | string            | no        | postgres                                | Specifies name of PostgreSQL database to connect by default.                                                                                                         |
| dbaas.labels                                | map[string]string | no        | n/a                                     | Specifies the set of Custom Labels for PostgreSQL DBaaS physical databases.                                                                                          |
| dbaas.aggregator.registrationAddress        | string            | no        | http://dbaas-aggregator.dbaas:8080      | Specifies the address of the Aggregator, where the Adapter registers its physical database cluster.                                                                  |
| dbaas.aggregator.registrationUsername       | string            | yes       | user                             | Specifies the username for registration in DBaaS Aggregator.                                                                                                         |
| dbaas.aggregator.registrationPassword       | string            | yes       | p@ssWOrD                             | Password for database registration.                                                                                                                                  |
| dbaas.aggregator.physicalDatabaseIdentifier | string            | no        | <postgres-ns>:postgres                  | Specifies the database identifier in DBaaS Aggregator.                                                                                                               |
| dbaas.adapter.username                      | string            | no        | dbaas-aggregator                        | Specifies the username for DBaaS Postgres Adapter basic authentication.                                                                                              |
| dbaas.adapter.password                      | string            | no        | dbaas-aggregator                        | Specifies the password for DBaaS Postgres Adapter basic authentication.                                                                                              |
| dbaas.adapter.address                       | string            | no        | http://dbaas-adapter.<postgres-ns>:8080 | Specifies the address of DBaaS Adapter during registration in Aggregator.                                                                                            |
| dbaas.vaultIntegration.enabled              | bool              | no        | false                                   | Indicates whether to enable integration with Vault.                                                                                                                  |
| dbaas.vaultIntegration.rotationPeriod       | string            | no        | 86400                                   | Specifies the DB password rotation period in seconds.                                                                                                                |
| dbaas.extensions                            | []string          | no        | n/a                                     | Specifies the list of default extensions for created databases.                                                                                                      |
| dbaas.updateExtensions                      | bool              | no        | false                                   | Specifies if default extensions should be created for existing databases.                                                                                            |
| dbaas.apiVersion                            | string            | no        | v2                                      | Specifies the version of DBaaS API.                                                                                                                                  |
| dbaas.multiUsers                            | bool              | no        | true                                   | Specifies if Multi Users functionality is enabled. |
| dbaas.priorityClassName                     | string            | no        | n/a                                     | Specifies [Priority Class](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass).                                          |
| dbaas.affinity                              | json              | no        | n/a                                     | Specifies the affinity scheduling rules.                                                                                                                             |
| dbaas.podLabels                             | yaml              | no        | n/a                                     | Specifies custom pod labels.                                                                                                                                         |
| dbaas.debug                                 | bool              | no        | false                                   | Specifies if debug logs are enabled.                                                                                                                                 |
| INTERNAL_TLS_ENABLED                        | bool              | no        | false                                   | Specifies if HTTPS should be enabled for DBaaS Adapter Endpoints and specification of certificates in requests to DBaaS Aggregator.                                  |

**Note :** In case of DR Mode PG Host for dbaas should be external.
```yaml
dbaas:
  pgHost: pg-patroni-external.<PG namespace>
```
For more info, please visit the following article regarding disaster recovery - [Disaster Recovery](/docs/public/features/disaster-recovery.md)

## siteManager

This sections describes all possible deploy parameters for Site Manager API in PostgreSQL Operator.

| Parameter                                                 | Type   | Mandatory | Default value                           | Description                                                                                                             |
|-----------------------------------------------------------|--------|-----------|-----------------------------------------|-------------------------------------------------------------------------------------------------------------------------|
| siteManager.install                                       | bool   | no        | true                                    | Indicates whether to enable Site Manager API and create SiteManager Kubernetes Objects.                                 |
| siteManager.installSiteManagerCR                          | bool   | no        | true                                    | Site Manager CR installation flag. Use false for install to an environment without siteManager(kind "SiteManager")      |
| siteManager.activeClusterHost                             | string | yes       | pg-patroni.postgres.svc.cluster-1.local | Specifies the host of the opposite patroni cluster in the DR schema.                                                    |
| siteManager.activeClusterPort                             | string | no        | 5432                                    | Specifies the port of the opposite patroni cluster in the DR schema.                                                    |
| siteManager.httpAuth.enabled                              | bool   | yes       | no                                      | Indicates whether to enable authentication of HTTP endpoints.                                                           |
| siteManager.httpAuth.smNamespace                          | string | no        | site-manager-auth                       | Specifies the name of Kubernetes Namespace from which API calls to Postgres Operator will be done.                      |
| siteManager.httpAuth.smServiceAccountName                 | string | no        | ""                                      | Specifies the name of Kubernetes Service Account under which API calls to PostgreSQL SiteManager will be done.          |
| siteManager.httpAuth.smSecureAuth                         | bool   | no        | false                                   | Specifies whether the `smSecureAuth` mode is enabled for Site Manager or not.                                           |
| siteManager.httpAuth.customAudience                       | string | no        | sm-services                             | The name of Kubernetes Service Account under which the site manager API calls are done.                                 |
| siteManager.standbyClusterHealthCheck.retriesLimit        | int    | no        | 3                                       | Specifies the number of retries for patroni cluster health check after standby mode reconciliation is done.             |
| siteManager.standbyClusterHealthCheck.failureRetriesLimit | int    | no        | 5                                       | Specifies the number of failureRetriesLimit for patroni cluster health check after standby mode reconciliation is done. |
| siteManager.standbyClusterHealthCheck.retriesWaitTimeout  | int    | no        | 30                                      | Specifies time interval between retries of health check in seconds.                                                     |

## tests

This sections describes all possible deploy parameters to run Integration Tests during Installation or Upgrade.

| Parameter              | Type                | Mandatory | Default value | Description                                                                 |
|------------------------|---------------------|-----------|---------------|-----------------------------------------------------------------------------|
| tests.install          | bool                | no        | true          | Indicates whether to run Integrations Tests or not.                         |
| tests.runTestScenarios | string              | no        | basic         | Specifies tests level runs. One of 'full', 'basic' or one of testScenarios. |
| tests.testScenarios    | map[string][]string | no        | n/a           | Specifies list of test tags to run.                                         |

## consulRegistration

Postgres Operator allows register of PostgreSQL connection properties in Consul. By default, registration disabled.

| Parameter                          | Type              | Mandatory | Default value | Description                                                           |
|------------------------------------|-------------------|-----------|---------------|-----------------------------------------------------------------------|
| consulRegistration.host            | string            | yes       | n/a           | Specifies Consul Host.                                                |
| consulRegistration.serviceName     | string            | yes       | n/a           | Specifies the desired name which will be used for Postgres in Consul. |
| consulRegistration.tags            | []string          | yes       | n/a           | Specifies tags for Postgres in Consul.                                |
| consulRegistration.meta            | map[string]string | yes       | n/a           | Specifies meta for Postgres in Consul.                                |
| consulRegistration.leaderTags      | []string          | yes       | n/a           | Specifies tags for Leader instance of Postgres in Consul.             |
| consulRegistration.leaderMeta      | map[string]string | yes       | n/a           | Specifies meta for Leader instance of Postgres in Consul.             |
| consulRegistration.checkInterval   | string            | yes       | n/a           | Specifies checkInterval for Consul ServiceCheck.                      |
| consulRegistration.checkTimeout    | string            | yes       | n/a           | Specifies checkTimeout for Consul ServiceCheck.                       |
| consulRegistration.deregisterAfter | string            | yes       | n/a           | Specifies after which time Service will be de-registered from Consul. |


## vaultRegistration

Postgres Operator allows store all Postgres Service credentials in Vault. By default, registration disabled.

| Parameter                                        | Type   | Mandatory | Default value    | Description                                                                                               |
|--------------------------------------------------|--------|-----------|------------------|-----------------------------------------------------------------------------------------------------------|
| vaultRegistration.enabled                        | bool   | no        | false            | Indicates that Vault will be used as storage credentials.                                                 |
| vaultRegistration.path                           | string | no        | <postgres-ns> | Specifies Vault path to key-value storage.                                                                |
| vaultRegistration.url                            | string | yes       | n/a              | Specifies url address to Vault service.                                                                   |
| vaultRegistration.token                          | string | yes       | n/a              | Specifies token to Vault service.                                                                         |
| vaultRegistration.paasPlatform                   | string | no        | kubernetes       | Specifies platform type.                                                                                  |
| vaultRegistration.dbEngine.enabled               | bool   | yes       | false            | Indicates that PostgreSQL engine enabled or not.                                                          |
| vaultRegistration.dbEngine.name                  | string | yes       | postgresql       | Specifies path name to Vault database Engine.                                                             |
| vaultRegistration.dbEngine.maxOpenConnections    | int    | no        | 5                | Specifies the maximum number of open connections to the database.                                         |
| vaultRegistration.dbEngine.maxIdleConnections    | int    | no        | 5                | Specifies the maximum number of idle connections to the database.                                         |
| vaultRegistration.dbEngine.maxConnectionLifetime | string | no        | 5s               | Specifies the maximum amount of time a connection may be reused. If <= 0s connections are reused forever. |

## externalDataBase

It's possible to use Postgres Operator with Managed DBs (Google CloudSQL, Azure Flexible PostgreSQL, Amazon Aurora PostgreSQL).

| Parameter                              | Type              | Mandatory | Default value | Description                                                                                              |
|----------------------------------------|-------------------|-----------|---------------|----------------------------------------------------------------------------------------------------------|
| externalDataBase.type                  | string            | yes       | n/a           | Specifies the type of the external DB. The possible values are `cloudsql`, `rds` and `azure`.            |
| externalDataBase.project               | string            | yes       | n/a           | Specifies the project name where the external DB is located.                                             |
| externalDataBase.instance              | string            | yes       | n/a           | Specifies the instance name of the external DB.                                                          |
| externalDataBase.port                  | string            | yes       | n/a           | Specifies the port of the external DB.                                                                   |
| externalDataBase.region                | string            | yes       | n/a           | Specifies the region of the external DB.                                                                 |
| externalDataBase.connectionName        | string            | yes       | n/a           | Specifies the connection name of the external DB.                                                        |
| externalDataBase.authSecretName        | string            | yes       | n/a           | Specifies the name of the Kubernetes Secret in which the configuration file for API accessing is stored. |
| externalDataBase.applyGrafanaDashboard | bool              | yes       | n/a           | Indicates whether to create Grafana Dashboard for Managed DB or not.                                     |
| externalDataBase.secret.create         | bool              | no        | n/a           | Specifies if secret with credentials should be created.                                                  |
| externalDataBase.secret.secretContents | map[string]string | no        | n/a           | Specifies the content of created secret.                                                                 |
| externalDataBase.accessKeyId           | string            | no        | n/a           | Specifies AWS accessKeyId credential.                                                                    |
| externalDataBase.secretAccessKey       | string            | no        | n/a           | Specifies AWS secretAccessKey credential.                                                                |
| externalDataBase.restoreConfig         | map[string]string | no        | n/a           | Specifies restoreConfig for external Database.                                                           |

## externalDataBase.restoreConfig

It's possible to specify additional configuration parameters for restore procedure of External Databases. 

As for now only Azure Database for PostgreSQL - Flexible Server supported.

| Parameter                        | Type   | Mandatory | Default value | Description                                                                              |
|----------------------------------|--------|-----------|---------------|------------------------------------------------------------------------------------------|
| externalDataBase.`mirror.subnet` | string | no        | n/a           | Specifies subnet override for newly restored instances. Usually used for mirror restore. |

An example of parameter:

```yaml
externalDataBase:
  restoreConfig:
    mirror.subnet: "test/subnet"
```

## queryExporter

This sections describes all possible deploy parameters for Query Exporter component.

| Parameter                                                           | Type                                                                            | Mandatory | Default value                                      | Description                                                                                                                                                                                                     |
|---------------------------------------------------------------------|---------------------------------------------------------------------------------|-----------|----------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| queryExporter.install                                            | bool                                                                            | no        | false                                              | Indicates that Query Exporter should be installed or not.                                                                                                                                                    |
| queryExporter.securityContext                                    | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a                                                | Specifies pod level security attributes and common container settings.                                                                                                                                          |
| queryExporter.resources.requests.memory                          | string                                                                          | no        | 128Mi                                              | Specifies memory requests.                                                                                                                                                                                      |
| queryExporter.resources.requests.cpu                             | string                                                                          | no        | 150m                                               | Specifies cpu requests.                                                                                                                                                                                         |
| queryExporter.resources.limits.memory                            | string                                                                          | no        | 128Mi                                              | Specifies memory limits.                                                                                                                                                                                        |
| queryExporter.resources.limits.cpu                               | string                                                                          | no        | 300m                                               | Specifies cpu limits.                                                                                                                                                                                           |
| queryExporter.scrapeTimeout                                      | int                                                                             | no        | 10                                                 | Specifies the timeout in seconds after which the scrape is ended.                                                                                                                                               |
| queryExporter.queryTimeout                                       | int                                                                             | no        | 30                                                 | Specifies the timeout in seconds for single query execution.                                                                                                                                                    |
| queryExporter.affinity                                           | json                                                                            | no        | n/a                                                | Specifies the affinity scheduling rules.                                                                                                                                                                        |
| queryExporter.podLabels                                          | yaml                                                                            | no        | n/a                                                | Specifies custom pod labels.                                                                                                                                                                                    |
| queryExporter.pgUser                                             | string                                                                          | no        | query-exporter                                  | Specifies name of user to create for postgres exporter.                                                                                                                                                         |
| queryExporter.pgPassword                                         | string                                                                          | no        | PaSsw0rDfoRExporT3r                                | Specifies password for postgres exporter user.                                                                                                                                                                  |
| queryExporter.maxMasterConnections                                  | int                                                                          | no        | 10                                                 | Specifies the number of simultaneous connections for master database.                                                                                                                                                        |
| queryExporter.maxLogicalConnections                                  | int                                                                          | no        | 1                                                 | Specifies the number of simultaneous connections for non-master databases.                                                                                                                                                        |
| queryExporter.selfMonitorDisabled                                | bool                                                                          | no        | false                                              | Specifies if self monitor metrics is disabled.                                                                                                                                                   | queryExporter.customQueries.enabled                              | bool                                                                            | no        | false                                              | Specifies the Query Exporter custom queries feature. [Custom queries watcher](/docs/public/features/query-exporter.md#custom-queries).                                                                                 |
| queryExporter.customQueries.namespacesList                       | []string                                                                        | no        | n/a                                                | Specifies the list of Namespaces for Query Exporter query watcher.                                                                                                                                           |
| queryExporter.customQueries.labels                               | map[string]string                                                               | no        | n/a                                                | Specifies the map of labels for config maps for watching.                                                                                                                                    |
| queryExporter.excludeQueries                                     | []string                                                                          | no        | n/a                                                | Specifies query list for exclusion from queries list.                                                                                                                                                             | queryExporter.collectionInterval                     | int                                                                               | no        | 60            | Specifies default interval in seconds to execute queries.                                                                                                                                 |
| queryExporter.maxFailedTimeouts                     | int                                                                               | no        | 3            | Specifies max failed timesqueries.                                                                                                                                 |
| queryExporter.selfMonitorBuckets                                 | string                                                                          | no        | "0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 30, 60" | Specifies list of buckets for self metric histogram as comma-separated floats.         
| queryExporter.collectionInterval                     | int                                                                               | no        | 60            | Specifies default collection interval in seconds.                                                                                                                                 |

## powaUI

This sections describes all possible deploy parameters for PoWA UI component.

| Parameter                        | Type                                                                            | Mandatory | Default value                                             | Description                                                            |
|----------------------------------|---------------------------------------------------------------------------------|-----------|-----------------------------------------------------------|------------------------------------------------------------------------|
| powaUI.install                   | bool                                                                            | no        | false                                                     | Indicates that PoWA UI should be installed or not.                     |
| powaUI.ingress.host          | sting                                                                           | yes       | <ingress FQDN> | Specifies FQDN for Kubernetes Ingress.                                 |
| powaUI.ingress.enabled       | bool                                                                           | no       | true | Specifies Ingress should be enabled.                                 |
| powaUI.securityContext           | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a                                                       | Specifies pod level security attributes and common container settings. |
| powaUI.resources.requests.memory | string                                                                          | no        | 256Mi                                                     | Specifies memory requests.                                             |
| powaUI.resources.requests.cpu    | string                                                                          | no        | 200m                                                      | Specifies cpu requests.                                                |
| powaUI.resources.limits.memory   | string                                                                          | no        | 512Mi                                                     | Specifies memory limits.                                               |
| powaUI.resources.limits.cpu      | string                                                                          | no        | 500m                                                      | Specifies cpu limits.                                                  |
| powaUI.cookieSecret              | sting                                                                           | no        | n/a                                                       | Specifies the secret for Powa UI cookies.                              |
| powaUI.affinity                  | json                                                                            | no        | n/a                                                       | Specifies the affinity scheduling rules.                               |
| powaUI.podLabels                 | yaml                                                                            | no        | n/a                                                       | Specifies custom pod labels.                                           |

## connectionPooler

This sections describes all possible deploy parameters for Connection Pooler (PGBouncer) component.

| Parameter                                  | Type                                                                            | Mandatory | Default value                                                   | Description                                                                                               |
|--------------------------------------------|---------------------------------------------------------------------------------|-----------|-----------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| connectionPooler.install                   | bool                                                                            | no        | false                                                           | Indicates that PG Bouncer should be installed or not.                                                     |
| connectionPooler.securityContext           | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a                                                             | Specifies pod level security attributes and common container settings.                                    |
| connectionPooler.resources.requests.memory | string                                                                          | no        | 256Mi                                                           | Specifies memory requests.                                                                                |
| connectionPooler.resources.requests.cpu    | string                                                                          | no        | 200m                                                            | Specifies cpu requests.                                                                                   |
| connectionPooler.resources.limits.memory   | string                                                                          | no        | 512Mi                                                           | Specifies memory limits.                                                                                  |
| connectionPooler.resources.limits.cpu      | string                                                                          | no        | 500m                                                            | Specifies cpu limits.                                                                                     |
| connectionPooler.replicas                  | int                                                                             | no        | 1                                                               | Specifies the number of replicas.                                                                         |
| connectionPooler.username                  | string                                                                          | no        | pgbouncer                                                       | Specifies the username for connection to Postgres.                                                        |
| connectionPooler.password                  | string                                                                          | no        | pgbouncer                                                       | Specifies the password for connection to Postgres.                                                        |
| connectionPooler.config                    | map[string]map[string]string                                                    | no        | [Default PG Bouncer parameters](#default-pg-bouncer-parameters) | Specifies the config parameters for PGBouncer. [Config parameters](https://www.pgbouncer.org/config.html) |
| connectionPooler.affinity                  | json                                                                            | no        | n/a                                                             | Specifies the affinity scheduling rules.                                                                  |

## replicationController

This sections describes all possible deploy parameters for Replication Controller component.

| Parameter                        | Type                                                                            | Mandatory | Default value                                             | Description                                                            |
|----------------------------------|---------------------------------------------------------------------------------|-----------|-----------------------------------------------------------|------------------------------------------------------------------------|
| replicationController.install                   | bool                                                                            | no        | false                                                     | Indicates that Replication Controller should be installed or not.                     |
| replicationController.securityContext           | [Kubernetes Sec Context](https://pkg.go.dev/k8s.io/api/core/v1#SecurityContext) | no        | n/a                                                       | Specifies pod level security attributes and common container settings. |
| replicationController.resources.requests.memory | string                                                                          | no        | 64Mi                                                     | Specifies memory requests.                                             |
| replicationController.resources.requests.cpu    | string                                                                          | no        | 200m                                                      | Specifies cpu requests.                                                |
| replicationController.resources.limits.memory   | string                                                                          | no        | 64Mi                                                     | Specifies memory limits.                                               |
| replicationController.resources.limits.cpu      | string                                                                          | no        | 200m                                                      | Specifies cpu limits.                                                  |
| replicationController.affinity                  | json                                                                            | no        | n/a                                                       | Specifies the affinity scheduling rules.                               |
| replicationController.podLabels                 | yaml                                                                            | no        | n/a                                                       | Specifies custom pod labels.                                           |
| replicationController.apiUser                 | string                                                                            | no        | n/a                                                       | Specifies the user for API usage.                                           |
| replicationController.apiPassword                 | string                                                                            | no        | n/a                                                       | Specifies the password for API usage.                                           |

## tracing

This sections describes all possible deploy parameters for Tracing configuration.

***NOTE*** For now tracing is supported for `backupDaemon` component only.

| Parameter       | Type   | Mandatory | Default value                        | Description                                         |
|-----------------|--------|-----------|--------------------------------------|-----------------------------------------------------|
| tracing.enabled | bool   | no        | false                                | Indicates that Tracing should be configured or not. |
| tracing.host    | string | no        | "jaeger-collector.tracing.svc:4317"  | Specifies tracing collector host.                   |


## tls

Postgres Operator allows configuration of TLS for supplementary and other components. By default, registration disabled.

| Parameter                                                      | Type     | Mandatory | Default value    | Description                                                                                                                                                                                                                                                                                                                   |
|----------------------------------------------------------------|----------|-----------|------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| tls.enabled                                                    | bool     | no        | false            | Indicates that TLS should be enabled or not.                                                                                                                                                                                                                                                                                  |
| tls.certificateSecretName                                      | string   | no        | services-pg-cert | Specifies the name of secret with certificate in PostgreSQL namespace. See [TLS Configuration](/docs/features/tls-configuration.md)                                                                                                                  |
| tls.generateCerts.enabled                                      | bool     | yes       | false            | Specifies whether to generate SSL certificates by cert-manager or not. If `false` specified, follow [manual certificate configuration guid](/docs/features/tls-configuration.md#manual). |
| tls.generateCerts.duration                                     | int      | no        | 365              | Specifies SSL certificate validity duration in days. The default value is 365.                                                                                                                                                                                                                                                |
| tls.generateCerts.subjectAlternativeName.additionalDnsNames    | []string | no        | n/a              | Specifies the list of additional DNS names to be added to the "Subject Alternative Name" field of SSL certificate. If access to Postgres Service for external clients is enabled, DNS names from externalHostNames parameter must be specified in here.                                                                       |
| tls.generateCerts.subjectAlternativeName.additionalIpAddresses | []string | no        | n/a              | Specifies the list of additional IP addresses to be added to the "Subject Alternative Name" field of SSL certificate. If access to Postgres Service for external clients is enabled, IP addresses from externalHostNames parameter must be specified in here.                                                                 |
| tls.generateCerts.clusterIssuerName                            | string   | yes       | n/a              | Specifies name of `ClusterIssuer` resource. If the parameter is not set or empty, `Issuer` resource in current Kubernetes namespace will be used.                                                                                                                                                                             |
| tls.certificates.tls_crt                                       | string   | no        | ""               | Specifies the certificate in BASE64 format. It is required if tls.enabled is true and tls.generateCerts.enabled is false. This allows user to specify their own certificate.                                                                                                                                                  |
| tls.certificates.tls_key                                       | string   | no        | ""               | Specifies the private key in BASE64 format. It is required if tls.enabled is true and tls.generateCerts.enabled is false. This allows user to specify their own key.                                                                                                                                                          |
| tls.certificates.ca_crt                                        | string   | no        | ""               | Specifies base 64 encoded CA certificate. It is required if tls.enabled is true and tls.generateCerts.enabled is false. This allows user to specify their own ca certificate.                                                                                                                                                 |                                                                                                                                                                                                                                                                

## pgBackRest

Patroni Core Operator allows configuration of TLS for PostgreSQL. By default, registration disabled.

| Parameter                    | Type     | Mandatory | Default value       | Description                                                                                                                   |
|------------------------------|----------|-----------|---------------------|-------------------------------------------------------------------------------------------------------------------------------|
| pgBackRest.repoType          | string   | yes       | rwx                 | Specifies type of the storage for backups. Allowed options are `s3` or `rwx`.                                                 |
| pgBackRest.repoPath          | string   | yes       | /var/lib/pgbackrest | Specifies folder to store backups.                                                                                            |
| pgBackRest.diffSchedule      | string   | yes       | 30 0/1 * * *        | Specifies schedule of the differential backups in cron format.                                                                |
| pgBackRest.incrSchedule      | string   | yes       | 30 0/1 * * *        | Specifies schedule of the incremental backups in cron format.                                                                 |
| pgBackRest.rwx.type          | string   | no        | n/a                 | Specifies the storage type. The possible values are `pv` and `provisioned`.                                                   |
| pgBackRest.rwx.size          | string   | no        | n/a                 | Specifies size of pgBackRest PVCs.                                                                                            |
| pgBackRest.rwx.storageClass  | string   | no        | n/a                 | Specifies storageClass that will be used for pgBackRest PVCs. Should be specified only in case of `provisioned` storageClass. |
| pgBackRest.rwx.volumes       | []string | no        | n/a                 | Specifies list of Persistence Volumes that will be used for PVCs.  Should be specified only in case of `pv` storageClass.     |
| pgBackRest.s3.bucket         | string   | no        | n/a                 | Specifies name of the bucket in s3.                                                                                           |
| pgBackRest.s3.endpoint       | string   | no        | n/a                 | Specifies link to the s3 server.                                                                                              |
| pgBackRest.s3.key            | string   | no        | n/a                 | Specifies key of the s3 storage to login.                                                                                     |
| pgBackRest.s3.secret         | string   | no        | n/a                 | Specifies secret of the s3 storage to login.                                                                                  |
| pgBackRest.s3.region         | string   | no        | n/a                 | Specifies region of the s3 storage.                                                                                           |
| pgBackRest.s3.verifySsl      | bool     | no        | n/a                 | Specifies do the pgBackRest verify secure connection to the s3, or not. Possible value true or false.                         |

# Installation

### Helm

For pure installation, please, follow [Quick Start Guide](/docs/public/quickstart.md).

## On-prem

### HA scheme

```
patroni:
  install: true
  resources:
    requests:
      cpu: {applicable_to_env_cpu} # 125m
      memory: {applicable_to_env_ram} # 250Mi
    limits:
      cpu: {applicable_to_env_cpu} # 252m
      memory: {applicable_to_env_ram} # 502Mi
  storage:
    storageClass: {storage_name_for_data} # manual
    volumes:
      - {pv_name_for_instance_1} # pgpv1
      - {pv_name_for_instance_2} # pgpv2
    nodes:
      - {worker_name_to_schedule_instance_1} # worker1
      - {worker_name_to_schedule_instance_2} # worker2

metricCollector:
  install: false

backupDaemon:
  install: true
  resources:
    requests:
      cpu: {applicable_to_env_cpu} # 504m
      memory: {applicable_to_env_ram} # 514Mi
    limits:
      cpu: {applicable_to_env_cpu} # 202m
      memory: {applicable_to_env_ram} # 252Mi
  storage:
    storageClass: {storage_class_name_for_backups} # manual
    volumes:
      - {pv_name_for_backups} # pgbckp
    nodes:
      - {worker_name_for_backups} # worker3

dbaas:
  install: true
  resources:
    requests:
      cpu: {applicable_to_env_cpu} # 200m
      memory: {applicable_to_env_ram} # 64Mi
    limits:
      cpu: {applicable_to_env_cpu} # 200m
      memory: {applicable_to_env_ram} # 64Mi
  aggregator:
    registrationAddress: {dbaas_registration_address} # "http://dbaas:8080"
```

### DR scheme

For more information about DR scheme, please, follow [Disaster Recovery](/docs/public/features/disaster-recovery.md) document.

An example of parameters for [Active Cluster](/docs/public/features/disaster-recovery.md#active-postgres-service-on-cluster-1).

An example of parameters for [Standby Cluster](/docs/public/features/disaster-recovery.md#standby-postgres-service-on-cluster-2).

### Non-HA scheme

***Note***: For development purposes only

Same as [HA Scheme](#ha-scheme), but `patroni.replicas` parameter should be set to `1`.

# Upgrade

## Major Upgrade of PostgreSQL

For more information on how to do the Major Upgrade of PostgreSQL, please, follow [Major Upgrade](/docs/public/features/major-upgrade.md) document.

# Appendix

## Default PostgreSQL Parameters

```yaml
  postgreSQLParams:
    - "password_encryption: md5"
    - "max_connections: 200"
    - "shared_preload_libraries: pg_stat_statements, pg_hint_plan, pg_cron"
    - "tcp_keepalives_idle: 300"
    - "tcp_keepalives_interval: 10"
    - "tcp_keepalives_count: 5"
```

## Default PG Bouncer Parameters

```yaml
    pgbouncer:
      listen_port: '6432'
      listen_addr: '0.0.0.0'
      auth_type: 'md5'
      auth_file: '/etc/pgbouncer/userlist.txt'
      auth_user: 'pgbouncer'
      auth_query: 'SELECT p_user, p_password FROM public.lookup($1)'
      ignore_startup_parameters: 'options,extra_float_digits'
```

