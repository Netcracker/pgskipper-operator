*** Settings ***
Documentation     Check granular API v1 backup and restore with S3 aliases
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           String
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Variables ***
${OPERATION_RETRY_COUNT}       60
${OPERATION_RETRY_INTERVAL}    3s

*** Test Cases ***
Check API V1 Backup And Restore With S3 Alias Without Query Params
    [Tags]  backup full  check_granular_api  s3_aliases  api_v1
    [Documentation]
    ...  This test validates new API v1 backup and restore flow with S3 alias.
    ...  Backup and restore are created with storageName/blobPath in request body.
    ...  Status and delete endpoints are checked without storageName/blobPath query parameters.

    ${res}=  Get Auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth API V1 S3 Alias Flow
    Run Keyword If  '${res}' == "true"   Check Enabled Auth API V1 S3 Alias Flow


*** Keywords ***
Prepare API V1 Session Without Auth
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    RETURN  ${headers}


Prepare API V1 Session With Auth
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    RETURN  ${headers}


Check Disabled Auth API V1 S3 Alias Flow
    ${headers}=  Prepare API V1 Session Without Auth
    Run API V1 S3 Alias Flow  ${headers}


Check Enabled Auth API V1 S3 Alias Flow
    ${headers}=  Prepare API V1 Session With Auth
    Run API V1 S3 Alias Flow  ${headers}


Run API V1 S3 Alias Flow
    [Arguments]  ${headers}

    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${storage_name}=  Get Environment Variable  S3_ALIAS_NAME  default=test
    ${postfix}=  Generate Random String  5  [LOWER]
    ${db_name}=  Set Variable  api_v1_${postfix}
    ${restore_db_name}=  Set Variable  api_v1_restore_${postfix}
    ${blob_path}=  Set Variable  /tmp/robot/api-v1-${postfix}

    Log To Console  API V1 S3 alias name: ${storage_name}
    Log To Console  API V1 blob path: ${blob_path}

    Create Database  ${db_name}
    ${RID}  ${EXPECTED}=  Insert Test Record  database=${db_name}

    ${backup_id}=  Create API V1 Backup And Wait  ${headers}  ${db_name}  ${blob_path}  ${storage_name}

    Delete Test DB  ${db_name}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    Should Not Contain  str(${databases})  ${db_name}

    ${restore_id}=  Create API V1 Restore And Wait  ${headers}  ${backup_id}  ${db_name}  ${restore_db_name}  ${blob_path}  ${storage_name}

    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    Should Contain  str(${databases})  ${restore_db_name}

    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID}  dbname=${restore_db_name}
    Should Be True  """${EXPECTED}""" in """${res}"""  msg=[insert test record] Expected string ${EXPECTED} not found after restore database: ${restore_db_name}. res: ${res}

    Delete API V1 Restore Without Query Params  ${restore_id}
    Delete API V1 Backup Without Query Params  ${backup_id}

    Delete Test DB  ${restore_db_name}
    Delete Test DB  ${db_name}


Create API V1 Backup And Wait
    [Arguments]  ${headers}  ${db_name}  ${blob_path}  ${storage_name}

    ${databases}=  Create List  ${db_name}
    &{data}=  Create Dictionary  storageName=${storage_name}  blobPath=${blob_path}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json

    ${resp}=  POST On Session
    ...  postgres_backup_daemon
    ...  /api/v1/backup
    ...  data=${json_data}
    ...  headers=${headers}
    ...  expected_status=anything

    Log To Console  API V1 backup request body: ${json_data}
    Log To Console  API V1 backup response code: ${resp.status_code}
    Log To Console  API V1 backup response body: ${resp.text}

    Should Be Equal As Integers  ${resp.status_code}  202
    Dictionary Should Contain Key  ${resp.json()}  backupId
    ${backup_id}=  Get From Dictionary  ${resp.json()}  backupId

    Wait Until Keyword Succeeds  ${OPERATION_RETRY_COUNT}  ${OPERATION_RETRY_INTERVAL}
    ...  Check API V1 Backup Successful Without Query Params  ${backup_id}  ${blob_path}  ${storage_name}

    RETURN  ${backup_id}


Check API V1 Backup Successful Without Query Params
    [Arguments]  ${backup_id}  ${blob_path}  ${storage_name}

    ${resp}=  GET On Session
    ...  postgres_backup_daemon
    ...  url=/api/v1/backup/${backup_id}
    ...  expected_status=anything

    Log  API V1 backup status response code: ${resp.status_code}
    Log  API V1 backup status response body: ${resp.text}

    Should Be Equal As Integers  ${resp.status_code}  200

    ${status}=  Get From Dictionary  ${resp.json()}  status
    Should Be Equal  ${status}  Successful

    ${actual_storage_name}=  Get From Dictionary  ${resp.json()}  storageName
    Should Be Equal  ${actual_storage_name}  ${storage_name}

    ${actual_blob_path}=  Get From Dictionary  ${resp.json()}  blobPath
    Should Be Equal  ${actual_blob_path}  ${blob_path}


Create API V1 Restore And Wait
    [Arguments]  ${headers}  ${backup_id}  ${source_db_name}  ${target_db_name}  ${blob_path}  ${storage_name}

    &{db_mapping}=  Create Dictionary  previousDatabaseName=${source_db_name}  databaseName=${target_db_name}
    ${databases}=  Create List  ${db_mapping}
    &{data}=  Create Dictionary  storageName=${storage_name}  blobPath=${blob_path}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json

    ${resp}=  POST On Session
    ...  postgres_backup_daemon
    ...  /api/v1/restore/${backup_id}
    ...  data=${json_data}
    ...  headers=${headers}
    ...  expected_status=anything

    Log To Console  API V1 restore request body: ${json_data}
    Log To Console  API V1 restore response code: ${resp.status_code}
    Log To Console  API V1 restore response body: ${resp.text}

    Should Be Equal As Integers  ${resp.status_code}  200
    Dictionary Should Contain Key  ${resp.json()}  restoreId
    ${restore_id}=  Get From Dictionary  ${resp.json()}  restoreId

    Wait Until Keyword Succeeds  ${OPERATION_RETRY_COUNT}  ${OPERATION_RETRY_INTERVAL}
    ...  Check API V1 Restore Successful Without Query Params  ${restore_id}  ${blob_path}  ${storage_name}

    RETURN  ${restore_id}


Check API V1 Restore Successful Without Query Params
    [Arguments]  ${restore_id}  ${blob_path}  ${storage_name}

    ${resp}=  GET On Session
    ...  postgres_backup_daemon
    ...  url=/api/v1/restore/${restore_id}
    ...  expected_status=anything

    Log  API V1 restore status response code: ${resp.status_code}
    Log  API V1 restore status response body: ${resp.text}

    Should Be Equal As Integers  ${resp.status_code}  200

    ${status}=  Get From Dictionary  ${resp.json()}  status
    Should Be Equal  ${status}  Successful

    ${actual_storage_name}=  Get From Dictionary  ${resp.json()}  storageName
    Should Be Equal  ${actual_storage_name}  ${storage_name}

    ${actual_blob_path}=  Get From Dictionary  ${resp.json()}  blobPath
    Should Be Equal  ${actual_blob_path}  ${blob_path}


Delete API V1 Restore Without Query Params
    [Arguments]  ${restore_id}

    ${resp}=  DELETE On Session
    ...  postgres_backup_daemon
    ...  /api/v1/restore/${restore_id}
    ...  expected_status=anything

    Log To Console  API V1 restore delete response code: ${resp.status_code}
    Log To Console  API V1 restore delete response body: ${resp.text}

    Should Be Equal As Integers  ${resp.status_code}  200
    ${status}=  Get From Dictionary  ${resp.json()}  status
    Should Be Equal  ${status}  Successful


Delete API V1 Backup Without Query Params
    [Arguments]  ${backup_id}

    ${resp}=  DELETE On Session
    ...  postgres_backup_daemon
    ...  /api/v1/backup/${backup_id}
    ...  expected_status=anything

    Log To Console  API V1 backup delete response code: ${resp.status_code}
    Log To Console  API V1 backup delete response body: ${resp.text}

    Should Be Equal As Integers  ${resp.status_code}  200
    ${status}=  Get From Dictionary  ${resp.json()}  status
    Should Be Equal  ${status}  Successful