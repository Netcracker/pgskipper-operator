# Troubleshooting Guide

**Location:** `tests/examples/spring-boot-failover-test/`

This guide covers common issues and their solutions when working with the PostgreSQL failover test example.

## Table of Contents
- [Storage Configuration Issues](#storage-configuration-issues)
- [pgskipper-operator Issues](#pgskipper-operator-issues)
- [Build Issues](#build-issues)
- [Deployment Issues](#deployment-issues)
- [Failover Testing Issues](#failover-testing-issues)

## Storage Configuration Issues

### Problem: No Default StorageClass

**Symptoms:**
- PVCs stuck in `Pending` state
- Error: `no persistent volumes available`

**Solution:**

Check available storage classes:
```bash
kubectl get storageclass
```

If no default storage class exists, configure one:
```bash
./scripts/configure-storage.sh
```

Or set manually for your cluster type:

**Docker Desktop / OrbStack:**
```bash
kubectl patch storageclass local-path -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
```

**Minikube:**
```bash
kubectl patch storageclass standard -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
```

**Kind:**
```bash
kubectl patch storageclass standard -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
```

### Problem: PVC Stuck in Pending

**Symptoms:**
- PostgreSQL pods in `Pending` state
- PVCs show `Pending` status

**Check:**
```bash
kubectl get pvc -n postgres
kubectl describe pvc <pvc-name> -n postgres
```

**Solutions:**
1. Ensure storage class exists and is default
2. Check if cluster has available storage
3. Verify storage provisioner is running

---

## pgskipper-operator Issues

### Issue: Operator Nil Pointer Dereference

**Error Message:**
```
ERROR Observed a panic {"controller": "patronicore",
"panic": "runtime error: invalid memory address or nil pointer dereference"}
```

**Root Cause:**
The patroni-core-operator crashes with a nil pointer dereference when trying to reconcile the PatroniCore custom resource.

**Status:** Historical issue - check operator logs to see if this still occurs with current version.

**Workaround:**
Ensure all required fields are set in patroni-core-values.yaml and patroni-services-values.yaml.

### Issue: OrbStack Dual-Stack IPv6/IPv4 Incompatibility

**Environment:** OrbStack Kubernetes (macOS)

**Symptoms:**
- PostgreSQL pods crash immediately after startup
- Error: `socket.gaierror: [Errno -2] Name or service not known`
- Patroni REST API fails to bind
- Pod logs show IPv6 and IPv4 concatenated: `fd07:b51a:cc66:a::17f 192.168.194.10:8008`

**Root Cause:**
OrbStack's Kubernetes runs in dual-stack mode, causing `status.podIP` to contain both IPv6 and IPv4 addresses separated by space. The pgskipper-operator's Patroni image uses this for `LISTEN_ADDR`, which Python's socket library cannot parse.

**Status:** ✅ FIXED - listen_addr and connect_address patches have been applied to the operator

**Solution (if using local build):**
```bash
# Deploy with locally-built fixed operator
helmfile sync -e local
```

**Alternative Workaround:**
Use a different Kubernetes distribution:
- kind
- minikube
- k3d
- Docker Desktop (Linux mode)

---

## Build Issues

### Problem: Buildpacks Build Fails

**Symptoms:**
- Error during `./scripts/build.sh`
- Pack CLI not found or fails

**Solutions:**

1. **Install Pack CLI:**
```bash
# macOS
brew install buildpacks/tap/pack

# Linux
(curl -sSL "https://github.com/buildpacks/pack/releases/download/v0.33.2/pack-v0.33.2-linux.tgz" | sudo tar -C /usr/local/bin/ --no-same-owner -xzv pack)

# Windows
choco install pack
```

2. **Use Maven Docker build instead:**
```bash
cd spring-app
mvn spring-boot:build-image
```

### Problem: Docker Build Fails

**Symptoms:**
- Docker daemon not running
- Permission denied errors

**Solutions:**

1. **Start Docker:**
```bash
# Check Docker status
docker info

# Start Docker Desktop (macOS/Windows)
open -a Docker
```

2. **Fix permissions (Linux):**
```bash
sudo usermod -aG docker $USER
newgrp docker
```

### Problem: Maven Build Fails

**Symptoms:**
- Dependency resolution errors
- Java version mismatch

**Solutions:**

1. **Check Java version:**
```bash
java -version
# Should be 17 or higher
```

2. **Clear Maven cache:**
```bash
rm -rf ~/.m2/repository
mvn clean install
```

---

## Deployment Issues

### Problem: Patroni Pods Crash on Startup

**Symptoms:**
- Pods in `CrashLoopBackOff`
- Patroni REST API binding errors

**Check Logs:**
```bash
kubectl logs -n postgres postgres-cluster-0
```

**Solutions:**

1. **For IPv6/IPv4 issues:** Use local operator build (see pgskipper-operator Issues above)

2. **For resource constraints:**
```bash
# Check node resources
kubectl top nodes
kubectl describe node

# Reduce resource requests in patroni-core-simple.yaml
```

3. **For storage issues:** See Storage Configuration Issues above

### Problem: Application Cannot Connect to Database

**Symptoms:**
- Application pods running but can't connect
- Connection timeout errors

**Check:**

1. **PostgreSQL service exists:**
```bash
kubectl get svc -n postgres
```

2. **PostgreSQL pods are running:**
```bash
kubectl get pods -n postgres -l app=postgres
```

3. **Check application logs:**
```bash
kubectl logs -n default -l app.kubernetes.io/name=postgresql-failover-test
```

4. **Verify JDBC URL:**
```bash
kubectl get configmap postgresql-failover-test-config -o yaml
```

**Solutions:**

1. **Wait for PostgreSQL to be fully ready:**
```bash
kubectl wait --for=condition=ready --timeout=600s pods -l app=postgres -n postgres
```

2. **Check connection string format:**
Should be: `jdbc:postgresql://postgres-cluster-0.postgres:5432,postgres-cluster-1.postgres:5432/postgres`

---

## Failover Testing Issues

### Problem: Application Doesn't Reconnect After Failover

**Symptoms:**
- Connection lost detected
- Application stays disconnected after new primary elected
- Manual restart required

**Check:**

1. **JDBC URL includes all hosts:**
```bash
curl http://localhost:8080/api/db-info
```

2. **Connection pool settings:**
```bash
curl http://localhost:8080/api/pool-info
```

**Solutions:**

1. **Tune HikariCP settings** in `spring-app/src/main/resources/application.yml`:
```yaml
spring:
  datasource:
    hikari:
      connection-timeout: 10000
      validation-timeout: 5000
      max-lifetime: 1800000
      keepalive-time: 30000
```

2. **Verify multi-host URL:**
Ensure JDBC URL contains all PostgreSQL pods

3. **Check for connection pool exhaustion:**
```bash
kubectl logs -n default -l app.kubernetes.io/name=postgresql-failover-test | grep -i "timeout\|exhausted"
```

### Problem: Slow Failover (> 2 minutes)

**Symptoms:**
- Long detection time
- Slow Patroni promotion
- Extended downtime

**Check:**

1. **Patroni configuration:**
```bash
kubectl exec -n postgres postgres-cluster-0 -- patronictl list
```

2. **Resource constraints:**
```bash
kubectl top pods -n postgres
```

**Solutions:**

1. **Tune Patroni timing** in patroni-core-simple.yaml:
```yaml
patroniParams:
  - "primary_start_timeout: 30"
  - "retry_timeout: 600"
  - "ttl: 30"
  - "loop_wait: 10"
```

2. **Increase resources** if constrained:
```yaml
patroni:
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
```

3. **Reduce validation timeout** in HikariCP

### Problem: Cannot Access Monitoring API

**Symptoms:**
- Connection refused on port 8080
- `kubectl port-forward` fails

**Solutions:**

1. **Ensure pods are running:**
```bash
kubectl get pods -n default -l app.kubernetes.io/name=postgresql-failover-test
```

2. **Start port forward:**
```bash
kubectl port-forward -n default svc/postgresql-failover-test 8080:8080
```

3. **Check for port conflicts:**
```bash
# Kill process using port 8080
lsof -ti:8080 | xargs kill -9
```

---

## Additional Help

If you encounter issues not covered here:

1. Check operator logs:
```bash
kubectl logs -n postgres -l app.kubernetes.io/name=patroni-core-operator
kubectl logs -n postgres -l app.kubernetes.io/name=patroni-services-operator
```

2. Check PostgreSQL logs:
```bash
kubectl logs -n postgres postgres-cluster-0
```

3. Check application logs:
```bash
kubectl logs -n default -l app.kubernetes.io/name=postgresql-failover-test -f
```

4. Verify Kubernetes events:
```bash
kubectl get events -n postgres --sort-by='.lastTimestamp'
kubectl get events -n default --sort-by='.lastTimestamp'
```

5. Consult the main [pgskipper-operator documentation](../../../README.md)
