*** Settings ***
Documentation     Check data-validation marker REST API
Library           RequestsLibrary
Library           Collections
Library           OperatingSystem
Resource          ../Lib/lib.robot

*** Keywords ***
Get Scheme
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    RETURN  ${scheme}

Create Marker Session
    [Arguments]  ${scheme}
    ${auth_enabled}=  Get Auth
    IF  '${auth_enabled}' == 'true'
        ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
        ${auth}=  Create List  postgres  ${PG_ROOT_PASSWORD}
        Create Session  backup_daemon_marker  ${scheme}://postgres-backup-daemon:8080  auth=${auth}  verify=False
    ELSE
        Create Session  backup_daemon_marker  ${scheme}://postgres-backup-daemon:8080  verify=False
    END

Create Unauthed Session
    [Arguments]  ${scheme}
    Create Session  backup_daemon_marker_noauth  ${scheme}://postgres-backup-daemon:8080  verify=False

Post Marker
    [Arguments]  ${marker_value}
    &{body}=  Create Dictionary  marker=${marker_value}
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${resp}=  POST On Session  backup_daemon_marker  /api/v1/data-validation/marker  json=${body}  headers=${headers}
    RETURN  ${resp}

Get Marker
    ${resp}=  GET On Session  backup_daemon_marker  /api/v1/data-validation/marker
    RETURN  ${resp}

*** Test Cases ***
Write And Read Marker
    [Tags]  backup basic  marker
    [Documentation]
    ...  Write a marker value via POST /api/v1/data-validation/marker and
    ...  verify it can be retrieved via GET /api/v1/data-validation/marker.
    ...
    ${scheme}=  Get Scheme
    Create Marker Session  ${scheme}
    ${write_resp}=  Post Marker  test-marker-value
    Should Be Equal  ${write_resp.status_code}  ${201}
    ${read_resp}=  Get Marker
    Should Be Equal  ${read_resp.status_code}  ${200}
    Dictionary Should Contain Key  ${read_resp.json()}  marker
    ${marker}=  Get From Dictionary  ${read_resp.json()}  marker
    Should Be Equal  ${marker}  test-marker-value

Overwrite Marker
    [Tags]  backup basic  marker
    [Documentation]
    ...  Verify that posting a second marker overwrites the first one.
    ...
    ${scheme}=  Get Scheme
    Create Marker Session  ${scheme}
    ${first_resp}=  Post Marker  first-marker
    Should Be Equal  ${first_resp.status_code}  ${201}
    ${second_resp}=  Post Marker  second-marker
    Should Be Equal  ${second_resp.status_code}  ${201}
    ${read_resp}=  Get Marker
    Should Be Equal  ${read_resp.status_code}  ${200}
    ${marker}=  Get From Dictionary  ${read_resp.json()}  marker
    Should Be Equal  ${marker}  second-marker

Post Marker With Missing Field Returns 400
    [Tags]  backup basic  marker
    [Documentation]
    ...  Verify that a POST body without the "marker" field returns HTTP 400.
    ...
    ${scheme}=  Get Scheme
    Create Marker Session  ${scheme}
    &{body}=  Create Dictionary  other_field=value
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${resp}=  POST On Session  backup_daemon_marker  /api/v1/data-validation/marker
    ...  json=${body}  headers=${headers}  expected_status=400
    Should Be Equal  ${resp.status_code}  ${400}
    Dictionary Should Contain Key  ${resp.json()}  error

Post Marker With Empty Marker Value Returns 400
    [Tags]  backup basic  marker
    [Documentation]
    ...  Verify that a POST body with an empty "marker" string returns HTTP 400.
    ...
    ${scheme}=  Get Scheme
    Create Marker Session  ${scheme}
    &{body}=  Create Dictionary  marker=${EMPTY}
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${resp}=  POST On Session  backup_daemon_marker  /api/v1/data-validation/marker
    ...  json=${body}  headers=${headers}  expected_status=400
    Should Be Equal  ${resp.status_code}  ${400}
    Dictionary Should Contain Key  ${resp.json()}  error

Marker Endpoint Requires Authentication
    [Tags]  backup basic  marker
    [Documentation]
    ...  Verify that GET and POST on /api/v1/data-validation/marker return
    ...  HTTP 401 when no credentials are supplied.
    ...
    ${res}=  Get Auth
    Run Keyword If  '${res}' == "true"  Check Marker Auth Required

*** Keywords ***
Check Marker Auth Required
    ${scheme}=  Get Scheme
    Create Unauthed Session  ${scheme}
    &{body}=  Create Dictionary  marker=some-value
    &{headers}=  Create Dictionary  Content-Type=application/json  Accept=application/json
    ${post_resp}=  POST On Session  backup_daemon_marker_noauth  /api/v1/data-validation/marker
    ...  json=${body}  headers=${headers}  expected_status=401
    Should Be Equal  ${post_resp.status_code}  ${401}
    ${get_resp}=  GET On Session  backup_daemon_marker_noauth  /api/v1/data-validation/marker
    ...  expected_status=401
    Should Be Equal  ${get_resp.status_code}  ${401}
