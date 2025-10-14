# Quickstart

This guide will help you get postgres up and running as quickly as possible.

- [Prerequisites](#prerequisites)
- [Installation](#installation)
  * [Storage configuration](#storage-configuration)
  * [Installation patroni-core via Helm](#installation-patroni-core-via-helm)
  * [Installation patroni-services via Helm](#installation-patroni-services-via-helm)
- [Validation of Installation](#validation-of-installation)
- [Connect to the Postgres](#connect-to-the-postgres)
- [Delete a Postgres cluster](#delete-a-postgres-cluster)

# Prerequisites
In order to continue, please, make sure the following utilities are installed on your local machine
* [git](https://git-scm.com/downloads)
* [kubectl](https://kubernetes.io/docs/tasks/tools/)
* [helm](https://helm.sh/docs/intro/install/)

# Installation

Clone the repository and change to the directory
```
git clone git@github.com:Netcracker/pgskipper-operator.git
cd postgres-operator
```
Note that in `charts` folder you can fide two separate `Helm Charts` named `patroni-core` and `patroni-services`  
So for each of `Helm Chart` you can find the sample.yaml  
Information of the services separation you can find in [Architecture Guide](/docs/public/architecture.md#postgres-operator)


## Storage configuration
Before install, configure `patroni.storage` in charts/patroni-core/patroni-core-quickstart-sample.yaml and `backupDaemon.storage` properties in charts/patroni-services/patroni-services-quickstart-sample.yaml
according to your PV configuration.

1. If you have a PV provisioner, set up your storage sections as follows:
```
storage:
  type: provisioned
  size: 2Gi
  storageClass: <storage-class>
```
Where `<storage-class>` is the name of your storageClass.

2. If you have no PV provisioner, point your PVs, and their nodes in `patroni.storage` sections:
```
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
Where `<first-pv>` and `<second-pv>` are the names of your PVs, and `<first-pv-node>`,`<second-pv-node>` are the names of nodes where is located.

For `backupDaemon.storage` section point your PV the same way, but in a single copy. 

## Installation via Helm

When setup is complete, we can proceed to install the postgres operator.

Manually install CRD for Patroni-Core:
```
kubectl apply -f ./charts/patroni-core/crds/netcracker.com_patronicores.yaml
```
Manually install CRD for Postgres-Services
```
kubectl apply -f ./charts/patroni-services/crds/netcracker.com_patroniservices.yaml
```

Install Patroni-Core Operator via Helm by following command:
```
helm install --namespace=postgres --create-namespace -f ./charts/patroni-core/patroni-core-quickstart-sample.yaml patroni-core ./charts/patroni-core
```
This will create all necessary resources and run the Patroni-Core Operator Pod.

To check the patroni-core installation status use the next command:
```
kubectl -n postgres get pods \
  --selector=app=patroni \
  --field-selector=status.phase=Running
```

It should return two patroni pods w/o any restarts
After that wait for the leader promotion, you may determine the leader pod by following command:
```
kubectl -n postgres get pods \
  --selector=pgtype=master \
  --field-selector=status.phase=Running
```

If the Postgres Operator Pod is not in the output of the previous command, check the operator logs:
```
kubectl logs -n postgres "$(kubectl get pod -n postgres -l name=patroni-core-operator --output='name')"
```

After leader has been promoted, you may install Patroni-Services Operator via Helm by following command:
```
helm install --namespace=postgres -f ./charts/patroni-services/patroni-services-quickstart-sample.yaml patroni-services ./charts/patroni-services
```
This will create all necessary resources and run the Postgres-Operator Pod.

To check the postgres-services installation status use the next command:
```
kubectl -n postgres get pods \
  --selector=name=postgres-operator \
  --field-selector=status.phase=Running
```

If the Postgres Operator Pod is not in the output of the previous command, check the operator logs:
```
kubectl logs -n postgres "$(kubectl get pod -n postgres -l name=postgres-operator --output='name')"
```


# Validation of Installation

After successful installation or upgrade, it is necessary to check the state of the PostgreSQL Service Cluster.

Navigate to the target PostgreSQL namespace and check the following:

* All the requested deployments exist. For example, components such as Patroni, PostgreSQL Backup Daemon, PostgreSQL Monitoring Collector, and so on should exist if they are marked for installation.
* Pods with the `pgtype=master` label exist.
* Pods with the `pgtype=replica` label exist.
* All the pods have Ready `1/1` status.

It's also possible to check status of installation via **status.conditions** of Patroni Core and Patroni Services Custom Resources.

Next command will show transition to `Successful` state and time when this transition happened.

```
kubectl -n postgres get patronicore patroni-core -o jsonpath='{.metadata.name}{"\t"}{.status.conditions[?(@.type=="Successful")].lastTransitionTime}'
```

```
kubectl -n postgres get patroniservices patroni-services -o jsonpath='{.metadata.name}{"\t"}{.status.conditions[?(@.type=="Successful")].lastTransitionTime}'
```

Also, it's possible to check all of the conditions and transition times by next command:

```
kubectl -n postgres get patronicore patroni-core -o=jsonpath='{range .status.conditions[*]}{.type }{"\t"}{.lastTransitionTime}{"\t"}{.message}{"\n"}{end}'
```


```
kubectl -n postgres get patroniservices patroni-services -o=jsonpath='{range .status.conditions[*]}{.type }{"\t"}{.lastTransitionTime}{"\t"}{.message}{"\n"}{end}'
```

The following lines describe the operator installation status:


* `lastTransitionTime` - status update timestamp.

* `type` - possible values are: In progress, Failed, Success.

* `message` - error description.

## Connect to the Postgres

Open new terminal and run the following commands for create port forward to the database Pod:
```
PG_MASTER_POD=$(kubectl get pod -n postgres -o name -l app=patroni,pgtype=master)
kubectl -n postgres port-forward "${PG_MASTER_POD}" 5432:5432
```

Now, you can establish connection with Postgres via psql:
```
PGUSER=$(kubectl get secrets -n postgres "postgres-credentials" -o go-template='{{.data.username | base64decode}}') \
PGPASSWORD=$(kubectl get secrets -n postgres "postgres-credentials" -o go-template='{{.data.password | base64decode}}') \
psql -h localhost
```


## Delete a Postgres cluster
To delete the postgres cluster, you need to delete the releases by release name:
```
helm uninstall patroni-services
helm uninstall patroni-core
```
