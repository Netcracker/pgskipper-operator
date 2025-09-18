*** Settings ***
Documentation     Check that encrypted backups are stored encrypted
Library           RequestsLibrary
Library           Collections
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Encryption For Encrypted Backups
    [Tags]  backup full  check_backup_api
    [Documentation]
    ...  This test case validates that if we will request full backup
    ...  full backup will be stored encrypted on file system
    ...  and will be not accessible by tar gz
    ...
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8080
    ${resp}=  GET On Session  postgres_backup_daemon  /health  timeout=10
    Should Be Equal  ${resp.status_code}  ${200}
    ${encryption}=  Get From Dictionary  ${resp.json()}  encryption
    Pass Execution If  '${encryption}' == "Off"  Skippping test, encryption is not turned On
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8080
    ${resp}=  POST On Session  postgres_backup_daemon  /backup
    Should Be Equal  ${resp.status_code}  ${200}
    FOR  ${INDEX}  IN RANGE  1  120
        Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8080
        ${resp}=  GET On Session  postgres_backup_daemon  /health  timeout=10
        ${backup_in_progress}=  Get From Dictionary  ${resp.json()}  backup_is_in_progress
        Run Keyword If  ${backup_in_progress} == ${false}  Exit For Loop
        Sleep  1s
    END
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8080
    ${resp}=  GET On Session  postgres_backup_daemon  /health  timeout=10
    ${storage}=  Get From Dictionary  ${resp.json()}  storage
    ${lastSuccessful}=  Get From Dictionary  ${storage}  lastSuccessful
    Log  ${lastSuccessful}
    ${lastSuccessfulId}=  Get From Dictionary  ${lastSuccessful}  id
    Log  ${lastSuccessfulId}
    ${daemon_pod}=  Get Pod Daemon
    Log  ${daemon_pod}
    ${exit_code}=  Check Archive Is Accessible  lastSuccessfulId  daemon_pod
    Should Be Equal  ${exit_code}  ${1}
