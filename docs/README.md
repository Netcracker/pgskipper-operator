# Documentation

Documentation for [PGSkipper Operator](../README.md), a PostgreSQL operator for Kubernetes and OpenShift.

## Getting Started

- [Quickstart](public/quickstart.md) — Deploy a PostgreSQL cluster with Helm.
- [Installation and Configuration](public/installation.md) — Requirements, Helm values, deployment options, and platform-specific setup.

## Operations and Troubleshooting

- [Troubleshooting](public/troubleshooting.md) — Resolve deployment, Helm, operator, upgrade, Site Manager, and DBaaS failures.
- [PostgreSQL Cluster Troubleshooting](public/troubleshooting-pg-cluster.md) — Diagnose application-side errors, Patroni HA, replication, backup, pooler, and DR issues; generic operations.
- [Prometheus Alerts Troubleshooting](public/alarm-troubleshooting.md) — Investigate PostgreSQL monitoring alerts and tune their thresholds.

## Features

### High Availability and Data Protection

- [Active-Standby Cluster](public/features/active-standby-cluster.md) — Configure active and standby PostgreSQL clusters.
- [Disaster Recovery](public/features/disaster-recovery.md) — Disaster recovery architecture and switchover procedures.
- [pgBackRest](public/features/pgBackRest.md) — Backup, restore, point-in-time recovery, and retention.
- [Major Version Upgrade](public/features/major-upgrade.md) — Upgrade PostgreSQL using `pg_upgrade`.

### Database Connectivity and Integration

- [Connection Pooler](public/features/connection-pooler.md) — PgBouncer integration and limitations.
- [Logical Replication Controller](public/features/logical-replication-controller.md) — Manage PostgreSQL publications and replication access.
- [Query Exporter](public/features/query-exporter.md) — Export SQL-based metrics to Prometheus.

### Security

- [TLS Configuration](public/features/tls-configuration.md) — Configure PostgreSQL TLS certificates.
- [LDAP Integration](public/features/ldap_integration.md) — Configure authentication with LDAP or Active Directory.
- [CIS Hardening](public/features/cis-hardening.md) — PostgreSQL security hardening guidance.

### Kubernetes Configuration

- [Toleration Policies](public/features/toleration-policies.md) — Configure Pod tolerations and scheduling.

## Contributing

- [Contribution Guide](../CONTRIBUTING.md) — How to contribute code and documentation.
- [Agent Instructions](../AGENTS.md) — Repository guidance for AI coding agents.

## Documentation Structure

```text
docs/
├── README.md                          # Documentation index
└── public/
    ├── quickstart.md                  # First deployment
    ├── installation.md                # Installation and configuration
    ├── troubleshooting.md             # Deployment and operator issues
    ├── troubleshooting-pg-cluster.md  # PostgreSQL cluster issues and operations
    ├── alarm-troubleshooting.md       # Prometheus alerts
    ├── features/                      # Feature-specific guides
    └── images/                        # Diagrams and screenshots
```
