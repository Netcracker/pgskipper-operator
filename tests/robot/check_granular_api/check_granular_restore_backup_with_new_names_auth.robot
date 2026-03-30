*** Settings ***
Documentation     Check granular backup request REST API
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           String
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Backup Restore Request Endpoint For Restore With Db Name Change
    [Tags]  backup full  check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request.
    ...  After authentication part test case validates that if databases mapping is specified in body of restore request,
    ...  database will be restored with new name, for example `target_test_bd` or `TaRgEt_TeSt_Bd`
    ...
    ${res}=  Get Auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth With Db Name Change
    Run Keyword If  '${res}' == "true"  Check Enabled Auth With Db Name Change

*** Keywords ***
Check Disabled Auth With Db Name Change
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    Create Database  test_db
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
#    ${name_space}=    Get Current Date    result_format=%Y%m%d%H%M
    ${name_space}=  Set Variable  default
    ${databases}=  Create List  test_db
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    ${backup_id}=  Get From Dictionary    ${resp.json()}    backupId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    Set To Dictionary  ${data}  backupId  ${backup_id}
    &{databases_mapping}=  Create Dictionary  test_db=target_test_bd
    Set To Dictionary  ${data}  databasesMapping  ${databases_mapping}
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
    ${output}=  Execute Query   pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_catalog.pg_database WHERE datname = 'target_test_bd'
    Should Be True  """target_test_bd""" in """${output}"""
    &{databases_mapping}=  Create Dictionary  test_db=TaRgEt_TeSt_Bd
    Set To Dictionary  ${data}  databasesMapping  ${databases_mapping}
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
    ${output}=  Execute Query   pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_catalog.pg_database WHERE datname = 'TaRgEt_TeSt_Bd'
    Should Be True  """TaRgEt_TeSt_Bd""" in """${output}"""
    Execute Query   pg-${PG_CLUSTER_NAME}  DROP DATABASE IF EXISTS test_db WITH (FORCE)
    Execute Query   pg-${PG_CLUSTER_NAME}  DROP DATABASE IF EXISTS TaRgEt_TeSt_Bd WITH (FORCE)
    Execute Query   pg-${PG_CLUSTER_NAME}  DROP DATABASE IF EXISTS target_test_bd WITH (FORCE)
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}


Check Enabled Auth With Db Name Change
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    ${PG_CLUSTER_NAME}=   Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=   Get Environment Variable  POSTGRES_USER  default=postgres
    Create Database  test_db
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
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    Set To Dictionary  ${data}  backupId  ${backup_id}
    &{databases_mapping}=  Create Dictionary  test_db=target_test_bd
    Set To Dictionary  ${data}  databasesMapping  ${databases_mapping}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Dictionary Should Contain Key  ${resp.json()}  trackingId
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    ${output}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_catalog.pg_database WHERE datname = 'target_test_bd'
    Should Be True  """target_test_bd""" in """${output}"""
    &{databases_mapping}=  Create Dictionary  test_db=TaRgEt_TeSt_Bd
    Set To Dictionary  ${data}  databasesMapping  ${databases_mapping}
    ${json_data}=  Evaluate  json.dumps(${data})  json
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Dictionary Should Contain Key  ${resp.json()}  trackingId
    ${restore_id}=  Get From Dictionary  ${resp.json()}  trackingId
    FOR  ${INDEX}  IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary  ${resp.json()}  status
        Run Keyword If  '${status}' == 'In progress'  Sleep  1s
        Run Keyword If  '${status}' == 'Successful'  Exit For Loop
    END
    ${output}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_catalog.pg_database WHERE datname = 'TaRgEt_TeSt_Bd'
    Should Be True  """TaRgEt_TeSt_Bd""" in """${output}"""
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP DATABASE IF EXISTS test_db WITH (FORCE)
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP DATABASE IF EXISTS TaRgEt_TeSt_Bd WITH (FORCE)
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP DATABASE IF EXISTS target_test_bd WITH (FORCE)
    #delete backup after test
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}
