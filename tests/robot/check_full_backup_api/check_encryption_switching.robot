*** Settings ***
Documentation     Check that encrypted backups are stored encrypted
Library           RequestsLibrary
Library           Collections
Resource          ../Lib/lib.robot

*** Test Cases ***
Check That After Redeploying Of Daemon DC Correct Health Probe Is Returned
    [Tags]  backup full  check_backup_api  check_encryption_switching
    [Documentation]
    ...  This test case validates that if we will turn on or off encryption
    ...  proper health responses will be received
    ...
    ${encryption}=  Get Encryption Status
    ${key_source}=  Get Env For Deployment  postgres-backup-daemon  KEY_SOURCE  default_value=kubernetes
    ${key_name}=  Get Env For Deployment  postgres-backup-daemon  KEY_NAME  default_value=backups-key
    Run Keyword If    '${encryption}' == "Off"    Turn Encryption On ${key_source} ${key_name}
    Run Keyword If    '${encryption}' == "On"    Turn Encryption Off
    [Teardown]  Return State ${encryption} ${key_source} ${key_name}

*** Keywords ***
Turn Encryption On ${key_source} ${key_name}
    Create Secret  ${key_name}  password  password
    ${envs}=  Create Dictionary  KEY_NAME=${key_name}  KEY_SOURCE=${key_source}
    Set Envs For Deployment  postgres-backup-daemon  ${envs}
    ${encryption}=  Get Encryption Status
    Should Be Equal  ${encryption}  On

Turn Encryption Off
    ${envs}=  Create List  KEY_NAME  KEY_SOURCE
    Unset Envs For Deployment   postgres-backup-daemon   ${envs}
    ${encryption}=  Get Encryption Status
    Should Be Equal  ${encryption}  Off

Get Encryption Status
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:8080
    ${resp}=  Get On Session  postgres_backup_daemon  /health  timeout=10
    Should Be Equal  ${resp.status_code}  ${200}
    Dictionary Should Contain Key  ${resp.json()}  storage
    ${status}=  Get From Dictionary  ${resp.json()}  status
    Should Be Equal  ${status}  UP

Return State ${state} ${key_source} ${key_name}
    Run Keyword If    '${state}' == "Off"    Turn Encryption Off
    Run Keyword If    '${state}' == "On"    Turn Encryption On ${key_source} ${key_name}
