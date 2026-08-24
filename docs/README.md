# Documentation Index

## Navigation

### `docs/public/`

- [Quickstart](public/quickstart.md) — Get a PostgreSQL cluster running with Helm in minutes.
- [Installation guide](public/installation.md) — Prerequisites, all Helm chart parameters, platform-specific configurations, and upgrade procedures.

### `docs/public/features/`

- [Active-standby cluster](public/features/active-standby-cluster.md) — Deploy two PostgreSQL clusters in an active-standby configuration across two Kubernetes clusters.
- [CIS hardening](public/features/cis-hardening.md) — Apply CIS benchmark recommendations to a PostgreSQL deployment on Kubernetes or OpenShift.
- [Connection pooler](public/features/connection-pooler.md) — PGBouncer integration: limitations and streaming-replication notes.
- [Disaster recovery](public/features/disaster-recovery.md) — DR scheme overview, Site Manager REST API, and switchover procedures.
- [LDAP integration](public/features/ldap_integration.md) — Configure PostgreSQL LDAP authentication with Active Directory.
- [Logical replication controller](public/features/logical-replication-controller.md) — REST API for managing PostgreSQL publications and granting replication users.
- [Major upgrade](public/features/major-upgrade.md) — Upgrade PostgreSQL major versions using `pg_upgrade` via the operator.
- [pgBackRest](public/features/pgBackRest.md) — pgBackRest sidecar: deployment, backup types, restore, PITR, and retention.
- [Query exporter](public/features/query-exporter.md) — Custom Prometheus metrics, exporter user, circuit-breaker, and parallel connections.
- [TLS configuration](public/features/tls-configuration.md) — Enable TLS for PostgreSQL connections with cert-manager or manual certificates.
- [Toleration policies](public/features/toleration-policies.md) — Apply Kubernetes taints and tolerations to all Postgres Service deployments.

## Project layout

```
docs/
├── README.md                             # This index
└── public/
    ├── quickstart.md                     # Fast-path installation guide
    ├── installation.md                   # Full Helm parameter reference and platform setup
    ├── features/
    │   ├── active-standby-cluster.md     # Active-standby deployment scheme
    │   ├── cis-hardening.md              # CIS benchmark compliance guidance
    │   ├── connection-pooler.md          # PGBouncer limitations and notes
    │   ├── disaster-recovery.md          # DR scheme and Site Manager API
    │   ├── ldap_integration.md           # LDAP / Active Directory authentication
    │   ├── logical-replication-controller.md  # Publications REST API
    │   ├── major-upgrade.md              # pg_upgrade-based major version upgrade
    │   ├── pgBackRest.md                 # pgBackRest backup and restore
    │   ├── query-exporter.md             # Prometheus query exporter
    │   ├── tls-configuration.md          # TLS / SSL setup
    │   └── toleration-policies.md        # Kubernetes toleration configuration
    └── images/
        ├── arch/                         # Architecture diagrams
        └── features/                     # Feature-specific diagrams
```
