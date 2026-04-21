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

Integration test settings live under `tests` in the Helm values for **patroni-core** and **patroni-services** (see [`operator/charts/patroni-core/values.yaml`](operator/charts/patroni-core/values.yaml) and [`operator/charts/patroni-services/values.yaml`](operator/charts/patroni-services/values.yaml)). The test image is based on [qubership-docker-integration-tests](https://github.com/Netcracker/qubership-docker-integration-tests).

ATP-related Helm values are `atpReport` (with nested `atpReport.atpStorage`), `atpReportViewUiUrl`, and `environmentName`. The chart maps them into the Custom Resource and the operator sets the usual `ATP_*` and `ENVIRONMENT_NAME` environment variables on the integration test pod.

| Value (Helm)                       | Description |
|------------------------------------|-------------|
| `atpReport.enabled`                | Opt-in for ATP report upload; when `false`, S3-related env vars are not applied as in other product charts. |
| `atpReport.atpStorage.provider`    | S3 provider (for example `aws`, `minio`, `s3`). |
| `atpReport.atpStorage.serverUrl`   | S3 API endpoint URL. |
| `atpReport.atpStorage.serverUiUrl` | Optional storage UI URL. |
| `atpReport.atpStorage.bucket`      | Bucket name; empty usually means no S3 upload in the base image flow. |
| `atpReport.atpStorage.region`      | Region (for example for AWS). |
| `atpReport.atpStorage.username`    | Access key (sensitive; stored in Kubernetes Secret when `atpReport.enabled=true`). |
| `atpReport.atpStorage.password`    | Secret key (same as username; stored in Kubernetes Secret when `atpReport.enabled=true`). |
| `atpReportViewUiUrl`               | Optional Allure report UI base URL. |
| `environmentName`                  | Optional logical name for paths or labels. |

### Smoke tests

There is no smoke tests.

### How to troubleshoot

There are no well-defined rules for troubleshooting, as each task is unique, but most frequent issues related to the wrong configuration, so please check:

* Deploy parameters.
* Logs from all Postgres Service pods: operator, postgres db and others.

## Useful links

* [Installation Guide](/docs/public/installation.md)
* [Features](/docs/public/features)
