# pgskipper-operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26.2-00ADD8?logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-0.35.x-326CE5?logo=kubernetes)](https://kubernetes.io/)

A Kubernetes operator that provides **PostgreSQL as a service** on Kubernetes and OpenShift platforms, featuring high availability through Patroni, automated backups with pgBackRest, and comprehensive monitoring capabilities.

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Documentation](#documentation)
- [Development](#development)
- [Testing](#testing)
- [Contributing](#contributing)
- [License](#license)
- [Support](#support)

## Overview

pgskipper-operator manages PostgreSQL clusters on Kubernetes with production-grade features including:

- **High Availability**: Automated failover and leader election using Patroni
- **Automated Backups**: pgBackRest integration for reliable backup and restore
- **Disaster Recovery**: Multi-site DR capabilities with active-standby clusters
- **Monitoring & Observability**: Built-in Prometheus exporters and Grafana dashboards
- **Connection Pooling**: Integrated PGBouncer for efficient connection management
- **Security**: TLS/SSL support, LDAP integration, and CIS hardening
- **Version Management**: Automated major version upgrades

## Features

### Core Capabilities

- **Patroni-based HA Cluster**: Automatic failover and leader election
- **pgBackRest Backups**: Full, incremental, and differential backups with S3/Azure/GCS support
- **Connection Pooler**: PGBouncer integration for connection management
- **Query Exporter**: Custom Prometheus metrics from SQL queries
- **TLS Configuration**: Secure connections with certificate management via cert-manager
- **Major Version Upgrades**: Automated PostgreSQL major version upgrades
- **Logical Replication**: Built-in logical replication controller
- **Disaster Recovery**: Active-standby cluster configuration across multiple sites
- **LDAP Integration**: Enterprise authentication support
- **Toleration Policies**: Advanced pod scheduling controls
- **CIS Hardening**: Security baseline compliance

See the [features documentation](docs/public/features/) for detailed information on each capability.

## Architecture

The operator consists of two main components:

### 1. Patroni Core Operator
Manages the `PatroniCore` custom resource, responsible for:
- PostgreSQL cluster deployment with Patroni
- High availability and failover management
- Storage provisioning and management

### 2. Patroni Services Operator
Manages the `PatroniServices` custom resource, providing:
- Backup daemon (pgBackRest integration)
- Monitoring agent and metrics exporters
- DBaaS adapter
- Replication controller
- Connection pooler (PGBouncer)
- Query exporter

Both operators are packaged as separate Helm charts and can be deployed independently, with PatroniCore required before PatroniServices.

## Prerequisites

### Required Tools
- [kubectl](https://kubernetes.io/docs/tasks/tools/) - Kubernetes CLI
- [helm](https://helm.sh/docs/intro/install/) - Helm 3.x or later
- [git](https://git-scm.com/downloads) - For cloning the repository

### Kubernetes Requirements
- Kubernetes 1.25+ or OpenShift 4.x+
- Cluster admin rights for CRD installation
- Storage provisioner (or manually created PersistentVolumes)
- Minimum resources: 4 CPU cores, 8GB RAM

### Optional Requirements
- **cert-manager**: For automatic TLS certificate management
- **Prometheus Operator**: For monitoring stack integration
- **Cloud Provider**: For cloud-native storage (AWS EBS, Azure Disk, GCP PD)

See the [installation guide](docs/public/installation.md) for platform-specific requirements (AWS, Azure, GCP).

## Installation

### Step 1: Clone the Repository

```bash
git clone https://github.com/Netcracker/pgskipper-operator.git
cd pgskipper-operator
```

### Step 2: Configure Storage

Before installation, configure storage in your Helm values files (`operator/charts/patroni-core/patroni-core-quickstart-sample.yaml` and `operator/charts/patroni-services/patroni-services-quickstart-sample.yaml`).

**Option A: With a storage provisioner (recommended)**
```yaml
patroni:
  storage:
    type: provisioned  # Options: provisioned, pv
    size: 20Gi
    storageClass: fast-ssd

backupDaemon:
  storage:
    type: provisioned  # Options: provisioned, pv, ephemeral
    size: 50Gi
    storageClass: fast-ssd
```

**Storage Types:**
- `provisioned`: Uses dynamic volume provisioning with specified storageClass
- `pv`: Uses manually created PersistentVolumes
- `ephemeral`: Uses ephemeral storage (backupDaemon only, not recommended for production)

**Option B: Without a provisioner (manual PVs)**
```yaml
patroni:
  storage:
    type: pv
    size: 20Gi
    volumes:
      - postgres-pv-0
      - postgres-pv-1
    nodes:
      - worker-node-1
      - worker-node-2

backupDaemon:
  storage:
    type: pv
    size: 50Gi
    volumes:
      - backup-pv-0
    nodes:
      - worker-node-1
```

### Step 3: Install Custom Resource Definitions

```bash
kubectl create -f ./operator/charts/patroni-core/crds/netcracker.com_patronicores.yaml
kubectl create -f ./operator/charts/patroni-services/crds/netcracker.com_patroniservices.yaml
```

### Step 4: Install Patroni Core Operator

```bash
helm install --namespace=postgres --create-namespace \
  -f ./operator/charts/patroni-core/patroni-core-quickstart-sample.yaml \
  patroni-core ./operator/charts/patroni-core
```

**Verify installation:**
```bash
# Check operator pod
kubectl logs -n postgres "$(kubectl get pod -n postgres -l name=patroni-core-operator --output='name')"

# Check Patroni pods are running
kubectl -n postgres get pods --selector=app=patroni --field-selector=status.phase=Running
```

### Step 5: Wait for Leader Election

Before installing Patroni Services, ensure a PostgreSQL leader has been elected:

```bash
# Wait for master pod to be ready
kubectl -n postgres get pods --selector=pgtype=master --field-selector=status.phase=Running

# Verify cluster status
kubectl -n postgres get patronicore patroni-core -o jsonpath='{.status.conditions[?(@.type=="Successful")]}'
```

### Step 6: Install Patroni Services Operator

Once the leader is elected, install the services operator:

```bash
helm install --namespace=postgres \
  -f ./operator/charts/patroni-services/patroni-services-quickstart-sample.yaml \
  patroni-services ./operator/charts/patroni-services
```

**Verify installation:**
```bash
# Check operator pod
kubectl logs -n postgres "$(kubectl get pod -n postgres -l name=postgres-operator --output='name')"

# Check all services are running
kubectl -n postgres get pods

# Verify services status
kubectl -n postgres get patroniservices patroni-services -o jsonpath='{.status.conditions[?(@.type=="Successful")]}'
```

### Installation Complete

Your PostgreSQL cluster is now ready! See the [Usage](#usage) section for connecting to the database.

For advanced installation scenarios (AWS, Azure, GCP, disaster recovery), see the [full installation guide](docs/public/installation.md).

## Configuration

### Common Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `patroni.replicas` | Number of PostgreSQL replicas | `2` |
| `patroni.storage.size` | PV size per replica | `5Gi` |
| `backupDaemon.install` | Enable backup daemon | `true` |
| `backupDaemon.schedule` | Backup schedule (cron) | `0 0/7 * * *` |
| `metricCollector.install` | Enable monitoring stack | `true` |
| `tls.enabled` | Enable TLS/SSL | `false` |

See Helm chart values files for comprehensive configuration options:
- [patroni-core values](operator/charts/patroni-core/values.yaml)
- [patroni-services values](operator/charts/patroni-services/values.yaml)

## Usage

### Connecting to PostgreSQL

```bash
# Port forward to the master pod
PG_MASTER_POD=$(kubectl get pod -n postgres -o name -l app=patroni,pgtype=master)
kubectl -n postgres port-forward "${PG_MASTER_POD}" 5432:5432

# Get credentials
PGUSER=$(kubectl get secrets -n postgres postgres-credentials -o go-template='{{.data.username | base64decode}}')
PGPASSWORD=$(kubectl get secrets -n postgres postgres-credentials -o go-template='{{.data.password | base64decode}}')

# Connect with psql
psql -h localhost -U $PGUSER
```

### Monitoring Cluster Status

```bash
# Check cluster health
kubectl -n postgres get patronicore patroni-core -o jsonpath='{.status.conditions[?(@.type=="Successful")]}'

kubectl -n postgres get patroniservices patroni-services -o jsonpath='{.status.conditions[?(@.type=="Successful")]}'

# View all pods
kubectl -n postgres get pods -l app=patroni

# Check master/replica status
kubectl -n postgres get pods -l pgtype=master
kubectl -n postgres get pods -l pgtype=replica
```

### Backup and Restore

Backups are automated via the backup daemon on a schedule. For manual operations, use the backup daemon REST API:

```bash
# Get backup daemon credentials
BACKUP_USER=$(kubectl get secret -n postgres postgres-credentials -o go-template='{{.data.username | base64decode}}')
BACKUP_PASS=$(kubectl get secret -n postgres postgres-credentials -o go-template='{{.data.password | base64decode}}')

# Port forward to backup daemon
kubectl -n postgres port-forward svc/backup-daemon 8080:8080 &

# Trigger manual backup
curl -X POST http://localhost:8080/backup -u "$BACKUP_USER:$BACKUP_PASS"
# or
curl -X POST http://localhost:8080/backups/request -u "$BACKUP_USER:$BACKUP_PASS"

# Check backup status
curl -X GET http://localhost:8080/status -u "$BACKUP_USER:$BACKUP_PASS"

# Check health and backup progress
curl -X GET http://localhost:8080/health -u "$BACKUP_USER:$BACKUP_PASS"

# Restore from external backup (for disaster recovery)
curl -X POST http://localhost:8080/external/restore -u "$BACKUP_USER:$BACKUP_PASS" \
  -H "Content-Type: application/json" \
  -d '{"backupId": "backup-timestamp", "dbType": "AZURE"}'
```

For detailed backup configuration and pgBackRest features, see the [pgBackRest documentation](docs/public/features/pgBackRest.md).

## Documentation

- [Quick Start Guide](docs/public/quickstart.md) - Get started in minutes
- [Installation Guide](docs/public/installation.md) - Comprehensive installation instructions
- [Features Documentation](docs/public/features/) - Detailed feature guides
  - [Active-Standby Clusters](docs/public/features/active-standby-cluster.md)
  - [Disaster Recovery](docs/public/features/disaster-recovery.md)
  - [Major Version Upgrades](docs/public/features/major-upgrade.md)
  - [pgBackRest Configuration](docs/public/features/pgBackRest.md)
  - [Connection Pooler](docs/public/features/connection-pooler.md)
  - [TLS Configuration](docs/public/features/tls-configuration.md)
  - [Query Exporter](docs/public/features/query-exporter.md)
  - [LDAP Integration](docs/public/features/ldap_integration.md)
  - [Logical Replication](docs/public/features/logical-replication-controller.md)
  - [CIS Hardening](docs/public/features/cis-hardening.md)
  - [Toleration Policies](docs/public/features/toleration-policies.md)

## Development

### Prerequisites

- Go 1.26.2 or later
- Docker or Podman
- Access to a Kubernetes cluster (for testing)

### Building the Operator

```bash
cd operator

# Quick build (recommended for development)
# Handles deps, gzip-charts, move-charts, compile, and docker-build
make sandbox-build

# Local development with formatting, linting, and push
TAG_ENV=dev DOCKER_NAMES="your-registry/pgskipper-operator:dev" make local
```

**Individual commands** (if needed):
```bash
# Format code
make fmt

# Run linters
make vet

# Update dependencies
make deps

# Compile binary only
make compile
```

### Building Services

Each service has its own build process:

```bash
cd services/<service-name>
make docker-build
```

### Generating CRDs

After modifying API types:

```bash
cd operator
make generate
```

This regenerates CRDs and DeepCopy methods from Go type definitions.

### Project Structure

```
.
├── operator/              # Main operator code
│   ├── api/              # CRD API definitions
│   ├── cmd/              # Operator entry point
│   ├── controllers/      # Reconciliation controllers
│   ├── pkg/              # Core business logic
│   └── charts/           # Helm charts
├── services/             # Supporting microservices
│   ├── backup-daemon/
│   ├── monitoring-agent/
│   ├── query-exporter/
│   └── ...
├── tests/                # Robot Framework tests
└── docs/                 # Documentation

```

## Testing

### Robot Framework Tests

```bash
cd tests
# Run specific test suite
robot robot/<test-name>/<test-name>.robot
```

### Unit Tests (Services)

```bash
cd services/<service-name>
make test
```

## Contributing

We welcome contributions! Please follow these steps:

1. Read the [Code of Conduct](CODE-OF-CONDUCT.md)
2. Sign the [Contributor License Agreement](https://pages.netcracker.com/cla-main.html)
3. Fork the repository
4. Create a feature branch (`git checkout -b feature/amazing-feature`)
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

See [CONTRIBUTING.md](CONTRIBUTING.md) for more details.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Support

### Reporting Issues

If you encounter a bug or have a feature request:
1. Check [existing issues](https://github.com/Netcracker/pgskipper-operator/issues)
2. Create a new issue with detailed information

### Security Vulnerabilities

For security concerns, please review our [Security Policy](SECURITY.md) and report vulnerabilities responsibly.

### Community

- **Issues**: [GitHub Issues](https://github.com/Netcracker/pgskipper-operator/issues)
