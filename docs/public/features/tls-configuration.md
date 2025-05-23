This chapter describes TLS configuration for the Postgres Service installation.

* [Overview](#overview)
* [Prerequisites](#prerequisites)
* [Configuration](#configuration)
  * [Manual](#manual)
  * [Integration with cert-manager](#integration-with-cert-manager)
* [Disable TLS](#disable-tls)
* [Certificate Update](#certificate-update)
* [Installation Parameters Description](#installation-parameters-description)
  * [#Example](#example)
* [Re-encrypt Route In Openshift Without NGINX Ingress Controller](#re-encrypt-route-in-openshift-without-nginx-ingress-controller)
* [Dbaas adapter configuration](#dbaas-adapter-configuration)

# Overview

To use TLS in Postgres Service it requires some manual steps.

**Note! TLSv1.2 supported only.**

# Prerequisites

* Postgres namespace created.

# Configuration

## Manual 

* Create certificate using `openssl` command. Replace `{{postgres-namespace}}` before.
```shell
openssl req -new -x509 -days 365 -nodes -text -out server.crt -keyout server.key -subj "/CN=pg-patroni.{{postgres-namespace}}.svc.cluster.local"
```

* Create TLS secret in `{{postgres-namespace}}`
```shell
kubectl create secret generic {{certificateSecretName}} --from-file=tls.key=server.key --from-file=tls.crt=server.crt -n {{postgres-namespace}} --kubeconfig ~/.kube/paas-miniha-kubernetes.conf
```
Use `{{certificateSecretName}}` name in TLS Configuration section

## Integration with cert-manager

If cert-manager is deployed on cloud, Postgres Service can provide certificate generation automatically.

Follow the TLS Configuration section and configure `generateCerts` parameters.

# Disable TLS

In case of `enabled` TLS in postgres service you can connect to `pg-patroni` service without ssl configuration.

If by some reason you can not use `ssl` in your services, you can use `sslmode=disable` to avoid connection with ssl.

# Certificate Update

To update certificate you need to follow steps from Postgres TLS certificate update guide.

# Installation Parameters Description

Most of the parameters are described in TLS Configuration section.

## Example 

With such deploy parameters PostgreSQL will be deployed with enabled TLS/SSL, certificates will be issued by Cert Manager with `CLUSTER_ISSUER_NAME` ClusterIssuer.

Also, PostgreSQL DBaaS endpoints will work in HTTPS mode, because `INTERNAL_TLS_ENABLED` install parameter is set to `true`. 

In case of `false` HTTPS will be turned off, but connection between Adapter and PostgreSQL will be established with TLS.

```yaml

tls:
  enabled: true
  generateCerts:
    enabled: true
    clusterIssuerName: <CLUSTER_ISSUER_NAME>

metricCollector:
  install: true
...

patroni:
  install: true
...

backupDaemon:
  install: true
...

dbaas:
  install: true
...
INTERNAL_TLS_ENABLED: true
```

# SSL Configuration using certs as deployment parameters with manually provided Certificates

You can also place your certificates manually in deployment parameters if you don't want to use certificates generated by cert-manager or in cases if you are not using cert-manager.

1) For that you should have the following certificates in BASE64 format :

````
ca.crt: ${ROOT_CA_CERTIFICATE}
tls.crt: ${CERTIFICATE}
tls.key: ${PRIVATE_KEY}

````

Where:


* ${ROOT_CA_CERTIFICATE} is the root CA in BASE64 format.

* ${CERTIFICATE} is the certificate in BASE64 format.

* ${PRIVATE_KEY} is the private key in BASE64 format.

Specify the certificates and other deployment parameters for patroni-core service and postgres-supplementary services like following example :

````yaml
tls:
  enabled: true
  generateCerts:
    clusterIssuerName: ""
    enabled: false
    duration: 365
    subjectAlternativeName:
      additionalDnsNames: []
      additionalIpAddresses: []
  certificates:
    tls_key: LS0tLS1CRUdJT........ 
    tls_crt: LS0tLS1CRUdJTi.......
    ca_crt: LS0tLS1CRUdJTi
   
````

## Re-encrypt Route In Openshift Without NGINX Ingress Controller

Automatic re-encrypt Route creation is not supported out of box, need to perform the following steps:

1. Disable Ingress in deployment parameters: `powaui.ingress.enabled: false`.

   Deploy with enabled Powa UI Ingress leads to incorrect Ingress and Route configuration.

2. Create Route manually. You can use the following template as an example:

   ```yaml
   kind: Route
   apiVersion: route.openshift.io/v1
   metadata:
     annotations:
       route.openshift.io/termination: reencrypt
     name: <specify-uniq-route-name>
     namespace: <specify-namespace-where-postgres-is-installed>
   spec:
     host: <specify-your-target-host-here>
     to:
       kind: Service
       name: powa-ui 
       weight: 100
     port:
       targetPort: web
     tls:
       termination: reencrypt
       destinationCACertificate: <place-CA-certificate-here-from-postgres-server-TLS-secret>
       insecureEdgeTerminationPolicy: Redirect
   ```

**NOTE**: If you can't access the Powa UI host after Route creation because of "too many redirects" error, then one of the possible root
causes is there is HTTP traffic between balancers and the cluster. To resolve that issue it's necessary to add the Route name to
the exception list at the balancers

# Dbaas adapter configuration

In order to configure dbaas-adapter with TLS it's necessary to set `INTERNAL_TLS_ENABLED=true` parameter and configure `tls` parameters section. Dbaas adapter will accepting connections by `https` protocol on `8443` port (will be forwarded on service level). If `dbaas.aggregator.registrationAddress` contains `https` protocol, there is no need to change `dbaas.adapter.address` parameter, as self address will be modified with `https` protocol and `8443` port automatically.