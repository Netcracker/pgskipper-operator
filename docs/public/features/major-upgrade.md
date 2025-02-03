This section covers the upgrade process for the PostgreSQL service.

* [Prerequisites](#prerequisites)
* [Input Parameters](#input-parameters)
* [Limitations](#limitations)
* [Upgrade Process Under the Hood](#upgrade-process-under-the-hood)
* [Validation Procedures](#validation-procedures)
* [Troubleshooting](#troubleshooting)
* [Upgrade Postgres in Active Standby scheme](#upgrade-postgres-in-active-standby-scheme)
* [Breaking Changes](#breaking-changes)

Supported versions for the migration are:

* 13.X
* 14.X
* 15.X

Hence, it's possible to do the Major Upgrade between all supported version.

**Note** Downgrade is not supported.

# Prerequisites

The prerequisites to upgrade the Major version of PostgreSQL are as follows:

* The cluster should be healthy. You can check it using the Monitoring Dashboard, where the PostgreSQL Cluster Status should be `UP`. 
* Important thing is to separate upgrade procedure of `Patroni Core` solution upgrade. Before you start MajorUpgrade procedure, please upgrade `Patroni Core` solution to the needed version.  
  For example:
    - Needed to upgrade from `postgres:1.32.0-delivery_pg14` to `postgres:1.33.0-delivery_pg15`
    - First of all you have to upgrade `Patroni Core` from `postgres:1.32.0-delivery_pg14` -> `postgres:1.33.0-delivery_pg14`
    - Only after manifest upgrade ends with success, you may do the MajorUpgrade from pg14 to pg15 `postgres:1.33.0-delivery_pg14` -> `postgres:1.33.0-delivery_pg15`
* Perform a full backup before the procedure.  

## Deprecated abstime

* In case of deprecated `abstime` data type for PostgreSQL 12 and above, you should check the database for the presence of such tables.
  Here is a script, that you may use to find all the databases includes `abstime`.

```
DBS="$(psql -d postgres -c "select datname from pg_database;" -tA )"
ABS_TIME_SELECT="SELECT count(*) FROM information_schema.columns WHERE data_type = 'abstime' AND table_schema <> 'pg_catalog';"
for db in $DBS; do echo -n "$db="; psql -d $db -c "$ABS_TIME_SELECT" -tA; done | grep -v "=0" > abstime_dbs
```
# Input Parameters


Mandatory parameter is:
```yaml
patroni:
  majorUpgrade:
    enabled: true
```

Also, you need to change `patroni.dockerImage` to an image with a new version that you expect in case of manual Helm deployment.
It will automatically set if you are using manifest.


# Limitations

The upgrade process has the following limitations:

* Zero downtime during the upgrade is not supported.

If you are using Helm upgrade or Jenkins Job, it is better to collect the actual installation parameters from the last installation procedure.

This is because Helm upgrade rewrites the whole `Custom Resource` and may change your cluster state, or break the Postgres Operator reconcile loop.  

In case with using `backupDaemon.walArchiving: true` upgrade process may be ends with error. Please turn it off (`backupDaemon.walArchiving: false`) before start automatic upgrade, and turn it on after your job ends with success.

# Upgrade Process Under the Hood

In fact, the upgrade procedure is performed using the standard `pg_upgrade` binary.  

To reduce the upgrading time, the `--link` key is used. For more information, refer to the official Postgres documentation at [https://www.postgresql.org/docs/current/pgupgrade.html](https://www.postgresql.org/docs/current/pgupgrade.html).  

To run the major upgrade, update `Custom Resource` in your namespace with parameters that are described in the [Input Parameters](#input-parameters) section in any convenient way.   

You may use Helm, Jenkins Job, or just change it manually.

The upgrade process is performed automatically and uses `InitContainers` for the pg_upgrade procedure.  

After Postgres Operator makes sure that the Patroni cluster is healthy enough, it scales down both the patroni pods and patches the master with `InitContainer`.    

Postgres Operator looking for exit code of the pg-upgrade `InitContainer`.   

Only if the exit code was 0 and master pod `UP` and new patroni cluster was initialized, upgrading process continues.  

For the last step, Postgres Operator adds `cleaner` `InitContainer` to the remaining Patroni follower pods, which deletes the old data files. So that followers can re-initialize data from the master.  


# Validation Procedures

After a successful major upgrade run, you have to check existence of next file in a Leader PostgreSQL Pod:

1. Go to PostgreSQL Leader Pod
2. Check if  `/var/lib/pgsql/data/update_extensions.sql` exists
3. If exists, execute next command: `psql -f /var/lib/pgsql/data/update_extensions.sql`

# Upgrade Postgres in Active Standby scheme

For some postgres limitations we can't provide full automatically upgrade both sides.
So follow the instruction below.

1. First of all upgrade version of your active side in common way.
2. After your active site up and ready, install standby site with version that matches active one (patroni.dockerImage).
3. Wait until it will be synced.

# Troubleshooting

All automatic upgrade process, depends on Postgres Operator.  

If the operator restarts or crashes for any reason, during the upgrade, the process will not end.

So you need to make manual steps to finish upgrade procedure.

There are several steps to make cluster upgraded. Depending on the state of the cluster, you should continue with the desired step, described below.

**1. PostgreSQL Cluster Status:**  

Only one Patroni pod is up (`leader` in the past) and there is a message in logs:  

```
CRITICAL: system ID mismatch, node pg-patroni-node1-d5d85d796-wx8ts belongs to a different cluster: 6994168840422187059 != 6994344201830150168
``` 

Please check output upgrade file `cat /var/lib/pgsql/data/upgrade.output` (from the `leader pod`), and make sure, that upgrade finishes without any errors.   

***Note: If there is no `upgrade.output` file, or you can find any errors, you need to rollback your cluster to previous state, and restore all the data from backup.***

**Manual steps are:**  

* Scale down Postgre Operator deployment.  
* Remove `initialize` key from annotations of `patroni-config` config map.
* Move to the next step.

**2. PostgreSQL Cluster Status:**  

Only one patroni pod is up, it has label `pgtype=master`, works fine and you may execute `psql` in terminal. Other Patroni pods are not working. 

**Manual steps are:**   

* Scale down Postgres Operator deployment  
* Update all other Patroni deployments with new version Patroni `dockerImage` [1]
* Add sleep command to each of the deployment from [1]

```
          command:
            - sh
            - '-c'
            - 'sleep 100500'
```   

***Note: The Sleep command that should be inserted after image `.spec.template.spec.containers.image` section*** 

2.3. Scale up followers, so it will be possible to DELETE all data from the replicas nodes (found in /var/lib/pgsql/data/postgreql_node<node number> for example) or delete and re-create claim in case of provisioned storage.  
2.4. Delete sleep command from deployments.

**3. PostgreSQL Cluster Status:**  

All patroni pods are up, and working fine, pods `pgtype=master` and `pgtype=replica` appears.

**Manual Steps Are:**   

* Scale down Postgres Operator deployment  
* Change `majorUpgrade.enabled` from `true` to `false` in `CustomResource` of your postgres cluster
* Scale up Postgres Operator deployment, and make sure in logs, that there is not error logs


## Upgrade Postgres with extensions

We caught the problem when POWA is activated. 

`pg_upgrade` binary generates file to update extensions manually. You mat find it `/var/lib/pgsql/data/update_extensions.sql` that contain UPDATE all the extensions:

```
ALTER EXTENSION "btree_gist" UPDATE;
ALTER EXTENSION "hypopg" UPDATE;
ALTER EXTENSION "pg_qualstats" UPDATE;
ALTER EXTENSION "pg_stat_statements" UPDATE;
ALTER EXTENSION "powa" UPDATE;
```

But after you will to use it, you will face error like that:  
`extension "hypopg" has no update path from version "1.3.0" to version "1.3.1"`  
The only way to make that extensions updated - is to `DROP` and `CREATE` them in this order:

```
DROP EXTENSION pg_stat_statements;
DROP EXTENSION pg_stat_kcache;
DROP EXTENSION powa;

CREATE EXTENSION pg_stat_statements;
CREATE EXTENSION pg_stat_kcache;
CREATE EXTENSION powa;
```

All of the `CREATE`/`DROP` statements should be performed in `powa` PostgreSQL database.

# Incompatible data type 'abstime'

This error refers to [Deprecated abstime](#deprecated-abstime)

```
{"level":"warn","timestamp":"2025-01-28T07:16:20.629Z","msg":"Cannot check incompatible data type 'abstime' on database hvnes_dbaas_db","error":"failed to connect to `host=pg-patroni.plat-dev-1-pg.svc.cluster.local user=postgres database=hvnes_dbaas_db`: server error (FATAL: cannot connect to invalid database \"hvnes_dbaas_db\" (SQLSTATE 55000))"}
``` 

**Manual Steps Are:**

It is necessary to check if database exists:

```
SELECT 1 FROM information_schema.columns WHERE data_type = 'abstime' AND table_schema <> 'pg_catalog';
```

If no rows was found then drop db and restart upgrade procedure:

```
drop database hvnes_dbaas_db;
```

# Breaking Changes

Follow official postgres release notes:
[https://www.postgresql.org/docs/13/release-15.html](https://www.postgresql.org/docs/13/release-13.html)
[https://www.postgresql.org/docs/14/release-15.html](https://www.postgresql.org/docs/14/release-14.html)
[https://www.postgresql.org/docs/15/release-15.html](https://www.postgresql.org/docs/15/release-15.html)