*** Settings ***
Documentation     Check granular backup restore with specified DBs during restore
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           String
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Variables ***
${OPERATION_RETRY_COUNT}                    30
${OPERATION_RETRY_INTERVAL}                 3s

*** Test Cases ***
Check Backup Restore Request Endpoint With Specified DB
    [Tags]  backup full  check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request.
    ...  After authentication part test case validates that if databases is specified in body of restore request,
    ...  only this databases will be restored
    ...
    ${res}=  Get Auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth With Specified DBs
    Run Keyword If  '${res}' == "true"  Check Enabled Auth With Specified DBs

*** Keywords ***
Check Existence DB
    [Arguments]  ${PG_CLUSTER_NAME}  ${db_name}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    Should Contain  str(${databases})   ${db_name}

Check Disabled Auth With Specified DBs
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    ${postfix}=  Generate Random String  5  [LOWER]
    ${db_name}  Set Variable  testdb_${postfix}
    Create Database  ${db_name}_0
    Create Database  ${db_name}_1
    Create Database  ${db_name}_2
    Wait Until Keyword Succeeds  ${OPERATION_RETRY_COUNT}  ${OPERATION_RETRY_INTERVAL}
    ...  Check Existence DB  ${PG_CLUSTER_NAME}  ${db_name}_2
    ${RID0}  ${EXPECTED0}=  Insert Test Record  database=${db_name}_0
    ${RID1}  ${EXPECTED1}=  Insert Test Record  database=${db_name}_1
    ${RID2}  ${EXPECTED2}=  Insert Test Record  database=${db_name}_2
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${array_db_name}=  Create List  ${db_name}_0  ${db_name}_1  ${db_name}_2
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${array_db_name}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary  ${resp.json()}  backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  GET On Session   postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
    END
    FOR  ${index}  IN RANGE    3
        Delete Test DB  ${db_name}_${index}
        ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
        Should Not Contain  str(${databases})   ${db_name}_${index}  msg="failed to delete the test database before restore from backup"
    END
    Set To Dictionary  ${data}  backupId=${backup_id}
    ${array_db_name}=  Create List  ${db_name}_1  ${db_name}_2
    Set To Dictionary  ${data}  databases=${array_db_name}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
    END
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    Should Not Contain  str(${databases})   ${db_name}_0
    Should Contain  str(${databases})   ${db_name}_1
    Should Contain  str(${databases})   ${db_name}_2
#   chech test record after restore
    ${res1}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID1}   dbname=${db_name}_1
    Should Be True  """${EXPECTED1}""" in """${res1}"""   msg=[insert test record] Expected string ${EXPECTED1} not found after restore database: ${db_name}_1. res: ${res1}
    ${res2}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID2}   dbname=${db_name}_2
    Should Be True  """${EXPECTED2}""" in """${res2}"""   msg=[insert test record] Expected string ${EXPECTED2} not found after restore database: ${db_name}_2. res: ${res2}
    #delete backup and database after test
    Delete Test DB  ${db_name}_1
    Delete Test DB  ${db_name}_2
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}

Check Enabled Auth With Specified DBs
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Secret Or Env  PG_ROOT_PASSWORD  ${PG_ROOT_PASSWORD_PATH}
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    ${postfix}=  Generate Random String  5  [LOWER]
    ${db_name}  Set Variable  testdb_${postfix}
    Create Database  ${db_name}_0
    Create Database  ${db_name}_1
    Create Database  ${db_name}_2
    Wait Until Keyword Succeeds  ${OPERATION_RETRY_COUNT}  ${OPERATION_RETRY_INTERVAL}
    ...  Check Existence DB  ${PG_CLUSTER_NAME}  ${db_name}_2
    ${RID0}  ${EXPECTED0}=  Insert Test Record  database=${db_name}_0
    ${RID1}  ${EXPECTED1}=  Insert Test Record  database=${db_name}_1
    ${RID2}  ${EXPECTED2}=  Insert Test Record  database=${db_name}_2
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${array_db_name}=  Create List  ${db_name}_0  ${db_name}_1  ${db_name}_2
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${array_db_name}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${restore_id}=  Get From Dictionary  ${resp.json()}  backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  GET On Session  postgres_backup_daemon  url=/backup/status/${restore_id}?namespace=${name_space}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    FOR  ${index}  IN RANGE    3
        Delete Test DB  ${db_name}_${index}
        ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
        Should Not Contain  str(${databases})   ${db_name}_${index}  msg="failed to delete the test database before restore from backup"
    END
    Set To Dictionary  ${data}  backupId=${backup_id}
    ${array_db_name}=  Create List  ${db_name}_1  ${db_name}_2
    Set To Dictionary  ${data}  databases=${array_db_name}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    log to console  RESTORE DATA: ${json_data}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    log to console  RESTORE RESPONCE ${resp.json()}
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  GET On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
    END
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    log to console  LIST OF DATABASES ${databases}
    Should Not Contain  str(${databases})   ${db_name}_0
    Should Contain  str(${databases})   ${db_name}_1
    Should Contain  str(${databases})   ${db_name}_2
#   chech test record after restore
    ${res1}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID1}   dbname=${db_name}_1
    Should Be True  """${EXPECTED1}""" in """${res1}"""   msg=[insert test record] Expected string ${EXPECTED1} not found after restore database: ${db_name}_1. res: ${res1}
    ${res2}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID2}   dbname=${db_name}_2
    Should Be True  """${EXPECTED2}""" in """${res2}"""   msg=[insert test record] Expected string ${EXPECTED2} not found after restore database: ${db_name}_2. res: ${res2}
    #delete backup and database after test
    Delete Test DB  ${db_name}_1
    Delete Test DB  ${db_name}_2
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}
