*** Settings ***
Documentation     Check master pod exists
Library           Collections
Library           OperatingSystem
Library           String
#Test Setup        checks before tests
Resource           ../Lib/lib.robot


*** Test Cases ***

check patroni REST authentication
#    [Tags]    check-installation
    [Documentation]
    ...
    ...  Check responce of REST on all patroni pods
    ...  curl -s pod_ip:8008
    ...
    Check if patroni REST authentication working

*** Keywords ***
