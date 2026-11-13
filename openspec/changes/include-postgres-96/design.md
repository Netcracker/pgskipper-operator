# Design

## Context

See `proposal.md` for motivation. Today:

- `.github/build-config.cfg` builds `pgskipper-docker-patroni-{15,16,17,18}` from one Dockerfile with `PG_VERSION` as a major integer string.
- `services/patroni/Dockerfile` uses Ubuntu 22.04 and `jammy-pgdg`, then installs a fixed extension list plus source builds (`pgsentinel`, `pg_dbms_stats`) and `credcheck`.
- PostgreSQL 9.6 is EOL and is **not** published on `jammy-pgdg`. Several extensions in the Dockerfile never shipped for 9.6 (`pgvector`, `pg_failover_slots`, `credcheck`, `pgnodemx`, and the current source pins).
- `services/upgrade/Dockerfile` installs PostgreSQL 12–18 only. `start.sh` still defaults a non-10/non-11 source (including 9.6) to target **10**, which is not in the image.
- The planned major-upgrade path for this change is **9.6 → 18** (not 9.6 → 10 → 11).
- Backup-daemon still falls back to CentOS-style `/usr/pgsql-9.6/bin/pg_basebackup` for versions below 10, which will not match a Debian-based Patroni 9.6 image.
- Operator `useSlots` companion logic loads `pg_failover_slots` for majors below 17; that library will not exist in the 9.6 image.

## Goals / Non-Goals

**Goals:**

- Produce a buildable `pgskipper-docker-patroni-9.6` image from CI.
- Keep a single primary Patroni Dockerfile with version-conditional package/extension installs where practical.
- Make 9.6 package resolution work despite `jammy-pgdg` not shipping 9.6.
- Keep modern major Patroni builds behaviorally unchanged.
- Ship PostgreSQL 9.6 in the upgrade image and select target **18** for source 9.6 so `pg_upgrade` has old and new bindirs.
- Fix obvious 9.6 path mismatch in backup-daemon client binaries when using the new image layout.

**Non-Goals:**

- Making 9.6 the chart default or a recommended production target.
- Adding PostgreSQL 10 or 11 packages to the upgrade image for a multi-hop chain.
- Full feature parity on 9.6 for PoWA, query-exporter preload libs, logical decoding plugins, or other features that depend on omitted extensions.
- Re-adding PostgreSQL 10–14 Patroni images unless separately requested.

## Decisions

### 1. Keep one Patroni Dockerfile; gate packages by `PG_VERSION`

**Choice:** Extend `services/patroni/Dockerfile` with shell conditionals on `PG_VERSION` (including dotted `9.6`) rather than a fully separate `Dockerfile.9.6`, unless a second base image forces a thin wrapper.

**Why:** The existing matrix already passes `PG_VERSION` into one file. A second full Dockerfile would duplicate Patroni, pgBackRest, and runtime setup.

**Alternative considered:** Dedicated `Dockerfile.9.6` on an older base. Rejected as the default unless archive-repo installs on Ubuntu 22.04 prove impractical; then fall back to a 9.6-specific Dockerfile referenced only by the 9.6 matrix entry.

### 2. Obtain 9.6 packages from PGDG archive / older suite

**Choice:** For PostgreSQL 9.6 installs (Patroni `PG_VERSION=9.6` and the upgrade image 9.6 package set), point apt at a PGDG suite that still carries 9.6 (for example `apt-archive.postgresql.org` with an older distro codename such as `bionic-pgdg`), or pin a compatible older Ubuntu base for that build only. Keep `jammy-pgdg` for modern majors.

**Why:** `jammy-pgdg` does not publish `postgresql-9.6`. Archive or older-suite packages are the only apt path without building PostgreSQL from source.

**Alternative considered:** Build PostgreSQL 9.6 from source inside the image. Rejected for size, time, and maintenance cost unless archive packages are unavailable.

### 3. Explicit skip list for 9.6-unavailable extensions

**Choice:** Split package installs into:

- **Common / 9.6-safe set** (install when packages resolve for 9.6): `postgresql-9.6`, `postgresql-contrib-9.6`, `postgresql-server-dev-9.6`, and any third-party packages confirmed present for 9.6 on the chosen suite.
- **Modern-only set** (skip when installing for 9.6): at least `pgvector`, `pg-failover-slots`, `credcheck`, `pgnodemx`; also skip incompatible source builds on the Patroni image (`pgsentinel`, `pg_dbms_stats` REL14 pin).

Apply the same hard skip list to the upgrade image’s 9.6 package set.

**Why:** Missing extensions are the primary build failure mode for 9.6. A hard skip list is clearer than “install whatever apt finds.”

**Implementation note:** Replace `[ "$PG_VERSION" -lt 18 ]`-style integer tests with a comparison that accepts `9.6` (for example major extraction or `sort -V`).

### 4. Wire CI/release lists; leave Helm default on modern image

**Choice:** Add matrix component `pgskipper-docker-patroni-9.6` with `pg_version: "9.6"`. Update `clean.yaml` and `charts-values-update-config.yaml` enumerations. Do **not** change `operator/charts/patroni-core/values.yaml` default `dockerImage`. The existing `pgskipper-docker-upgrade` component already covers the upgrade image build.

**Why:** Matches how 15–18 Patroni images are published today and avoids silently deploying EOL PostgreSQL.

### 5. Upgrade image: 9.6 → 18 only

**Choice:** Add PostgreSQL 9.6 (plus 9.6-safe extensions) to `services/upgrade/Dockerfile` alongside the existing 12–18 installs. Change `check_pgsql_version` so source `9.6` resolves to target `18` (explicit branch and/or enforced `target_version`). Do **not** add PostgreSQL 10 or 11 packages.

**Why:** The planned path is a direct major upgrade from 9.6 to 18. The legacy `else → 10` default would select binaries that are not in the image and are out of scope.

**Alternative considered:** Install 10 and 11 to preserve the old hop defaults. Rejected; product intent is 9.6 → 18.

### 6. Align backup-daemon 9.6 client path to Debian layout

**Choice:** When selecting 9.6 `pg_basebackup`, prefer `/usr/lib/postgresql/9.6/bin/pg_basebackup` (with a fallback to `/usr/pgsql-9.6/bin/...` if needed for older sidecar images).

**Why:** New Patroni/backup tooling on Ubuntu uses `/usr/lib/postgresql/<ver>/bin`. The CentOS path alone will break against the new 9.6 image.

### 7. Operator runtime: do not preload missing 9.6 libraries by default

**Choice:** Where the reconciler adds `pg_failover_slots` for `useSlots` on majors &lt; 17, gate that addition so PostgreSQL 9.6 (and any major without the library) does not get a broken `shared_preload_libraries` entry. Prefer version-aware skips over changing global defaults for modern clusters.

**Why:** A built image that cannot start because of a missing preload library fails the practical goal of “include 9.6.”

## Risks / Trade-offs

- **[Risk] Archive PGDG packages or older-suite deps conflict with Ubuntu 22.04 libraries** → Mitigation: try archive-on-jammy first; if unresolved, switch the 9.6 Patroni matrix entry (and/or upgrade 9.6 install) to an older base image / dedicated Dockerfile fragment.
- **[Risk] PostgreSQL 18 `pg_upgrade` may reject a 9.6 cluster** → Mitigation: verify with `pg_upgrade --check` during implementation; if unsupported, stop and revisit (documented multi-hop would be a separate change).
- **[Risk] pgBackRest 2.55.1 or current Patroni pip pins drop PostgreSQL 9.6** → Mitigation: verify during implementation; if unsupported, pin compatible versions only for the 9.6 build path and document the pin.
- **[Risk] Reduced extension set breaks optional features (PoWA, query exporter preload, logical plugins) and post-upgrade `ALTER EXTENSION` scripts** → Mitigation: treat as expected for 9.6; document the reduced set; do not claim feature parity.
- **[Risk] `PG_VERSION=9.6` breaks scripts that assume integer majors** → Mitigation: audit Dockerfile and nearby scripts for `-lt`/`-ge` integer comparisons; normalize to major or version-sort comparisons.
- **[Trade-off] EOL PostgreSQL increases security exposure** → Accept only as an explicit compatibility image; keep chart default on a supported modern major.

## Migration Plan

1. Land Patroni Dockerfile, upgrade Dockerfile/`start.sh`, and build-matrix changes; confirm CI builds `pgskipper-docker-patroni-9.6` and `pgskipper-docker-upgrade`.
2. Publish the images on the usual tag pipeline.
3. Consumers set `patroni.dockerImage` to `ghcr.io/netcracker/pgskipper-docker-patroni-9.6:<tag>` when they need 9.6.
4. Major upgrade from 9.6 uses the updated upgrade image with target 18.
5. Rollback: remove the Patroni 9.6 matrix entry and 9.6 branches in Patroni/upgrade Dockerfiles; modern images remain unaffected.

## Open Questions

- Exact archive suite / base image that successfully resolves `postgresql-9.6` together with the retained extension subset on the build runners (confirm during first implementation attempt).
- Final verified list of third-party packages that still exist for 9.6 on that suite (beyond the hard skip list above).
- Confirm PG 18 `pg_upgrade` accepts 9.6 as a source (implementation gate).
