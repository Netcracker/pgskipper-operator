{{/*
Expand the name of the chart.
*/}}
{{- define "patroni-core.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "patroni-core.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "patroni-core.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "patroni-core.labels" -}}
helm.sh/chart: {{ include "patroni-core.chart" . }}
{{ include "patroni-core.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "patroni-core.selectorLabels" -}}
app.kubernetes.io/name: {{ include "patroni-core.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Kubernetes labels */}}
{{- define "kubernetes.labels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/name: {{ include "patroni-core.name" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: "operator"
app.kubernetes.io/part-of: "postgres"
app.kubernetes.io/managed-by: "Helm"
app.kubernetes.io/technology: "go"
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "patroni-core.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "patroni-core.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

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

{{- define "patroni-core-operator.vaultEnvs" }}
{{- if or .Values.vaultRegistration.enabled .Values.vaultRegistration.dbEngine.enabled }}
            - name: VAULT_ADDR
              value: {{ default "http://vault-service.vault:8200" .Values.vaultRegistration.url }}
            - name: VAULT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: vault-secret
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

{{- define "find_image" -}}
  {{- $image := .default -}}
  
  {{ printf "%s" $image }}
{{- end -}}


{{/*
DNS names used to generate SSL certificate with "Subject Alternative Name" field
*/}}
{{- define "postgres.certDnsNames" -}}
  {{- $dnsNames := list "localhost" "pg-patroni" (printf "%s.%s" "pg-patroni" .Release.Namespace)  (printf "%s.%s.svc" "pg-patroni" .Release.Namespace) -}}
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


{{- define "postgres.storageClassName" -}}
  {{- if and .Values.STORAGE_RWO_CLASS .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.STORAGE_RWO_CLASS | toString }}
  {{- else -}}
    {{- default "" .Values.patroni.storage.storageClass -}}
  {{- end -}}
{{- end -}}


{{- define "postgres.replicasCount" -}}
  {{- if and .Values.INFRA_POSTGRES_REPLICAS .Values.global.cloudIntegrationEnabled -}}
    {{- .Values.INFRA_POSTGRES_REPLICAS | toString }}
  {{- else -}}
    {{- default "2" .Values.patroni.replicas -}}
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

{{/*
Init container section for postgres-operator
*/}}
{{- define "patroni-core-operator.init-container" -}}
{{- end }}

{{- define "patroni-tests.monitoredImages" -}}
{{- end -}}