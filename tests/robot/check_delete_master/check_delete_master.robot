*** Settings ***
Documentation     Check delete master
Library           Collections
Library           OperatingSystem
Library           String
Resource          ../Lib/lib.robot


*** Test Cases ***
Check Delete Master
    [Tags]  patroni full  check_delete_master
    Run Keyword  Checks Before Tests

    ${MASTER}=  Get Master Pod
    ${OLD_MASTER_NAME}=  Set Variable  ${MASTER.metadata.name}

    # insert test records before deleting master
    ${RID}  ${EXPECTED}=  Insert Test Record  ${MASTER.status.pod_ip}

    Log To Console  Deleting Master Pod "${OLD_MASTER_NAME}"
    Run Keyword  Delete Pod  ${OLD_MASTER_NAME}  30

    Log To Console  Wait until cluster recovers after master deletion
    Wait Until Keyword Succeeds  300 sec  5 sec  Wait Replica Pods In Up State
    Wait Until Keyword Succeeds  300 sec  5 sec  Check Replica Count

    ${NEW_MASTER}=  Get Master Pod
    Log To Console  Current Master ${NEW_MASTER.metadata.name}

    Log To Console   Test Current Master Works
    Wait Until Keyword Succeeds  300 sec  5 sec  Insert Test Record  ${NEW_MASTER.status.pod_ip}

    Wait Until Keyword Succeeds  300 sec  5 sec  Check Replica Count
    Wait Until Keyword Succeeds  300 sec  5 sec  Replication Works