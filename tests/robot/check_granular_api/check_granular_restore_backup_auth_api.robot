*** Settings ***
Documentation     Check granular backup restore backup REST API
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           String
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Backup Restore Request Endpoint For Regular Backup
    [Tags]  backup full  check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request.
    ...  After authentication part test case validates that if we will try to restore successful backup
    ...  restore procedure will not fail and trackingId of restore process will be provided
    ...
    ${res}=  Get Auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth Regular Backup
    Run Keyword If  '${res}' == "true"  Check Enabled Auth Regular Backup

Check Backup Restore Request Endpoint For Failed Backup
    [Tags]  backup full  check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request.
    ...  After authentication part test case validates that if we will try to restore failed backup
    ...  restore procedure will fail with apropriate message
    ...
    ${res}=  Get Auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth Failed Backup
    Run Keyword If  '${res}' == "true"  Check Enabled Auth Failed Backup

*** Keywords ***
Check Disabled Auth Regular Backup
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    ${db_name}  Set Variable  test_db
    Create Database  ${db_name}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${RID}  ${EXPECTED}=  Insert Test Record  database=${db_name}
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${array_db_name}=  Create List  ${db_name}
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${array_db_name}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon   /backup/request   data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary  ${resp.json()}  backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  GET On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Log  ${status}
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    Delete Database  ${db_name}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    List Should Not Contain Value  ${databases}  ${db_name}  msg="failed to delete the test database before restore from backup"
    Set To Dictionary  ${data}  backupId  ${backup_id}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Dictionary Should Contain Key  ${resp.json()}  trackingId
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
#   chech test record after restore
    ${res}=  Execute Query   pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID}   dbname=${db_name}
    Should Be True  """${EXPECTED}""" in """${res}"""   msg=[insert test record] Expected string ${EXPECTED} not found after restore database: ${db_name}. res: ${res}
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
    Should Be Equal  ${resp.status_code}  ${200}
    #delete backup and drop database after test
    Delete Database  ${db_name}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}

Check Enabled Auth Regular Backup
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    Create Database  ${db_name}
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${databases}=  Create List  ${db_name}
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary  ${resp.json()}  backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Log  ${status}
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    Delete Database  ${db_name}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    List Should Not Contain Value  ${databases}  ${db_name}  msg="failed to delete the test database before restore from backup"
    Set To Dictionary  ${data}  backupId  ${backup_id}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Dictionary Should Contain Key  ${resp.json()}  trackingId
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id} auth=${auth}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
    Should Be Equal  ${resp.status_code}  ${200}
#   chech test record after restore
    ${res}=  Execute Query   pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID}   dbname=${db_name}
    Should Be True   """${EXPECTED}""" in """${res}"""   msg=[insert test record] Expected string ${EXPECTED} not found after restore database: ${db_name}. res: ${res}
    #delete backup and drop database after test
    Delete Database  ${db_name}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}

Check Disabled Auth Failed Backup
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${databases}=  Create List  not_existing_bd
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}  expected_status=400
    Should Be Equal  ${resp.status_code}  ${400}

Check Enabled Auth Failed Backup
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORDF
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${name_space}=   Get Current Date  result_format=%Y%m%d%H%M
    ${databases}=  Create List  not_existing_bd
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}  expected_status=400
    Should Be Equal  ${resp.status_code}  ${400}
