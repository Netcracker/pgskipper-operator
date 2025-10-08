# Spring Boot PostgreSQL Failover Test Example

**Location:** `tests/examples/spring-boot-failover-test/`

## Overview

This example demonstrates how to test PostgreSQL failover behavior with Spring Boot applications using HikariCP connection pooling and pgskipper-operator for high availability.

## Problem Statement

Java applications may fail to reconnect to a new primary PostgreSQL node after database failover, potentially causing service disruptions. This example helps:

1. **Reproduce** the issue in a controlled environment
2. **Monitor** connection behavior during failover events
3. **Test** different connection pool configurations
4. **Analyze** reconnection patterns and timing

## Architecture

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
│               │ JDBC Connection                  │
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

## Features

### Spring Boot Application
- **HikariCP Connection Pool** with optimized failover settings
- **Multi-host JDBC URLs** for automatic failover support
- **Health Monitoring** - Continuous connection checks every 5 seconds
- **REST API Endpoints**:
  - `/api/health` - Application and database health
  - `/api/db-info` - Current database connection details (primary/replica, IP)
  - `/api/pool-info` - Connection pool statistics
  - `/api/monitor-stats` - Failover monitoring metrics
  - `/api/write-test` - Test write operations
  - `/api/read-test` - Test read operations
  - `/api/test-connection` - Manual connection validation

### PostgreSQL HA Cluster
- **3-node cluster** (1 primary + 2 replicas)
- **Automatic failover** using Patroni
- **Streaming replication** for data synchronization
- **Service discovery** for primary/replica routing

### Monitoring & Testing
- **Automated monitoring** with detailed logging
- **Failover detection** and tracking
- **Connection statistics** (uptime, failure count, recovery time)
- **Scripts for testing** failover scenarios

## Prerequisites

**Note:** This example assumes you have cloned the pgskipper-operator repository and are working from within it.

- **Kubernetes cluster** (local or cloud)
  - Minikube, Kind, K3s, Docker Desktop, OrbStack, GKE, EKS, or AKS
- **kubectl** - Kubernetes CLI tool
- **Helm 3** - Kubernetes package manager
- **helmfile** - Declarative Helm deployment tool
- **Docker** - Container runtime
- **Maven 3.9+** - For building the application (automatically called by helmfile)
- **Java 17+** - For local development (optional)

## Building the Application

This project uses **Cloud Native Buildpacks** to build container images - no Dockerfile needed!

### Quick Build

```bash
./scripts/build.sh
```

### Build Benefits

- ✅ **No Dockerfile maintenance** - Buildpacks auto-configure everything
- ✅ **Automatic security updates** - Rebuild to get latest patches
- ✅ **SBOM included** - Software Bill of Materials for compliance
- ✅ **Optimized caching** - Faster builds with intelligent layer reuse

See [BUILDPACKS.md](BUILDPACKS.md) for advanced configuration.

## Quick Start

### 1. Navigate to Example Directory

```bash
# From pgskipper-operator repository root
cd tests/examples/spring-boot-failover-test
```

### 2. Deploy Everything

```bash
# Deploy all components (storage, operators, PostgreSQL, application)
helmfile sync

# Or use apply for a diff preview first
helmfile apply
```

This single command will:
1. Configure storage automatically
2. Install Patroni-Core operator
3. Install Patroni-Services operator
4. Wait for PostgreSQL cluster to be ready
5. Build Spring Boot application Docker image
6. Deploy the test application
7. Display deployment status

### 3. Verify Installation

```bash
# Check PostgreSQL cluster status
kubectl get pods -n postgres -l app=postgres

# Expected output:
# NAME                              READY   STATUS    RESTARTS   AGE
# postgres-cluster-0                1/1     Running   0          5m
# postgres-cluster-1                1/1     Running   0          5m
# postgres-cluster-2                1/1     Running   0          5m

# Check application status
kubectl get pods -n default -l app.kubernetes.io/name=postgresql-failover-test

# View application logs
kubectl logs -f -n default -l app.kubernetes.io/name=postgresql-failover-test
```

### 3. Test Failover

Open two terminal windows:

**Terminal 1 - Start monitoring:**
```bash
./scripts/test-reconnection.sh
```

**Terminal 2 - Trigger failover:**
```bash
./scripts/trigger-failover.sh
```

### 4. Analyze Results

Monitor the output to observe:
- **Connection loss detection** - How quickly the app detects the failure
- **Failover duration** - Time for Patroni to promote a new primary
- **Reconnection time** - How long the app takes to reconnect
- **Connection stability** - Post-failover behavior

### 5. Clean Up

```bash
# Remove all deployed components
helmfile destroy

# Remove CRDs (optional - WARNING: affects all pgskipper instances)
kubectl delete crd patronicores.qubership.org patroniservices.qubership.org
```

## Advanced Usage

### Environment Management

The project supports multiple environments via `environments.yaml`:

```bash
# Deploy to local environment (default)
helmfile -e orbstack sync

# Deploy to minikube
helmfile -e minikube sync

# Deploy to development environment
helmfile -e dev sync

# Deploy to staging
helmfile -e staging sync

# Deploy to production (requires RELEASE_VERSION env var)
RELEASE_VERSION=v1.0.0 helmfile -e prod sync
```

### Helmfile Commands

```bash
# Show what would change without applying
helmfile diff

# List all releases
helmfile list

# Check status of releases
helmfile status

# Sync only specific release
helmfile -l component=postgres-operator sync

# Sync only the application
helmfile -l component=test-application sync

# Template releases (show rendered manifests)
helmfile template

# Test releases
helmfile test
```

### Manual Storage Configuration

Storage is automatically configured during deployment. For manual configuration:

```bash
# Interactive mode
./scripts/configure-storage.sh

# Automatic mode (no prompts)
./scripts/configure-storage.sh --auto
```

### Building the Application Manually

The application is automatically built during deployment. To build manually:

```bash
# Build Docker image
./scripts/build.sh

# Build with custom tag
IMAGE_TAG=v1.0.0 ./scripts/build.sh
```

## Configuration

### HikariCP Connection Pool Settings

Edit `spring-app/src/main/resources/application.yml`:

```yaml
spring:
  datasource:
    hikari:
      minimum-idle: 2
      maximum-pool-size: 10
      connection-timeout: 10000      # 10 seconds
      validation-timeout: 5000       # 5 seconds
      max-lifetime: 600000           # 10 minutes
      idle-timeout: 300000           # 5 minutes
      connection-test-query: SELECT 1
```

### Multi-Host JDBC URL

The application uses a multi-host JDBC URL for failover:

```
jdbc:postgresql://host1:5432,host2:5432,host3:5432/testdb?targetServerType=primary&loadBalanceHosts=true
```

Key parameters:
- `targetServerType=primary` - Always connect to primary (read-write)
- `loadBalanceHosts=true` - Try hosts in random order
- `connectTimeout=10` - Socket connection timeout (seconds)
- `socketTimeout=30` - Socket read timeout (seconds)
- `tcpKeepAlive=true` - Enable TCP keepalive

### PostgreSQL Cluster Configuration

Edit `helm-charts/postgresql/patroni-core-values.yaml`:

```yaml
patroniCore:
  topology:
    replicas: 3  # Number of PostgreSQL instances

  storage:
    storageClassName: "standard"  # Update for your cluster
    size: "10Gi"

  patroni:
    ttl: 30
    loop_wait: 10
    retry_timeout: 10
```

## Testing Scenarios

### Scenario 1: Basic Failover Test

```bash
# Start monitoring
./scripts/test-reconnection.sh

# In another terminal, trigger failover
./scripts/trigger-failover.sh
```

**Expected behavior:**
- Application detects connection loss within 5-10 seconds
- Patroni promotes replica to primary within 30-60 seconds
- Application reconnects automatically within 10-20 seconds

### Scenario 2: Load Testing During Failover

```bash
# Port forward to access the application
kubectl port-forward -n default svc/postgresql-failover-test 8080:8080

# Run continuous write operations
while true; do
  curl -X POST "http://localhost:8080/api/write-test?message=Load+test+$(date +%s)"
  sleep 1
done

# In another terminal, trigger failover
./scripts/trigger-failover.sh
```

### Scenario 3: Multiple Failovers

```bash
# Trigger multiple failovers to test stability
./scripts/trigger-failover.sh
sleep 120
./scripts/trigger-failover.sh
sleep 120
./scripts/trigger-failover.sh
```

## Monitoring and Debugging

### Application Logs

```bash
# Follow application logs
kubectl logs -f -n default -l app.kubernetes.io/name=postgresql-failover-test

# Search for connection events
kubectl logs -n default -l app.kubernetes.io/name=postgresql-failover-test | grep -i "connection\|failover"
```

### Database Status

```bash
# Get current primary
kubectl get pods -n postgres --selector=pgtype=master

# Check Patroni cluster status
kubectl exec -n postgres postgres-cluster-0 -- patronictl list

# Check PostgreSQL replication
kubectl exec -n postgres postgres-cluster-0 -- psql -U postgres -c "SELECT * FROM pg_stat_replication;"
```

### API Endpoints (via port-forward)

```bash
# Port forward to application
kubectl port-forward -n default svc/postgresql-failover-test 8080:8080

# Get current database info
curl http://localhost:8080/api/db-info

# Get monitoring statistics
curl http://localhost:8080/api/monitor-stats

# Test connection
curl http://localhost:8080/api/test-connection

# Write test data
curl -X POST "http://localhost:8080/api/write-test?message=Test"

# Read test data
curl http://localhost:8080/api/read-test
```

## Common Issues and Solutions

### Issue: Application fails to reconnect after failover

**Symptoms:**
- Application logs show persistent connection errors
- Monitor stats show high failure count
- Health endpoint returns 503

**Solutions:**

1. **Check JDBC URL configuration:**
   ```bash
   kubectl get configmap postgresql-failover-test-config -o yaml
   ```
   Ensure it includes all PostgreSQL hosts and `targetServerType=primary`

2. **Increase connection timeout:**
   Edit `application.yml` and increase:
   ```yaml
   hikari:
     connection-timeout: 20000  # 20 seconds
     validation-timeout: 10000  # 10 seconds
   ```

3. **Verify PostgreSQL service:**
   ```bash
   kubectl get svc -n postgres
   kubectl describe svc postgres-service -n postgres
   ```

### Issue: Slow failover (>2 minutes)

**Symptoms:**
- Patroni takes long to promote replica
- Application downtime exceeds 2 minutes

**Solutions:**

1. **Adjust Patroni settings:**
   Edit `patroni-core-values.yaml`:
   ```yaml
   patroni:
     ttl: 20              # Reduce from 30
     loop_wait: 5         # Reduce from 10
     retry_timeout: 5     # Reduce from 10
   ```

2. **Check resource constraints:**
   ```bash
   kubectl top pods -n postgres
   ```

### Issue: Connection pool exhaustion

**Symptoms:**
- "Connection timeout" errors
- Application becomes unresponsive

**Solutions:**

1. **Increase pool size:**
   ```yaml
   hikari:
     maximum-pool-size: 20  # Increase from 10
   ```

2. **Check for connection leaks:**
   ```bash
   curl http://localhost:8080/api/pool-info
   ```

## Performance Tuning

### For Faster Failover Detection

1. **Reduce connection validation timeout:**
   ```yaml
   hikari:
     validation-timeout: 3000  # 3 seconds
   ```

2. **Enable TCP keepalive with shorter intervals:**
   ```yaml
   data-source-properties:
     tcpKeepAlive: true
     socketTimeout: 15  # 15 seconds instead of 30
   ```

### For Better Connection Stability

1. **Increase max lifetime:**
   ```yaml
   hikari:
     max-lifetime: 1800000  # 30 minutes
   ```

2. **Use connection validation on borrow:**
   ```yaml
   hikari:
     connection-test-query: SELECT 1
   ```

## Project Structure

```
postgresql-stability/
├── helmfile.yaml                    # Main helmfile configuration
├── environments.yaml                # Environment-specific settings
├── spring-app/                      # Spring Boot application
│   ├── src/main/java/
│   │   └── com/example/pgtest/
│   │       ├── Application.java     # Main class
│   │       ├── controller/
│   │       │   └── HealthController.java  # REST endpoints
│   │       ├── service/
│   │       │   ├── DatabaseService.java   # DB operations
│   │       │   └── ConnectionMonitor.java # Health monitoring
│   │       ├── repository/
│   │       │   └── TestRepository.java
│   │       └── model/
│   │           └── TestEntity.java
│   ├── src/main/resources/
│   │   ├── application.yml          # Spring Boot config
│   │   └── schema.sql
│   └── pom.xml                      # Maven dependencies
├── helm-charts/
│   ├── spring-app/                  # Application Helm chart
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   └── postgresql/                  # pgskipper configurations
│       ├── patroni-core-minimal.yaml
│       └── patroni-services-minimal.yaml
├── scripts/
│   ├── build.sh                     # Build Docker image
│   ├── configure-storage.sh         # Storage configuration
│   ├── trigger-failover.sh          # Failover testing
│   └── test-reconnection.sh         # Connection monitoring
├── pgskipper-operator/              # Auto-cloned operator repository
└── README.md
```

## Comparison with Shell Scripts

### Old Approach (Shell Scripts)
```bash
./scripts/setup.sh      # Setup everything
./scripts/cleanup.sh    # Clean up (with prompts)
```

**Issues:**
- Manual sleep/wait loops
- Interactive prompts blocking automation
- Manual PVC patching after deployment
- Hard to manage multiple environments
- No built-in diffing or rollback

### New Approach (Helmfile)
```bash
helmfile sync           # Setup everything
helmfile destroy        # Clean up (no prompts)
```

**Benefits:**
- ✅ Declarative configuration
- ✅ Idempotent operations
- ✅ Automatic dependency management
- ✅ Built-in diffing and preview
- ✅ Environment management
- ✅ No manual wait loops or sleep statements
- ✅ Better error handling and rollback
- ✅ Version controlled configuration

## Technology Stack

- **Spring Boot 3.2.0** - Application framework
- **Spring Data JPA** - Database access
- **HikariCP** - Connection pooling
- **PostgreSQL 15** - Database
- **Patroni** - PostgreSQL HA solution
- **pgskipper-operator** - Kubernetes operator for PostgreSQL
- **Helmfile** - Declarative Helm deployment
- **Helm 3** - Kubernetes package manager
- **Maven** - Build tool

## References

- [pgskipper-operator Documentation](../../../README.md) - Main operator documentation
- [pgskipper-operator Repository](https://github.com/Netcracker/pgskipper-operator)
- [Helmfile Documentation](https://helmfile.readthedocs.io/)
- [Patroni Documentation](https://patroni.readthedocs.io/)
- [HikariCP Configuration](https://github.com/brettwooldridge/HikariCP)
- [PostgreSQL JDBC Driver](https://jdbc.postgresql.org/documentation/)
- [Spring Boot Data Access](https://docs.spring.io/spring-boot/docs/current/reference/html/data.html)

## Contributing

Feel free to submit issues or pull requests to improve this testing framework.

## License

This project is provided as-is for testing and educational purposes.
