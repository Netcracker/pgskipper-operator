*** Settings ***
Documentation     Check daemon installation correctness
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Daemon Full Backup Creation With Previous Backup Without Metric File
    [Tags]  backup full  check_backup_no_metric_in_last_backup
    [Documentation]
    ...  This test validates if daemon pod can perform full backup with previous backup without metric file
    ...
    Check Backup Api With Broken Metric File
