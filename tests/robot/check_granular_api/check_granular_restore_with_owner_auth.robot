*** Settings ***
Documentation     Check granular backup restore with owner of DB REST API
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           String
Library           OperatingSystem
Resource          ../Lib/lib.robot
Test Setup        Prepare Database
Test Teardown     Teardown Database

*** Test Cases ***
Check backup restore request endpoint for restore with owner of DB
    [Tags]    backup full   check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request.
    ...  After authentication part test case validates that if restoreRoles is specified in body of restore request,
    ...  database will be restored with all connected roles and owner will remain the same
    ...
    ${res}=  Get Auth
    Run Keyword If     '${res}' == "false"     Check Disabled Auth with owner of DB
    Run Keyword If     '${res}' == "true"     Check Enabled Auth with owner of DB

*** Keywords ***
Prepare Database
    ${PG_CLUSTER_NAME}=  Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    ${POSTGRES_USER}=  Get Environment Variable   POSTGRES_USER   default=postgres
    ${db_name}  Set Variable  smoketest_gb_base
    ${db_role}  Set Variable  smoketest_gb_role
    Set Global Variable   ${PG_CLUSTER_NAME}
    Set Global Variable   ${POSTGRES_USER}
    Set Global Variable   ${db_name}
    Set Global Variable   ${db_role}
    Create New Role  ${db_role}
    Create Database With Owner  ${db_name}  ${db_role}
    Execute Query  pg-${PG_CLUSTER_NAME}  CREATE TABLE test_gb_table (ID BIGINT PRIMARY KEY NOT NULL, VALUE TEXT NOT NULL)   dbname=${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  ALTER TABLE test_gb_table OWNER TO smoketest_gb_role   dbname=${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  INSERT INTO test_gb_table VALUES (42, '42')   dbname=${db_name}

Teardown Database
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP ROLE ${db_role}
    Execute Query  pg-${PG_CLUSTER_NAME}  drop database if exists ${db_name}

Check That Role Exists
    ${output}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT rolname FROM pg_roles where rolname = '${db_role}'
    Should Be True   """${db_role}""" in """${output}"""

    ${output}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT pg_catalog.pg_get_userbyid(d.datdba) as "Owner" FROM pg_catalog.pg_database d WHERE d.datname = 'smoketest_gb_base'
    Should Be True   """${db_role}""" in """${output}"""

Check That Table Contains Values
    ${output}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT count(*) FROM test_gb_table   dbname=${db_name}
    Should Be True   """1""" in """${output}"""


Check Disabled Auth with owner of DB
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session   postgres_backup_daemon   ${scheme}://postgres-backup-daemon:9000
    ${name_space}=   Get Current Date   result_format=%Y%m%d%H%M
    ${databases}=  Create List  smoketest_gb_base
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    ${json_data}=   Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary   ${resp.json()}    backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary   ${resp.json()}   status
        Run Keyword If    '${status}' == 'In progress'    Sleep    1s
        Run Keyword If    '${status}' == 'Successful'    Exit For Loop
    END
    Set To Dictionary   ${data}   backupId   ${backup_id}
    &{databases_mapping}=  Create Dictionary  restoreRoles=true
    Set To Dictionary    ${data}    databasesMapping    ${databases_mapping}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Sleep  5s
    Check That Role Exists
    Check That Table Contains Values
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}

Check Enabled Auth With Owner Of DB
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session   postgres_backup_daemon   ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List   postgres  ${PG_ROOT_PASSWORD}
    Create Session   postgres_backup_daemon   ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${name_space}=   Get Current Date   result_format=%Y%m%d%H%M
    ${databases}=  Create List  smoketest_gb_base
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary    ${resp.json()}    backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If    '${status}' == 'In progress'    Sleep    1s
        Run Keyword If    '${status}' == 'Successful'    Exit For Loop
    END
    Set To Dictionary   ${data}   backupId   ${backup_id}
    &{databases_mapping}=  Create Dictionary  restoreRoles=true
    Set To Dictionary  ${data}  databasesMapping  ${databases_mapping}
    ${json_data}=   Evaluate  json.dumps(${data})  json
    Create Session   postgres_backup_daemon   ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Sleep    5s
    Check That Role Exists
    Check That Table Contains Values
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}
