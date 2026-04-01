*** Settings ***
Library  String
Library  Collections
Resource  ../Lib/lib.robot

*** Keywords ***
Compare Images From Resources With Dd
    [Arguments]  ${dd_images}
    ${stripped_resources}=  Strip String  ${dd_images}  characters=,  mode=right
    @{list_resources} =  Split String	${stripped_resources} 	,
    FOR  ${resource}  IN  @{list_resources}
      ${type}  ${name}  ${container_name}  ${image}=  Split String	${resource}
      ${resource_image}=  Get Image From Resource  ${type}  ${name}  ${container_name}
      Should Be Equal  ${resource_image}  ${image}
    END

*** Test Cases ***
Test Hardcoded Images For Core Services
    [Tags]  patroni basic  check_pg_images
    ${dd_images}=  Get Dd Images From Config Map  patroni-tests-config
    Skip If  '${dd_images}' == '${None}'  There is no dd, not possible to check case!
    Compare Images From Resources With Dd  ${dd_images}

Test Hardcoded Images For Supplementary Services
    [Tags]  backup basic  check_pg_images
    ${dd_images}=  Get Dd Images From Config Map  supplementary-tests-config
    Skip If  '${dd_images}' == '${None}'  There is no dd, not possible to check case!
    Compare Images From Resources With Dd  ${dd_images}
