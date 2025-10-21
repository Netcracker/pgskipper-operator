*** Settings ***
Library    String
Library    Collections
Resource   ../Lib/lib.robot

*** Variables ***
${PG_NAMESPACE}    %{OPENSHIFT_WORKSPACE_WA}

*** Keywords ***
Get Image Tag
    [Arguments]    ${image}
    ${parts}=    Split String    ${image}    :
    ${length}=   Get Length      ${parts}
    Run Keyword If  ${length} > 1  Return From Keyword  ${parts[2]}  
    Run Keywords
    ...  Log To Console  \n[ERROR] Image ${parts} has no tag: ${image}\nMonitored images list: ${MONITORED_IMAGES}
    ...  AND  Fail  Some images were not found, please check your .helpers template and description.yaml in the repository

Compare Images From Resources With Dd
    [Arguments]    ${dd_images}
    ${stripped_resources}=    Strip String    ${dd_images}    characters=,    mode=right
    @{list_resources}=        Split String    ${stripped_resources}    ,
    FOR    ${resource}    IN    @{list_resources}
        ${type}    ${name}    ${container_name}    ${image}=    Split String    ${resource}
        ${resource_image}=    Get Image From Resource    ${type}    ${name}    ${container_name}

        ${expected_tag}=    Get Image Tag    ${image}
        ${actual_tag}=      Get Image Tag    ${resource_image}

        Log To Console    \n[COMPARE] ${resource}: Expected tag = ${expected_tag}, Actual tag = ${actual_tag}
        Run Keyword And Continue On Failure    Should Be Equal    ${actual_tag}    ${expected_tag}
    END

*** Test Cases ***
Test Hardcoded Images For Core Services
    [Tags]    patroni    basic    check_pg_images
    ${dd_images}=    Get Dd Images From Config Map    patroni-tests-config
    Skip If    '${dd_images}' == '${None}'    There is no dd, not possible to check case!
    Compare Images From Resources With Dd    ${dd_images}

Test Hardcoded Images For Supplementary Services
    [Tags]    backup    basic    check_pg_images
    ${dd_images}=    Get Dd Images From Config Map    supplementary-tests-config
    Skip If    '${dd_images}' == '${None}'    There is no dd, not possible to check case!
    Compare Images From Resources With Dd    ${dd_images}
