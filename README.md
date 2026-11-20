# PGSkipper Operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27.x-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Operator-326CE5?logo=kubernetes&logoColor=white)](https://kubernetes.io/)
[![Helm](https://img.shields.io/badge/Helm-3-0F1689?logo=helm&logoColor=white)](https://helm.sh/)
[![Build](https://github.com/Netcracker/pgskipper-operator/actions/workflows/build.yaml/badge.svg?branch=main)](https://github.com/Netcracker/pgskipper-operator/actions/workflows/build.yaml)

PGSkipper Operator is a Kubernetes operator for deploying and managing PostgreSQL clusters on **Kubernetes and OpenShift**. It combines Patroni-based high availability with backup and recovery, monitoring, and Database-as-a-Service (DBaaS) integration.

## Key Features

- **High availability** — Patroni-managed leader election and automatic failover.
- **Backup and recovery** — Automated backups, restores, and pgBackRest integration.
- **Disaster recovery** — Active-standby clusters and recovery workflows.
- **DBaaS integration** — PostgreSQL database management through the DBaaS adapter.
- **Observability** — PostgreSQL metrics, monitoring, and query exporter support.
- **Connectivity and security** — PgBouncer connection pooling, TLS, and LDAP authentication.
- **Lifecycle management** — Cluster configuration, logical replication, and PostgreSQL major version upgrades.

See the [feature guides](docs/README.md) for details.

## Architecture

PGSkipper is organized around two custom resources:

- **`PatroniCore`** — Manages PostgreSQL clusters, persistent storage, and Patroni high availability.
- **`PatroniServices`** — Manages supporting components, including the backup daemon, monitoring, DBaaS adapter, and connection pooler.

Both components are deployed using Helm charts. For deployment requirements and configuration, see the [installation guide](docs/public/installation.md).

## Repository Structure

| Path | Purpose |
| --- | --- |
| [`operator/`](operator/) | Operator implementation and Helm charts |
| [`services/`](services/) | PostgreSQL supporting services |
| [`docs/`](docs/) | User and feature documentation |
| [`tests/`](tests/) | Automated tests |
| [`agent-packages/`](agent-packages/) | Agent-oriented troubleshooting guides |

## Documentation

Start with the **[Documentation Index](docs/README.md)**, or go directly to:

- [Quickstart](docs/public/quickstart.md) — Deploy your first cluster.
- [Installation and Configuration](docs/public/installation.md) — Configure and deploy the operator.
- [Feature Guides](docs/public/features/) — Explore supported capabilities.
- [Troubleshooting](docs/public/troubleshooting.md) — Diagnose and resolve common issues.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines and the contributor license agreement.

## License

Licensed under the [Apache License 2.0](LICENSE).

## Support and Security

Report bugs or request features through [GitHub Issues](https://github.com/Netcracker/pgskipper-operator/issues). Report security vulnerabilities according to the [Security Policy](SECURITY.md).
