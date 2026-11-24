# Spec Delta

## Purpose

Defines how the upgrade service image supplies PostgreSQL binaries and packages so clusters can major-upgrade from PostgreSQL 9.6 to PostgreSQL 18.

## ADDED Requirements

### Requirement: Upgrade image includes PostgreSQL 9.6 binaries
The upgrade service image MUST include PostgreSQL 9.6 server binaries under the Debian layout (`/usr/lib/postgresql/9.6/bin/`) so `pg_upgrade` can use them as the old bindir when the cluster data directory is version 9.6.

#### Scenario: 9.6 old bindir present
- **WHEN** the upgrade image is built
- **THEN** `/usr/lib/postgresql/9.6/bin` contains the PostgreSQL 9.6 tools required for `pg_upgrade` (including `postgres` and related binaries)
- **AND** the image continues to include PostgreSQL 18 binaries for use as the new bindir

### Requirement: Source 9.6 targets PostgreSQL 18
When the data directory reports PostgreSQL 9.6, upgrade version selection MUST choose target version 18 (via explicit logic and/or `target_version`), not the legacy default of 10. The selected target MUST correspond to binaries present in the upgrade image. PostgreSQL 10 and 11 packages are not required for this path.

#### Scenario: 9.6 data directory selects target 18
- **WHEN** upgrade runs against a data directory whose `PG_VERSION` is `9.6`
- **THEN** the resolved `PG_VERSION_TARGET` is `18`
- **AND** `/usr/lib/postgresql/18/bin` is used as the new bindir

#### Scenario: No dependency on 10 or 11 packages
- **WHEN** the upgrade image is built for the 9.6-to-18 path
- **THEN** the image does not need PostgreSQL 10 or 11 packages to complete that upgrade path

### Requirement: Skip extensions unavailable for PostgreSQL 9.6 in the upgrade image
When installing the PostgreSQL 9.6 package set in the upgrade image, the build MUST NOT attempt to install extensions or packages that are not available for 9.6. At minimum, omit `pgvector`, `pg_failover_slots`, `credcheck`, and `pgnodemx` for the 9.6 set.

#### Scenario: 9.6 package install omits unavailable extensions
- **WHEN** the upgrade image installs PostgreSQL 9.6-related packages
- **THEN** the build does not fail due to missing 9.6 packages such as `postgresql-9.6-pgvector` or `postgresql-9.6-pg-failover-slots`

### Requirement: PostgreSQL 9.6 package source for the upgrade image
Because PostgreSQL 9.6 packages are not published on `jammy-pgdg`, the upgrade image build MUST obtain 9.6 packages from a compatible package source or base-image approach so `postgresql-9.6` resolves.

#### Scenario: 9.6 packages resolve in the upgrade image build
- **WHEN** the upgrade image build installs `postgresql-9.6`
- **THEN** the package manager can resolve that package without relying on `jammy-pgdg` to publish 9.6
