# Proposal

## Why

The CI build matrix publishes Patroni images only for PostgreSQL 15–18, so clusters that still need 9.6 cannot use the current image pipeline. Restoring a versioned Patroni image for 9.6 requires packaging that version and skipping extensions that do not exist for it, otherwise the Docker build fails. The upgrade image must also ship 9.6 binaries so those clusters can major-upgrade to PostgreSQL 18.

## What Changes

- Add PostgreSQL 9.6 to the Patroni image build matrix as `pgskipper-docker-patroni-9.6` (`PG_VERSION=9.6`).
- Adapt the Patroni Dockerfile so a 9.6 build installs only packages and extensions available for that version.
- Wire 9.6 into related CI housekeeping (for example branch image cleanup) and chart/release image lists where versioned Patroni images are enumerated.
- Include PostgreSQL 9.6 server packages in the upgrade image (`pgskipper-docker-upgrade`) so `pg_upgrade` can run with old bindir 9.6 and new bindir 18.
- Update upgrade version selection so a 9.6 data directory targets PostgreSQL 18 (not the legacy default of 10). Do not add PostgreSQL 10 or 11 packages for this path.
- Skip 9.6-unavailable extensions when installing the 9.6 package set in the upgrade image.
- Align backup-daemon 9.6 client binary paths with the Debian layout used by the Patroni image (`/usr/lib/postgresql/9.6/...`) when the image path and existing `/usr/pgsql-9.6/...` assumption diverge.
- Document that 9.6 is an EOL compatibility target with a reduced extension set.

## Capabilities

### New Capabilities

- `patroni-postgres-image`: Versioned Patroni Docker images for supported PostgreSQL majors, including per-version package and extension selection so builds succeed when an extension is unavailable.
- `postgres-build-matrix`: Build and release configuration that publishes one Patroni image per supported PostgreSQL version, including 9.6.
- `postgres-upgrade-image`: Upgrade service image that provides PostgreSQL binaries for major upgrades from 9.6 to 18, with a 9.6-safe extension package set.

### Modified Capabilities

- (none — no existing specs)

## Impact

- `services/patroni/Dockerfile` and possibly Makefile build-args for local 9.6 builds
- `services/upgrade/Dockerfile` and `services/upgrade/docker/start.sh` (9.6 packages, target 18, extension skips)
- `.github/build-config.cfg`, `.github/workflows/clean.yaml`, `.github/charts-values-update-config.yaml`
- Possibly `services/backup-daemon` scripts that still point at CentOS-style 9.6 bin paths
- Operator/runtime feature defaults that assume extensions absent on 9.6 (for example `pg_failover_slots`) only where needed for a usable cluster; chart default image stays on a modern version
