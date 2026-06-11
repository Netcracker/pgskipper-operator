*** Settings ***
Documentation     Check granular backups delete REST API
Library           RequestsLibrary
Resource          ../Lib/lib.robot


*** Keywords ***
Backup Not Exist
    ${resp}=  GET On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}  expected_status=404
    Should Be Equal  ${resp.status_code}  ${404}

*** Test Cases ***
Check Backup Requests Status Endpoint
    [Tags]  backup full  check_granular_api
    Given Check /backups Endpoint For Granular Backups
    When Create Backup And Wait Till Complete
    And Delete Granular Backup
    Then Backup Not Exist
