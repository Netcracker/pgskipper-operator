*** Settings ***
Library           RequestsLibrary
Library           Collections
Library           OperatingSystem
Resource          ../Lib/lib.robot


*** Variables ***
${DBAAS_ADAPTER_HOST}                  dbaas-postgres-adapter
${DBAAS_ADAPTER_PORT}                  8080
${RETRY_TIME}                          60s
${RETRY_INTERVAL}                      1s
${MICROSERVICE_NAME}                   pgtest
${SCOPE}                               pgtestscope
${NAMESPACE}                           %{POD_NAMESPACE}


*** Keywords ***
Prepare Dbaas Adapter
    ${DBAAS_ADAPTER_API_USER}  ${DBAAS_ADAPTER_API_PASSWORD}=  Get Dbaas Adapter Creds
    Set Suite Variable  ${DBAAS_ADAPTER_API_USER}
    Set Suite Variable  ${DBAAS_ADAPTER_API_PASSWORD}
    ${auth}=  Create List  ${DBAAS_ADAPTER_API_USER}  ${DBAAS_ADAPTER_API_PASSWORD}
    ${INTERNAL_TLS_ENABLED}=  Get Environment Variable  INTERNAL_TLS_ENABLED
    ${scheme}=  Set Variable If  '${INTERNAL_TLS_ENABLED}' == 'true'  https  http
    Create Session  dbaassession  ${scheme}://${DBAAS_ADAPTER_HOST}:${DBAAS_ADAPTER_PORT}  auth=${auth}  verify=False
    ${prefix}=  Generate Random String  5  [LOWER]
    ${name}=  Set Variable  dbaas_db
    ${db_name}=  Catenate  SEPARATOR=_  ${prefix}  ${name}
    Set Suite Variable  ${db_name}
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    Set Suite Variable  ${PG_CLUSTER_NAME}
    ${api_version}=  Get API Version
    Set Suite Variable  ${api_version}

Check Database Existence By Dbaas Adapter
    [Arguments]  ${database}
    ${resp}=  GET On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/databases
    Should Be Equal  ${resp.status_code}  ${200}
    Should Contain  str(${resp.content})  ${database}

Check Database Creating By Dbaas Adapter
    [Arguments]  ${database}
    ${data}=  Set Variable  {"dbName":"${database}","metadata":{"classifier":{"microserviceName":"${MICROSERVICE_NAME}","namespace":"${NAMESPACE}","scope": "${SCOPE}"}}}
    ${resp}=  POST On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/databases  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    Dictionary Should Contain Key  ${resp.json()}  name
    ${resp_name}=  Get From Dictionary  ${resp.json()}  name
    Should Be Equal  ${database}  ${resp_name}

Delete User And Database
    [Arguments]  ${db_name}  ${user_name}
    Delete Test DB  ${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP USER ${user_name}