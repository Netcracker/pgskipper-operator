# AGENTS.md

This file provides guidance to AI coding assistants when working with code in this repository.

## Project Overview

pgskipper-operator is a Kubernetes operator that provides PostgreSQL as a service on Kubernetes and OpenShift. The operator manages PostgreSQL clusters using Patroni for high availability and includes various supporting services.

## Repository Architecture

This is a **monorepo** with multiple Go modules:

- **`/operator`** - Main operator implementation that manages two Custom Resource Definitions:
  - `PatroniCore` (api/patroni/v1) - Core PostgreSQL cluster management with Patroni
  - `PatroniServices` (api/apps/v1) - Additional PostgreSQL services layer
  
- **`/services`** - Supporting microservices that run alongside PostgreSQL:
  - `backup-daemon` - Handles PostgreSQL backups
  - `dbaas-adapter` - Database-as-a-Service adapter
  - `monitoring-agent` - Collects PostgreSQL metrics
  - `patroni` - Patroni service implementation
  - `pgbackrest-sidecar` - pgBackRest backup sidecar
  - `query-exporter` - Prometheus query exporter
  - `replication-controller` - Manages PostgreSQL replication
  - `upgrade` - Handles PostgreSQL version upgrades

- **`operator/charts`** - Helm charts
  - `patroni-core` - Helm chart for Patroni Core operator
  - `patroni-services` - Helm chart for PostgreSQL Services operator

- **`/tests`** - Robot Framework tests

## Operator Dual-Mode Operation

The operator runs in one of two modes controlled by the `OPERATOR_ROLE` environment variable:

1. **`OPERATOR_ROLE=patroni`** - Runs PatroniCoreReconciler (manages PatroniCore CRD)
2. **Default (or any other value)** - Runs PostgresServiceReconciler (manages PatroniServices CRD)

Entry point: `operator/cmd/pgskipper-operator/main.go`

## Building and Development

### Operator

Work from `/operator` directory:

```bash
cd operator

# Format code
make fmt

# Vet code
make vet

# Update dependencies
make deps

# Compile binary
make compile

# Build Docker image (requires TAG_ENV and DOCKER_NAMES env vars)
make docker-build

# Full local build
make local  # fmt + gzip-charts + deps + vet + compile + docker-build + docker-push

# Generate CRDs from API types
make generate
```

### Services

Each service in `/services` has its own Makefile with similar targets:

```bash
cd services/<service-name>

# Build Docker image
make docker-build

# Run tests (where available)
make test

# Format code
make fmt
```

### Default Docker Image Naming

Without environment variables, images default to:
- Operator: `ghcr.io/netcracker/pgskipper-operator:local`
- Services: `ghcr.io/netcracker/pgskipper-docker-<service-name>:local`

Override with:
```bash
TAG_ENV=v1.2.3 DOCKER_NAMES="custom-registry/image:tag" make docker-build
```

## Key Package Structure

In `operator/pkg`:

- `reconciler/` - Core reconciliation logic
- `deployment/` - Kubernetes deployment management
- `storage/` - Storage provisioning and PV management
- `credentials/` - Secret and credential management
- `disasterrecovery/` - DR manager initialization
- `consul/` - Consul integration
- `pooler/` - Connection pooler management
- `postgresexporter/`, `pgbackrestexporter/`, `queryexporter/` - Metrics exporters
- `util/` - Utility functions
- `client/` - Kubernetes client wrappers

## Testing

Robot Framework tests are in `/tests/robot`:

```bash
# Tests include:
# - check_installation
# - check_crud_user
# - check_manual_switchover
# - patroni_rest_api_auth
```

Service-specific Go tests:
```bash
cd services/<service-name>
make test
```

## CRD Generation

CRDs are generated from Go types using controller-gen:

```bash
cd operator
make generate
```

This generates:
- CRDs in `operator/charts/patroni-core/crds/` and `operator/charts/patroni-services/crds/`
- DeepCopy methods in `api/*/zz_generated.deepcopy.go`
- OpenAPI specs in `api/*/zz_generated.openapi.go`

API type definitions:
- `operator/api/common/v1` - Common types
- `operator/api/apps/v1/postgresservice_types.go` - PatroniServices CRD
- `operator/api/patroni/v1/patronicore_types.go` - PatroniCore CRD

## Helm Chart Preparation

The operator embeds Helm charts into its deployment:

```bash
cd operator
make gzip-charts   # Compresses Grafana dashboards
make move-charts   # Copies charts to operator/deployments/charts/patroni-services
```

## Installation (From Quickstart)

1. Install CRDs:
```bash
kubectl create -f ./operator/charts/patroni-core/crds/netcracker.com_patronicores.yaml
kubectl create -f ./operator/charts/patroni-services/crds/netcracker.com_patroniservices.yaml
```

2. Install Patroni Core:
```bash
helm install --namespace=postgres --create-namespace \
  -f ./operator/charts/patroni-core/patroni-core-quickstart-sample.yaml \
  patroni-core ./operator/charts/patroni-core
```

3. Wait for leader promotion:
```bash
kubectl -n postgres get pods --selector=pgtype=master --field-selector=status.phase=Running
```

4. Install Patroni Services:
```bash
helm install --namespace=postgres \
  -f ./operator/charts/patroni-services/patroni-services-quickstart-sample.yaml \
  patroni-services ./operator/charts/patroni-services
```

## Dependencies

- Go 1.26.2
- Kubernetes 0.35.x (client-go, api, apimachinery)
- controller-runtime 0.23.3
- Patroni (for PostgreSQL HA)
- pgBackRest (for backups)
- Uses Consul for distributed configuration
- Google Cloud APIs for cloud provider integration
- operator-framework/operator-lib for leader election

## Common Gotchas

- The operator uses **leader election** via operator-lib (lock name depends on OPERATOR_ROLE)
- Grafana dashboards must be gzipped before deployment
- PatroniCore must be fully running (leader elected) before installing PatroniServices
- Storage configuration is critical - supports both PV provisioners and manual PV assignment
- TLS can be enabled on operator server (port 8443 vs 8080)

## Commit Message Pattern

All commits in this project MUST follow this format:

```
<type>: [<ticket-number>] <meaningful message>
```

Where:
- `<type>` = fix, chore, feat, chore(deps), etc.
- `<ticket-number>` = REQUIRED - Always ask the user for the ticket number before creating any commit
- `<meaningful message>` = Clear, concise description of the change

**Example:**
```
feat: [CPCAP-1234] add backup retention policy configuration
fix: [CPCAP-5678] resolve memory leak in monitoring agent
chore(deps): [CPCAP-9012] bump kubernetes dependencies to v0.35.1
```

**Important:** Never create a commit without first asking the user for the ticket number.
