*** Settings ***
Documentation     Check scaledown replica
Library           Collections
Library           OperatingSystem
Library           String
Resource          ../Lib/lib.robot


*** Test Cases ***
Check Delete Master
    [Tags]  patroni full  check_delete_master
    Run Keyword  Checks Before Tests
    ${MASTER}=  Get Master Pod
    # insert test records
    ${RID}  ${EXPECTED}=  Insert Test Record  ${MASTER.status.pod_ip}
    # delete mater pod
    Log To Console  Deleting Master Pod "${MASTER.metadata.name}"
    Run Keyword  Delete Pod  ${MASTER.metadata.name}  30
    # wait new master
    Log To Console   Wait new master election keyword
    Wait Until Keyword Succeeds  120 sec  1 sec  Check If New Master Elected  ${MASTER.metadata.name}
    # wait while all replicas back
    Wait Until Keyword Succeeds  120 sec  1 sec  Check Replica Count
    ${NEW_MASTER}=  Get Master Pod
    Log To Console  New Master ${NEW_MASTER.metadata.name}
    # wait new replica pod is up
    Wait Until Keyword Succeeds   120 sec   2 sec   Wait Replica Pods In Up State
    # check master not read-only
    Log To Console   Test New Master Works
    Wait Until Keyword Succeeds  ${120}  1 sec  Insert Test Record  ${NEW_MASTER.status.pod_ip}
    # check existance unavaliabled replicas
    Run Keyword  Check Replica Count
    # check replication again, becouse it is simple! :)
    Run Keyword   Replication Works
    