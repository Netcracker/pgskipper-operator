# Development Guide - Spring Boot Failover Test

**Location:** `tests/examples/spring-boot-failover-test/`

This guide provides comprehensive development instructions for working with the PostgreSQL failover test example.

## Primary Goal

This project provides a **controlled test environment to reproduce and analyze PostgreSQL failover behavior** with Java Spring Boot applications using HikariCP connection pooling.

### The Problem

Java applications may fail to reconnect to a new primary PostgreSQL node after database failover, potentially causing service disruptions. This happens because:

1. Connection pools may cache stale connections to the failed primary
2. JDBC drivers may not immediately detect primary node changes
3. Connection validation settings may be insufficient
4. Multi-host JDBC URL configuration may be incorrect

### The Solution

This project helps you:

- **Reproduce** the issue in a controlled Kubernetes environment
- **Monitor** connection behavior during failover events with detailed metrics
- **Test** different connection pool configurations (HikariCP settings)
- **Analyze** reconnection patterns, timing, and failure recovery
- **Validate** fixes and optimizations before production deployment

## Architecture Overview

```
┌─────────────────────────────────────────────────┐
│           Kubernetes Cluster                    │
│                                                  │
│  ┌──────────────────────────────────────────┐  │
│  │  Spring Boot Application (2 replicas)    │  │
│  │  - HikariCP Connection Pool              │  │
│  │  - Continuous Health Monitoring          │  │
│  │  - REST API for Testing                  │  │
│  └────────────┬─────────────────────────────┘  │
│               │                                  │
│               │ JDBC Multi-Host Connection       │
│               ▼                                  │
│  ┌──────────────────────────────────────────┐  │
│  │  PostgreSQL HA Cluster (pgskipper)       │  │
│  │  ┌────────────┐  ┌────────────┐          │  │
│  │  │  Primary   │  │  Replica 1 │          │  │
│  │  │  (Master)  │  │  (Standby) │          │  │
│  │  └────────────┘  └────────────┘          │  │
│  │  ┌────────────┐                          │  │
│  │  │  Replica 2 │  Patroni for HA          │  │
│  │  │  (Standby) │  Auto-failover           │  │
│  │  └────────────┘                          │  │
│  └──────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

### Key Components

1. **PostgreSQL HA Cluster**
   - 3-node cluster (1 primary + 2 replicas)
   - Managed by pgskipper-operator (Patroni-based)
   - Automatic failover detection and promotion
   - Streaming replication

2. **Spring Boot Test Application**
   - HikariCP connection pool with configurable settings
   - Multi-host JDBC URL for automatic failover
   - Continuous connection health monitoring (every 5 seconds)
   - REST API for testing and metrics

3. **Monitoring & Observability**
   - Real-time connection status tracking
   - Failover detection and timing metrics
   - Connection pool statistics
   - Detailed logging of reconnection attempts

## Deployment Instructions

### Prerequisites

Ensure you have the following installed:

- **Kubernetes cluster** (Docker Desktop, OrbStack, Minikube, Kind, or cloud provider)
- **kubectl** - Kubernetes CLI
- **helm** - Kubernetes package manager (v3+)
- **helmfile** - Declarative Helm deployment tool
- **docker** - Container runtime
- **git** - Version control

#### Install Helmfile

```bash
# macOS
brew install helmfile

# Linux
curl -L https://github.com/helmfile/helmfile/releases/latest/download/helmfile_linux_amd64 -o helmfile
chmod +x helmfile
sudo mv helmfile /usr/local/bin/

# Windows
choco install helmfile
```

### Step 1: Verify Kubernetes Cluster

```bash
# Check cluster connectivity
kubectl cluster-info

# Verify cluster is ready
kubectl get nodes
```

### Step 2: Deploy Everything

```bash
# Navigate to example directory
cd tests/examples/spring-boot-failover-test

# Deploy all components (one command!)
helmfile sync
```

This single command will:
1. ✅ Auto-configure storage for your cluster
2. ✅ Install Patroni-Core operator (PostgreSQL core)
3. ✅ Install Patroni-Services operator (PostgreSQL services)
4. ✅ Wait for PostgreSQL cluster to be ready
5. ✅ Build Spring Boot application Docker image
6. ✅ Deploy the test application (2 replicas)
7. ✅ Display deployment status

**Expected output:**
```
Building Spring Boot application Docker image...
✓ Docker image built successfully

Waiting for PostgreSQL cluster to initialize...
Waiting for primary PostgreSQL pod...
PostgreSQL cluster is ready!

Waiting for application pods to be ready...
Application is ready!

=====================================
Deployment Complete!
=====================================
```

### Step 3: Verify Deployment

```bash
# Check PostgreSQL cluster status
kubectl get pods -n postgres -l app=postgres

# Expected output:
# NAME                 READY   STATUS    RESTARTS   AGE
# postgres-cluster-0   1/1     Running   0          2m
# postgres-cluster-1   1/1     Running   0          2m
# postgres-cluster-2   1/1     Running   0          2m

# Check application status
kubectl get pods -n default -l app.kubernetes.io/name=postgresql-failover-test

# Expected output:
# NAME                                       READY   STATUS    RESTARTS   AGE
# postgresql-failover-test-xxxxxxxxx-xxxxx   1/1     Running   0          1m
# postgresql-failover-test-xxxxxxxxx-xxxxx   1/1     Running   0          1m

# View application logs
kubectl logs -f -n default -l app.kubernetes.io/name=postgresql-failover-test
```

## Failover Testing Instructions

### Test Scenario: Simulated PostgreSQL Failover

This test simulates a real-world scenario where the primary PostgreSQL database fails and Patroni automatically promotes a replica to become the new primary.

#### Step 1: Start Connection Monitoring

Open a terminal window and start the monitoring script:

```bash
./scripts/test-reconnection.sh
```

**What this does:**
- Continuously queries the application's monitoring endpoint
- Displays real-time connection status
- Shows metrics: uptime, failure count, current database node
- Updates every 2 seconds

**Expected output:**
```
==========================================
PostgreSQL Failover Reconnection Monitor
==========================================

[2025-10-06 10:30:15] ✓ Connected | Uptime: 120s | Failures: 0 | Current DB: postgres-cluster-0

[2025-10-06 10:30:17] ✓ Connected | Uptime: 122s | Failures: 0 | Current DB: postgres-cluster-0

[2025-10-06 10:30:19] ✓ Connected | Uptime: 124s | Failures: 0 | Current DB: postgres-cluster-0
```

#### Step 2: Trigger Failover

Open a **second terminal window** and trigger the failover:

```bash
./scripts/trigger-failover.sh
```

**What this does:**
1. Identifies the current primary PostgreSQL pod
2. Deletes the primary pod to simulate a failure
3. Patroni automatically detects the failure
4. One of the replicas is promoted to new primary
5. Kubernetes recreates the deleted pod as a new replica

**Expected output:**
```
==========================================
PostgreSQL Failover Test
==========================================

Current primary pod: postgres-cluster-0
Triggering failover by deleting primary pod...
pod "postgres-cluster-0" deleted

Waiting for new primary election...
New primary elected: postgres-cluster-1

Failover complete!
==========================================
```

#### Step 3: Observe Application Behavior

Watch the monitoring terminal (from Step 1). You should observe:

**Phase 1: Connection Loss Detection (5-10 seconds)**
```
[2025-10-06 10:30:21] ✗ DISCONNECTED | Last connected: 2s ago | Failures: 1
[2025-10-06 10:30:23] ✗ DISCONNECTED | Last connected: 4s ago | Failures: 1
[2025-10-06 10:30:25] ✗ DISCONNECTED | Last connected: 6s ago | Failures: 1
```

**Phase 2: Patroni Failover (30-60 seconds)**
- Patroni detects primary failure
- Replica promotion happens
- New primary is ready

**Phase 3: Application Reconnection (10-20 seconds)**
```
[2025-10-06 10:30:45] ✓ Connected | Uptime: 5s | Failures: 1 | Current DB: postgres-cluster-1
[2025-10-06 10:30:47] ✓ Connected | Uptime: 7s | Failures: 1 | Current DB: postgres-cluster-1
[2025-10-06 10:30:49] ✓ Connected | Uptime: 9s | Failures: 1 | Current DB: postgres-cluster-1
```

**Key Metrics to Track:**
- **Detection Time**: How quickly app detects connection loss (should be < 10s)
- **Failover Duration**: Time for new primary election (typically 30-60s)
- **Reconnection Time**: How long app takes to reconnect (should be < 20s)
- **Total Downtime**: From connection loss to recovery (should be < 90s)
- **Failure Count**: Should increment by 1 after each failover

#### Step 4: Verify New Primary

Check which pod is now the primary:

```bash
# Check primary pod
kubectl get pods -n postgres --selector=pgtype=master

# Verify Patroni cluster status
kubectl exec -n postgres postgres-cluster-1 -- patronictl list
```

Expected output showing the new topology:
```
+ Cluster: postgres (7123456789012345678) ----+----+-----------+
| Member             | Host        | Role    | State   | Lag in MB |
+--------------------+-------------+---------+---------+-----------+
| postgres-cluster-1 | 10.244.0.15 | Leader  | running |           |
| postgres-cluster-2 | 10.244.0.16 | Replica | running | 0         |
| postgres-cluster-0 | 10.244.0.17 | Replica | running | 0         |
+--------------------+-------------+---------+---------+-----------+
```

### Advanced Testing Scenarios

#### Scenario 1: Load Testing During Failover

Test application behavior under load:

```bash
# Port forward to application
kubectl port-forward -n default svc/postgresql-failover-test 8080:8080

# Run continuous write operations (in a new terminal)
while true; do
  curl -X POST "http://localhost:8080/api/write-test?message=Load+test+$(date +%s)"
  sleep 1
done

# Trigger failover (in another terminal)
./scripts/trigger-failover.sh
```

Monitor how many write operations fail during failover.

#### Scenario 2: Multiple Sequential Failovers

Test stability across multiple failovers:

```bash
# Start monitoring
./scripts/test-reconnection.sh

# In another terminal, trigger multiple failovers
./scripts/trigger-failover.sh
sleep 120  # Wait for recovery
./scripts/trigger-failover.sh
sleep 120
./scripts/trigger-failover.sh
```

Verify that:
- Each failover is handled correctly
- Failure count increments consistently
- No connection pool exhaustion occurs
- Application remains stable

#### Scenario 3: API Testing During Failover

```bash
# Port forward
kubectl port-forward -n default svc/postgresql-failover-test 8080:8080

# Get current database info
curl http://localhost:8080/api/db-info

# Get monitoring statistics
curl http://localhost:8080/api/monitor-stats

# Test read operations
curl http://localhost:8080/api/read-test

# Test write operations
curl -X POST "http://localhost:8080/api/write-test?message=Test"

# Trigger failover and repeat API calls
./scripts/trigger-failover.sh
```

## Interpreting Results

### Successful Failover Behavior

✅ **Good indicators:**
- Connection loss detected within 5-10 seconds
- Application reconnects within 10-20 seconds
- Failure count increments by exactly 1
- No cascading failures or connection pool exhaustion
- Application switches to new primary automatically

### Problematic Behavior

❌ **Warning signs:**
- Connection loss not detected (monitoring shows false positives)
- Reconnection takes > 30 seconds
- Multiple failure increments for single failover
- Connection pool exhaustion errors
- Application requires manual restart

### Configuration Tuning

If results are not optimal, consider tuning:

**For faster detection:**
```yaml
# spring-app/src/main/resources/application.yml
spring:
  datasource:
    hikari:
      validation-timeout: 3000  # Reduce from 5000
      connection-timeout: 8000  # Reduce from 10000
```

**For better stability:**
```yaml
spring:
  datasource:
    hikari:
      maximum-pool-size: 20     # Increase from 10
      max-lifetime: 1800000     # Increase to 30 minutes
```

## Cleanup

When testing is complete:

```bash
# Remove all deployed components
helmfile destroy

# Optional: Remove CRDs (WARNING: affects all pgskipper instances)
kubectl delete crd patronicores.qubership.org patroniservices.qubership.org
```

## Troubleshooting

### Issue: Application doesn't reconnect after failover

**Check:**
1. JDBC URL includes all PostgreSQL hosts:
   ```bash
   kubectl get configmap postgresql-failover-test-config -o yaml
   ```

2. Connection pool settings:
   ```bash
   curl http://localhost:8080/api/pool-info
   ```

3. PostgreSQL service endpoints:
   ```bash
   kubectl get endpoints -n postgres
   ```

### Issue: Slow failover (> 2 minutes)

**Check:**
1. Patroni configuration (TTL, loop wait times)
2. Resource constraints on PostgreSQL pods
3. Network latency between pods

### Issue: Cannot access monitoring

**Check:**
1. Application pods are running:
   ```bash
   kubectl get pods -n default -l app.kubernetes.io/name=postgresql-failover-test
   ```

2. Port forward is active:
   ```bash
   kubectl port-forward -n default svc/postgresql-failover-test 8080:8080
   ```

## Next Steps

After successful testing:

1. **Document findings** - Record reconnection times and failure patterns
2. **Optimize configuration** - Apply learnings to production settings
3. **Implement monitoring** - Add Prometheus/Grafana for production observability
4. **Create runbooks** - Document failover procedures for operations team
5. **Test in staging** - Validate configuration in pre-production environment

## Known Issues with pgskipper-operator

**IMPORTANT:** This project **only supports pgskipper-operator (netcracker/pgskipper-operator)** for PostgreSQL deployment. Alternative PostgreSQL operators or Helm charts are not an option for this testing framework.

### Issue 1: YAML Template Rendering Error (Line 111 in cr.yaml)

**Error Message:**
```
Error: YAML parse error on patroni-core/templates/cr.yaml: error converting YAML to JSON:
yaml: line 111: found character that cannot start any token
```

**Root Cause:**
The `patroni-core` Helm chart template at `pgskipper-operator/charts/patroni-core/templates/cr.yaml` (lines 111-114) uses improper YAML indentation:

```yaml
  {{ else }}
      limits:
{{ toYaml .Values.patroni.resources.limits | indent 8}}
      requests:
{{ toYaml .Values.patroni.resources.requests | indent 8}}
  {{ end }}
```

**Fix Applied:**
Modified the template to use `nindent` instead of `indent` for proper YAML formatting:

```yaml
  {{- else }}
      limits: {{- toYaml .Values.patroni.resources.limits | nindent 8 }}
      requests: {{- toYaml .Values.patroni.resources.requests | nindent 8 }}
  {{- end }}
```

**File:** `pgskipper-operator/charts/patroni-core/templates/cr.yaml:111-114`

**Status:** ✅ FIXED - Template patch applied locally

---

### Issue 2: Operator Nil Pointer Dereference

**Error Message:**
```
2025-10-06T13:55:52Z ERROR Observed a panic {"controller": "patronicore",
"panic": "runtime error: invalid memory address or nil pointer dereference"}
```

**Root Cause:**
The patroni-core-operator crashes with a nil pointer dereference at `pkg/reconciler/patroni.go:503` in `processPatroniStatefulset()` when trying to reconcile the PatroniCore custom resource.

The operator attempts to access fields from the PatroniServices CR that haven't been populated yet, causing a race condition between the two operators.

**Error Location:** `/workspace/pkg/reconciler/patroni.go:503`

**Symptoms:**
- Both `patroni-core` and `patroni-services` Helm releases deploy successfully
- PatroniCore and PatroniServices custom resources are created
- patroni-core-operator pod is running but continuously crashes during reconciliation
- No PostgreSQL StatefulSet pods are created
- Operator logs show: `PatroniCr: &{{ } {      0 0001-01-01 00:00:00 +0000 UTC <nil> <nil>...}`

**Attempted Solutions:**
1. ✅ Added all required fields from default values.yaml to simple configuration
2. ✅ Ensured `patroni.clusterName` matches between patroni-core and patroni-services
3. ✅ Added missing `pgBackRest.dockerImage` field
4. ❌ Operator still crashes with nil pointer error

**Current Status:** ⚠️ BLOCKED - This appears to be a bug in the pgskipper-operator code itself

**Workaround Needed:**
The operator may require:
- Additional undocumented required fields in the CRs
- A specific deployment order or timing
- Different configuration approach than documented
- Bug fix in the upstream operator code

### Issue 2: OrbStack Dual-Stack IPv6/IPv4 Incompatibility

**Environment:** OrbStack Kubernetes (macOS)

**Symptoms:**
- PostgreSQL pods crash immediately after startup with: `socket.gaierror: [Errno -2] Name or service not known`
- Patroni REST API fails to bind to listen address
- Pod logs show: `listen: fd07:b51a:cc66:a::17f 192.168.194.10:8008` (IPv6 and IPv4 concatenated with space)
- Both `pgBackRest` sidecar image pull fails (requires authentication)
- `backup-daemon` fails with `runAsNonRoot` security context error

**Root Cause:**
OrbStack's Kubernetes runs in dual-stack mode (PreferDualStack) by default, which causes `status.podIP` to contain both IPv6 and IPv4 addresses separated by space (e.g., "fd07:b51a:cc66:a::17f 192.168.194.10"). The pgskipper-operator's Patroni image startup script uses `POD_IP` environment variable (populated from `status.podIP`) to set `LISTEN_ADDR`, which is then used for Patroni's REST API bind address. Python's `socket.getaddrinfo()` cannot parse this format, causing the crash.

**Attempted Solutions:**
1. ❌ Patch StatefulSet to use `status.podIPs[0].ip` - Not supported by Kubernetes fieldPath
2. ❌ Disable IPv6 in OrbStack - No configuration option available
3. ❌ Modify operator code - Would require maintaining a fork

**Current Status:** ⚠️ BLOCKED on OrbStack - Works on other Kubernetes distributions

**Recommended Solution:**
Use a different Kubernetes distribution that either:
- Runs in single-stack IPv4 mode (e.g., kind, minikube, k3d)
- Correctly populates `status.podIP` with only the primary IP

**Alternative Workarounds:**
1. Build custom Patroni image with modified entrypoint to extract only IPv4 from POD_IP
2. Use Kubernetes mutating webhook to inject an init container that sets LISTEN_ADDR properly
3. Submit PR to pgskipper-operator to handle dual-stack scenarios

## Additional Resources

- [README.md](README.md) - Comprehensive project documentation
- [HikariCP Configuration Guide](https://github.com/brettwooldridge/HikariCP)
- [PostgreSQL JDBC Failover](https://jdbc.postgresql.org/documentation/use/#connection-fail-over)
- [Patroni Documentation](https://patroni.readthedocs.io/)
- [pgskipper-operator](https://github.com/Netcracker/pgskipper-operator) - **Required PostgreSQL operator for this project**
