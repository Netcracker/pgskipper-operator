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

### Integration tests and ATP storage

Integration test settings live under `tests` in the Helm values for **patroni-core** and **patroni-services** (see [`operator/charts/patroni-core/values.yaml`](operator/charts/patroni-core/values.yaml) and [`operator/charts/patroni-services/values.yaml`](operator/charts/patroni-services/values.yaml)). The test image is based on [qubership-docker-integration-tests](https://github.com/Netcracker/qubership-docker-integration-tests). Optional `tests.atpStorage`, `tests.atpReportViewUiUrl`, and `tests.environmentName` map to the same `ATP_*` / `ENVIRONMENT_NAME` variables as in other Qubership demos (Consul `integrationTests.*`, RabbitMQ `tests.*`). The Patroni Services chart renders these into the custom resource (`operator/charts/patroni-services/templates/cr.yaml`).

| Value (Helm)                 | Description |
|------------------------------|-------------|
| `tests.atpStorage.provider`  | S3 provider (for example `aws`, `minio`, `s3`). When set, the chart can emit ATP storage environment variables for the test pod. |
| `tests.atpStorage.serverUrl` | S3 API endpoint URL. |
| `tests.atpStorage.serverUiUrl` | Optional storage UI URL. |
| `tests.atpStorage.bucket`    | Bucket name; empty usually means no S3 upload in the base image flow. |
| `tests.atpStorage.region`    | Region (for example for AWS). |
| `tests.atpStorage.username`  | Access key (sensitive; prefer secrets / external overrides in real environments). |
| `tests.atpStorage.password`  | Secret key (same as username). |
| `tests.atpReportViewUiUrl`   | Optional Allure report UI base URL. |
| `tests.environmentName`      | Optional logical name for paths or labels. |

### Smoke tests

There is no smoke tests.

### How to troubleshoot

There are no well-defined rules for troubleshooting, as each task is unique, but most frequent issues related to the wrong configuration, so please check:

* Deploy parameters.
* Logs from all Postgres Service pods: operator, postgres db and others.

## Useful links

* [Installation Guide](/docs/public/installation.md)
* [Features](/docs/public/features)
