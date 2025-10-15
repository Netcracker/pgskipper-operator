# pgskipper-operator

Postgres-Operator provides PostgreSQL as a service on Kubernetes and OpenShift.

## Repository structure

* `./charts` - directory with HELM chart for Postgres components.
* * `./charts/patroni-core` - directory with HELM chart for Patroni Core.
* * `./charts/patroni-services` - directory with HELM chart for Postgres Services.
* `./pkg` - directory with operator source code, which is used for running Postgres Operator.
* `./tests` - directory with robot test source code, `Dockerfile`.

## How to start

Please refer to the [Quick Start Guide](/docs/public/quickstart.md)

### Smoke tests

There is no smoke tests.

### How to troubleshoot

There are no well-defined rules for troubleshooting, as each task is unique, but most frequent issues related to the wrong configuration, so please check:

* Deploy parameters.
* Logs from all Postgres Service pods: operator, postgres db and others.

## Useful links

* [Installation Guide](/docs/public/installation.md)
* [Features](/docs/public/features)
