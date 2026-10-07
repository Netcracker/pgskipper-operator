# physical-database-registration

Implement PhysicalDatabase CR and adapter endpoint following the "Declarative Physical Database Registration Design" pattern.

## Purpose

Adds declarative physical database registration to a DBaaS adapter and operator following the design document pattern where the operator manages registration instead of adapter self-registration.

## When to use

- Implementing PhysicalDatabase support for any database adapter
- Following the declarative registration pattern from the design doc

## Usage

```
/physical-database-registration <database-type>
```

The skill asks for repository-specific paths if not obvious from context.

## What the design doc specifies

This skill implements the pattern from **"Declarative Physical Database Registration Design (Draft)"**.

### Required components

**1. Adapter GET endpoint**

Endpoint that returns physical database information for the operator to fetch.

**Contract (from design doc "Adapter information endpoint"):**
- **Path:** `/api/v2/adapter/physical_database`
- **Method:** GET
- **Auth:** Basic auth (adapter credentials)
- **Response:**

```json
{
  "physicalDatabaseId": "<adapter-id>",
  "type": "<database-type>",
  "labels": {...},
  "apiVersions": {
    "specs": [{
      "specRootUrl": "/api",
      "major": 2,
      "minor": 1,
      "supportedMajors": [2]
    }]
  },
  "features": {...},
  "supportedRoles": [...],
  "readOnlyHost": "..."
}
```

**Response codes:**
- `200 OK` - Physical database info returned
- `401 Unauthorized` - Invalid credentials
- `404 Not Found` - Endpoint not implemented
- `503 Service Unavailable` - Adapter not ready

**2. PhysicalDatabase CRD**

Kubernetes Custom Resource for declarative registration.

**Spec fields (from design doc "PhysicalDatabase Resource Fields"):**

| Field | Type | Required | Mutable | Validation |
|-------|------|----------|---------|------------|
| `operatorNamespace` | string | Yes | **No** | RFC-1123 label, max 63 chars, immutable |
| `adapterAddress` | string | Yes | Yes | Pattern: `^[^\s:/?#]+://[^\s/?#]+` |
| `credentialsSecretRef.name` | string | Yes | Yes | Secret in same namespace |

**Validation markers:**
```go
// operatorNamespace
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="operatorNamespace is immutable"

// adapterAddress  
// +kubebuilder:validation:Pattern=`^[^\s:/?#]+://[^\s/?#]+`
```

**Status fields (from design doc "Status fields reported by the adapter"):**

| Field | Maps to aggregator field | Description |
|-------|-------------------------|-------------|
| `observedGeneration` | - | Spec generation at last terminal state |
| `physicalDatabaseId` | `{phydbid}` path | Adapter-assigned identifier |
| `conditions[]` | - | Standard Kubernetes conditions |

Additional status fields from adapter response (optional, design doc shows these):
- `type`, `labels`, `supportedRoles`, `features`, `readOnlyHost`, `apiVersions`

## Implementation requirements

### Adapter side

**Endpoint implementation:**
- Pre-build response at startup (all values immutable)
- Protect with basic auth
- Return all required JSON fields
- Use adapter's existing `features`, `supportedRoles` configuration
- Database type is specific to the adapter (e.g., `"postgresql"`, `"mongodb"`)

**Key design decision from doc:**
> Response built once at startup since all values are immutable after adapter starts

### CRD side

**Types:**
- `PhysicalDatabaseSpec` with three required fields
- `PhysicalDatabaseStatus` with `observedGeneration`, `physicalDatabaseId`, `conditions`
- Validation markers as specified in design doc

**After adding types:**
- Run CRD generator (`make generate` or equivalent)
- Verify generated CRD YAML has validation rules

## Design doc quotes

Key behaviors from the design doc:

> **Registration outlives the CR** - deleting the CR stops managing the registration but does not remove it, because a physical database carries logical databases.

> **operatorNamespace** must equal that operator's `CLOUD_NAMESPACE`. Same rule as the seven CRs that already carry this field: an RFC-1123 label within `maxLength: 63`, immutable after creation.

> **adapterAddress** must match `^[^\s:/?#]+://[^\s/?#]+`: a scheme token, `://`, and a non-empty host.

## Example CR (from design doc)

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dbaas-adapter-credentials
  namespace: dbaas-db-adapters
stringData:
  username: "dbaas-aggregator"
  password: "<adapter-password>"
---
apiVersion: dbaas.netcracker.com/v1
kind: PhysicalDatabase
metadata:
  name: postgres-core
  namespace: dbaas-db-adapters
spec:
  operatorNamespace: dbaas-system
  adapterAddress: http://pg-dbaas-adapter.postgres:8080
  credentialsSecretRef:
    name: dbaas-adapter-credentials
```

## What the operator does (context, not implemented by this skill)

The operator reconciler (separate component):
1. Probes adapter: `GET /api/v2/adapter/physical_database`
2. Registers with aggregator: `PUT /api/v3/dbaas/{type}/physical_databases/{phydbid}`
3. Updates CR status based on responses
4. Handles conflicts, errors, and role migration

Response code mapping to condition reasons specified in design doc table.

## Checklist

### Adapter
- [ ] Add response struct types matching design doc JSON format
- [ ] Implement GET handler returning all required fields
- [ ] Pre-build response at startup (immutable values)
- [ ] Register route with basic auth middleware
- [ ] Test endpoint returns 200 with valid JSON

### CRD
- [ ] Add `PhysicalDatabaseSpec` with validation markers
- [ ] Add `PhysicalDatabaseStatus` with required fields
- [ ] Add kubebuilder root markers (`+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`)
- [ ] Run CRD generator
- [ ] Verify immutability rule on `operatorNamespace`
- [ ] Verify URL pattern validation on `adapterAddress`

## Notes

- **Design doc is source of truth** for all field names, patterns, and behavior
- Different repositories may organize code differently - adapt to local structure
- The operator reconciler is a separate component (not part of this skill)
- Field mapping: adapter's `readOnlyHost` becomes aggregator's `metadata.roHost`
