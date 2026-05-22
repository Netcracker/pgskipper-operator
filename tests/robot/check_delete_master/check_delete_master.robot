*** Settings ***
Documentation     Check delete master
Library           Collections
Library           OperatingSystem
Library           String
Library           Process
Resource          ../Lib/lib.robot


*** Test Cases ***
Check Delete Master
    [Tags]  patroni full  check_delete_master
    Run Keyword  Checks Before Tests

    ${MASTER}=  Get Master Pod
    ${OLD_MASTER_NAME}=  Set Variable  ${MASTER.metadata.name}
    ${OLD_MASTER_STS}=   Get Statefulset Name From Pod Name  ${OLD_MASTER_NAME}

    # insert test records before failover
    ${RID}  ${EXPECTED}=  Insert Test Record  ${MASTER.status.pod_ip}

    Log To Console  Scaling down old master StatefulSet "${OLD_MASTER_STS}"
    Scale Statefulset  ${OLD_MASTER_STS}  0

    Log To Console  Deleting Master Pod "${OLD_MASTER_NAME}"
    Delete Pod  ${OLD_MASTER_NAME}  30

    Log To Console  Wait old master pod deletion
    Wait Until Keyword Succeeds  120 sec  2 sec  Pod Should Not Exist  ${OLD_MASTER_NAME}

    Log To Console  Wait new master election
    Wait Until Keyword Succeeds  180 sec  2 sec  Check If New Master Elected  ${OLD_MASTER_NAME}

    ${NEW_MASTER}=  Get Master Pod
    Log To Console  New Master ${NEW_MASTER.metadata.name}

    Log To Console  Scaling old master StatefulSet "${OLD_MASTER_STS}" back to 1
    Scale Statefulset  ${OLD_MASTER_STS}  1

    # wait while all replicas are back
    Wait Until Keyword Succeeds  180 sec  2 sec  Check Replica Count
    Wait Until Keyword Succeeds  180 sec  2 sec  Wait Replica Pods In Up State

    # check new master is writable
    Log To Console   Test New Master Works
    Wait Until Keyword Succeeds  120 sec  1 sec  Insert Test Record  ${NEW_MASTER.status.pod_ip}

    Run Keyword  Check Replica Count
    Run Keyword  Replication Works


*** Keywords ***
Get Statefulset Name From Pod Name
    [Arguments]  ${pod_name}
    ${sts_name}=  Evaluate  "${pod_name}".rsplit("-", 1)[0]
    RETURN  ${sts_name}

Scale Statefulset
    [Arguments]  ${sts_name}  ${replicas}
    ${result}=  Run Process  kubectl  -n  %{POD_NAMESPACE}  scale  statefulset  ${sts_name}  --replicas  ${replicas}  stdout=PIPE  stderr=PIPE
    Log  ${result.stdout}
    Log  ${result.stderr}
    Should Be Equal As Integers  ${result.rc}  0

Pod Should Not Exist
    [Arguments]  ${pod_name}
    ${result}=  Run Process  kubectl  -n  %{POD_NAMESPACE}  get  pod  ${pod_name}  stdout=PIPE  stderr=PIPE
    Should Not Be Equal As Integers  ${result.rc}  0