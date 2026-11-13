# pgskipper-patroni

Patroni Docker image for pgskipper. Build with a PostgreSQL major via `PG_VERSION`.

```bash
# Modern major (default 15)
make docker-build

# PostgreSQL 9.6 compatibility image
PG_VERSION=9.6 DOCKER_NAMES=ghcr.io/netcracker/pgskipper-docker-patroni-9.6:local make docker-build
```

## PostgreSQL 9.6 extension set

`jammy-pgdg` publishes core 9.6 packages only. The 9.6 image installs:

- `postgresql-9.6`, `postgresql-contrib-9.6`, `postgresql-server-dev-9.6`, `postgresql-plpython3-9.6`
- `postgresql-9.6-pgaudit`

Skipped on 9.6 (not available for this major on the current package source): `hypopg`, `powa`, `orafce`, `cron`, `repack`, `pg-stat-kcache`, `pg-qualstats`, `set-user`, `postgis`, `pg-wait-sampling`, `pg-track-settings`, `pg-hint-plan`, `pgnodemx`, `decoderbufs`, `pglogical`, `pgvector`, `pg-failover-slots`, `credcheck`, `pg_profile`, `pgsentinel`, and `pg_dbms_stats`.
