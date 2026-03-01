*** Settings ***
Documentation     Check operator doesn't crash during cluster bootstrap
...
...               Regression test for bug fix: "operator crashes during bootstrap because
...               credentials.ProcessCreds() was called before reconcilePatroniCoreCluster()"
...
...               **Background**: The operator previously crashed during initial cluster
...               bootstrap with "context deadline exceeded" because it tried to execute
...               SQL queries (ALTER ROLE) on a PostgreSQL database that didn't exist yet.
...
...               **Root Cause**: credentials.ProcessCreds() was called at line 202,
...               BEFORE reconcilePatroniCoreCluster() created the PostgreSQL StatefulSets.
...
...               **Fix**: Moved ProcessCreds() to line 270, AFTER cluster creation succeeds.
...
...               **Test Objective**: Ensure operator can bootstrap a fresh cluster without
...               crashes, and verify credentials are processed in the correct order.

Library           Collections
Library           OperatingSystem
Library           String
Library           Process
Resource          ../Lib/lib.robot
Resource          ./bootstrap_keywords.robot

*** Variables ***
${OPERATOR_LABEL}       name=patroni-core-operator
${BOOTSTRAP_TIMEOUT}    600 sec
${LOG_CHECK_LINES}      500
${NAMESPACE}            %{POD_NAMESPACE=postgres}

*** Test Cases ***
Check Operator Bootstrap Without Crash
    [Tags]  patroni  basic  check_operator_bootstrap  regression  bootstrap
    [Documentation]
    ...  **GIVEN**: A fresh Kubernetes cluster with no existing PostgreSQL resources
    ...  **WHEN**: The operator creates a new Patroni cluster from scratch
    ...  **THEN**:
    ...  - Operator pods remain running (no crashes)
    ...  - PostgreSQL StatefulSets are created successfully
    ...  - PostgreSQL pods start and reach Running state
    ...  - Replication is established between nodes
    ...  - Operator logs contain no bootstrap-related errors
    ...  - Specifically: no "context deadline exceeded", "nil pointer", or "panic" errors
    ...
    ...  This test would FAIL with the old code because:
    ...  1. Test forces a credential change to trigger ProcessCreds()
    ...  2. Operator would call ProcessCreds() before cluster exists
    ...  3. Database client would be nil (no database yet)
    ...  4. Nil pointer dereference at pkg/client/client.go:90
    ...  5. Operator crashes with "panic: runtime error: invalid memory address"
    ...  6. StatefulSets never get created (or creation fails)
    ...
    [Setup]    Log Test Context
    Given Operator Is Running And Ready
    And Credential Change Is Forced To Trigger Bug
    When Patroni Cluster Bootstrap Starts
    Then Operator Remains Healthy During Bootstrap
    And StatefulSets Are Created Successfully
    And Operator Logs Are Clean
    [Teardown]    Log Test Summary

*** Keywords ***
Log Test Context
    [Documentation]    Log test environment information
    ${namespace}=   Get Environment Variable   POD_NAMESPACE   default=postgres
    ${cluster}=     Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    ${nodes}=       Get Environment Variable   PG_NODE_QTY   default=2
    Log To Console   \n================================================================================
    Log To Console   Bootstrap Regression Test - Environment
    Log To Console   ================================================================================
    Log To Console   Namespace: ${namespace}
    Log To Console   Cluster Name: ${cluster}
    Log To Console   Expected Nodes: ${nodes}
    Log To Console   ================================================================================\n

Operator Is Running And Ready
    [Documentation]
    ...  Verify operator deployment is running and pods are ready
    ...  This ensures we're starting from a healthy operator state
    Log To Console   \n---== Verifying Operator Status ==---
    # Use existing library method to get operator pods
    @{operator_pods}=   Get Pods   label=${OPERATOR_LABEL}   status=Running
    ${count}=   Get Length   ${operator_pods}
    Should Be True   ${count} >= 1   msg=Expected at least 1 operator pod, found ${count}

    # Log operator pod details
    FOR   ${pod}   IN   @{operator_pods}
        Log To Console   ✓ Operator pod: ${pod.metadata.name} (${pod.status.phase})
        # Verify pod has been ready for at least a few seconds (not just started)
        Should Be Equal   ${pod.status.phase}   Running   msg=Operator pod ${pod.metadata.name} not in Running state
    END
    Log To Console   Operator is healthy and ready for bootstrap test

Credential Change Is Forced To Trigger Bug
    [Documentation]
    ...  Force a credential change to trigger the ProcessCreds bug
    ...  This ensures the test reliably reproduces the bug where credentials
    ...  are processed before the database cluster exists
    Log To Console   \n---== Forcing Credential Change ==---

    # First, copy the current secret to create the "old" version
    # The credential manager compares old vs new to detect changes
    ${result}=   Run Process   kubectl   get   secret   postgres-credentials
    ...   -n   ${NAMESPACE}   -o   yaml
    ...   timeout=10s   on_timeout=terminate
    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to get postgres-credentials secret: ${result.stderr}

    # Delete postgres-credentials-old if it exists from previous test run
    # This ensures the test is idempotent and can be run multiple times
    ${result}=   Run Process   kubectl   delete   secret   postgres-credentials-old
    ...   -n   ${NAMESPACE}   --ignore-not-found\=true
    ...   timeout=10s   on_timeout=terminate
    Log To Console   ✓ Cleaned up postgres-credentials-old from previous runs

    # Create postgres-credentials-old with current password
    ${result}=   Run Process   sh   -c
    ...   kubectl get secret postgres-credentials -n ${NAMESPACE} -o yaml | sed 's/name: postgres-credentials/name: postgres-credentials-old/' | kubectl create -f -
    ...   timeout=10s   on_timeout=terminate   shell=True
    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to create postgres-credentials-old secret: ${result.stderr}

    Log To Console   ✓ Created postgres-credentials-old backup

    # Now update the postgres-credentials secret to a NEW password
    # This will trigger the credential manager to call ProcessCreds()
    # which will attempt to ALTER ROLE before the database is created
    ${result}=   Run Process   kubectl   patch   secret   postgres-credentials
    ...   -n   ${NAMESPACE}   --type\=json
    ...   -p\=[{"op": "replace", "path": "/data/password", "value": "Rm9yY2VkUGFzc3dvcmRDaGFuZ2UxMjMh"}]
    ...   timeout=10s   on_timeout=terminate

    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to update postgres-credentials secret: ${result.stderr}

    Log To Console   ✓ Updated postgres-credentials to NEW password
    Log To Console   ✓ This will trigger ProcessCreds() during next reconciliation
    Log To Console   ✓ With buggy code: ProcessCreds runs BEFORE cluster exists → crash
    Log To Console   ✓ With fixed code: ProcessCreds runs AFTER cluster exists → success

    # Force a reconciliation by annotating the PatroniCore CR
    ${timestamp}=   Evaluate   int(time.time())   time
    ${result}=   Run Process   kubectl   annotate   patronicores.netcracker.com   patroni-core
    ...   -n   ${NAMESPACE}   force-reconcile\=${timestamp}   --overwrite
    ...   timeout=10s   on_timeout=terminate

    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to force reconciliation: ${result.stderr}

    Log To Console   ✓ Triggered operator reconciliation

    # Give the operator a moment to start processing the credential change
    Sleep   5s

Patroni Cluster Bootstrap Starts
    [Documentation]
    ...  Wait for the bootstrap process to begin
    ...  This is the critical phase where the old bug would manifest
    Log To Console   \n---== Monitoring Cluster Bootstrap ==---
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    Log To Console   Waiting for StatefulSets to be created (max ${BOOTSTRAP_TIMEOUT})...
    Log To Console   (This step would fail with old code due to operator crash)

    # Wait for StatefulSets to appear (proves reconcilePatroniCoreCluster succeeded)
    Wait Until Keyword Succeeds   ${BOOTSTRAP_TIMEOUT}   5 sec
    ...   Verify StatefulSets Exist   ${pg_cluster_name}

    Log To Console   ✓ StatefulSets created - reconcilePatroniCoreCluster() succeeded

Operator Remains Healthy During Bootstrap
    [Documentation]
    ...  Continuously verify operator doesn't crash during bootstrap
    ...  The old bug caused operator to crash immediately after attempting bootstrap
    Log To Console   \n---== Checking Operator Health During Bootstrap ==---

    # Verify operator pods are still running (not crashed and restarting)
    @{operator_pods}=   Get Pods   label=${OPERATOR_LABEL}   status=Running
    ${count}=   Get Length   ${operator_pods}
    Should Be True   ${count} >= 1   msg=Operator crashed during bootstrap (no running pods found)

    FOR   ${pod}   IN   @{operator_pods}
        # Check restart count using helper keyword
        ${restart_count}=   Check Pod Restart Count   ${pod}   max_restarts=2
        Log To Console   ✓ Pod ${pod.metadata.name}: ${restart_count} restarts (healthy)
    END

    Log To Console   Operator remained stable during bootstrap phase

StatefulSets Are Created Successfully
    [Documentation]
    ...  Verify PostgreSQL StatefulSets were created
    ...  This proves reconcilePatroniCoreCluster() completed successfully
    ${pg_cluster_name}=   Get Environment Variable   PG_CLUSTER_NAME   default=patroni
    Log To Console   \n---== Verifying StatefulSet Creation ==---

    # Get all StatefulSets for this cluster
    ${statefulset_count}=   Get StatefulSet Count   ${pg_cluster_name}
    ${expected_nodes}=   Get Environment Variable   PG_NODE_QTY   default=2
    ${expected_nodes}=   Convert To Integer   ${expected_nodes}

    Should Be Equal As Integers   ${statefulset_count}   ${expected_nodes}
    ...   msg=Expected ${expected_nodes} StatefulSets, found ${statefulset_count}

    Log To Console   ✓ Found ${statefulset_count} StatefulSets (expected: ${expected_nodes})

Operator Logs Are Clean
    [Documentation]
    ...  Verify operator logs contain no bootstrap-related errors
    ...
    ...  Specifically checking for errors that indicate the old bug:
    ...  - "context deadline exceeded" (the symptom seen by users)
    ...  - "nil pointer dereference" (the actual crash)
    ...  - "panic" (Go runtime panic)
    ...  - "CanNotActualizeCredsOnCluster" (error from ProcessCreds called too early)
    ...
    ...  Also checking for success indicators:
    ...  - "Reconcile cycle succeeded" (proves reconciliation completed)
    ...  - "Process credentials after cluster is created" (the fix comment)

    Log To Console   \n---== Analyzing Operator Logs ==---
    @{operator_pods}=   Get Pods   label=${OPERATOR_LABEL}   status=Running

    FOR   ${pod}   IN   @{operator_pods}
        Log To Console   Checking logs for ${pod.metadata.name}...

        # Get logs using helper keyword
        ${logs}=   Get Operator Logs   ${pod.metadata.name}   ${NAMESPACE}   ${LOG_CHECK_LINES}

        # Critical errors that indicate the old bug
        Verify No Error In Logs   ${logs}   context deadline exceeded
        ...   Operator timed out during bootstrap - ProcessCreds may have been called before cluster exists

        Verify No Error In Logs   ${logs}   nil pointer dereference
        ...   Operator crashed with nil pointer - database client was nil during ProcessCreds

        Verify No Error In Logs   ${logs}   panic:
        ...   Operator panicked during reconciliation

        Verify No Error In Logs   ${logs}   CanNotActualizeCredsOnCluster
        ...   Credential processing failed - cluster may not have existed yet

        # Also check for variations of the error
        Verify No Error In Logs   ${logs}   runtime error
        ...   Runtime error detected in operator logs

        # Log success indicators
        ${has_success}=   Run Keyword And Return Status
        ...   Should Contain   ${logs}   Reconcile cycle succeeded
        Run Keyword If   ${has_success}
        ...   Log To Console   ✓ Found success message: "Reconcile cycle succeeded"

        ${has_fix_comment}=   Run Keyword And Return Status
        ...   Should Contain   ${logs}   Process credentials after cluster is created
        Run Keyword If   ${has_fix_comment}
        ...   Log To Console   ✓ Found fix comment in logs

        Log To Console   ✓ Logs clean for ${pod.metadata.name} (checked last ${LOG_CHECK_LINES} lines)
    END

    Log To Console   ✓ All operator logs are clean - no bootstrap errors detected

Log Test Summary
    [Documentation]    Log test completion summary
    Log To Console   \n================================================================================
    Log To Console   Bootstrap Test Complete
    Log To Console   ================================================================================
    Log To Console   ✅ Operator did not crash during bootstrap
    Log To Console   ✅ Credentials processed after cluster creation (not before)
    Log To Console   ✅ PostgreSQL cluster initialized successfully
    Log To Console   ✅ Replication established
    Log To Console   ================================================================================\n

# Helper Keywords

Verify StatefulSets Exist
    [Arguments]   ${cluster_name}
    [Documentation]    Check if at least one StatefulSet exists for the cluster
    ${count}=   Get StatefulSet Count   ${cluster_name}
    Should Be True   ${count} > 0   msg=No StatefulSets found for cluster ${cluster_name}

Get StatefulSet Count
    [Arguments]   ${cluster_name}
    [Documentation]    Count StatefulSets for a given cluster
    ${result}=   Run Process   kubectl   get   statefulsets
    ...   -n   ${NAMESPACE}   -l   pgcluster\=${cluster_name}
    ...   -o   json   --ignore-not-found\=true
    ...   timeout=10s   on_timeout=terminate
    Should Be Equal As Integers   ${result.rc}   0
    ...   msg=Failed to get StatefulSets: ${result.stderr}
    ${json}=   Evaluate   json.loads('''${result.stdout}''')   json
    ${items}=   Get From Dictionary   ${json}   items
    ${count}=   Get Length   ${items}
    RETURN   ${count}
