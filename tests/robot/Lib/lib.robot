*** Settings ***
Documentation     Lib
Library           Collections
Library           DateTime
Library           OperatingSystem
Library           String
Library           RequestsLibrary
Library           ../Lib/pgsLibrary.py  namespace=${NAMESPACE}  ssl_mode=${PGSSLMODE}  internal_tls=${INTERNAL_TLS_ENABLED}

*** Variables ***
${NAMESPACE}        %{POD_NAMESPACE}
${PGSSLMODE}        %{PGSSLMODE}
${INTERNAL_TLS_ENABLED}    %{INTERNAL_TLS_ENABLED}

*** Keywords ***
Checks Before Tests
    [Documentation]
    ...
    ...   standart tests before tastcases
    ...
    Check Master Exists
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    Check Replica Count
    Replication Works

Patroni Ready
    [Documentation]
    ...
    ...  Check patroni is ready
    ...
    ...  1. all replicas available
    ...  2. master pod exist
    ...  3. Try connect to postgres
    ...  4. Check if replication works
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    Wait Until Keyword Succeeds   300 sec   3 sec   Check Replica Count
    Wait Until Keyword Succeeds   300 sec   3 sec   Check Master Exists
    Wait Until Keyword Succeeds   120 sec   2 sec   Execute Query  pg-${pg_cluster_name}  SELECT 1
    Replication Works

    Wait Until Keyword Succeeds   300 sec   3 sec   Check replica count
    Wait Until Keyword Succeeds   300 sec   3 sec   Check master exists
    Wait Until Keyword Succeeds   120 sec   2 sec   execute query  pg-${pg_cluster_name}  select 1
#    Check if replication works
    Replication works
Check master exists
    [Documentation]
    ...
    ...  Check  if master pod patroni exists
    ...
    Log To Console   \n---== Check master exists ==---
    ${MASTER}=   Get Pod   label=pgtype:master   status=Running
    Should Not Be Empty  ${MASTER.metadata.name}
    Log To Console  Master pod: "${MASTER.metadata.name}"

Check Replica Count
    [Documentation]
    ...
    ...  Check  count on replicas
    ...
    ${pg_node_qty}=   Get Environment Variable   PG_NODE_QTY   default=1
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    ${pg_node_qty}=  Convert To Integer  ${pg_node_qty}
    @{pods}=   Get Pods   label=pgcluster:${pg_cluster_name}   status=Running
    ${count}=   Get Length   ${pods}
    Should Be Equal  ${count}  ${pg_node_qty}

Replication Works
    [Documentation]
    ...
    ...  Check replication works
    ...  Insert random message and check if it replicated to replica
    ...
    Log To Console  -== Check if replication works ==-
    ${MASTER}=  Get Environment Variable   PG_HOST   default=pg-patroni
    ${pg_cluster_name}=  Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    ${RID}  ${EXPECTED}=  Insert Test Record   ${MASTER}
    Log To Console  Insterted test recordes with ID=${RID} (${EXPECTED})
    Log To Console  search test recordes on replicas
    &{REPLICAS}=   Get Pods Names Ip   label=pgcluster:${pg_cluster_name}  status=Running
    Log To Console  Pods and ip &{REPLICAS}
    FOR    ${key}    IN    @{REPLICAS.keys()}
        Log To Console   Finding test records in ${key} IP ${REPLICAS["${key}"]}
        Wait Until Keyword Succeeds   ${120}   1 sec   wait query   ${key}   ${REPLICAS["${key}"]}   select * from test_insert_robot where id=${RID}   ${EXPECTED}
        Log To Console   Test record found on ${key} IP ${REPLICAS["${key}"]}
    END
    [Teardown]

Insert Test Record
    [Arguments]    ${MASTERHOST}=pg-patroni   ${database}=postgres
    [Documentation]
    ...
    ...  Generate and insert test record into master
    ...
    ${MASTERHOST}=  Get Environment Variable   PG_HOST   default=pg-patroni
    ${RSTRING}=  Generate Random String   32
    ${RID}=  evaluate  1000 * int(time.time()) + random.randint(1,9999)    random,time
    ${EXPECTED}=  Set Variable   ${RID}, '${RSTRING}'
    Log   random values ${RID} ${RSTRING}
    ${res}=  Execute Query   ${MASTERHOST}  create table IF NOT EXISTS test_insert_robot (id bigint primary key not null, value text not null)   dbname=${database}
    ${res}=  Execute Query   ${MASTERHOST}  insert into test_insert_robot values (${RID}, '${RSTRING}')   dbname=${database}
    Run Keyword If   ${res} != None   Fail   msg=[insert test record] error in insert : ${res}
    ${res}=  Execute Query   ${MASTERHOST}  select * from test_insert_robot where id=${RID}   dbname=${database}
    Should Be True   """${EXPECTED}""" in """${res}"""   msg=[insert test record] Expected string ${EXPECTED} not found on ${MASTERHOST} : res: ${res}
    Log To Console  Test records found on ${MASTERHOST}
    [Return]   ${RID}   ${EXPECTED}

Check Test Record
    [Arguments]    ${pod_name}   ${RID}   ${EXPECTED}    ${database}=postgres
    [Documentation]
    ...
    ...  check test record existance
    ...
    ${res}=  Execute Query   ${pod_name}   select * from test_insert_robot where id=${RID}   dbname=${database}
    Should Be True   """${EXPECTED}""" in """${res}"""   msg=[insert test record] Expected string ${EXPECTED} not found after restore database: ${database}. res: ${res}

Wait Query
    [Arguments]    ${pod_name}   ${pod_ip}   ${query}   ${expected}
    [Documentation]
    ...
    ...  wait while query return expected value
    ...
    Log To Console  Wait ${expected} in ${pod_name}
    ${res}=  Execute Query   ${pod_ip}  ${query}
    Should Be True   """${expected}""" in """${res}"""   msg=Expected string not found in pod ${pod_name} IP:${pod_ip} output: ${res}

Check Pods Binding
    [Documentation]
    ...
    ...  Check patroni's pod binding
    ...
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    @{PODS}=   Get Pods   repl_name=pg-${pg_cluster_name}
    FOR   ${POD}   IN  @{PODS}
       ${skip}=   Check Affinity Rules   ${POD}
       Exit For Loop If   ${skip}==True
       Log To Console   ${POD.spec.node_selector}
       Log To Console   Found selector ${POD.spec.node_selector['kubernetes.io/hostname']}
       Should Not Be Empty   ${POD.spec.node_selector['node-role.kubernetes.io/compute']   msg=pod ${POD.metadata.name} has no node-selector
       Log To Console   Pod ${POD.metadata.name} binded to ${POD.spec.node_selector['kubernetes.io/hostname']}
    END

Check Limits
    [Documentation]
    ...
    ...  Check patroni pod's limits
    ...
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    ${unlimited}=   Get Environment Variable   UNLIMITED   default=false
    ${condtion}=	Convert To Lower Case	${unlimited}
    @{PODS}=  Get Pods   repl_name=pg-${pg_cluster_name}
    IF  '${condtion}' != 'true'
        FOR   ${POD}   IN   @{PODS}
           Check Pod Resource   ${POD.metadata.name}  limits   memory
           Check Pod Resource   ${POD.metadata.name}  limits   cpu
           Check Pod Resource   ${POD.metadata.name}  requests   memory
           Check Pod Resource   ${POD.metadata.name}  requests   cpu
        END
    ELSE
        FOR   ${POD}   IN   @{PODS}
           Check Pod Resource   ${POD.metadata.name}  requests   memory
           Check Pod Resource   ${POD.metadata.name}  requests   cpu
        END
    END

Check If Patroni CLI Works
    [Documentation]
    ...
    ...  Check if patroni CLI works
    ...
    ${MASTER}=  Get Master Pod Id
    ${CMD}   Set Variable   cd /patroni && patronictl -c ./pg_node.yml list patroni
    ${resp}  ${error} =   Execute In Pod   ${MASTER}   ${CMD}
    Should Not Be Empty   ${resp}

Patroni REST Working
    [Documentation]
    ...
    ...  Check if patroni REST works
    ...
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    @{PODS}=   Get Pods   repl_name=pg-${pg_cluster_name}
    FOR   ${POD}   IN   @{PODS}
       ${pod_ip}   Get Ip   ${POD.status.pod_ip}
       Log To Console   Pod ${POD.metadata.name} has ip ${pod_ip}
       ${resp}  ${error} =   Execute In Pod   ${POD.metadata.name}   curl -s ${pod_ip}:8008
       Should Not Be Empty   ${resp}
       Log To Console  REST responce: ${resp}
    END

Check Pod Resource
    [Arguments]    ${pod_name}   ${type}   ${resource_name}
    [Documentation]
    ...
    ...  Check patroni pod's limits
    ...  pod_name - name of pod
    ...  type = requests or limits
    ...  resource_name - name of resource
    ...
    ${POD}=   Get Pods   repl_name=${pod_name}
    ${resources}=   Set Variable   ${POD[0].spec.containers[0].resources}
    ${dict}=   Set Variable   ${resources.${type}}
    Dictionary Should Contain Key   ${dict}   ${resource_name}   msg=Pod ${pod_name} has no ${type} for ${resource_name}
    ${value}=   Set Variable   ${dict['${resource_name}']}
    Log To Console  ${type} ${resource_name} in pod ${pod_name} has value ${value}
    Should Not Be Empty   ${value}   msg=${type} ${resource_name} is empty in pod ${pod_name}
    Should Not Be Equal   '${value}'   'None'    msg=${type} ${resource_name} has None value in pod ${pod_name}


Check If Patroni REST Authentication Working
    [Documentation]
    ...
    ...  Check if patroni REST authentication works
    ...
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    @{PODS}=   Get Pods   repl_name=pg-${pg_cluster_name}
    FOR   ${POD}   IN   @{PODS}
       ${pod_ip}   Set Variable   ${POD.status.pod_ip}
       Log To Console   pod ${POD.metadata.name} has ip ${pod_ip}
       ${resp}=   Execute Auth Check
       Should Be Equal As Strings   ${resp}   ${true}   msg=Patroni REST authentication is not working: resp: ${resp}
       Log To Console  REST responce: ${resp}
    END

Check Pod Running
    [Arguments]    ${key}   ${value}
    ${POD}=  Get Pod   ${key}=${value}   status=Running
    Should Not Be Empty  ${POD.metadata.name}
    Log To Console   Pod: "${POD.metadata.name}" is running

Create Empty List
    Set Test Variable  @{databases}    @{EMPTY}

Check If New Master Elected
    [Arguments]    ${MASTER}
    [Documentation]
    ...
    ...  Check if new master elected
    ...
    ${new_master}=   Get Master Pod Id  # function fails if cannot find master so no check for empty or None values
    Run Keyword If   '${new_master}' == '${MASTER}'   fail   msg=New master not selected: old:[${MASTER}] now: ${new_master}
    Log To Console   Found master ${new_master}

#########################
### for backup daemon ###
#########################
Backup-daemon Pod Running
    Wait Until Keyword Succeeds   300 sec   2 sec   Check pod running   label   app:postgres-backup-daemon

Backup-deamon Health Status Through Rest Is OK
    Wait Until Keyword Succeeds   90 sec   2 sec   Check Deamon Health Status Through Rest

Check Daemon Replicas Count
    ${replicas}=   Convert To Integer   1
    @{pods}=   Get Pods   label=app:postgres-backup-daemon
    ${count}=   Get Length   ${pods}
    ${count_int}=   Convert To Integer   ${count}
    Log To Console   Daemon replicas count must be: ${replicas}, Now daemon replicas is ${count}
    Should Be Equal   ${count_int}   ${replicas}

Check Deamon Health Status Through Rest
    ${PGSSLMODE} =  Get Environment Variable  PGSSLMODE
    ${scheme} =  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    ${url} =  Set Variable  ${scheme}://postgres-backup-daemon:8080
    Log  ${url}
    Create Session  postgres_backup_daemon  ${url}
    ${resp}=  GET On Session  postgres_backup_daemon  /health  timeout=10
    Should Be Equal As Integers  ${resp.status_code}  200
    Dictionary Should Contain Key  ${resp.json()}  status
    ${status}=  Get From Dictionary  ${resp.json()}  status
    Should Contain Any  ${status}  WARNING  UP

Check /health Endpoint For Full Backups
    [Documentation]
    ...  This test case validates that if we will request `/health` endpoint
    ...  response code should be `200` and response should contain `storage` key
    ...  and `status` key with `UP` value
    ...
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:8080  verify=False
    ${resp}=  GET On Session  postgres_backup_daemon  /health  timeout=10
    Should Be Equal  ${resp.status_code}  ${200}
    Dictionary Should Contain Key    ${resp.json()}    storage
    ${status}=  Get From Dictionary    ${resp.json()}    status
    Should Be Equal  ${status}  UP

Create Backup And Wait Till Complete
    [Arguments]   ${name_space}=app_test_download  ${databases}={}
    [Documentation]
    ...  This test case validates that if Authentication is enabled it needs to
    ...  provide `postgres` credentials, otherwise it is no needed to provide credentials for request
    ...  After authentication part test case validates that if we will try to request backup
    ...  with data it will not fail
    ...
    Log   ${databases}
    ${res}=  Get Auth
    Run Keyword If     "${databases}" == "&{EMPTY}"    Create Empty List
    Run Keyword If     '${res}' == "false"     Check Disabled Auth   ${name_space}    ${databases}
    Run Keyword If     '${res}' == "true"     Check Enabled Auth   ${name_space}     ${databases}

Create Granular Backup
    [Arguments]    ${name_space}  ${databases}  ${auth}={}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    Set Test Variable   \${name_space}     ${name_space}
    &{data}=  Create Dictionary  namespace=${name_space}  databases=${databases}
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    Log   ${databases}
    Log   ${data}
    ${resp}=  POST On Session  postgres_backup_daemon  /backup/request  json=${data}  headers=${headers}
    Should Be Equal  ${resp.status_code}  ${202}
    Dictionary Should Contain Key    ${resp.json()}    backupId
    ${backup_id}=  Get From Dictionary    ${resp.json()}    backupId
    Set Test Variable   \${backup_id}    ${backup_id}

Wait Complete Granular Backup
    [Arguments]     ${backup_id}    ${name_space}   ${auth}={}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000    auth=${auth}
    ${resp}=    GET On Session  postgres_backup_daemon  url=/backup/status/${backup_id}?namespace=${name_space}
    Log   ${resp.json()}
    ${status}=  Get From Dictionary    ${resp.json()}   status
    Should Be Equal   ${status}  Successful

Delete Granular Backup
    ${resp}=  GET On Session  postgres_backup_daemon  url=/delete/${backup_id}?namespace=${name_space}
    Should Be Equal  ${resp.status_code}  ${200}

Check /backups Endpoint For Granular Backups
    [Documentation]
    ...  This test case validates that if we will request `/health` endpoint
    ...  response code should be `200` and response should contain `storage` key
    ...  and `status` key with `UP` value
    ...
    ${PG_ROOT_PASSWORD}=   Get Environment Variable   PG_ROOT_PASSWORD
    ${auth}=  Create List    postgres  ${PG_ROOT_PASSWORD}
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000  auth=${auth}
    ${resp}=  GET On Session  postgres_backup_daemon  /backups
    Should Be Equal  ${resp.status_code}  ${200}

Create Database
    [Arguments]  ${base_name}
    Create Test Db  ${base_name}
    Set Test Variable  @{databases}    ${base_name}
    Log    ${databases}

Create Database With Owner
    [Arguments]  ${base_name}  ${role_name}
    Create Test Db With Role  ${role_name}  ${base_name}
    Log  Created DB: ${base_name} with owner: ${role_name}

Create New Role
    [Arguments]  ${role_name}
    Create Role  ${role_name}
    Log  Created role: ${role_name}

Delete Database
    [Arguments]  ${base_name}
     Delete Test Db  ${base_name}

Check Enabled Auth
    [Arguments]    ${name_space}     ${databases}={}
    #check that unauthorized access is forbidden
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session    postgres_backup_daemon    ${scheme}://postgres-backup-daemon:9000
    ${resp}=  GET On Session  postgres_backup_daemon  /delete/test?namespace=test
    Should Be Equal  ${resp.status_code}  ${401}
    #set auth credentials
    ${PG_ROOT_PASSWORD}=   Get Environment Variable   PG_ROOT_PASSWORD
    ${auth}=  Create List    postgres  ${PG_ROOT_PASSWORD}
    Create Granular Backup  ${name_space}  ${auth}  ${databases}
    #wait backup complete
    Wait Until Keyword Succeeds  10min   10sec    Wait Complete Granular Backup    ${backup_id}     ${name_space}    ${auth}

Check Disabled Auth
    [Arguments]    ${name_space}  ${databases}={}
    Create Granular Backup  ${name_space}   ${databases}
    #wait backup complete
    Wait Until Keyword Succeeds  10min   10sec    Wait complete granular backup    ${backup_id}     ${name_space}

#Add keywords for backup
Check Full Backup Creation
    Log To Console  Schedule new backup
    ${pod} =  pgsLibrary.Get Pod  label=app:postgres-backup-daemon  status=Running
    ${dump_count} =  Get Backup Count
    Log To Console  DUMP: ${dump_count}
    Schedule Backup
    Log to console  new backup scheduled
    Wait For Backup To Complete  ${pod.metadata.name}  ${dump_count}
    Log to console  finish waiting
    Log To Console  Check if list call returns all finished backups
    # check case if we start test around xx:00 and scheduled backup was executed during tests
    Check Backup Count  ${dump_count}
    ${last_backup_size} =  check_last_backup_size
    Run Keyword If  int(${last_backup_size}) > 0  Log To Console  SUCCESS
    ${last_backup_id} =  check_last_backup_id
    Log To Console  LAST_BACKUP_ID: ${last_backup_id}
    Log To Console  POD_NAME: ${pod.metadata.name}
    Check If Backup Files Present  ${pod}  ${last_backup_id}

Check Evict Api
    Log To Console  Start check for evict API
    ${pod} =  pgsLibrary.Get Pod  label=app:postgres-backup-daemon  status=Running
    Log To Console  Schedule new backup
    ${dump_count} =  Get Backup Count
    Schedule Backup
    Wait For Backup To Complete  ${pod.metadata.name}  ${dump_count}
    ${last_backup_id} =  Check Last Backup Id
    Check If Backup Files Present  ${pod}  ${last_backup_id}
    ${health_json} =  Schedule Evict  ${last_backup_id}
    Log To Console  SCHEDULE_EVICT: ${health_json}
    Wait For Evict To Complete  ${pod.metadata.name}  ${last_backup_id}
    Check If Backup Files Absent  ${pod}  ${last_backup_id}
    Log To Console  SUCCESS

Check Backup Api With Broken Metric File
    Log To Console  Start check for backup API with lost metric file
    Log To Console  Schedule new backup for tests
    ${pod} =  pgsLibrary.Get Pod  label=app:postgres-backup-daemon  status=Running
    ${dump_count} =  Get Backup Count
    Schedule Backup
    Wait For Backup To Complete  ${pod.metadata.name}  ${dump_count}
    ${last_backup_id} =  Check Last Backup Id
    Log To Console  Remove metric file for backup ${last_backup_id}
    Log to console  Check folder and version
    ${pg_ver}  ${backup_dir} =  Detect Pg Version And Storage Path
    Log to console  folder and version: ${pg_ver} ${backup_dir}
    Check Storage Type  ${pod}  ${last_backup_id}  ${backup_dir}
    ${corrupted_backup_id} =  Set Variable  ${last_backup_id}
    Log To Console  Try to schedule backup after removed file
    ${dump_count} =  Get Backup Count
    Schedule Backup
    Wait For Backup To Complete  ${pod.metadata.name}  ${dump_count}
    ${last_backup_id} =  Check Last Backup Id
    Log To Console  New backup created: ${last_backup_id}
    Delete Corupted Backup  ${pod.metadata.name}  ${backup_dir}  ${corrupted_backup_id}
    Log To Console  SUCCESS

Wait Replica Pods In Up State
    ${replicas_status}=  Wait Replica Pods Scale up
    Should Be Equal  ${replicas_status}  ${True}
