*** Settings ***
Documentation     Check positive full restore cycle with PgBackRest storage
Library           Collections
Library           OperatingSystem
Library           String
Resource          ../Lib/lib.robot

*** Variables ***
${OPERATION_RETRY_COUNT}       60
${OPERATION_RETRY_INTERVAL}    5s

*** Test Cases ***
Check PgBackRest Full Backup Restore
    [Tags]  pgbackrest  pgbackrest_restore
    [Documentation]
    ...  Positive PgBackRest cycle:
    ...  1. Verify backup daemon uses PgBackRest storage.
    ...  2. Create database and seed data.
    ...  3. Create full backup through backup daemon.
    ...  4. Add data after backup.
    ...  5. Restore Patroni cluster from the created PgBackRest backup.
    ...  6. Verify cluster is healthy and data state matches the backup.
    ${pg_cluster_name}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${postfix}=  Generate Random String  5  [LOWER]
    ${db_name}=  Set Variable  pgbackrest_restore_${postfix}
    Set Test Variable  \${db_name}  ${db_name}
    Log To Console  \n[pgbackrest] cluster=${pg_cluster_name}, database=${db_name}
    Skip Test If PgBackRest Is Not Configured
    Create Database  ${db_name}
    Wait Until Keyword Succeeds  ${OPERATION_RETRY_COUNT}  ${OPERATION_RETRY_INTERVAL}
    ...  Check Database Exists  ${pg_cluster_name}  ${db_name}
    ${rid_before}  ${expected_before}=  Insert Test Record  database=${db_name}
    ${restart_count_before}=  Get Backup Daemon Restart Count
    ${backup_id}=  Create PgBackRest Full Backup
    Log To Console  [pgbackrest] backup_id=${backup_id}, restart_count_before=${restart_count_before}
    ${rid_after}  ${expected_after}=  Insert Test Record  database=${db_name}
    ${restore_output}=  Restore Pgbackrest Backup  ${backup_id}
    Log  ${restore_output}
    Log To Console  [pgbackrest] restore started for backup_id=${backup_id}
    Wait Until Keyword Succeeds  20 min  10 sec  Patroni Ready
    Check Test Record  pg-${pg_cluster_name}  ${rid_before}  ${expected_before}  ${db_name}
    Check Test Record Is Absent  pg-${pg_cluster_name}  ${rid_after}  ${expected_after}  ${db_name}
    ${restart_count_after}=  Get Backup Daemon Restart Count
    Log To Console  [pgbackrest] restart_count_after=${restart_count_after}
    Should Be Equal As Integers  ${restart_count_after}  ${restart_count_before}
    [Teardown]  Delete Database  ${db_name}

*** Keywords ***
Skip Test If PgBackRest Is Not Configured
    ${status}=  Get Pgbackrest Prerequisite Status
    Log  PgBackRest prerequisites: ${status}
    ${missing}=  Get From Dictionary  ${status}  missing
    ${missing_count}=  Get Length  ${missing}
    Run Keyword If  ${missing_count} > 0  Pass Execution  PgBackRest is not configured for this environment: ${missing}

Check Database Exists
    [Arguments]  ${pg_cluster_name}  ${db_name}
    ${databases}=  Execute Query  pg-${pg_cluster_name}  SELECT datname FROM pg_database
    Should Contain  str(${databases})  ${db_name}

Create PgBackRest Full Backup
    ${pod}=  Get Pod  label=app:postgres-backup-daemon  status=Running
    ${dump_count}=  Get Backup Count
    ${schedule_response}=  Schedule Backup
    Log  PgBackRest backup schedule response: ${schedule_response}
    Dictionary Should Contain Key  ${schedule_response}  backup_id
    ${backup_id}=  Get From Dictionary  ${schedule_response}  backup_id
    Log To Console  [pgbackrest] waiting backup_id=${backup_id}
    Wait Until Keyword Succeeds  30 min  15 sec  Check PgBackRest Backup Exists  ${backup_id}
    ${dump_count_after}=  Get Backup Count
    Log  PgBackRest backup ${backup_id} is listed, dump_count_before=${dump_count}, dump_count_after=${dump_count_after}
    RETURN  ${backup_id}

Check PgBackRest Backup Exists
    [Arguments]  ${backup_id}
    ${exists}=  Pgbackrest Backup Exists  ${backup_id}
    Should Be True  ${exists}  msg=PgBackRest backup ${backup_id} was not found in backrest list

Check Test Record Is Absent
    [Arguments]  ${pod_name}  ${rid}  ${expected}  ${database}
    ${res}=  Execute Query  ${pod_name}  select * from test_insert_robot where id=${rid}  dbname=${database}
    Should Not Be True  """${expected}""" in """${res}"""  msg=Record added after backup is still present after restore: ${res}
