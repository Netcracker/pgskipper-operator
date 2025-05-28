*** Settings ***
Resource          keywords.robot
Test Setup        Prepare Dbaas Adapter


*** Test Cases ***
Check List Databases By Dbaas Adapter
    [Tags]  full  dbaas
    Wait Until Keyword Succeeds  ${RETRY_TIME}  ${RETRY_INTERVAL}
    ...  Check Database Existence By Dbaas Adapter  postgres

Check List Databases By Dbaas Adapter With Wrong Credentials
    [Tags]  full  dbaas
    ${auth}=  Create List  ${DBAAS_ADAPTER_API_USER}wrong  ${DBAAS_ADAPTER_API_PASSWORD}wrong
    ${INTERNAL_TLS_ENABLED}=  Get Environment Variable  INTERNAL_TLS_ENABLED
    ${scheme}=  Set Variable If  '${INTERNAL_TLS_ENABLED}' == 'true'  https  http
    Create Session  dbaaswrongcreds  ${scheme}://${DBAAS_ADAPTER_HOST}:${DBAAS_ADAPTER_PORT}  auth=${auth}
    ${resp}=  GET On Session  dbaaswrongcreds  /api/${api_version}/dbaas/adapter/postgresql/databases  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}

Check Creating Database By Dbaas Adapter
    [Tags]  full  dbaas
    Check Database Creating By Dbaas Adapter  ${db_name}
    Check Database Existence By Dbaas Adapter  ${db_name}
    [Teardown]  Delete Test DB  ${db_name}

Check Creating Database By Dbaas Adapter With Extensions
    [Tags]  full  dbaas
    ${data}=  Set Variable  {"dbName":"${db_name}","settings": {"pgExtensions": ["cube","dblink"]}, "metadata":{"classifier":{"microserviceName":"${MICROSERVICE_NAME}","namespace":"${NAMESPACE}","scope": "${SCOPE}"}}}
    ${resp}=  POST On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/databases  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    Dictionary Should Contain Key  ${resp.json()}  name
    ${resp_name}=  Get From Dictionary  ${resp.json()}  name
    Should Be Equal  ${db_name}  ${resp_name}
    Check Database Existence By Dbaas Adapter  ${db_name}
    Sleep  5s
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT extname FROM pg_extension  ${db_name}
    Should Be True   """dblink""" in """${res}"""   msg=Expected user extension dblink is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    Should Be True   """cube""" in """${res}"""   msg=Expected user extension cude is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    [Teardown]  Delete Test DB  ${db_name}

Check Creating Database By Dbaas Adapter With Not Valid Extensions
    [Tags]  full  dbaas
    ${data}=  Set Variable  {"dbName":"${db_name}","settings": {"pgExtensions": ["cube","dblink","not_valid_ext"]}, "metadata":{"classifier":{"microserviceName":"${MICROSERVICE_NAME}","namespace":"${NAMESPACE}","scope": "${SCOPE}"}}}
    ${resp}=  POST On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/databases  data=${data}  expected_status=400
    Should Be Equal As Strings  ${resp.status_code}  400

Check Deleting Database By Dbaas Adapter
    [Tags]  full  dbaas
    Check Database Creating By Dbaas Adapter  ${db_name}
    Check Database Existence By Dbaas Adapter  ${db_name}
    ${data}=  Set Variable  [{"kind":"database","name":"${db_name}"}]
    ${resp}=  POST On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/resources/bulk-drop  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  200
    ${resp}=  GET On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/databases
    Should Be Equal  ${resp.status_code}  ${200}
    Should Not Contain  str(${resp.content})  ${db_name}

Check Creating User By Dbaas Adapter
    [Tags]  full  dbaas
    Check Database Creating By Dbaas Adapter  ${db_name}
    ${data}=  Set Variable  {"dbName":"${db_name}","password":"qwerty123","role":"admin" }
    ${resp}=  PUT On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/users  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    ${resp_conne_properties}=  Get From Dictionary  ${resp.json()}  connectionProperties
    ${resp_username}=  Get From Dictionary  ${resp_conne_properties}  username
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${resp_username}""" in """${res}"""   msg=[creating user] Expected user ${resp_username} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    [Teardown]  Delete User And Database  ${db_name}  ${resp_username}

Check Updating User By Dbaas Adapter
    [Tags]  full  dbaas
    Check Database Creating By Dbaas Adapter  ${db_name}
    ${data}=  Set Variable  {"dbName":"${db_name}","password":"qwerty123","role":"admin" }
    ${resp}=  PUT On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/users  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    ${resp_conne_properties}=  Get From Dictionary  ${resp.json()}  connectionProperties
    ${resp_username}=  Get From Dictionary  ${resp_conne_properties}  username
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${resp_username}""" in """${res}"""   msg=[creating user] Expected user ${resp_username} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    ${data}=  Set Variable  {"dbName":"${db_name}","password":"qwerty123_updated","role":"admin" }
    ${resp}=  PUT On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/users  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    ${resp_conne_properties}=  Get From Dictionary  ${resp.json()}  connectionProperties
    ${resp_pass}=  Get From Dictionary  ${resp_conne_properties}  password
    Should Be Equal As Strings  ${resp_pass}  qwerty123_updated
    [Teardown]  Delete User And Database  ${db_name}  ${resp_username}

Check Deleting User By Dbaas Adapter
    [Tags]  full  dbaas
    Check Database Creating By Dbaas Adapter  ${db_name}
    ${data}=  Set Variable  {"dbName":"${db_name}","password":"qwerty123","role":"admin" }
    ${resp}=  PUT On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/users  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  201
    ${resp_conne_properties}=  Get From Dictionary  ${resp.json()}  connectionProperties
    ${resp_username}=  Get From Dictionary  ${resp_conne_properties}  username
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${resp_username}""" in """${res}"""   msg=[creating user] Expected user ${resp_username} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    ${data}=  Set Variable  [{"kind":"user","name":"${resp_username}"}]
    ${resp}=  POST On Session  dbaassession  /api/${api_version}/dbaas/adapter/postgresql/resources/bulk-drop  data=${data}
    Should Be Equal As Strings  ${resp.status_code}  200
    [Teardown]  Delete User And Database  ${db_name}  ${resp_username}
    