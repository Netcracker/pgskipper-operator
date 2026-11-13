# Tasks

## 1. Patroni Dockerfile for PostgreSQL 9.6

- [x] 1.1 Add a `PG_VERSION`-aware apt source path so `postgresql-9.6` resolves (PGDG archive / older suite, or fall back to a 9.6-specific base image only if archive-on-jammy fails) and verify `apt-get install postgresql-9.6` succeeds in a dry-run or local build stage
- [x] 1.2 Split package installs into a 9.6-safe set and a modern-only set; skip at least `pgvector`, `pg-failover-slots`, `credcheck`, `pgnodemx`, and incompatible source builds (`pgsentinel`, `pg_dbms_stats` REL14) when `PG_VERSION=9.6`; verify the Dockerfile does not reference those packages on the 9.6 path
- [x] 1.3 Replace integer-only `PG_VERSION` comparisons (for example `[ "$PG_VERSION" -lt 18 ]`) with comparisons that accept dotted `9.6`; verify a local `docker build --build-arg PG_VERSION=9.6` reaches package install without shell arithmetic errors
- [x] 1.4 Confirm retained 9.6 third-party packages against the chosen suite index and document the final installed extension list in a short comment or README note under `services/patroni`; verify the note matches what the 9.6 image actually installs
- [x] 1.5 Build `services/patroni` with `PG_VERSION=9.6` locally (or in CI) and verify the image contains `/usr/lib/postgresql/9.6/bin/postgres` and starts without missing shared-library preload errors from omitted extensions

## 2. Build matrix and release wiring

- [x] 2.1 Add `.github/build-config.cfg` component `pgskipper-docker-patroni-9.6` with `pg_version: "9.6"`, same context/file as other Patroni entries; verify `jq` shows the new component alongside 15–18
- [x] 2.2 Add `pgskipper-docker-patroni-9.6` to `.github/workflows/clean.yaml` matrix; verify the workflow YAML lists the new name
- [x] 2.3 Add `ghcr.io/netcracker/pgskipper-docker-patroni-9.6:${release}` to `.github/charts-values-update-config.yaml` Patroni image list; verify the file enumerates 9.6 with the other versioned Patroni images
- [x] 2.4 Confirm `operator/charts/patroni-core/values.yaml` default `dockerImage` remains the current modern image; verify no accidental default switch to 9.6

## 3. Upgrade image for 9.6 → 18

- [x] 3.1 Add a compatible apt source for PostgreSQL 9.6 in `services/upgrade/Dockerfile` and install `postgresql-9.6` (plus server-dev / 9.6-safe extensions as needed for `pg_upgrade`); verify the build resolves `postgresql-9.6` without requiring jammy-pgdg 9.6 packages
- [x] 3.2 Skip 9.6-unavailable extension packages (`pgvector`, `pg-failover-slots`, `credcheck`, `pgnodemx`, and any other missing 9.6 packages) on the upgrade image’s 9.6 install path; verify the Dockerfile does not install those for 9.6
- [x] 3.3 Update `services/upgrade/docker/start.sh` so source `9.6` resolves `PG_VERSION_TARGET` to `18` (not the legacy default of 10); verify a dry run of version selection against `PG_VERSION=9.6` yields target 18
- [x] 3.4 Build the upgrade image and verify both `/usr/lib/postgresql/9.6/bin` and `/usr/lib/postgresql/18/bin` exist; run `pg_upgrade --check` (or equivalent) for 9.6 → 18 when a sample data dir is available, and stop if PG 18 rejects 9.6 as a source

## 4. Runtime and backup path compatibility

- [x] 4.1 Gate operator `useSlots` companion logic so PostgreSQL 9.6 does not add `pg_failover_slots` to `shared_preload_libraries`; verify with a unit test or focused check that majors without the library skip the preload update
- [x] 4.2 Update backup-daemon 9.6 `pg_basebackup` selection to prefer `/usr/lib/postgresql/9.6/bin/pg_basebackup` (keep CentOS path as fallback if useful); verify the script path for version &lt; 10 matches the Debian layout used by the Patroni image

## 5. Integration check

- [x] 5.1 Run or simulate the build matrix selection for a `services/patroni` change and verify `pgskipper-docker-patroni-9.6` is included with `PG_VERSION=9.6`
- [x] 5.2 Spot-check that a modern Patroni `PG_VERSION` (for example 15) build path still installs the modern-only extension packages; verify no regression in the non-9.6 Dockerfile branch
- [x] 5.3 Confirm a `services/upgrade` change still builds `pgskipper-docker-upgrade` and that the image includes 9.6 and 18 bindirs for the planned upgrade path
