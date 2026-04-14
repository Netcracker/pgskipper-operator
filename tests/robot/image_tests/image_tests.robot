*** Variables ***
${MONITORED_IMAGES}         %{MONITORED_IMAGES}

*** Settings ***
Library  String
Library  Collections
Resource  ../Lib/lib.robot

*** Keywords ***
Get Image Tag
    [Arguments]  ${image}
    @{parts}=  Split String  ${image}  :
    ${length}=  Get Length  ${parts}
    Run Keyword If  ${length} > 1  Return From Keyword  ${parts}[${length-1}]
    Fail  Image has no tag: ${image}

Compare Images From Resources
    [Arguments]  ${images}
    @{list_resources}=  Split String  ${images}  ,
    FOR  ${resource}  IN  @{list_resources}
        ${resource}=  Strip String  ${resource}
        Continue For Loop If  '${resource}' == ''

        ${type}  ${name}  ${container_name}  ${image}=  Split String  ${resource}
        ${resource_image}=  Get Image From Resource  ${type}  ${name}  ${container_name}

        ${expected_tag}=  Get Image Tag  ${image}
        ${actual_tag}=    Get Image Tag  ${resource_image}

        Log To Console  \n[COMPARE] ${resource}: Expected tag=${expected_tag}, Actual tag=${actual_tag}
        Run Keyword And Continue On Failure  Should Be Equal  ${actual_tag}  ${expected_tag}
    END

*** Test Cases ***
Test Hardcoded Images
    [Tags]  patroni  basic  check_pg_images
    Skip If  '${MONITORED_IMAGES}' == '${None}' or '${MONITORED_IMAGES}' == ''  There are no monitored images
    Compare Images From Resources  ${MONITORED_IMAGES}
