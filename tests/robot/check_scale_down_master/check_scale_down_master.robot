*** Settings ***
Documentation     Check scaledown replica
Library           Collections
Library           OperatingSystem
Library           String
Resource          ../Lib/lib.robot

*** Keywords ***
Check Master Reelection
    [Arguments]  ${MASTER_POD_NAME}
    ${new_master}=   Get Master Pod Id
    Should Be True  '${new_master}' != '${None}'
    Should Be True  '${new_master}' != '${MASTER_POD_NAME}'
    RETURN  ${new_master}


*** Test Cases ***
Check Scale Down Master
    [Tags]   patroni full  check_scale_down_master
    [Documentation]
    ...
    ...  Check election new master
    ...
    # skip check scale down master if only one patroni pod
    ${pg_node_qty}=  Get Environment Variable   PG_NODE_QTY   default=1
    ${pg_node_qty}=  Convert To Integer  ${pg_node_qty}
    Pass Execution If   '${pg_node_qty}'<'2'   Log To Console skip test because pg_node_qty ${pg_node_qty}
    # check relication works before tests
    Log To Console   Run pretest tests
    Run Keyword   Checks before tests
    Log To Console   --== check scale down master ==--
    # get master and check it work
    ${MASTER}=  Get Master Pod
    ${MASTER_POD_NAME}   Set Variable  ${MASTER.metadata.name}
    ${MASTER_STATEFULSET_NAME}=  Evaluate  "${MASTER_POD_NAME}".rsplit("-", 1)[0]
    Log To Console   Master : ${MASTER_STATEFULSET_NAME} -> ${MASTER_POD_NAME}
    Log To Console   Insert test record into master ${MASTER_POD_NAME}
    ${RID}  ${EXPECTED}=  Insert Test Record   ${MASTER_POD_NAME}
    # check records on replicas. What for? It's checked in "get master and check it work" keyword
    Sleep  10
    Log To Console  check test data on all replicas
    @{REPLICAS}=   Get Pods   label=pgtype:replica   status=Running
    FOR   ${REPLICA}   IN   @{REPLICAS}
        Log To Console   Check test data on ${REPLICA.metadata.name} pod
        Check Test Record    ${REPLICA.status.pod_ip}   ${RID}   ${EXPECTED}
    END

    # scale down master
    Log To Console  Scale Down Master Statefulset ${MASTER_STATEFULSET_NAME}
    Scale Down Stateful Set  ${MASTER_STATEFULSET_NAME}
    Sleep  10

    # wait new master election
    Wait Until Keyword Succeeds   120 sec   2 sec
    ...  Check Master Reelection  ${MASTER_POD_NAME}
    ${new_master}=   Get Master Pod Id
    Log To Console   New master '${new_master}' elected successeful
    Run Keyword If   '${new_master}' == '${MASTER_POD_NAME}'   fail   msg=New master not selected: old:[${MASTER_POD_NAME}] now: ${new_master}

    # statefulsetconfig name must be differs
    Log To Console  Check statefulset config
    ${NEW_MASTER}=   Get Master Pod
    ${NEW_MASTER_POD_NAME}   Set Variable  ${NEW_MASTER.metadata.name}
    ${NEW_MASTER_STATEFULSET_NAME}=  Evaluate  "${NEW_MASTER_POD_NAME}".rsplit("-", 1)[0]
    Run Keyword If   '${NEW_MASTER_POD_NAME}' == '${MASTER_STATEFULSET_NAME}'   Fail   msg=Unexpected statefulsetconfig name

    # check master
    Log To Console   Check Test Record On New Master
    Check Test Record    ${NEW_MASTER.status.pod_ip}   ${RID}   ${EXPECTED}

    # check if new master read-only
    Log To Console   Insert Test Record Into New Master [${NEW_MASTER_POD_NAME}]
    Wait Until Keyword Succeeds   60 sec   1 sec
    ...  Insert Test Record   ${NEW_MASTER.status.pod_ip}

    # scale up old master
    Log To Console  Scale Up Old Master Pod Stateful Set ${MASTER_STATEFULSET_NAME}
    Scale Up Stateful Set  ${MASTER_STATEFULSET_NAME}
    Sleep  20s

    # wait while all replicas back
    Wait Until Keyword Succeeds   120 sec   3 sec   Check replica count

    # check replication
    Run Keyword    Replication Works
    Sleep  20s
