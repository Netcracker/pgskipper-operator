# Quickstart

Get PostgreSQL up and running on Kubernetes or OpenShift using Helm.

- [Prerequisites](#prerequisites)
- [Installation](#installation)
  - [Storage Configuration](#storage-configuration)
  - [Installation via Helm](#installation-via-helm)
    - [CRD Installation](#crd-installation)
    - [Restricted Environments](#restricted-environments)
    - [Install Patroni-Core Operator](#install-patroni-core-operator)
    - [Install Patroni-Services Operator](#install-patroni-services-operator)
- [Validation of Installation](#validation-of-installation)
- [Connect to PostgreSQL](#connect-to-postgresql)
- [Delete a PostgreSQL Cluster](#delete-a-postgresql-cluster)

## Prerequisites

Make sure the following tools are installed on your local machine:

- [git](https://git-scm.com/downloads)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [helm](https://helm.sh/docs/intro/install/)

## Installation

Clone the repository and change to its directory:

```bash
git clone git@github.com:Netcracker/pgskipper-operator.git
cd postgres-operator
```

The `operator/charts` directory contains two Helm charts: `patroni-core` and `patroni-services`.

### Storage Configuration

Before installation, configure `patroni.storage` in `./operator/charts/patroni-core/patroni-core-quickstart-sample.yaml` and `backupDaemon.storage` in `./operator/charts/patroni-services/patroni-services-quickstart-sample.yaml` to match your persistent volume (PV) setup.

1. **With a PV provisioner**, use the following storage configuration:

   ```yaml
   storage:
     type: provisioned
     size: 2Gi
     storageClass: <storage-class>
   ```

   Replace `<storage-class>` with your StorageClass name.

2. **Without a PV provisioner**, specify the PVs and their nodes in `patroni.storage`:

   ```yaml
   storage:
       type: pv
       size: 2Gi
       volumes:
         - <first-pv>
         - <second-pv>
       nodes:
         - <first-pv-node>
         - <second-pv-node>
   ```

   Replace `<first-pv>` and `<second-pv>` with your PV names, and `<first-pv-node>` and `<second-pv-node>` with their node names.

Configure `backupDaemon.storage` similarly, but with a single PV.

### Installation via Helm

After configuring storage, install the PostgreSQL operators.

#### CRD Installation

Custom Resource Definitions (CRDs) are cluster-scoped resources managed as a **dedicated application**. They must be installed and synced before the main operator charts. The main microservice deployments rely on pre-installed CRDs and must not install them independently.

Install the CRDs first:

```bash
kubectl create -f ./operator/charts/patroni-core/crds/netcracker.com_patronicores.yaml
kubectl create -f ./operator/charts/patroni-services/crds/netcracker.com_patroniservices.yaml
```

Wait for the CRDs to be registered before proceeding:

```bash
kubectl get crd netcracker.com_patronicores.yaml patroniservices.netcracker.com
```

##### Restricted Environments

If the deployment user lacks cluster-wide permissions, the dedicated CRDs application must **not** be installed by that user. Instead, a cluster administrator must pre-install the CRDs out-of-band, and the `DISABLE_CRD=true` flag must be set on the main microservice application.

> **Note:** `DISABLE_CRD=true` is deprecated for environments with cluster-wide permissions. Use the dedicated CRDs application approach in other environments.

#### Install Patroni-Core Operator

Install the Patroni-Core Operator:

```bash
helm install --namespace=postgres --create-namespace -f ./operator/charts/patroni-core/patroni-core-quickstart-sample.yaml patroni-core ./operator/charts/patroni-core
```

This creates the necessary resources and starts the Patroni-Core Operator Pod.

Check the Patroni pods:

```bash
kubectl -n postgres get pods \
  --selector=app=patroni \
  --field-selector=status.phase=Running
```

The command should return two Patroni pods without restarts. Wait for leader election, then identify the leader pod:

```bash
kubectl -n postgres get pods \
  --selector=pgtype=primary \
  --field-selector=status.phase=Running
```

If the operator pod is not running, check its logs:

```bash
kubectl logs -n postgres "$(kubectl get pod -n postgres -l name=patroni-core-operator --output='name')"
```

#### Install Patroni-Services Operator

Once a leader has been elected, install the Patroni-Services Operator:

```bash
helm install --namespace=postgres -f ./operator/charts/patroni-services/patroni-services-quickstart-sample.yaml patroni-services ./operator/charts/patroni-services
```

This creates the necessary resources and starts the Postgres-Operator Pod.

Check the Patroni-Services Operator pod:

```bash
kubectl -n postgres get pods \
  --selector=name=postgres-operator \
  --field-selector=status.phase=Running
```

If the operator pod is not running, check its logs:

```bash
kubectl logs -n postgres "$(kubectl get pod -n postgres -l name=postgres-operator --output='name')"
```

## Validation of Installation

After installation or upgrade, verify the PostgreSQL cluster:

- All requested components are deployed, such as Patroni, PostgreSQL Backup Daemon, and PostgreSQL Monitoring Collector, if enabled.
- A pod with the `pgtype=primary` label exists.
- Pods with the `pgtype=replica` label exist.
- All pods have `Ready 1/1` status.

You can also check `status.conditions` in the `PatroniCore` and `PatroniServices` custom resources.

The following commands show the transition time to the `Successful` state:

```bash
kubectl -n postgres get patronicore patroni-core -o jsonpath='{.metadata.name}{"\t"}{.status.conditions[?(@.type=="Successful")].lastTransitionTime}'
```

```bash
kubectl -n postgres get patroniservices patroni-services -o jsonpath='{.metadata.name}{"\t"}{.status.conditions[?(@.type=="Successful")].lastTransitionTime}'
```

To view all conditions and their transition times:

```bash
kubectl -n postgres get patronicore patroni-core -o=jsonpath='{range .status.conditions[*]}{.type }{"\t"}{.lastTransitionTime}{"\t"}{.message}{"\n"}{end}'
```

```bash
kubectl -n postgres get patroniservices patroni-services -o=jsonpath='{range .status.conditions[*]}{.type }{"\t"}{.lastTransitionTime}{"\t"}{.message}{"\n"}{end}'
```

The condition fields are:

- `lastTransitionTime` — Status transition timestamp.
- `type` — Possible status values: In progress, Failed, Success.
- `message` — Error description.

## Connect to PostgreSQL

Open a new terminal and port-forward the PostgreSQL leader pod:

```bash
PG_MASTER_POD=$(kubectl get pod -n postgres -o name -l app=patroni,pgtype=primary)
kubectl -n postgres port-forward "${PG_MASTER_POD}" 5432:5432
```

Connect to PostgreSQL using `psql`:

```bash
PGUSER=$(kubectl get secrets -n postgres "postgres-credentials" -o go-template='{{.data.username | base64decode}}') \
PGPASSWORD=$(kubectl get secrets -n postgres "postgres-credentials" -o go-template='{{.data.password | base64decode}}') \
psql -h localhost
```

## Delete a PostgreSQL Cluster

To remove the PostgreSQL cluster, uninstall the Helm releases by name:

```bash
helm uninstall patroni-services
helm uninstall patroni-core
```
