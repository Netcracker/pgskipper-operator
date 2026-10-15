# physical-database-registration

## Purpose

Implement **declarative physical database registration** for **any database adapter** integrated with the Netcracker DBaaS platform (for example, PostgreSQL, MongoDB, or another supported engine).

Follow the **Declarative Physical Database Registration Design** and the current DBaaS API contract. The **DBaaS Operator** reconciles registrations in Aggregator; the database-specific Adapter exposes its descriptor and no longer self-registers when declarative mode is active.

This skill is **database-agnostic**: discover actual repository structure, frameworks, names, capabilities, credentials, namespace, and Helm values. Do **not** assume PostgreSQL, Go, Fiber, Patroni, port 8080, or any specific repository layout.

## When to use

- Adding declarative registration to a database Adapter.
- Implementing an authenticated physical-database descriptor endpoint.
- Creating a `PhysicalDatabase` custom resource (CR) through Helm.
- Granting DBaaS Operator access to a namespaced Adapter credentials Secret.
- Migrating from Adapter self-registration to DBaaS Operator-managed registration.
- Testing Helm CRD discovery, backwards compatibility, RBAC, and registration lifecycle.

## Usage

```text
/physical-database-registration <database-type>
```

Example: `/physical-database-registration mongodb`

**First inspect the target project** and identify:

1. Adapter implementation and HTTP framework.
2. Existing descriptor / registration code and any shared registration library.
3. Helm chart, chart installation condition, values and `values.schema.json`.
4. Adapter Kubernetes Service, port, TLS settings, and reachable address.
5. Adapter Basic Auth credentials Secret, its namespace, and keys.
6. DBaaS Operator namespace and actual ServiceAccount.
7. Actual database type, supported API versions, roles, features, and read-only host semantics.

Never blindly copy implementation-specific file paths or example capabilities.

## 1. Ownership and architecture

### DBaaS platform team

- Owns and installs the shared `PhysicalDatabase` **CRD** (`dbaas.netcracker.com/v1`).
- Defines and validates `PhysicalDatabase.spec` and `status`.
- Implements the **DBaaS Operator** controller for `PhysicalDatabase` CRs.
- Maintains its own RBAC for CR `get/list/watch` and status updates.
- Reads the CR and referenced credentials Secret (subject to namespace RBAC).
- Calls the Adapter descriptor endpoint using the referenced credentials.
- Registers or updates the physical database in **DBaaS Aggregator**.
- Handles retries, conflicts, additional-role migration, and status.

### Database Adapter / integration team

- Exposes the authenticated descriptor endpoint and returns accurate Adapter information.
- Creates a **CR instance**, not a CRD, in its Helm release.
- References its existing Adapter Service URL and Basic Auth Secret.
- Grants only the necessary Secret access to the DBaaS Operator, if that access is not already provided.
- Disables legacy Adapter self-registration while declarative mode is active.
- Keeps legacy registration working when declarative mode is unavailable.

**Do not** implement another `PhysicalDatabase` controller or duplicate the shared CRD in a database-specific operator.

## 2. Registration flow

```text
DBaaS team installs PhysicalDatabase CRD
                  |
                  v
Helm checks CRD API capability at render time
           /                     \
    API available             API unavailable
         |                          |
    Create CR and RBAC          No declarative CR
    Disable legacy             Keep legacy mode
         |
         v
DBaaS Operator reads CR and Secret
         |
         | GET /api/v2/adapter/physical_database + Basic Auth
         v
Database Adapter returns descriptor JSON
         |
         v
DBaaS Operator registers with Aggregator
         |
         v
DBaaS Operator updates CR status
```

The descriptor GET handler **must not call Aggregator or perform registration**. The DBaaS Operator owns that work.

## 3. Adapter descriptor endpoint

### Shared contract

- **Method:** `GET`
- **Path:** `/api/v2/adapter/physical_database`
- **Authentication:** HTTP Basic Auth using the Adapter's **existing** credentials.
- **Success:** `200 OK` with valid JSON.
- **Missing or incorrect credentials:** `401 Unauthorized`.
- **Required data unavailable:** report an appropriate non-2xx response (for example, `503`); do not return a success descriptor with invalid mandatory fields.

**Illustrative descriptor** (replace all example data with real Adapter data):

```json
{
  "physicalDatabaseId": "<namespace>:<physical-db-id>",
  "type": "<database-type>",
  "labels": { "<label-key>": "<label-value>" },
  "apiVersions": {
    "specs": [
      {
        "specRootUrl": "/api",
        "major": 2,
        "minor": 1,
        "supportedMajors": [2]
      }
    ]
  },
  "features": {
    "multiusers": true,
    "tls": false,
    "tlsNotStrict": false
  },
  "supportedRoles": ["<role-1>", "<role-2>"],
  "readOnlyHost": "<read-only-host>"
}
```

| Field | Source / requirement |
|---|---|
| `physicalDatabaseId` | Adapter's configured physical DB identifier |
| `type` | Actual database engine type |
| `labels` | Existing registration metadata; may be empty |
| `apiVersions` | **Actual** supported Adapter API versions |
| `features` | **Actual** feature support, not defaults copied from an example |
| `supportedRoles` | **Actual** supported roles |
| `readOnlyHost` | Contract field; populate according to the engine and agreed schema |

The response field is **`readOnlyHost`**, not the legacy Aggregator field `roHost`. Consult the authoritative contract for field optionality and engine-specific semantics, especially when there is no read-only endpoint.

### Implementation guidance

- Reuse the Adapter's existing server, authentication and configuration facilities.
- Expose the endpoint independently of the legacy registration service.
- Prefer preparing stable descriptor data once at startup, if consistent with runtime behavior.
- Validate required information before returning `200`.
- Avoid hardcoding API versions, hostnames, ports, database roles, or features.
- Add tests for `401`, valid authenticated `200`, response schema, and unavailable fields.

## 4. Helm CRD detection and resource creation

### Capability-driven mode selection

Use Helm's **render-time API discovery** to determine whether the CRD API is available:

```gotemplate
.Capabilities.APIVersions.Has "dbaas.netcracker.com/v1/PhysicalDatabase"
```

Use the existing chart's **Adapter installation** condition as an additional guard. For charts that use `.Values.dbaas.install`, the condition is:

```gotemplate
{{- if and .Values.dbaas.install (.Capabilities.APIVersions.Has "dbaas.netcracker.com/v1/PhysicalDatabase") }}
```

**Adapt `.Values.dbaas.install` to the target chart** rather than assuming this exact value exists.

- **`.Values`** controls user-specified chart configuration, such as whether the Adapter is installed and which DBaaS Operator namespace to use.
- **`.Capabilities`** reports APIs available to Helm during rendering; it is **not** a continuously updated runtime check.
- Do not retain a redundant `declarativeRegistration.enabled` toggle as the mode selector when adopting capability-driven detection, unless project requirements explicitly call for an override.
- Check the CRD with `kubectl get crd physicaldatabases.dbaas.netcracker.com` for actual cluster deployments.
- CRD availability **does not mean** the DBaaS Operator is running, healthy, or ready for migration. Coordinate rollout.
- If the CRD becomes available **after** Helm install, rerun Helm upgrade to select declarative mode.
- If capabilities change or API discovery is temporarily unavailable on a later Helm upgrade, gated resources may be removed from the rendered release. Evaluate this lifecycle risk before depending solely on discovery.

### CR template (adapt values and names)

Create one namespaced CR in the target chart, for example `templates/dbaas/physical-database.yaml`:

```yaml
{{- if and .Values.dbaas.install (.Capabilities.APIVersions.Has "dbaas.netcracker.com/v1/PhysicalDatabase") }}
apiVersion: dbaas.netcracker.com/v1
kind: PhysicalDatabase
metadata:
  name: {{ .Release.Name }}-physical-db
  namespace: {{ .Release.Namespace }}
spec:
  operatorNamespace: {{ required "DBaaS Operator namespace is required" .Values.dbaas.declarativeRegistration.operatorNamespace | quote }}
  adapterAddress: {{ required "Adapter address is required" .Values.dbaas.declarativeRegistration.adapterAddress | quote }}
  credentialsSecretRef:
    name: {{ required "Adapter credentials Secret name is required" .Values.dbaas.declarativeRegistration.credentialsSecretName | quote }}
{{- end }}
```

This is a **generic reference**: prefer deriving `adapterAddress` and `credentialsSecretRef.name` from the chart's **existing Service and Secret settings** rather than introducing duplicate values. The address must be reachable by the DBaaS Operator and must match the actual HTTP(S) scheme and port. Use release-scoped resource names to avoid collisions. Avoid duplicate CR definitions.

`spec.operatorNamespace` is the DBaaS Operator's namespace (according to the CRD contract, matching its `CLOUD_NAMESPACE`), **not necessarily the Adapter namespace**. The Secret reference is resolved in the CR's own namespace. Respect the CRD's immutability constraints.

### Values and schema

Declare only settings that the target chart needs and validate them in `values.schema.json`, for example:

```yaml
dbaas:
  install: false
  declarativeRegistration:
    operatorNamespace: ""
    operatorServiceAccount: ""
    # Optional: use existing Service/Secret settings instead when possible.
    adapterAddress: ""
    credentialsSecretName: ""
```

These are **example chart values**, not required global field names. Their mandatory/non-empty validation should apply when declarative mode is selected. Helm's `required` works for conditional templates. Remove stale values/schema fields only after verifying no consumers depend on them.

## 5. Cross-namespace Secret RBAC

A `credentialsSecretRef` **does not grant permissions**. If the DBaaS Operator lacks access to the Adapter namespace, the Adapter's Helm chart should add a namespaced `Role` and `RoleBinding`.

Use the **same capability and installation guard** as the CR. Generic template example:

```yaml
{{- if and .Values.dbaas.install (.Capabilities.APIVersions.Has "dbaas.netcracker.com/v1/PhysicalDatabase") }}
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ .Release.Name }}-dbaas-credentials-reader
  namespace: {{ .Release.Namespace }}
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames:
      - {{ required "Adapter credentials Secret name is required" .Values.dbaas.declarativeRegistration.credentialsSecretName | quote }}
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ .Release.Name }}-dbaas-credentials-reader
  namespace: {{ .Release.Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ .Release.Name }}-dbaas-credentials-reader
subjects:
  - kind: ServiceAccount
    name: {{ required "DBaaS Operator ServiceAccount is required" .Values.dbaas.declarativeRegistration.operatorServiceAccount | quote }}
    namespace: {{ required "DBaaS Operator namespace is required" .Values.dbaas.declarativeRegistration.operatorNamespace | quote }}
{{- end }}
```

- `Role` and `RoleBinding` live in the **Adapter/CR namespace**.
- The ServiceAccount may live in another namespace; reference its real name and namespace.
- Grant the least privilege required: `get` for the specific Secret if the controller reads it by name. Add permissions only when implementation actually needs them.
- The DBaaS Operator's own `PhysicalDatabase` CR/status permissions remain the **DBaaS team's responsibility**.
- Do not hardcode a universally assumed ServiceAccount name.

## 6. Disable legacy self-registration in declarative mode

**Only one component should own registration** at a time:

| Mode | Adapter registers with Aggregator | DBaaS Operator registers via CR |
|---|---|---|
| Legacy (CRD unavailable) | Yes | No |
| Declarative (CRD available, Operator ready) | No | Yes |

Drive Adapter behavior from **the same Helm capability condition** used for the CR and RBAC. If the Adapter uses an environment flag, a possible generic Deployment fragment is:

```yaml
- name: DBAAS_DECLARATIVE_REGISTRATION_ENABLED
  value: {{ and .Values.dbaas.install (.Capabilities.APIVersions.Has "dbaas.netcracker.com/v1/PhysicalDatabase") | quote }}
```

The variable name above is an **example integration convention**; adapt it to the Adapter's configuration system.

In declarative mode:

1. Do **not** create or start periodic/forced self-registration workers.
2. Do **not** construct an Aggregator registration client solely for the legacy registration path.
3. Keep the Adapter HTTP service, descriptor endpoint, and database-administration routes running.
4. Make shared framework code nil-safe or explicitly conditional: never expose legacy handlers that dereference an absent registration service.
5. Do not arbitrarily force a specific API version or feature set; detect or configure the engine's genuinely supported capabilities.

In legacy mode, preserve existing initialization and registration behavior. Verify the **deployed binary/image includes updated shared-library code**, not just changes in a checkout.

## 7. Lifecycle and failure handling

### Deleting the CR

Deleting a `PhysicalDatabase` CR should stop declarative reconciliation of that resource; it must **not** be interpreted as permission to delete the underlying database or its logical databases. Aggregator registration may remain and become unmanaged; confirm actual deletion, finalizer, and unregister semantics with the DBaaS team instead of assuming automatic cleanup. The Adapter does not automatically resume legacy registration just because an individual CR was deleted. Helm/GitOps can recreate a still-templated CR.

### Legacy HTTP `202` and non-JSON responses

Some legacy registration clients treat **all HTTP 2xx** responses as successful JSON; an Aggregator response such as `202` with plain text `Operation 'internalMigration' is already in progress` can then cause parse errors and crashes through panic/repanic paths.

A safe fix **in the legacy registration client** should:

- Distinguish real registration success from an in-progress/migration response by both status and response content/contract.
- Avoid treating plain text as the expected JSON registration descriptor.
- Record a temporary registration failure or pending state without crashing the Adapter.
- Retry according to a bounded or scheduled policy.
- Preserve error reporting for unrelated failures.

This is **separate from** disabling legacy self-registration in declarative mode. Do not assume that a proposed legacy error-handling fix has already shipped.

## 8. Testing

### Endpoint

```bash
curl -i http://localhost:<port>/api/v2/adapter/physical_database
# Expect 401 if authentication is required and credentials are missing.

curl -i -u "$USER:$PASS" \
  http://localhost:<port>/api/v2/adapter/physical_database
# Expect 200 with valid, engine-accurate JSON.
```

Use the **actual** host, port, TLS scheme and credentials for the target Adapter.

### Helm: capability present

From the target chart's parent directory:

```bash
helm lint ./<chart-path>

helm template test ./<chart-path> \
  --namespace <adapter-namespace> \
  --set <adapter-install-value>=true \
  --api-versions dbaas.netcracker.com/v1/PhysicalDatabase
```

Add values needed for the DBaaS Operator namespace, ServiceAccount and Adapter URL/Secret name. Verify **one CR**, correct `spec`, namespaced Secret RBAC, and the Adapter's **declarative mode enabled**.

### Helm: capability absent

Repeat without `--api-versions` in a local offline render. Verify **no declarative CR or dedicated RBAC** and **legacy registration enabled**. A local `helm template` does not necessarily query the live cluster. `--api-versions` simulates capability; it does **not** install a CRD.

### Real cluster

```bash
kubectl get crd physicaldatabases.dbaas.netcracker.com
kubectl get physicaldatabases -n <adapter-namespace>

kubectl auth can-i get secrets \
  --resource-name=<adapter-credentials-secret> \
  -n <adapter-namespace> \
  --as=system:serviceaccount:<dbaas-operator-namespace>:<dbaas-operator-sa>
```

The impersonation check requires suitable permission to impersonate the ServiceAccount. Verify that the DBaaS Operator can watch the CR, read the Secret, call the descriptor endpoint, register with Aggregator, and update CR status. **Helm rendering alone is not end-to-end validation.**

Test deployment readiness, TLS, duplicate registrations, CR deletion/recreation, retries, and backwards compatibility.

## 9. Implementation checklist

### Adapter

- [ ] Locate the real Adapter configuration, server, and existing registration logic.
- [ ] Implement the authenticated descriptor GET endpoint.
- [ ] Return accurate engine-specific descriptor fields and error codes.
- [ ] Disable legacy registration without disabling normal Adapter routes.
- [ ] Keep shared registration-framework integration nil-safe.
- [ ] Confirm the deployed image includes the changes.

### Helm / RBAC

- [ ] Detect `dbaas.netcracker.com/v1/PhysicalDatabase` using `.Capabilities.APIVersions.Has`.
- [ ] Apply the same selection logic to the CR, Secret RBAC, and Adapter mode.
- [ ] Render exactly one appropriately named `PhysicalDatabase` CR.
- [ ] Reference the real Adapter Service address (HTTP/TLS) and Secret.
- [ ] Read the real DBaaS Operator namespace and ServiceAccount from configuration.
- [ ] Restrict Secret RBAC to least privilege.
- [ ] Update values and JSON schema; remove unused manual mode flags.
- [ ] Verify CRD-present and CRD-absent Helm renders.

### DBaaS integration

- [ ] DBaaS team installed CRD and configured Operator CR/status RBAC.
- [ ] DBaaS Operator is ready before relying on declarative registration.
- [ ] Credentials read, descriptor GET, Aggregator registration, status updates work.
- [ ] Confirm CR deletion/finalizer and migration semantics.
- [ ] Confirm legacy mode remains functional where the CRD is unavailable.

## 10. Non-negotiable design rules

1. **DBaaS owns CRD and reconciliation; the database-specific integration owns its CR and Adapter endpoint.**
2. **Do not assume database type, language, framework, Helm chart layout, port, TLS, or ServiceAccount.**
3. **Use one coherent mode selector** for CR, Secret access, and legacy registration.
4. **CRD availability is not Operator readiness**; coordinate rollout.
5. **A Secret reference does not grant Kubernetes permissions**; provide/verify namespace RBAC.
6. **Never have Operator-managed and Adapter-managed registration competing for the same physical database.**
7. **Descriptor GET must not perform Aggregator registration.**
8. **Deleting a CR must not delete the underlying databases.** Confirm actual Aggregator cleanup semantics.
9. **Successful rendering is not proof of correct runtime behavior.**
10. **Use the latest agreed DBaaS design for exact field validation, API versions, deletion, and role migration semantics.**
