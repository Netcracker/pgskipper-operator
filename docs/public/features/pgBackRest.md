This chapter describes how pgBackRest integrated to Postgres Operator solution.
* [Overview](#overview)
* [How to deploy](#how-to-deploy)
* [Do a Backup](#do-a-backup)
* [Do a Restore](#do-a-restore)
* [How to schedule backup](#how-to-schedule-backup)
* [Retention](#retention)

# Overview

PgBackRest is the alternative 3rd party tool for the backups and restore procedures.
About pgBackRest [Official docs](https://pgbackrest.org/).

In our case pgBackRest included into `Sidecar` container with web server onboard and provides REST API.

![pgbackrest](../images/features/pgbackrest.png)

# How to deploy

pgBackRest integrated as a `Sidecar` container to the patroni pods.
But backup management is carried out by Postgres Backup Daemon.

So first of all you have to install `Patroni Core` manifest with additional values section pgBackRest:
Here is all possible values. You have to specify storage type as `rwx` or `s3`.

`rwx` storage example:


```yaml

pgBackRest:
  repoType: "rwx"
  repoPath: "/var/lib/pgbackrest"
  diffSchedule: "30 0/1 * * *"
  incrSchedule: "30 0/1 * * *"
  rwx:
    type: pv
    size: 3Gi
    volumes:
      - pg-backrest-backups-pv-1
```

`s3` storage example:

```yaml

pgBackRest:
  repoType: "s3"
  repoPath: "/var/lib/pgbackrest"
  diffSchedule: "30 0/1 * * *"
  incrSchedule: "30 0/1 * * *"
  s3:
    bucket: "pgbackrest"
    endpoint: "https://minio-ingress-minio-service"
    key: "minio"
    secret: "minio"
    region: "us-east-1"
    verifySsl: false
```

After the installation correct state will be:

1. Config map `pgbackrest-conf` created
2. Patroni pods has sidecar container `pgbackrest-sidecar`
3. ***Optional*** In case of `rwx` storage pv `pgbackrest-backups` should be created


After the reconciliation will be done next step is to install `Patroni Services` manifest with the same additional section in values:  
***NOTE*** BackupDaemon have to be installed to


`rwx` storage example:

```yaml

pgBackRest:
  repoType: "rwx"
  repoPath: "/var/lib/pgbackrest"
  diffSchedule: "30 0/1 * * *"
  incrSchedule: "30 0/1 * * *"
  rwx:
    type: pv
    size: 3Gi
    volumes:
      - pg-backrest-backups-pv-1
```

`s3` storage example:

```yaml

pgBackRest:
  repoType: "s3"
  repoPath: "/var/lib/pgbackrest"
  diffSchedule: "30 0/1 * * *"
  incrSchedule: "30 0/1 * * *"
  s3:
    bucket: "pgbackrest"
    endpoint: "https://minio-ingress-minio-service"
    key: "minio"
    secret: "minio123"
    region: "us-east-1"
    verifySsl: false
```

After the installation correct state will be:

1. postgres-backup-damon pod created
2. Environment variable `STORAGE_TYPE` should be set as `pgbackrest`


# Do a Backup

`Full Backup:`   
pgBackRest copies the entire contents of the database cluster to the backup.   
The first backup of the database cluster is always a Full Backup. pgBackRest is always able to restore a full backup directly.  
The full backup does not depend on any files outside of the full backup for consistency.

Full backup as a usual might be made by using `pg-backup-daemon` API

```
    curl -XPOST postgres-backup-daemon:8080/backup 
```
You will get response like `{"accepted": true, "reason": "http-request", "backup_requests_in_queue": 1, "message": "PostgreSQL backup has been scheduled successfully.", "backup_id": "20240913T1104"}`  
To check backups please navigate to patroni master pod, enter to the terminal pgbackrest-side car and execute command
```
pgbackrest info --output=json
```
```yaml

[{
  "archive": [{
    "database": {
      "id": 2,
      "repo-key": 1
    },
    "id": "14-2",
    "max": "00000002000000000000002A",
    "min": "000000010000000000000014"
  }, {
    "database": {
      "id": 3,
      "repo-key": 1
    },
    "id": "14-3",
    "max": "000000030000000000000015",
    "min": "000000010000000000000004"
  }
  ],
  "backup": [{
    "annotation": {
      "timestamp": "20240919T0000"
    },
    "archive": {
      "start": "000000010000000000000014",
      "stop": "000000010000000000000014"
    },
    "backrest": {
      "format": 5,
      "version": "2.51"
    },
    "database": {
      "id": 2,
      "repo-key": 1
    },
    "error": false,
    "info": {
      "delta": 26497427,
      "repository": {
        "delta": 3347229,
        "size": 3347229
      },
      "size": 26497427
    },
    "label": "20240919-000001F",
    "lsn": {
      "start": "0/14000028",
      "stop": "0/14000138"
    },
    "prior": null,
    "reference": null,
    "timestamp": {
      "start": 1726704001,
      "stop": 1726704021
    },
    "type": "full"
  },
  ],
  "cipher": "none",
  "db": [{
    "id": 1,
    "repo-key": 1,
    "system-id": 7416017901776986155,
    "version": "14"
  }, {
    "id": 2,
    "repo-key": 1,
    "system-id": 7416023245594288171,
    "version": "14"
  }, {
    "id": 3,
    "repo-key": 1,
    "system-id": 7416272553939988523,
    "version": "14"
  }
  ],
  "name": "patroni",
  "repo": [{
    "cipher": "none",
    "key": 1,
    "status": {
      "code": 0,
      "message": "ok"
    }
  }
  ],
  "status": {
    "code": 0,
    "lock": {
      "backup": {
        "held": false
      }
    },
    "message": "ok"
  }
}
]
```


# Do a Restore

Navigate to the postgres-backup-daemon terminal and execute the command

```
cd /maintenance/recovery/ && python3 pg_back_rest_recovery.py
```

Restore procedure automatically choose latest backup and restore the database with applying all latest WAL.
*NOTE:* That type of restore will try to return the database to the latest possible state using all available WAL files.

# Point In Time Recovery

Navigate to the postgres-backup-daemon terminal and execute the command

There are two arguments that have to be provided.
```
TYPE could be:
    lsn - recover to the LSN (Log Sequence Number) specified in --target
    name - recover the restore point specified in --target.
    xid - recover to the transaction id specified in --target.
    time - recover to the time specified in --target.
```
`TARGET` depends on provided `TYPE`, so it should be match expected value.

Example:

```
export TYPE=time && export TARGET='2024-10-23 14:11:04+00' && cd /maintenance/recovery/ && python3 pg_back_rest_recovery.py

```

Restore procedure automatically choose latest backup before timestamp and restore the database with WAL files from the archive to the point in time.

# How to schedule backup

## Diff backup

`Differential Backup:`  
pgBackRest copies only those database cluster files that have changed since the last full backup.  
pgBackRest restores a differential backup by copying all of the files in the chosen differential backup and the appropriate unchanged files from the previous full backup.   
The advantage of a differential backup is that it requires less disk space than a full backup, however, the differential backup and the full backup must both be valid to restore the differential backup.

Scheduling of the differential backups may be set up as cron formatted string and passed in installation values:

For example, differential backups in 30 minute of every hour will be set like that:

```yaml
pgBackrest:
  diffSchedule: "30 0/1 * * *"
 // other paramteters
```

# Incremental backup

`Incremental Backup:`  
pgBackRest copies only those database cluster files that have changed since the last backup (which can be another incremental backup, a differential backup, or a full backup).  
As an incremental backup only includes those files changed since the prior backup, they are generally much smaller than full or differential backups.  
As with the differential backup, the incremental backup depends on other backups to be valid to restore the incremental backup.  
Since the incremental backup includes only those files since the last backup, all prior incremental backups back to the prior differential, the prior differential backup, and the prior full backup must all be valid to perform a restore of the incremental backup.  
If no differential backup exists then all prior incremental backups back to the prior full backup, which must exist, and the full backup itself must be valid to restore the incremental backup.

Scheduling to be done...


# Retention

To configure retention policy for any type of backups, navigate to Config Maps and find all the `pgBackRest` vanilla parameters in `pgbackrest-conf` Config Map

## Full

The `repo1-retention-full-type` determines how the option repo1-retention-full is interpreted; either as the count of full backups to be retained or how many days to retain full backups.  
New backups must be completed before expiration will occur — that means if repo1-retention-full-type=count and repo1-retention-full=2 then there will be three full backups stored before the oldest one is expired.  
Or if repo1-retention-full-type=time and repo1-retention-full=20 then there must be one full backup that is at least 20 days old before expiration can occur.

```yaml
{
"pgbackrest.conf": "[global]  
    log-level-file=detail
    log-level-console=info
    repo1-path=/var/lib/pgbackrest
    repo1-retention-full=5"
}
```
repo1-retention-full=5 so six differentials will need to be performed before one is expired.

## Differential

Set repo1-retention-diff to the number of differential backups required.  
Differentials only rely on the prior full backup so it is possible to create a rolling set of differentials for the last day or more.  
This allows quick restores to recent points-in-time but reduces overall space consumption.

```yaml
{
"pgbackrest.conf": "[global]
    log-level-file=detail
    log-level-console=info
    repo1-path=/var/lib/pgbackrest
    repo1-retention-full=5  
    repo1-retention-diff=3"
}
```
Backup repo1-retention-diff=3 so four differentials will need to be performed before one is expired.

## Archive

Although pgBackRest automatically removes archived WAL segments when expiring backups (the default expires WAL for full backups based on the repo1-retention-full option).

More information about retention policy please find in [Official pgBackRest docs](https://pgbackrest.org/user-guide.html#retention)



