# Spec Delta

## Purpose

Defines how versioned Patroni Docker images are built for each supported PostgreSQL major version, including which packages and extensions MUST be present and which MUST be omitted when unavailable for that version.

## ADDED Requirements

### Requirement: Versioned Patroni image build argument
The Patroni image build MUST accept a `PG_VERSION` build argument that selects the PostgreSQL major version packages to install. Supported values MUST include `9.6` in addition to the existing modern majors already built by the project.

#### Scenario: Build Patroni image for PostgreSQL 9.6
- **WHEN** the Patroni image is built with `PG_VERSION=9.6`
- **THEN** the image contains PostgreSQL 9.6 server binaries under the Debian layout (`/usr/lib/postgresql/9.6/bin/`)
- **AND** the image entrypoint can start Patroni against that PostgreSQL installation

#### Scenario: Build Patroni image for an existing modern major
- **WHEN** the Patroni image is built with a currently supported modern `PG_VERSION` (for example `15`)
- **THEN** the image still installs that version’s packages and the previously available extension set for that major

### Requirement: Skip extensions unavailable for PostgreSQL 9.6
When `PG_VERSION` is `9.6`, the Patroni image build MUST NOT attempt to install extensions or packages that are not available for PostgreSQL 9.6. The build MUST complete successfully without those packages.

At minimum, the following MUST be omitted for 9.6 (they are not available for that major in the current packaging model):

- `pgvector`
- `pg_failover_slots` (`postgresql-$PG_VERSION-pg-failover-slots`)
- `credcheck`
- `pgnodemx`
- source builds that target newer PostgreSQL APIs than 9.6 supports (for example the current `pg_dbms_stats` REL14 branch and `pgsentinel` when incompatible)

Extensions that remain available for 9.6 through the chosen package source MAY still be installed (for example contrib modules, and third-party packages that still publish 9.6 builds).

#### Scenario: 9.6 build omits unavailable extension packages
- **WHEN** the Patroni image is built with `PG_VERSION=9.6`
- **THEN** the build does not fail due to missing packages such as `postgresql-9.6-pgvector`, `postgresql-9.6-pg-failover-slots`, `postgresql-9.6-credcheck`, or `postgresql-9.6-pgnodemx`
- **AND** those extensions are not present in the finished image

#### Scenario: Modern build keeps modern-only extensions
- **WHEN** the Patroni image is built with a modern `PG_VERSION` that publishes those packages
- **THEN** the modern-only extensions that the Dockerfile already installs for that major remain installable

### Requirement: PostgreSQL 9.6 package source compatibility
Because PostgreSQL 9.6 packages are not published on the current Ubuntu 22.04 (`jammy-pgdg`) repository used by the Dockerfile, the 9.6 image build MUST obtain 9.6 packages from a compatible package source or base image so `postgresql-9.6` and related packages resolve.

#### Scenario: 9.6 packages resolve during image build
- **WHEN** the Patroni image is built with `PG_VERSION=9.6`
- **THEN** the package manager can install `postgresql-9.6` (and the 9.6-compatible extension subset selected for that image)
- **AND** the build does not depend on `jammy-pgdg` providing 9.6 packages

### Requirement: Version comparisons tolerate dotted 9.6
Any build-time logic that branches on `PG_VERSION` MUST treat `9.6` as a valid version identifier. Integer-only comparisons that reject dotted values MUST NOT be used for that argument.

#### Scenario: Conditional install logic accepts 9.6
- **WHEN** Dockerfile or build scripts evaluate version gates with `PG_VERSION=9.6`
- **THEN** the logic selects the 9.6 install path without shell or arithmetic errors caused by the dotted version string
