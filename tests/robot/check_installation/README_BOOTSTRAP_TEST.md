# Bootstrap Regression Test

## Purpose

This test validates the fix for: **"operator crashes during bootstrap because credentials.ProcessCreds() was called before reconcilePatroniCoreCluster()"**

## What It Tests

The `check_operator_bootstrap.robot` test ensures:

1. ✅ Operator starts successfully
2. ✅ Patroni cluster is created without operator crashes
3. ✅ Credentials are processed **after** cluster exists (not before)
4. ✅ PostgreSQL StatefulSets are created
5. ✅ PostgreSQL pods come up successfully
6. ✅ No nil pointer dereference or panic errors in operator logs
7. ✅ No "context deadline exceeded" errors during bootstrap
8. ✅ Replication works

## How to Run

### Option 1: Run via Docker (Recommended)

```bash
# From repository root
cd tests

# Build test image
docker build -t pgskipper-operator-tests:local .

# Run the bootstrap test
docker run --rm \
  -e POD_NAMESPACE=postgres \
  -e PG_CLUSTER_NAME=patroni \
  -e PG_NODE_QTY=2 \
  -e KUBECONFIG=/config/kubeconfig \
  -v ~/.kube/config:/config/kubeconfig \
  pgskipper-operator-tests:local \
  robot -i check_operator_bootstrap /test_runs/check_installation/
```

### Option 2: Run with Robot Framework directly

```bash
# Install Robot Framework
pip install robotframework robotframework-requests kubernetes

# Set environment variables
export POD_NAMESPACE=postgres
export PG_CLUSTER_NAME=patroni
export PG_NODE_QTY=2

# Run test
cd tests/robot
robot -i check_operator_bootstrap check_installation/check_operator_bootstrap.robot
```

## Expected Results

### ✅ Success

```
==============================================================================
Check Installation :: Check operator doesn't crash during cluster bootstrap
==============================================================================
Check Operator Bootstrap Without Crash                               | PASS |
------------------------------------------------------------------------------
Check Installation :: Check operator doesn't crash during clust... | PASS |
1 test, 1 passed, 0 failed
```

**Operator Logs**: No errors related to:
- `context deadline exceeded`
- `nil pointer dereference`
- `Error during actualization of creds on cluster`
- `panic`

### ❌ Failure (Old Bug)

If the fix is reverted, you would see:

```
Check Operator Bootstrap Without Crash                               | FAIL |
Operator logs contain: "Error during actualization of creds on cluster"
```

**Operator Logs** would contain:
```
ERROR: Error during actualization of creds on cluster
panic: runtime error: invalid memory address or nil pointer dereference
```

## Related Files

- **Fix**: `operator/controllers/patroni_core_controller.go:270`
- **Original Bug**: ProcessCreds was at line 202 (before cluster creation)
- **Current Fix**: ProcessCreds moved to line 270 (after cluster creation)

## Maintenance

If the code structure changes:

1. Update line numbers in test documentation
2. Verify error messages still match
3. Update log assertions if error format changes
4. Keep test tags up to date
