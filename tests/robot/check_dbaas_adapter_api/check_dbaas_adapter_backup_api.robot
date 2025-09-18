*** Settings ***
Resource          keywords.robot
Test Setup        Prepare Dbaas Adapter


*** Keywords ***
Backup Database By Dbaas Adapter
    [Arguments]  ${database}
    ${data}=  Set Variable  ["${database}"]
    ${resp}=  POST On Session  dbaassession  url=/api/${api_version}/dbaas/adapter/postgresql/backups/collect?allowEviction=true  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  202
    Dictionary Should Contain Key  ${resp.json()}  trackId
    ${trackId}=  Get From Dictionary  ${resp.json()}  trackId
    [Return]  ${trackId}

Check Backup Status By Dbaas Adapter
    [Arguments]  ${trackId}
    ${resp}=  GET On Session  dbaassession  url=/api/${api_version}/dbaas/adapter/postgresql/backups/track/backup/${trackId}
    Should Be Equal  ${resp.status_code}  ${200}
    Should Contain  str(${resp.content})  status":"SUCCESS

Restore Database By Dbaas Adapter
    [Arguments]  ${database}  ${trackId}
    ${data}=  Set Variable  ["${database}"]
    ${resp}=  POST On Session  dbaassession  url=/api/${api_version}/dbaas/adapter/postgresql/backups/${trackId}/restore  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  202
    Dictionary Should Contain Key  ${resp.json()}  trackId
    ${trackId}=  Get From Dictionary  ${resp.json()}  trackId
    [Return]  ${trackId}

Check Restore Status By Dbaas Adapter
    [Arguments]  ${trackId}
    ${resp}=  GET On Session  dbaassession  url=/api/${api_version}/dbaas/adapter/postgresql/backups/track/restore/${trackId}
    Should Be Equal  ${resp.status_code}  ${200}
    Should Contain  str(${resp.content})  status":"SUCCESS

Check Eviction Backup By Dbaas Adapter
    [Arguments]  ${trackId}
    ${resp}=  DELETE On Session  dbaassession  url=/api/${api_version}/dbaas/adapter/postgresql/backups/${trackId}
    Should Be Equal  ${resp.status_code}  ${200}
    Should Be Equal As Strings  ${resp.content}  SUCCESS


*** Test Cases ***
Check Backup And Restore By Dbaas Adapter
    [Tags]  full  dbaas
    Check Database Creating By Dbaas Adapter  ${db_name}
    ${RID0}  ${EXPECTED0}=  Insert Test Record  database=${db_name}
    ${trackId}=  Backup Database By Dbaas Adapter  ${db_name}
    Wait Until Keyword Succeeds  ${RETRY_TIME}  ${RETRY_INTERVAL}
    ...  Check Backup Status By Dbaas Adapter  ${trackId}
    Delete Test DB  ${db_name}
    ${trackId}=  Restore Database By Dbaas Adapter  ${db_name}  ${trackId}
    Wait Until Keyword Succeeds  ${RETRY_TIME}  ${RETRY_INTERVAL}
    ...  Check Restore Status By Dbaas Adapter  ${trackId}
    ${databases}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT datname FROM pg_database
    Should Contain  str(${databases})  ${db_name}
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  select * from test_insert_robot where id=${RID0}   dbname=${db_name}
    Should Be True  """${EXPECTED0}""" in """${res}"""   msg=[insert test record] Expected string ${EXPECTED0} not found after restore database: ${db_name}. res: ${res}
    [Teardown]  Delete Test DB  ${db_name}

Check Evict Backup By Dbaas Adapter
    [Tags]  full  dbaas
   Check Database Creating By Dbaas Adapter  ${db_name}
    ${RID0}  ${EXPECTED0}=  Insert Test Record  database=${db_name}
    ${trackId}=  Backup Database By Dbaas Adapter  ${db_name}
    Wait Until Keyword Succeeds  ${RETRY_TIME}  ${RETRY_INTERVAL}
    ...  Check Backup Status By Dbaas Adapter  ${trackId}
    Check Eviction Backup By Dbaas Adapter  ${trackId}
    [Teardown]  Delete Test DB  ${db_name}
