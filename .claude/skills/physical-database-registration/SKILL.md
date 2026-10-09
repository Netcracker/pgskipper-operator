# physical-database-registration

## Purpose

Implement declarative physical database registration for a DBaaS adapter following **Declarative Physical Database Registration Design (Draft)**.

The DBaaS Operator manages registration with Aggregator instead of relying on the Adapter's legacy self-registration mechanism.

## When to use

- Adding declarative registration support to a database adapter.
- Implementing the `PhysicalDatabase` descriptor endpoint.
- Creating a `PhysicalDatabase` CR through an adapter's Helm chart.
- Migrating from Adapter self-registration to Operator-managed registration.

## Usage

```text
/physical-database-registration <database-type>
```

Example:

```text
/physical-database-registration postgresql
```

If repository structure is unknown, first identify the Adapter implementation, Helm chart, existing Adapter credentials Secret, Adapter Service, and configuration values.

## 1. Architecture and ownership

### DBaaS team responsibilities

The DBaaS team owns:

- `PhysicalDatabase` CRD (`dbaas.netcracker.com/v1`).
- CRD schema and Kubernetes validation rules.
- DBaaS Operator controller and reconciliation.
- Reading the `PhysicalDatabase` CR and credentials Secret.
- Calling the Adapter information endpoint.
- Registering physical databases in Aggregator.
- Handling registration conflicts, errors, retries, and role migration.
- Updating `PhysicalDatabase.status`.

**Do not implement these responsibilities inside the database-specific operator.**

### Database Adapter team responsibilities

The database-specific team owns:

1. Implementing the physical database information endpoint.
2. Protecting that endpoint using existing Adapter Basic Auth credentials.
3. Returning a descriptor matching the contract.
4. Adding a Helm template that creates a `PhysicalDatabase` CR.
5. Adding the corresponding Helm values and JSON schema.
6. Ensuring the CR uses the correct Adapter Service address and credentials Secret.
7. Maintaining backward compatibility with legacy self-registration.

**Do not generate or install a duplicate `PhysicalDatabase` CRD.**

The CRD must be installed by the DBaaS component responsible for owning the shared API.

## 2. Registration flow

```text
Database Helm Chart
        |
        | Creates PhysicalDatabase CR
        v
DBaaS Operator
        |
        | Reads spec.adapterAddress
        | Reads spec.credentialsSecretRef
        |
        | GET /api/v2/adapter/physical_database
        | Basic Auth
        v
Database Adapter
        |
        | 200 OK + PhysicalDatabaseInformation
        v
DBaaS Operator
        |
        | PUT /api/v3/dbaas/{type}/physical_databases/{phydbid}
        | ?internalMigration=true
        v
DBaaS Aggregator
        |
        | Registration result
        v
DBaaS Operator
        |
        | Updates PhysicalDatabase.status
```

The Adapter does not perform Aggregator registration as part of the new endpoint.

## 3. Adapter GET endpoint

### Contract

**Method:** `GET`

**Path:**

```text
/api/v2/adapter/physical_database
```

**Authentication:** HTTP Basic Auth, using the Adapter's existing credentials.

**Successful response:** `200 OK`, `Content-Type: application/json`.

Example:

```json
{
  "physicalDatabaseId": "postgres-nuye:postgres",
  "type": "postgresql",
  "labels": {
    "clusterName": "patroni"
  },
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
  "supportedRoles": [
    "admin",
    "streaming",
    "rw",
    "ro"
  ],
  "readOnlyHost": "pg-patroni-ro.postgres-nuye"
}
```

### Response fields

| Field | Required | Description |
|---|---|---|
| `physicalDatabaseId` | Yes | Adapter-assigned physical database identifier |
| `type` | Yes | Database type, e.g. `postgresql` |
| `labels` | Optional | Physical database metadata |
| `apiVersions` | Yes | Supported Adapter API version information |
| `features` | Yes | Adapter capabilities, including `multiusers` |
| `supportedRoles` | Yes | Roles supported by the Adapter |
| `readOnlyHost` | Yes | Read-only connection host |

The field `readOnlyHost` maps to the Aggregator registration metadata field `roHost`. Do not rename the JSON field exposed by this endpoint.

The version numbers, features, and supported roles must accurately describe the running Adapter. Do not copy example values if the Adapter supports different versions or capabilities.

### Response codes

| HTTP code | Meaning |
|---|---|
| `200 OK` | Descriptor successfully returned |
| `401 Unauthorized` | Credentials missing or invalid |
| `404 Not Found` | Endpoint not implemented or unavailable on this Adapter version |
| `503 Service Unavailable` | Adapter temporarily cannot provide a valid descriptor |

A missing or invalid required descriptor field must not be presented as a successful, valid registration descriptor.

### Implementation requirements

- Add response types that match the JSON contract.
- Register the GET endpoint in the existing Adapter HTTP server.
- Use the existing Adapter Basic Auth mechanism and credentials.
- Reuse the Adapter's configured physical database ID, labels, features, roles, and read-only host.
- Prefer building the immutable descriptor once during Adapter startup and reusing it for requests.
- Validate required data before returning `200`.
- Return `503` when required information is temporarily unavailable.
- Do not add Aggregator calls to this endpoint.

For the PostgreSQL implementation, the existing Fiber HTTP server and `ServiceAdapter` should be reused.

## 4. PhysicalDatabase CR — Helm side

The database-specific team creates a **CR instance**, not the CRD.

The required resource API is:

```yaml
apiVersion: dbaas.netcracker.com/v1
kind: PhysicalDatabase
```

### Required CR fields

| Field | Description |
|---|---|
| `spec.operatorNamespace` | Namespace of the responsible DBaaS Operator; must match its `CLOUD_NAMESPACE` |
| `spec.adapterAddress` | Reachable HTTP(S) base URL of the Adapter |
| `spec.credentialsSecretRef.name` | Name of the Adapter Basic Auth Secret, in the CR's namespace |

`operatorNamespace` is immutable according to the CRD contract. The DBaaS team's CRD is responsible for enforcing its validation.

### Example CR

```yaml
apiVersion: dbaas.netcracker.com/v1
kind: PhysicalDatabase
metadata:
  name: postgres-core
  namespace: postgres-nuye
spec:
  operatorNamespace: dbaas
  adapterAddress: http://dbaas-postgres-adapter.postgres-nuye:8080
  credentialsSecretRef:
    name: dbaas-adapter-credentials
```

The namespace and address above are examples, not universal defaults.

### Helm implementation

Create the CR template alongside the existing Adapter Helm templates.

For PostgreSQL:

```text
charts/patroni-services/templates/dbaas/physical-database.yaml
```

Recommended Helm configuration:

```yaml
dbaas:
  declarativeRegistration:
    enabled: false
    operatorNamespace: ""
    adapterAddress: ""
```

Use `enabled: false` by default so existing environments without the new CRD continue installing successfully.

The Helm template should:

- Render only when the Adapter is installed and declarative registration is enabled.
- Generate a unique, release-specific resource name.
- Use `.Release.Namespace` for the CR namespace.
- Obtain `operatorNamespace` from Helm values.
- Use the actual deployed Adapter Service URL.
- Reuse the existing Adapter credentials Secret.
- Support HTTP and HTTPS, including the correct Service port.
- Avoid hardcoded namespaces and environment-specific hostnames.
- Avoid rendering duplicate `PhysicalDatabase` resources.

Prefer reusing the existing Adapter address configuration. If that value is unsuitable, expose an explicit registration address setting and validate that it is provided when enabled.

### JSON schema

Update the chart's `values.schema.json` to define any added settings.

For example, under `$defs.dbaas.properties`:

```json
"declarativeRegistration": {
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "enabled": {
      "type": "boolean",
      "default": false
    },
    "operatorNamespace": {
      "type": "string"
    },
    "adapterAddress": {
      "type": "string"
    }
  }
}
```

Use Helm `required` or equivalent validation for fields that must be non-empty when declarative registration is enabled.

## 5. Backward compatibility

Legacy adapters may already register themselves directly:

```text
Adapter -> Aggregator
```

Declarative registration introduces:

```text
DBaaS Operator -> Aggregator
```

Do not blindly remove legacy self-registration because older installations may still require it.

Implement or coordinate an explicit mechanism to disable legacy registration when declarative registration is active.

Recommended behavior:

| Mode | Adapter self-registration | PhysicalDatabase CR |
|---|---|---|
| Legacy | Enabled | Not created |
| Declarative | Disabled | Created |

Avoid two components repeatedly updating the same registration in Aggregator.

Do not remove existing self-registration until the migration and compatibility strategy is agreed with the DBaaS team.

## 6. Testing

### Adapter endpoint

Verify unauthenticated access:

```bash
curl -i http://localhost:8080/api/v2/adapter/physical_database
```

Expected: `401 Unauthorized`.

Verify authenticated access:

```bash
curl -s -u "$USER:$PASS" \
  http://localhost:8080/api/v2/adapter/physical_database | jq
```

Expected: `200 OK` with a valid descriptor.

Also test invalid credentials and temporary unavailability where applicable.

### Helm chart

Run:

```bash
helm lint ./charts/patroni-services
```

Enable rendering:

```bash
helm template test ./charts/patroni-services \
  --namespace postgres-nuye \
  --set dbaas.install=true \
  --set dbaas.declarativeRegistration.enabled=true \
  --set dbaas.declarativeRegistration.operatorNamespace=dbaas \
  --set dbaas.declarativeRegistration.adapterAddress=http://dbaas-postgres-adapter.postgres-nuye:8080
```

Adapt the commands to the target repository's actual Helm values.

Verify:

- Exactly one CR is rendered when enabled.
- No CR is rendered when disabled.
- The CR contains `apiVersion: dbaas.netcracker.com/v1`.
- The Secret reference, namespace, and Adapter address are correct.
- Invalid Helm value types fail JSON schema validation.
- HTTP and HTTPS configurations render correctly.

### Kubernetes integration

Check whether the DBaaS team's CRD exists:

```bash
kubectl get crd physicaldatabases.dbaas.netcracker.com
```

If it is absent, do not enable CR creation during a real Helm installation.

Once the CRD and DBaaS Operator are available:

1. Install the PostgreSQL Helm chart with declarative registration enabled.
2. Verify that the `PhysicalDatabase` CR was created.
3. Verify that the DBaaS Operator calls the Adapter endpoint with Basic Auth.
4. Verify that Aggregator registration succeeds.
5. Verify that the DBaaS Operator updates CR status.

Do not treat successful Helm rendering as proof of end-to-end registration.

## 7. Implementation checklist

### Adapter

- [ ] Response structs match the contract.
- [ ] Descriptor is prepared using real Adapter configuration.
- [ ] GET route is registered.
- [ ] Basic Auth uses existing Adapter credentials.
- [ ] Valid request returns `200` and correct JSON.
- [ ] Invalid credentials return `401`.
- [ ] Unavailable required information is handled.
- [ ] Legacy self-registration compatibility is addressed.

### Helm

- [ ] `PhysicalDatabase` CR template added.
- [ ] Correct API group: `dbaas.netcracker.com/v1`.
- [ ] Helm values added.
- [ ] `values.schema.json` updated.
- [ ] CR creation disabled by default.
- [ ] Adapter address works with HTTP and HTTPS.
- [ ] Credentials Secret is referenced correctly.
- [ ] `helm lint` succeeds.
- [ ] Enabled and disabled rendering tested.
- [ ] Exactly one CR is generated per intended physical DB.

### DBaaS team dependencies

- [ ] DBaaS team provides and installs the CRD.
- [ ] DBaaS Operator watches the correct API group.
- [ ] `operatorNamespace` value is confirmed.
- [ ] Adapter descriptor contract is agreed.
- [ ] Migration from legacy self-registration is coordinated.
- [ ] End-to-end integration test succeeds.

## 8. Important design rules

- **CRD ownership:** DBaaS team.
- **CR creation:** Database Adapter team, through Helm.
- **Adapter descriptor endpoint:** Database Adapter team.
- **Registration reconciliation:** DBaaS Operator team.
- **Aggregator behavior and role migration:** DBaaS components.
- **Deleting the CR does not delete the registered physical database**; it stops declarative management.
- **The design document is the source of truth** for CR fields, API contracts, and registration behavior.
- Do not introduce an additional `PhysicalDatabase` controller into the database-specific operator.