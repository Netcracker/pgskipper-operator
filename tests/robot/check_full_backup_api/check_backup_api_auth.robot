*** Settings ***
Documentation     Feature: Authentication for backup agent for full backups
Library           RequestsLibrary
Library           Collections
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Test Cases ***
Auth Is Needed When Enabled
    [Tags]  backup full  check_backup_api
    [Documentation]
    ...  This test case validates that if Authentication is enabled need to
    ...  provide `postgres` credentials, otherwise no need to provide credentials
    ...
    ${res}=  Get Auth
    Run Keyword If    '${res}' == "false"    Check Disabled Auth
    Run Keyword If    '${res}' == "true"    Check Enabled Auth

*** Keywords ***
Check Disabled Auth
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8081
    ${resp}=  GET On Session  postgres_backup_daemon  /backups/list
    Should Be Equal  ${resp.status_code}  ${200}

Check Enabled Auth
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8081
    ${resp}=  GET On Session  postgres_backup_daemon  /backups/list  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}
    ${PG_ROOT_PASSWORD}=  Get Secret Or Env   PG_ROOT_PASSWORD  ${PG_ROOT_PASSWORD_PATH}
    ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8081  auth=${auth}
    ${resp}=  GET On Session  postgres_backup_daemon  /backups/list
    Should Be Equal  ${resp.status_code}  ${200}
