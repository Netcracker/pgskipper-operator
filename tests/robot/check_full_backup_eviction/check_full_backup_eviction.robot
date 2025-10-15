*** Settings ***
Documentation     Check daemon installation correctness
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Daemon Eviction API For Full Backups
    [Tags]  backup full  check_evict_api
    [Documentation]
    ...  This test validates if daemon can evict backup via REST API
    ...
    Check Evict Api
