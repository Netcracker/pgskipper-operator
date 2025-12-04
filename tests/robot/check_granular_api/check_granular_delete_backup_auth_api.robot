*** Settings ***
Documentation     Check granular backups delete REST API
Library           RequestsLibrary
Resource          ../Lib/lib.robot


*** Keywords ***
Backup Not Exist
    ${resp}=  Get Request  postgres_backup_daemon  /backup/status/${backup_id}?namespace=${name_space}
    Should Be True  '${resp.status_code}'!=' ${200}'

*** Test Cases ***
Check Backup Requests Status Endpoint
    [Tags]  backup full  check_granular_api
    Given Check /backups Endpoint For Granular Backups
    When Create Backup And Wait Till Complete
    And Delete Granular Backup
    Then Backup Not Exist
