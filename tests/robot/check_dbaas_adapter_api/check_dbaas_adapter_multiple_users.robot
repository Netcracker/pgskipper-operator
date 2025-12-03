*** Settings ***
Resource          keywords.robot
Test Setup        Prepare Dbaas Adapter


*** Test Cases ***
Check Multiple Users Creating By Dbaas Adapter
    [Tags]  full  dbaas
    ${multi_user_enabled}=  Get Env For Deployment  dbaas-postgres-adapter  MULTI_USERS_ENABLED
    Pass Execution If  "${api_version}" != "v2"  API version v1, not possible to check case!
    Pass Execution If  "${multi_user_enabled}" == "false"  MULTI_USERS_ENABLED = False, not possible to check case!
    ${data}=  Set Variable  {"dbName":"${db_name}","metadata": {"classifier": {"namespace": "%{POD_NAMESPACE}", "microserviceName": "${db_name}"}, "data": "meta", "eschodata": "echo"}}
    ${resp}=  POST On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/databases  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    Dictionary Should Contain Key  ${resp.json()}  name
    ${resp_name}=  Get From Dictionary  ${resp.json()}  name
    Should Be Equal  ${db_name}  ${resp_name}
    ${resp_conne_properties}=  Get From Dictionary  ${resp.json()}  connectionProperties
    ${length} =	Get Length	${resp_conne_properties}
    Should Be Equal As Integers	 ${length}  4
    Should Contain  str(${resp_conne_properties})  'role': 'admin'
    Should Contain  str(${resp_conne_properties})  'role': 'streaming'
    Should Contain  str(${resp_conne_properties})  'role': 'rw'
    Should Contain  str(${resp_conne_properties})  'role': 'ro'
    [Teardown]  Delete Test DB  ${db_name}
