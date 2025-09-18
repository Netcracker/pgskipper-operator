*** Settings ***
Documentation     Check authentication for granular backup restore status REST API
Library           RequestsLibrary
Library           Collections
Library           OperatingSystem
Resource          ../Lib/lib.robot
Library           DateTime

*** Test Cases ***
Check Status Restore Endpoint
    [Tags]  backup full  check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request
    ...  After authenticate part test case validates that if we will try to restore successful backup
    ...  restore procedure will not fail and trackingId of restore process will be provided
    ...  and restore process has `Successful` status
    ...
    ${res}=  Get Auth
    Run Keyword If     '${res}' == "false"     Check Disabled Auth restore endpoint
    Run Keyword If     '${res}' == "true"     Check Enabled Auth restore endpoint

Check status restore request endpoint for not existing backup
    [Tags]    backup full    check_granular_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request
    ...  After authenticate part test case validates that if we will try to check status for not existing
    ...  backup restore process response code will be 404 `NOT_FOUND`
    ...
    ${res}=    get_auth
    Run Keyword If  '${res}' == "false"  Check Disabled Auth Not Existing
    Run Keyword If  '${res}' == "true"  Check Enabled Auth Not Existing

*** Keywords ***
Check Disabled Auth Restore Endpoint
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER
    ${db_name}  Set Variable  test_restore_status_db
    Create Database  ${db_name}
    ${databases}=  Create List  ${db_name}
    &{data}=  Create Dictionary  databases=${databases}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${json_data}=    Evaluate    json.dumps(${data})    json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    Dictionary Should Contain Key    ${resp.json()}    backupId
    ${backup_id}=  Get From Dictionary    ${resp.json()}    backupId

    FOR    ${INDEX}    IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If    '${status}' == 'In progress'    Sleep    1s
        Run Keyword If    '${status}' == 'Successful'    Exit For Loop
    END

    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}
    Should Be Equal  ${resp.status_code}  ${200}
    ${status}=  Get From Dictionary    ${resp.json()}    status
    Should Be Equal  ${status}  Successful

    &{data}=  Create Dictionary  backupId=${backup_id}
    ${json_data}=    Evaluate    json.dumps(${data})    json
    &{headers}=  Create Dictionary  Content-Type=application/json
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Dictionary Should Contain Key    ${resp.json()}    trackingId
    ${restore_id}=  Get From Dictionary    ${resp.json()}    trackingId

    FOR    ${INDEX}    IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If    '${status}' == 'In progress'    Sleep    1s
        Run Keyword If    '${status}' == 'Successful'    Exit For Loop
    END

    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
    Should Be Equal  ${resp.status_code}  ${200}
    execute query   pg-${PG_CLUSTER_NAME}   drop database if exists ${db_name}

    #delete backup after test
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}
    Should Be Equal  ${resp.status_code}  ${200}

Check Enabled Auth restore endpoint
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/test  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}

    ${PG_ROOT_PASSWORD}=   Get Environment Variable   PG_ROOT_PASSWORD
    ${auth}=  Create List    postgres  ${PG_ROOT_PASSWORD}
    ${PG_CLUSTER_NAME}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    ${POSTGRES_USER}=   Get Environment Variable   POSTGRES_USER   default=postgres
    ${db_name}   set variable   test_restore_status_db

    Create database  ${db_name}
    ${databases}=  Create List  ${db_name}
    &{data}=  Create Dictionary  databases=${databases}
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${json_data}=    Evaluate    json.dumps(${data})    json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    Dictionary Should Contain Key    ${resp.json()}    backupId
    ${backup_id}=  Get From Dictionary    ${resp.json()}    backupId

    FOR    ${INDEX}    IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If    '${status}' == 'In progress'    Sleep    1s
        Run Keyword If    '${status}' == 'Successful'    Exit For Loop
    END

    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/${backup_id}
    Should Be Equal  ${resp.status_code}  ${200}
    ${status}=  Get From Dictionary    ${resp.json()}    status
    Should Be Equal  ${status}  Successful

    &{data}=  Create Dictionary  backupId=${backup_id}
    ${json_data}=    Evaluate    json.dumps(${data})    json
    &{headers}=  Create Dictionary  Content-Type=application/json
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  data=${json_data}  headers=${headers}
    Dictionary Should Contain Key    ${resp.json()}    trackingId
    ${restore_id}=  Get From Dictionary    ${resp.json()}    trackingId

    FOR    ${INDEX}    IN RANGE  60
        ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
        ${status}=  Get From Dictionary    ${resp.json()}    status
        Run Keyword If    '${status}' == 'In progress'    Sleep    1s
        Run Keyword If    '${status}' == 'Successful'    Exit For Loop
    END

    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/${restore_id}
    Should Be Equal  ${resp.status_code}  ${200}
    execute query   pg-${PG_CLUSTER_NAME}   drop database if exists ${db_name}

    #delete backup after test
    ${resp}=  Get On Session  postgres_backup_daemon  url=/delete/${backup_id}
    Should Be Equal  ${resp.status_code}  ${200}

Check Disabled Auth not existing
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/restore-42  expected_status=404
    Should Be Equal  ${resp.status_code}  ${404}

Check Enabled Auth not existing
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  Get On Session  postgres_backup_daemon  url=/restore/status/test  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}

    ${PG_ROOT_PASSWORD}=   Get Environment Variable   PG_ROOT_PASSWORD
    ${auth}=  Create List    postgres  ${PG_ROOT_PASSWORD}

    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  Get On Session  postgres_backup_daemon  url=/backup/status/restore-42  expected_status=404
    Should Be Equal  ${resp.status_code}  ${404}
