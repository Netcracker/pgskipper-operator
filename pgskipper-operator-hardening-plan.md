# pgskipper-operator Hardening Plan

## Context

The pgskipper-operator runs PostgreSQL clusters on Kubernetes using Patroni. A security review of the operator, its Helm charts, and its companion services reveals a mix of existing hardening (securityContext, capability dropping, non-root UIDs on Go services) alongside significant gaps: no NetworkPolicies, no `readOnlyRootFilesystem`, `automountServiceAccountToken` still enabled, hardcoded default passwords in the backup-daemon image, command-injection risks in Python scripts, SSHd running as root inside Patroni containers, and wide RBAC permissions.

This plan implements all hardening in sequenced steps — ordered by risk impact and implementation complexity — so each step is independently reviewable and shippable.

---

## Current State (Key Findings)

### What's already in place
- `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault` — enabled when `GLOBAL_SECURITY_CONTEXT: true` (default)
- Dedicated ServiceAccounts (`patroni-sa`, `postgres-sa`)
- Resource limits/requests on all components
- Go services (dbaas-adapter, monitoring-agent, query-exporter, replication-controller) use Alpine 3.23 + UID 1001
- Operator binary is statically compiled, multi-stage Alpine-based Dockerfile

### What's missing / broken
| Gap | Severity |
|-----|----------|
| `backup-daemon`: `ENV PGPASSWORD=password` hardcoded | Critical |
| `pg_backup.py`: `shell=True` + unescaped openssl key → command injection | Critical |
| `patroni`: SSHd launched as root inside container | High |
| No `readOnlyRootFilesystem: true` anywhere | High |
| `automountServiceAccountToken` not disabled on ServiceAccounts | High |
| No NetworkPolicy templates in either chart | High |
| RBAC: `secrets` allows `list`/`watch` (full contents disclosure) | High |
| RBAC: `pods/exec` is overly broad | Medium |
| `backup-daemon` and `patroni` use full `ubuntu:22.04` base (large CVE surface) | Medium |
| No `readOnlyRootFilesystem` or ephemeral writable volumes | Medium |
| PodDisruptionBudget disabled by default | Low |
| No startup probes | Low |
| Secret values exposed as env vars (should prefer volume mounts) | Low |

---

## Implementation Steps

---

### Step 1 — Fix critical backup-daemon vulnerabilities ✅ DONE
**Files:** `services/backup-daemon/Dockerfile`, `services/backup-daemon/docker/granular/pg_backup.py`

**Changes:**
1. **Remove hardcoded `PGPASSWORD`**: Delete `ENV PGPASSWORD=password` from Dockerfile line 17. The password must come from a mounted Secret at runtime.
2. **Fix command injection in `pg_backup.py`**: Replace all `subprocess.Popen(..., shell=True)` calls that embed the encryption key or user-supplied data in a shell string with explicit argument lists (`shell=False`). Affected patterns (lines ~243, 288, 393, 425-428): refactor openssl pipeline to use `subprocess.Popen` with a list of arguments and pipe stdout/stdin between processes explicitly — no shell expansion.
3. **Remove `vim` and other debug tools** from the Dockerfile (`apt-get install` list); they have no place in production.

**Verification:** Run `grep -n "shell=True" services/backup-daemon/docker/granular/pg_backup.py` — must return zero results. `docker inspect` the image and confirm `ENV PGPASSWORD` is absent.

---

### Step 2 — Disable `automountServiceAccountToken` on all ServiceAccounts ✅ DONE
**Files:**
- `operator/charts/patroni-core/templates/serviceaccount.yaml`
- `operator/charts/patroni-services/templates/service_account.yaml`
- Corresponding `values.yaml` files

**Changes:**
1. Add `automountServiceAccountToken: false` to both ServiceAccount manifests.
2. For the operator Deployment itself, set `automountServiceAccountToken: true` at pod-spec level (the operator needs the token; disable it by default and re-enable only for the pod that needs it).
3. Add a `serviceAccount.automountToken` value (default `false`) in each chart's `values.yaml` to allow override.

**Why:** By default, Kubernetes injects the SA token into every pod in the namespace. Only the operator pod needs API access; PostgreSQL data pods do not.

**Verification:** `kubectl get pod <postgres-pod> -o yaml | grep automountServiceAccountToken` — must be false or absent (defaulting to SA).

---

### Step 3 — Tighten RBAC permissions ✅ DONE
**Files:**
- `operator/charts/patroni-core/templates/role.yaml`
- `operator/charts/patroni-services/templates/role.yaml`

**Changes:**
1. **Secrets**: Remove `list` and `watch` verbs; keep only `get`, `create`, `update`, `patch`, `delete`. (Listing secrets exposes all secret data to anyone who can read the role bindings.)
2. **`pods/exec`**: Add a conditional — wrap in `{{ if .Values.debug.allowPodExec }}` gated by a `debug.allowPodExec: false` default value. Exec into pods is not needed for normal operation.
3. **Audit remaining verbs**: Confirm `netcracker.com/*` wildcard is truly needed for all sub-resources; if sub-resources can be enumerated, replace `*` with explicit resource names.

**Verification:** `kubectl auth can-i list secrets --as=system:serviceaccount:postgres:patroni-sa` — must return `no`.

---

### Step 4 — Add `readOnlyRootFilesystem` + ephemeral volumes
**Files:**
- `operator/charts/patroni-core/templates/_helpers.tpl` (container securityContext helper)
- `operator/charts/patroni-services/templates/_helpers.tpl`
- `operator/charts/patroni-core/templates/deployment.yaml`
- `operator/charts/patroni-services/templates/deployment.yaml`
- `operator/charts/patroni-services/templates/dbaas/dbaas-adapter-deployment.yaml`
- `operator/build/Dockerfile` (ensure operator binary writes nothing to FS at runtime)

**Changes:**
1. Add `readOnlyRootFilesystem: true` inside the container securityContext helper block in both `_helpers.tpl` files (alongside existing `allowPrivilegeEscalation: false`).
2. In each affected Deployment template, add `emptyDir: {}` volumes for directories the operator writes to at runtime (e.g., `/tmp`, any temp cert path). Mount them as `volumeMounts` with appropriate `subPath`.
3. For the operator binary: verify that `operator/build/bin/user_setup` (which writes `/etc/passwd`) is only run at image build time, not at container start. If it runs at start, convert it to a build-time step.
4. Gate behind `GLOBAL_SECURITY_CONTEXT` so it can be disabled if an environment requires it.

**Verification:** Set `readOnlyRootFilesystem: true` and confirm operator pod starts cleanly; try `kubectl exec -- touch /test` — must fail with "read-only file system".

---

### Step 5 — Add NetworkPolicy templates
**Files (new):**
- `operator/charts/patroni-core/templates/networkpolicy.yaml`
- `operator/charts/patroni-services/templates/networkpolicy.yaml`
**Files (modified):**
- `operator/charts/patroni-core/values.yaml` — add `networkPolicy.enabled: false` (opt-in)
- `operator/charts/patroni-services/values.yaml` — same

**Policies to create (per chart, when enabled):**

*Patroni-Core (operator pod):*
- **Egress allow**: Kubernetes API server (TCP 443/6443) — needed for controller-runtime
- **Egress allow**: DNS in kube-system (UDP 53)
- **Egress allow**: Patroni REST API port 8008 in the managed namespace
- **Ingress allow**: Health probe port 8081 from within cluster
- **Ingress allow**: Operator server port 8080/8443 from within cluster
- **Default deny** all other ingress and egress

*Patroni-Services (operator pod):*
- Same as above, plus:
  - Egress to PostgreSQL port 5432
  - Egress to backup-daemon port 8080/8082

*Postgres data pods (applied by operator via CRD reconciliation — add to operator Go code):*
- Ingress 5432 from application namespaces (label-based)
- Ingress 8008 (Patroni) from within cluster namespace only
- Egress DNS only by default
- Default deny all else

**Default:** `networkPolicy.enabled: false` (avoid breaking existing installs); document that enabling is strongly recommended.

**Verification:** Enable NetworkPolicy, then `kubectl exec` from an unauthorized pod and attempt `curl <postgres-svc>:5432` — must time out.

---

### Step 6 — Harden the Patroni container (remove SSHd from default config)
**Files:** `services/patroni/Dockerfile`, `services/patroni/scripts/start.sh`

**Changes:**
1. **Make SSH optional**: Gate the `sshd` startup block in `start.sh` behind an environment variable `PGBACKREST_SSH_ENABLED` (default unset/false). Only start sshd if pgBackRest cross-host replication is explicitly configured.
2. **Remove openssh-server from Dockerfile** unless `PGBACKREST_PG2_HOST` feature is needed. If it must remain, install it only in a variant image (separate `Dockerfile.ssh` or build arg `ARG ENABLE_SSH=false`).
3. **Drop unused build tools** from final image: `gcc`, `make`, `meson`, `ninja`, `git` are build-time dependencies — move them to a builder stage and copy only compiled artifacts.
4. **Avoid `/etc/passwd` modification at runtime**: Pre-create the postgres user at build time; remove the dynamic `/etc/passwd` write from `start.sh`.

**Verification:** `docker run --rm <patroni-image> ps aux` — `sshd` must NOT appear when `PGBACKREST_SSH_ENABLED` is unset.

---

### Step 7 — Reduce base image attack surface (ubuntu → distroless/alpine where feasible)
**Files:**
- `services/backup-daemon/Dockerfile`
- `services/upgrade/Dockerfile`

**Changes:**
1. **backup-daemon**: Separate the Python build dependencies from the runtime. Use a multi-stage build: builder stage installs `gcc`, `python3.11`, `cython3`, `libpq-dev`; runtime stage is `ubuntu:22.04-minimal` (or `python:3.11-slim`) and only copies the compiled `.so` files and Python sources. Remove `vim`, `manpages-dev`, `build-essential` from runtime stage.
2. **upgrade service**: Already runs as UID 26; audit whether build tools (`protoc`, `alien`) can be confined to a builder stage and removed from the final image.
3. **Go services** (dbaas-adapter, monitoring-agent, query-exporter, replication-controller): Already Alpine-based + UID 1001 — no changes needed.
4. Add Trivy scan step to each service's `Makefile` as a `make scan` target (nice-to-have, non-blocking).

**Verification:** `docker run --rm <new-backup-daemon-image> apt list --installed 2>/dev/null | grep -E "vim|build-essential|gcc"` — must return empty.

---

### Step 8 — Harden secret handling (volumes over env vars where possible)
**Files:**
- `operator/charts/patroni-services/templates/deployment.yaml`
- `operator/charts/patroni-core/templates/deployment.yaml`
- `operator/charts/patroni-services/values.yaml`

**Changes:**
1. For PostgreSQL credentials (`PG_ADMIN_PASSWORD`, `PG_REPL_PASSWORD`) currently injected as env vars from Secrets: add a `secureSecrets.mountAsFiles: false` values toggle. When `true`, mount the Secret as a volume at `/etc/secrets/` with `defaultMode: 0400`, and update the application to read from files instead of environment variables.
2. Set `defaultMode: 0400` on all existing Secret volume mounts (currently unset, which defaults to 0644).
3. Mark static Secrets as `immutable: true` where the values don't change (e.g., initial credentials Secret created at install time).

**Note:** Full migration from env vars to file mounts requires corresponding changes in Go and Python code that read those env vars. Do this incrementally per component; the toggle allows opt-in per environment.

**Verification:** `kubectl exec <operator-pod> -- env | grep PASSWORD` — must return empty when `mountAsFiles: true`.

---

### Step 9 — Enable Pod Disruption Budget by default + add startup probes
**Files:**
- `operator/charts/patroni-core/values.yaml`
- `operator/charts/patroni-services/values.yaml`
- `operator/charts/patroni-core/templates/patroni-pdb.yaml`
- `operator/charts/patroni-services/templates/patroni-pdb.yaml`
- Both deployment templates (add startupProbe)

**Changes:**
1. Change `patroni.applyPodDisruptionBudget` default from `false` to `true` in both `values.yaml` files.
2. Add `startupProbe` to operator containers in both deployment templates (HTTP GET `/healthz` on port 8081, `failureThreshold: 30`, `periodSeconds: 5` — allows 150s for startup before liveness kicks in).

**Verification:** `kubectl get pdb -n postgres` — PDB must exist. Kill a patroni pod and verify minAvailable constraint is respected.

---

### Step 10 — Add Pod Security Standards namespace labels to chart documentation + opt-in enforcement
**Files:**
- `operator/charts/patroni-core/templates/namespace.yaml` (new, optional)
- `operator/charts/patroni-services/templates/namespace.yaml` (new, optional)
- Both `values.yaml` — add `podSecurity.enforce: ""` and `podSecurity.warn: "restricted"` defaults

**Changes:**
1. Add an optional `Namespace` patch template that applies `pod-security.kubernetes.io/enforce` and `pod-security.kubernetes.io/warn` labels when `podSecurity.enforce` is non-empty.
2. Default to `warn: "restricted"` (audit/warn only, no enforcement breakage) so operators see warnings without failing installs.
3. Document in chart README that `enforce: "restricted"` is the hardened target configuration.

**Verification:** Install chart with `podSecurity.warn: restricted` and check `kubectl get events -n postgres | grep PolicyViolation` — should be zero after all other steps are complete.

---

## Critical Files Reference

| File | Steps |
|------|-------|
| `services/backup-daemon/Dockerfile` | 1, 7 |
| `services/backup-daemon/docker/granular/pg_backup.py` | 1 |
| `services/patroni/Dockerfile` | 6, 7 |
| `services/patroni/scripts/start.sh` | 6 |
| `services/upgrade/Dockerfile` | 7 |
| `operator/charts/patroni-core/templates/_helpers.tpl` | 4 |
| `operator/charts/patroni-services/templates/_helpers.tpl` | 4 |
| `operator/charts/patroni-core/templates/serviceaccount.yaml` | 2 |
| `operator/charts/patroni-services/templates/service_account.yaml` | 2 |
| `operator/charts/patroni-core/templates/role.yaml` | 3 |
| `operator/charts/patroni-services/templates/role.yaml` | 3 |
| `operator/charts/patroni-core/templates/deployment.yaml` | 4, 8, 9 |
| `operator/charts/patroni-services/templates/deployment.yaml` | 4, 8, 9 |
| `operator/charts/patroni-services/templates/dbaas/dbaas-adapter-deployment.yaml` | 4 |
| `operator/charts/patroni-core/values.yaml` | 2, 5, 9, 10 |
| `operator/charts/patroni-services/values.yaml` | 2, 5, 8, 9, 10 |
| New: `operator/charts/*/templates/networkpolicy.yaml` | 5 |

---

## Recommended Execution Order

Steps 1–3 are highest risk/impact and should ship first:

1. **Step 1** (backup-daemon critical CVEs) — ship alone, urgent
2. **Steps 2 + 3** (ServiceAccount + RBAC) — can be done in one PR, low breakage risk
3. **Step 4** (readOnlyRootFilesystem) — requires testing each component for FS writes
4. **Step 5** (NetworkPolicy) — opt-in flag, safe to ship but must be validated in test cluster
5. **Step 6** (Patroni SSH) — moderate complexity, requires coordination with pgBackRest config
6. **Steps 7 + 8** (base images + secret mounts) — larger effort, lower urgency
7. **Steps 9 + 10** (PDB + PSS) — polish, low risk

---

## Verification Strategy (end-to-end)

1. After all steps: run `trivy image <each-image>` — confirm no CRITICAL CVEs
2. Run existing Robot Framework tests in `tests/robot/` — all must pass (no regressions)
3. Run `kubectl auth can-i --list --as=system:serviceaccount:postgres:patroni-sa` — verify minimal permission set
4. With NetworkPolicy enabled: attempt cross-namespace connection to PostgreSQL port — must fail
5. With `readOnlyRootFilesystem: true`: all pods must start and pass liveness/readiness probes
6. With `podSecurity.warn: restricted`: `kubectl get events -n postgres | grep PolicyViolation` — zero warnings
