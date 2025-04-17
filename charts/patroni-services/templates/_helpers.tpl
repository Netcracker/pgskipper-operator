{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "helm-chart.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "helm-chart.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "helm-chart.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "restricted.globalPodSecurityContext" -}}
{{- if .Values.GLOBAL_SECURITY_CONTEXT }}
runAsNonRoot: true
seccompProfile:
  type: "RuntimeDefault"
{{- end }}
{{- if and .Values.INFRA_POSTGRES_FS_GROUP .Values.global.cloudIntegrationEnabled }}
runAsUser: {{ .Values.INFRA_POSTGRES_FS_GROUP }}
fsGroup: {{ .Values.INFRA_POSTGRES_FS_GROUP }}
{{- end -}}
{{- end -}}

{{- define "restricted.globalContainerSecurityContext" -}}
{{- if .Values.GLOBAL_SECURITY_CONTEXT }}
allowPrivilegeEscalation: false
capabilities:
  drop: ["ALL"]
{{- end }}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "helm-chart.labels" -}}
helm.sh/chart: {{ include "helm-chart.chart" . }}
{{ include "helm-chart.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "helm-chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "helm-chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Kubernetes labels */}}
{{- define "kubernetes.labels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/name: {{ include "helm-chart.name" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: "postgres-operator"
app.kubernetes.io/part-of: "postgres-operator"
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/technology: "go"
{{- end -}}

{{/* Monitoring Kubernetes labels */}}
{{- define "monitoring.kubernetes.labels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/name: {{ include "helm-chart.name" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: "monitoring"
app.kubernetes.io/part-of: "postgres-operator"
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/technology: "go"
{{- end -}}


{{/*
Create the name of the service account to use
*/}}
{{- define "helm-chart.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "helm-chart.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Vault operator envs
*/}}
{{- define "postgres-operator.vaultEnvs" }}
{{- if or .Values.vaultRegistration.enabled .Values.vaultRegistration.dbEngine.enabled }}
            - name: VAULT_ADDR
              value: {{ default "http://vault-service.vault:8200" .Values.vaultRegistration.url }}
            - name: VAULT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: vault-secret-services
                  key: token
            - name: PAAS_PLATFORM
              value: {{ default "kubernetes" .Values.vaultRegistration.paasPlatform }}
            - name: PAAS_VERSION
              value: {{ default "1.14" .Values.vaultRegistration.paasVersion | quote }}
            - name: OPENSHIFT_SERVER
            {{- if and .Values.CLOUD_PROTOCOL .Values.CLOUD_API_HOST .Values.CLOUD_API_PORT }}
              value: {{ printf "%s://%s:%v" .Values.CLOUD_PROTOCOL .Values.CLOUD_API_HOST .Values.CLOUD_API_PORT }}
            {{- else }}
              value: "https://kubernetes.default:443"
            {{- end }}
{{- else }}
            - name: PAAS_PLATFORM
              value: "kubernetes"
            - name: PAAS_VERSION
              value: "1.14"
            - name: OPENSHIFT_SERVER
              value: "https://kubernetes.default:443"
{{- end }}
{{- end }}

{{/*
Vault env variables for DBaaS
*/}}
{{- define "postgres-dbaas.vaultEnvs" }}
{{- if or .Values.vaultRegistration.enabled .Values.vaultRegistration.dbEngine.enabled }}
            {{- if and .Values.vaultRegistration.enabled ( not .Values.vaultRegistration.dbEngine.enabled) }}
            - name: POSTGRES_ADMIN_PASSWORD
              value: {{ printf "vault:%s/postgres-credentials#password" ( .Values.vaultRegistration.path | default .Release.Namespace ) }}
            - name: POSTGRES_ADMIN_USERNAME
              value: {{ printf "vault:%s/postgres-credentials#username" ( .Values.vaultRegistration.path | default .Release.Namespace ) }}
            {{- else }}
            - name: POSTGRES_ADMIN_PASSWORD
              value: {{ printf "vault:database/static-creds/%s_%s_patroni-sa_postgres#password" .Values.CLOUD_PUBLIC_HOST .Release.Namespace }}
            - name: POSTGRES_ADMIN_USERNAME
              value: {{ printf "vault:database/static-creds/%s_%s_patroni-sa_postgres#username" .Values.CLOUD_PUBLIC_HOST .Release.Namespace }}
            {{- end }}
            - name: VAULT_SKIP_VERIFY
              value: "True"
            - name: VAULT_ADDR
              value: {{ .Values.vaultRegistration.url }}
            - name: VAULT_PATH
              value: {{ printf "%s_%s" .Values.CLOUD_PUBLIC_HOST .Release.Namespace }}
            - name: VAULT_ROLE
              value: {{ .Values.serviceAccount.name }}
            - name: VAULT_IGNORE_MISSING_SECRETS
              value: "True"
            - name: VAULT_ENV_PASSTHROUGH
              value: "VAULT_ADDR,VAULT_ROLE,VAULT_SKIP_VERIFY,VAULT_PATH,VAULT_ENABLED"
{{- else }}
            - name: POSTGRES_ADMIN_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: password
            - name: POSTGRES_ADMIN_USER
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: username
{{- end }}
{{- end }}

{{/*
Vault env variables for DBaaS
*/}}
{{- define "postgres-dbaas.vaultEnvsReg" }}
{{- if .Values.vaultRegistration.enabled }}
            - name: DBAAS_AGGREGATOR_REGISTRATION_USERNAME
              value: {{ printf "vault:%s/dbaas-aggregator-registration-credentials#username" ( .Values.vaultRegistration.path | default .Release.Namespace ) }}
            - name: DBAAS_AGGREGATOR_REGISTRATION_PASSWORD
              value: {{ printf "vault:%s/dbaas-aggregator-registration-credentials#password" ( .Values.vaultRegistration.path | default .Release.Namespace ) }}
{{- else }}
            - name: DBAAS_AGGREGATOR_REGISTRATION_USERNAME
              valueFrom:
                secretKeyRef:
                  name: dbaas-aggregator-registration-credentials
                  key: username
            - name: DBAAS_AGGREGATOR_REGISTRATION_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: dbaas-aggregator-registration-credentials
                  key: password
{{- end }}
{{- end }}

{{- define "find_image" -}}
  {{- $image := .default -}}

  {{ printf "%s" $image }}
{{- end -}}

{{- define "postgres.smServiceAccount" -}}
  {{- if .Values.siteManager.httpAuth.smServiceAccountName -}}
    {{- .Values.siteManager.httpAuth.smServiceAccountName -}}
  {{- else -}}
    {{- if .Values.siteManager.httpAuth.smSecureAuth -}}
      {{- "site-manager-sa" -}}
    {{- else -}}
      {{- "sm-auth-sa" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "postgres-operator.smEnvs" }}
{{- if .Values.siteManager.install }}
{{- if .Values.siteManager.httpAuth.enabled }}
            - name: SM_NAMESPACE
              value: {{ .Values.siteManager.httpAuth.smNamespace }}
            - name: SM_AUTH_SA
              value: {{ include "postgres.smServiceAccount" . }}
            - name: SM_HTTP_AUTH
              value: "true"
            - name: SM_CUSTOM_AUDIENCE
              value: {{ .Values.siteManager.httpAuth.customAudience }}
{{ else }}
            - name: SM_HTTP_AUTH
              value: "false"
{{- end }}
{{- end }}
{{- end }}

{{/*
Content for postgres-exporter-queries config map
*/}}
{{- define "postgres-exporter.queryContentNonFiltered" -}}
{{ .Files.Get "postgres-exporter/postgres-exporter-queries.yaml" }}
{{- $append := true  }}
{{- if .Values.externalDataBase  }}
{{- if eq (lower .Values.externalDataBase.type) "rds" }}
{{- $append = false }}
{{- end }}
{{- end }}
{{- if $append }}
{{ .Files.Get "postgres-exporter/rds-incompatible-queries.yaml" }}
{{- end }}
{{- end -}}

{{/*
Content for query-exporter-queries config map
*/}}
{{- define "query-exporter.queryContent" -}}
{{ .Files.Get "query-exporter/query-exporter-queries.yaml" }}
self-monitoring:
  query-latency-buckets: [{{ default "0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 30, 60" .Values.queryExporter.selfMonitorBuckets }}]
{{- end -}}

{{/*
Content for CloudSQL config labels
*/}}
{{- define "external-database.cloudSQLConfigLabels" -}}
{{- end -}}

{{/*
DNS names used to generate SSL certificate with "Subject Alternative Name" field
*/}}
{{- define "postgres.certDnsNames" -}}
  {{- $dnsNames := list "localhost" -}}
  {{- $dnsNames = concat $dnsNames ( list "dbaas-postgres-adapter" "dbaas-postgres-adapter.svc" (printf "%s.%s" "dbaas-postgres-adapter" .Release.Namespace) (printf "%s.%s.svc" "dbaas-postgres-adapter" .Release.Namespace) ) -}}
  {{- $dnsNames = concat $dnsNames ( list "postgres-backup-daemon" (printf "%s.%s" "postgres-backup-daemon" .Release.Namespace) (printf "%s.%s.svc" "postgres-backup-daemon" .Release.Namespace) ) -}}
  {{- $dnsNames = concat $dnsNames ( list "powa-ui" (printf "%s.%s" "powa-ui" .Release.Namespace) (printf "%s.%s.svc" "powa-ui" .Release.Namespace) ) -}}
  {{- $dnsNames = concat $dnsNames ( list "logical-replication-controller" (printf "%s.%s" "logical-replication-controller" .Release.Namespace) (printf "%s.%s.svc" "logical-replication-controller" .Release.Namespace) ) -}}
  {{- $dnsNames = concat $dnsNames .Values.tls.generateCerts.subjectAlternativeName.additionalDnsNames -}}
  {{- $dnsNames | toYaml -}}
{{- end -}}

{{/*
IP addresses used to generate SSL certificate with "Subject Alternative Name" field
*/}}
{{- define "postgres.certIpAddresses" -}}
  {{- $ipAddresses := list "127.0.0.1" -}}
  {{- $ipAddresses = concat $ipAddresses .Values.tls.generateCerts.subjectAlternativeName.additionalIpAddresses -}}
  {{- $ipAddresses | toYaml -}}
{{- end -}}

{{/*
Get TLS secret name for services
*/}}
{{- define "postgres.certServicesSecret" -}}
{{ printf "%s-services" .Values.tls.certificateSecretName }}
{{- end -}}

{{/*
Postgres host for backup daemon
*/}}
{{- define "backupDaemon.pgHost" -}}
{{- if .Values.connectionPooler.install  -}}
pg-{{ default "patroni" .Values.patroni.clusterName }}-direct
{{- else if .Values.externalDataBase }}
{{- if or (eq (lower .Values.externalDataBase.type) "azure")  (eq (lower .Values.externalDataBase.type) "rds") }}
{{- printf "%s.%s" "pg-patroni" .Release.Namespace }}
{{- else }}
{{- default (printf "%s.%s" "pg-patroni" .Release.Namespace) .Values.backupDaemon.pgHost }}
{{- end }}
{{- else }}
{{- default (printf "%s.%s" "pg-patroni" .Release.Namespace) .Values.backupDaemon.pgHost }}
{{- end }}
{{- end -}}

{{/*
Postgres host for DBaaS adapter
*/}}
{{- define "dbaas.pgHost" -}}
{{- if .Values.externalDataBase }}
{{- if or (eq (lower .Values.externalDataBase.type) "azure")  (eq (lower .Values.externalDataBase.type) "rds") }}
{{- printf "%s.%s" "pg-patroni" .Release.Namespace }}
{{- else }}
{{- default (printf "%s.%s" "pg-patroni" .Release.Namespace) .Values.dbaas.pgHost }}
{{- end }}
{{- else }}
{{- default (printf "%s.%s" "pg-patroni" .Release.Namespace) .Values.dbaas.pgHost }}
{{- end }}
{{- end -}}

{{/*
ReadOnly Postgres host for DBaaS adapter
*/}}
{{- define "dbaas.pgHostRO" -}}
{{- if .Values.externalDataBase }}
{{- if or (eq (lower .Values.externalDataBase.type) "azure")  (eq (lower .Values.externalDataBase.type) "rds") }}
{{- printf "%s.%s" "pg-patroni" .Release.Namespace }}
{{- else }}
{{- default (printf "%s.%s" "pg-patroni-ro" .Release.Namespace) .Values.dbaas.readOnlyHost }}
{{- end }}
{{- else }}
{{- default (printf "%s.%s" "pg-patroni-ro" .Release.Namespace) .Values.dbaas.readOnlyHost }}
{{- end }}
{{- end -}}

{{- define "postgres.storageClassName" -}}
  {{- if and .Values.STORAGE_RWO_CLASS .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.STORAGE_RWO_CLASS | toString }}
  {{- else -}}
    {{- default "" .Values.backupDaemon.storage.storageClass -}}
  {{- end -}}
{{- end -}}


{{- define "postgres.adminPassword" -}}
  {{- if and .Values.INFRA_POSTGRES_ADMIN_PASSWORD .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.INFRA_POSTGRES_ADMIN_PASSWORD | toString }}
  {{- else -}}
    {{- .Values.postgresPassword -}}
  {{- end -}}
{{- end -}}


{{- define "postgres.adminUser" -}}
  {{- if and .Values.INFRA_POSTGRES_ADMIN_USER .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.INFRA_POSTGRES_ADMIN_USER | toString }}
  {{- else -}}
    {{- default "postgres" .Values.postgresUser -}}
  {{- end -}}
{{- end -}}


{{- define "postgres.API_DBAAS_ADDRESS" -}}
  {{- if and .Values.API_DBAAS_ADDRESS  .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.API_DBAAS_ADDRESS | toString }}
  {{- else -}}
    {{- default "http://dbaas-aggregator.dbaas:8080" .Values.dbaas.aggregator.registrationAddress -}}
  {{- end -}}
{{- end -}}

{{- define "postgres.DBAAS_CLUSTER_DBA_CREDENTIALS_USERNAME" -}}
  {{- if and .Values.DBAAS_CLUSTER_DBA_CREDENTIALS_USERNAME .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.DBAAS_CLUSTER_DBA_CREDENTIALS_USERNAME | toString }}
  {{- else -}}
    {{- default "cluster-dba" .Values.dbaas.aggregator.registrationUsername -}}
  {{- end -}}
{{- end -}}

{{- define "postgres.DBAAS_CLUSTER_DBA_CREDENTIALS_PASSWORD" -}}
  {{- if and .Values.DBAAS_CLUSTER_DBA_CREDENTIALS_PASSWORD .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.DBAAS_CLUSTER_DBA_CREDENTIALS_PASSWORD | toString }}
  {{- else -}}
    {{- .Values.dbaas.aggregator.registrationPassword -}}
  {{- end -}}
{{- end -}}

{{- define "monitoring.install" -}}
  {{- if and .Values.MONITORING_ENABLED .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.MONITORING_ENABLED }}
  {{- else -}}
    {{- .Values.metricCollector.install -}}
  {{- end -}}
{{- end -}}

{{- define "supplementary-tests.monitoredImages" -}}
{{- end -}}

{{/*
Init container section for postgres-operator
*/}}
{{- define "postgres-operator.init-container" -}}
{{- end }}

{{/*
Service Name for dbaas adapter
*/}}
{{- define "dbaas.serviceName" -}}
{{ printf "dbaas-postgres-adapter" }}
{{- end -}}

{{/*
Return securityContext for powaUI
*/}}
{{- define "powaUI.securityContext" -}}
  {{- if .Values.powaUI.securityContext -}}
    {{- if not (.Capabilities.APIVersions.Has "security.openshift.io/v1/SecurityContextConstraints") -}}
      {{- toYaml .Values.powaUI.securityContext | nindent 6 }}
    {{- end -}}
  {{- end -}}
{{- end -}}
