*** Variables ***
${OPERATION_RETRY_COUNT}                    300
${OPERATION_RETRY_INTERVAL}                 5s

*** Settings ***
Documentation     Check scaledown backup daemon
Library           RequestsLibrary
Library           Collections
Library           OperatingSystem
Library           String
Resource          ../Lib/lib.robot


*** Keywords ***
Setup
    ${postfix}=  Generate Random String  5  [LOWER]
    Set Suite Variable  ${db_name}  test_ha_backup_${postfix}

Make Backup And Return ID
    ${PG_ROOT_PASSWORD}=   Get Environment Variable   PG_ROOT_PASSWORD
    ${auth}=  Create List    postgres  ${PG_ROOT_PASSWORD}
    ${databases}=  Create List  ${db_name}
    &{data}=  Create Dictionary  databases=${databases}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    &{headers}=  Create Dictionary  Content-Type=application/json
    ${json_data}=    Evaluate    json.dumps(${data})    json
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  data=${json_data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    Dictionary Should Contain Key    ${resp.json()}    backupId
    ${backup_id}=  Get From Dictionary    ${resp.json()}    backupId
    RETURN  ${backup_id}

Check Existence Backup Files
    [Arguments]  ${last_backup_id}
    ${backup_pod}=  Get Pod Daemon
    ${files}=  Get Backup Files  ${backup_pod}  ${last_backup_id}
    RETURN  ${files}

*** Test Cases ***
Check Scale Down Backup Daemon
    [Tags]   backup full  check_scale_down_backup_daemon
    Setup
    Create Database  ${db_name}
    ${backup_id}=  Make Backup And Return ID
    Sleep  15s
    ${files}=  Check Existence Backup Files  ${backup_id}
    Should Contain  str(${files})  status.json
    ${backup_daemon_deployment}=  Get Deployment  label_app=postgres-backup-daemon
    ${scales}=  Scale Deployment ${backup_daemon_deployment.metadata.name} To 0
    Should Not Be True  ${scales}
    Sleep  10
    ${new_backup_daemon_deployment}=  Get Deployment  label_app=postgres-backup-daemon
    ${scales}=  Scale Deployment ${new_backup_daemon_deployment.metadata.name} To 1
    Wait Until Keyword Succeeds  ${OPERATION_RETRY_COUNT}  ${OPERATION_RETRY_INTERVAL}
    ...  Check /backups Endpoint For Granular Backups
    ${new_files}=  Check Existence Backup Files  ${backup_id}
    Should Contain  str(${new_files})  status.json
    Delete Test DB  ${db_name}

