# Orchestration Tool Selection

## Overview

This document explains why Helmfile was chosen as the orchestration tool for this PostgreSQL failover test project and evaluates alternative tools.

## Project Context

This is an **ephemeral test infrastructure** project, not a production deployment system. Key characteristics:

- **Lifecycle**: Create → Test → Destroy (ephemeral)
- **Execution context**: Local developer machine
- **State management**: Transient (no long-term state needed)
- **Orchestration needs**: Multi-component with strict ordering
- **Customization**: Environment-specific (OrbStack vs Rancher vs cloud)

### User Workflow Pattern

```bash
# Setup test environment
USE_LOCAL_IMAGES=true helmfile -e orbstack sync

# Run tests
./scripts/trigger-failover.sh

# Cleanup
helmfile destroy
```

## Critical Requirements

1. **Local execution** - No cluster-based controllers needed
2. **Conditional builds** - Based on env vars (USE_LOCAL_IMAGES)
3. **Docker context switching** - OrbStack vs Rancher vs default
4. **Storage auto-configuration** - Before deployment
5. **Strict ordering** - storage → patroni-core → wait → patroni-services → wait → app
6. **Rich validation/status reporting** - Pre/post-deployment hooks
7. **Environment-specific overrides** - storageClass, dockerContext
8. **One-command deployment and teardown**

## Tool Comparison

### 1. Helmfile (Current) - 95/100 ✅ RECOMMENDED

**Why it's the right choice:**
- ✅ Purpose-built for multi-Helm-release orchestration
- ✅ Environment configs are first-class (default.yaml, orbstack.yaml, rancher.yaml)
- ✅ Local execution with pre/post-sync hooks
- ✅ One-command workflows (`helmfile sync`, `helmfile destroy`)
- ✅ Helm release state tracking automatic (upgrade vs install)
- ✅ Dependency management via `needs:` enforces strict ordering

**Minor weaknesses:**
- Bash hooks verbose (inherent to complex orchestration, not a tool issue)

**Verdict:** ✅ Keep Helmfile - architecturally correct choice

---

### 2. Tilt - 85/100 🟡 VIABLE ALTERNATIVE

**What it offers:**
- ✅ Built specifically for local Kubernetes development
- ✅ Excellent build integration (better than Helmfile)
- ✅ Resource dependency graph native
- ✅ Live updates, file watching, port-forwarding built-in

**Why it's overengineered for this use case:**
- ❌ Designed for continuous development (file watching unused)
- ❌ No native environment concept (need separate Tiltfiles)
- ❌ Storage validation requires `local_resource()` workarounds
- ❌ You deploy once, not iteratively develop

**Migration effort:** Medium (~2-3 days)
**Value:** Marginal (10-15% better build UX, but loses environment elegance)

**When to consider:**
- Users frequently rebuild operator images during development
- Build caching/optimization becomes important
- Live update features would be valuable

---

### 3. Taskfile (Task) - 75/100 🟡 SIMPLER BUT LESS POWERFUL

**Strengths:**
- ✅ Clean, readable YAML syntax
- ✅ Excellent for script-heavy workflows
- ✅ Cross-platform (better than Makefile)
- ✅ Simple dependency management (`deps:`)

**Critical gaps:**
- ❌ No Helm release state tracking (manual `--install` vs `--upgrade` logic)
- ❌ Environment configs would be `.env` files (less elegant)
- ❌ Manual Kubernetes waiting (`kubectl wait` in every task)

**Example structure:**
```yaml
tasks:
  sync:
    deps: [storage, build, deploy-core, deploy-services, deploy-app]

  storage:
    cmds: [./scripts/configure-storage.sh --auto]

  build:
    cmds: [make docker-build]
    status: ["test $USE_LOCAL_IMAGES != 'true'"]
```

**Migration effort:** Medium (~1-2 days)
**Value:** Debatable (simpler but more manual)

**When to consider:**
- Team prefers explicit task scripts over Helmfile abstractions
- Helm release state tracking isn't critical
- Comfortable with more manual kubectl commands

---

### 4. ArgoCD / FluxCD - 20/100 ❌ WRONG TOOL CATEGORY

**Fatal architectural mismatches:**
- ❌ Cluster-based controllers (requires installation, adds complexity)
- ❌ GitOps workflow (commit → detect → reconcile adds latency)
- ❌ **Cannot trigger local Docker builds** (dealbreaker)
- ❌ Cannot switch Docker contexts (OrbStack vs Rancher)
- ❌ Storage validation runs locally (ArgoCD runs in-cluster)
- ❌ Teardown requires deleting Applications, not simple `destroy`

**Verdict:** GitOps tools for ephemeral test infrastructure is an anti-pattern

**Why they're wrong:**
- These are production continuous delivery systems
- Require git commits for changes (adds friction)
- Controllers run in-cluster (not local)
- Designed for drift detection/reconciliation (not needed)
- Massive complexity increase for zero benefit

---

### 5. Helm Umbrella Chart + Scripts - 60/100 🟡 AWKWARD SPLIT

**What it would look like:**
```bash
setup.sh  # build logic, storage config
  → helm install test-env ./umbrella-chart -f values-orbstack.yaml
```

**Problems:**
- ❌ Splits orchestration (setup.sh + Helm hooks)
- ❌ Helm hooks run in-cluster (can't do local builds)
- ❌ Sub-chart dependencies loose (no clean waiting)
- ❌ Defeats "one command" goal

---

### 6. Skaffold - 75/100 🟡 BETTER FOR ACTIVE DEVELOPMENT

**What it offers:**
- ✅ Unified build config (Maven/buildpacks and Docker in one file)
- ✅ File watching and auto-rebuild (`skaffold dev`)
- ✅ Less verbose build orchestration
- ✅ Better dev workflow (port-forwarding, log tailing, hot reload)
- ✅ Helm integration with dependencies

**Why it's not ideal for this use case:**
- ❌ Validation hooks less elegant (require external scripts or Jobs)
- ❌ Environment management less clean than helmfile
- ❌ One-time deployment pattern (`helmfile sync`) vs continuous dev (`skaffold dev`)
- ❌ File watching features unused

**Verbosity comparison:**
- Helmfile: ~237 lines (helmfile.yaml.gotmpl + env files)
- Skaffold: ~150-180 lines (but validation logic moves to external scripts)
- **Net reduction: ~5-10%** (minimal benefit)

**When to consider:**
- If users frequently modify Spring Boot code during testing
- If you add a continuous development workflow
- If you eliminate most validation hooks

---

## Strategic Recommendations

### Option 1: Keep Helmfile ✅ **RECOMMENDED**
- Zero migration cost
- Architecturally correct for ephemeral test infrastructure
- Bash verbosity is symptom of complex requirements, not poor tool choice
- Environment management superior to alternatives

### Option 2: Migrate to Tilt (Only if build UX critical)
**ROI:** Marginal (~10% improvement in build experience)

### Option 3: Simplify with Taskfile (If team prefers explicit over magic)
**ROI:** Debatable (trades elegance for explicitness)

## Final Verdict

**Keep Helmfile.** The architecture is well-designed for its purpose. The alternatives either:
- Add massive complexity without benefits (ArgoCD/FluxCD)
- Trade Helm-specific features for marginal gains (Taskfile)
- Provide features you don't need (Tilt's continuous dev, Skaffold's file watching)

The bash hook verbosity is inherent to orchestration requirements (storage config, build logic, validation), not a symptom of wrong tool choice.

## Design Pattern Validation

✅ **Ephemeral test infrastructure pattern correctly implemented**
✅ **Local execution model appropriate for developer tooling**
✅ **Multi-component orchestration with strict dependencies well-handled**
✅ **Environment variability (OrbStack/Rancher/cloud) properly abstracted**
✅ **Build integration (conditional local images) architecturally sound**

**No overengineering detected.** Complexity comes from requirements (3-tier orchestration, validation, builds), not tool choice.

## References

- [Helmfile Documentation](https://helmfile.readthedocs.io/)
- [Tilt Documentation](https://docs.tilt.dev/)
- [Task Documentation](https://taskfile.dev/)
- [ArgoCD Documentation](https://argo-cd.readthedocs.io/)
- [Skaffold Documentation](https://skaffold.dev/)
