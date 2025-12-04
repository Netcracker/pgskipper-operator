*** Settings ***
Resource    ../Lib/lib.robot

*** Keywords ***
Patroni Cluster Is Healthy
    ${pg_node_qty}=  Get Environment Variable   PG_NODE_QTY   default=1
    ${pg_node_qty}=  Convert To Integer  ${pg_node_qty}
    Pass Execution If   '${pg_node_qty}'<'2'   Log To Console skip test because pg_node_qty ${pg_node_qty}
    Checks Before Tests

Manual Switchover Via Patroni REST Is Called
    Make Switchover Via Patroni REST

*** Test Cases ***
Manual Switchover Via Patroni REST
    [Tags]  patroni full   check_manual_switchover
    Given Patroni Cluster Is Healthy
    When Manual Switchover Via Patroni REST Is Called
    Then Patroni Cluster Is Healthy
