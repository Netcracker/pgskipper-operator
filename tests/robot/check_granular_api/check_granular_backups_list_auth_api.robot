*** Settings ***
Documentation     Check granular backups list REST API
Library           RequestsLibrary
Library           Collections
Library           DateTime
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Keywords ***
Create Backup And Wait
    ${name_space}=  Get Current Date  result_format=%Y%m%d%H%M
    Create Backup And Wait Till Complete  ${name_space}

Check Backup List
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    # wait while daemon will start backup
    ${backups_in_namespace}=  Create Dictionary
    FOR  ${INDEX}  IN RANGE  1  60
        ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
        ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
        Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000  auth=${auth}
        ${resp}=  Get On Session  postgres_backup_daemon  url=/backups
        Run Keyword If  ${resp.status_code} != ${200}  Log  ${resp.text}
        Run Keyword If  ${resp.status_code} == ${200}  Set Backups  ${resp}  ${name_space}
        Log  ${backups_in_namespace}
        ${b_count}=  Get Length  ${backups_in_namespace}
        Log  ${b_count}
        Run Keyword If  ${b_count} != ${1}  Log  ${resp.text}
        Run Keyword If  ${b_count} == ${1}  Exit For Loop
        Sleep  1s
    END
    Should Be Equal  ${resp.status_code}  ${200}
    Length Should Be  ${backups_in_namespace}    1
    Delete Granular Backup

Set Backups
    [Arguments]  ${resp}  ${name_space}
    ${result}=  Get From Dictionary  ${resp.json()}  ${name_space}
    Set Test Variable  ${backups_in_namespace}  ${result}
    Log  ${result}
    RETURN  ${result}

*** Test Cases ***
Check Backup Requests Status Endpoint
    [Tags]  backup full  check_granular_api
    Given Check /backups Endpoint For Granular Backups
    When Create Backup And Wait
    Then Check Backup List


