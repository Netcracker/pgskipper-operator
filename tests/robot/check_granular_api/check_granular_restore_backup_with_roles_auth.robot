*** Settings ***
Documentation     Check granular backup restore with roles REST API
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           String
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Backup Restore Request Endpoint For Restore With Roles
    [Tags]  full backups  check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request.
    ...  After authentication part test case validates that if restoreRoles is specified in body of restore request,
    ...  database will be restored with all connected roles
    ...
    ${res}=  Get Auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth With Roles
    Run Keyword If  '${res}' == "true"  Check Enabled Auth With Roles

*** Keywords ***
Check Disabled Auth With Roles
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    ${db_name}  Set Variable  smoketest_gb_base
    ${db_role}  Set Variable  smoketest_gb_role
    Create New Role  ${db_role}
    Create Database With Owner  ${db_name}  ${db_role}
    ${RID}  ${EXPECTED}=  Insert Test Record  database=${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  ALTER TABLE test_insert_robot OWNER TO ${db_role}  dbname=${db_name}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${array_db_name}=  Create List  ${db_name}
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
    Delete Database  ${db_name}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    List Should Not Contain Value  ${databases}   ${db_name}  msg="failed to delete the test database before restore from backup"
    Set To Dictionary  ${data}  backupId=${backup_id}
    Set To Dictionary  ${data}  restoreRoles=true
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
    ${output}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT rolname FROM pg_roles where rolname = '${db_role}'
    Should Be True  """${db_role}""" in """${output}"""
#   chech test record after restore
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID}   dbname=${db_name}
    Should Be True  """${EXPECTED}""" in """${res}"""   msg=[insert test record] Expected string ${EXPECTED} not found after restore database: ${db_name}. res: ${res}
    #delete backup and database after test
    Delete Database  ${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP ROLE IF EXISTS ${db_role}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}

Check Enabled Auth With Roles
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    ${db_name}  Set Variable  smoketest_gb_base
    ${db_role}  Set Variable  smoketest_gb_role
    Create New Role  ${db_role}
    Create Database With Owner  ${db_name}  ${db_role}
    ${RID}  ${EXPECTED}=  Insert Test Record  database=${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  ALTER TABLE test_insert_robot OWNER TO ${db_role}  dbname=${db_name}
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    ${array_db_name}=  Create List  ${db_name}
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${array_db_name}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary  ${resp.json()}  backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  GET On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    Delete Database  ${db_name}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    List Should Not Contain Value  ${databases}  ${db_name}  msg="failed to delete the test database before restore from backup"
    Set To Dictionary  ${data}  backupId=${backup_id}
    Set To Dictionary  ${data}  restoreRoles=true
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  GET On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
    END
    ${output}=   Execute Query  pg-${PG_CLUSTER_NAME}  SELECT rolname FROM pg_roles where rolname = '${db_role}'
    Should Be True  """smoketest_gb_role""" in """${output}"""
#   chech test record after restore
    ${res}=   Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID}  dbname=${db_name}
    Should Be True  """${EXPECTED}""" in """${res}"""  msg=[insert test record] Expected string ${EXPECTED} not found after restore database: ${db_name}. res: ${res}
    #delete backup after test
    Delete Database  ${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP ROLE IF EXISTS ${db_role}
    ${resp}=  GET On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}
