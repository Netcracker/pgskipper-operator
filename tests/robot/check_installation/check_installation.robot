*** Settings ***
Documentation     Check master pod exists
Library           Collections
Library           OperatingSystem
Library           String
Resource          ../Lib/lib.robot

*** Test Cases ***
Check Installation Patroni
    [Tags]  patroni basic  check_installation_patroni
    [Documentation]
    ...
    ...  Check installation patroni
    ...
    Given Patroni Ready
    When Check Pods Binding
    And Check Limits
    And Check If Patroni CLI Works
    And Patroni REST Working
    Then Replication Works

Check Backup-daemon Installation Correctness
    [Tags]   backup basic   check_installation_backup_daemon
    [Documentation]
    ...  This test validates if daemon pod present and functioning properly
    ...
    Given Backup-daemon Pod Running
    When Check Daemon Replicas Count
    Then Backup-deamon Health Status Through Rest Is OK

Test Container Hardening
    [Tags]  basic
    Check Container Hardening
