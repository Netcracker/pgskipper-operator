*** Settings ***
Documentation     Reusable keywords for bootstrap testing
Library           Process

*** Keywords ***
Get Operator Logs
    [Arguments]   ${pod_name}   ${namespace}=postgres   ${lines}=500
    [Documentation]
    ...  Retrieve operator pod logs using kubectl
    ...  Returns the last N lines of logs from the specified pod
    ${result}=   Run Process   kubectl   logs   ${pod_name}
    ...   -n   ${namespace}   --tail\=${lines}
    ...   timeout=30s   on_timeout=terminate
    # Retry once if the first attempt fails (pod might be in transitional state)
    Run Keyword If   ${result.rc} != 0   Sleep   5s
    ${result}=   Run Keyword If   ${result.rc} != 0
    ...   Run Process   kubectl   logs   ${pod_name}
    ...   -n   ${namespace}   --tail\=${lines}
    ...   timeout=30s   on_timeout=terminate
    ...   ELSE   Set Variable   ${result}
    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to get logs from ${pod_name}: ${result.stderr}
    RETURN   ${result.stdout}

Get StatefulSet Names
    [Arguments]   ${cluster_name}   ${namespace}=postgres
    [Documentation]
    ...  Get list of StatefulSet names for a cluster
    ${result}=   Run Process   kubectl   get   statefulsets
    ...   -n   ${namespace}
    ...   -l   pgcluster\=${cluster_name}
    ...   -o   jsonpath\={.items[*].metadata.name}
    ...   --ignore-not-found\=true
    ...   timeout=10s   on_timeout=terminate
    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to get StatefulSets: ${result.stderr}
    ${names}=   Split String   ${result.stdout}
    RETURN   ${names}

Verify No Error In Logs
    [Arguments]   ${logs}   ${error_pattern}   ${error_message}
    [Documentation]
    ...  Check logs don't contain a specific error pattern
    ...  Provide clear regression message if error is found
    ${has_error}=   Run Keyword And Return Status
    ...   Should Contain   ${logs}   ${error_pattern}
    Run Keyword If   ${has_error}
    ...   Fail   ❌ REGRESSION DETECTED: ${error_message}\nFound pattern: "${error_pattern}"

Check Pod Restart Count
    [Arguments]   ${pod}   ${max_restarts}=2
    [Documentation]
    ...  Verify pod hasn't restarted excessively
    ...  High restart count indicates crashes
    # Access Kubernetes object attributes directly
    ${containers}=   Set Variable   ${pod.status.container_statuses}
    ${container}=   Get From List   ${containers}   0
    ${restart_count}=   Set Variable   ${container.restart_count}
    Should Be True   ${restart_count} <= ${max_restarts}
    ...   msg=Pod ${pod.metadata.name} has ${restart_count} restarts (max allowed: ${max_restarts})
    RETURN   ${restart_count}
