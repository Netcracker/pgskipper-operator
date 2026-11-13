# Spec Delta

## Purpose

Defines the CI and release build matrix that publishes one versioned Patroni container image per supported PostgreSQL major version, including PostgreSQL 9.6.

## ADDED Requirements

### Requirement: Patroni 9.6 component in the build matrix
The project build configuration MUST include a component that builds the Patroni image with `PG_VERSION` set to `9.6` and publishes it under the image name `pgskipper-docker-patroni-9.6`.

#### Scenario: Matrix entry for Patroni 9.6
- **WHEN** a full component build runs from `.github/build-config.cfg` (release or workflow dispatch, or a change under `services/patroni`)
- **THEN** the matrix includes a component named `pgskipper-docker-patroni-9.6`
- **AND** that component uses `services/patroni` as context, the Patroni Dockerfile, and `pg_version` `9.6`

### Requirement: Cleanup and release lists include Patroni 9.6
Workflows and release helpers that enumerate versioned Patroni images MUST include `pgskipper-docker-patroni-9.6` alongside the existing versioned Patroni images so branch cleanup and chart release tagging stay consistent.

#### Scenario: Branch cleanup covers the 9.6 image
- **WHEN** a branch is deleted and the cleanup workflow removes versioned Patroni package tags
- **THEN** `pgskipper-docker-patroni-9.6` is among the components considered for deletion

#### Scenario: Chart release image list can reference Patroni 9.6
- **WHEN** chart/release configuration enumerates versioned Patroni images for tagging
- **THEN** `ghcr.io/netcracker/pgskipper-docker-patroni-9.6` is included so a release can publish that tag

### Requirement: Default chart image remains a modern major
Adding PostgreSQL 9.6 to the build matrix MUST NOT change the default Patroni Docker image in Helm values away from the current modern default. Operators opt into 9.6 by setting the Patroni image explicitly.

#### Scenario: Default values stay on the modern Patroni image
- **WHEN** a user installs Patroni Core with chart defaults
- **THEN** the default `patroni.dockerImage` still points at the existing modern Patroni image (not the 9.6 image)
