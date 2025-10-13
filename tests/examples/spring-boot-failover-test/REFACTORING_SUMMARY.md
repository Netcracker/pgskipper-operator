# Helmfile Configuration Refactoring Summary

## Changes Made

### 1. Separated Concerns

**Before:** Environments controlled both deployment target AND image selection
**After:**
- **Environments** = WHERE to deploy (OrbStack, Rancher, Cloud)
- **Environment Variables** = WHAT to deploy (image repos, tags, local builds)

### 2. Environment Files Simplified

**Before (`environments/local.yaml`):**
```yaml
storageClass: local-path
pgskipperOperator:
  useLocalBuild: true
  coreImage:
    repository: pgskipper-operator
    tag: local
  pullPolicy: Never
```

**After (`environments/local.yaml`):**
```yaml
# OrbStack environment - for local development with OrbStack
storageClass: local-path
dockerContext: orbstack
```

Environment files now only contain deployment-target-specific configuration.

### 3. Image Configuration via Environment Variables

Added support for environment variables to control image selection:

```bash
USE_LOCAL_IMAGES=true          # Build locally vs use registry
PGSKIPPER_IMAGE=<registry>     # Override image repository
PGSKIPPER_TAG=<tag>            # Override image tag
```

### 4. helmfile.yaml.gotmpl Changes

- Added variable parsing from environment
- Image configuration logic at the top of the file
- Hooks now respect `USE_LOCAL_IMAGES` environment variable
- Automatic Docker context switching based on environment

### 5. New Files Created

1. **`.envrc.example`** - Example direnv configuration
2. **`DEPLOYMENT.md`** - Comprehensive deployment guide
3. **`REFACTORING_SUMMARY.md`** (this file) - Summary of changes
4. **`environments/rancher.yaml`** - Rancher Desktop environment

### 6. Updated .gitignore

Added `.envrc` to prevent committing personal environment settings.

## Migration Guide

### Old Usage → New Usage

| Old Command | New Command |
|------------|-------------|
| `helmfile -e orbstack sync` | `USE_LOCAL_IMAGES=true helmfile -e orbstack sync` |
| `helmfile sync` | `helmfile sync` (no change) |

### Breaking Changes

**Before:** `helmfile -e orbstack sync` automatically built local images

**After:** Need to explicitly set `USE_LOCAL_IMAGES=true`

**Workaround:** Use direnv with `.envrc`:
```bash
export USE_LOCAL_IMAGES=true
```

Then `helmfile -e orbstack sync` works as before.

## Benefits

### 1. Flexibility
```bash
# Same environment, different images
helmfile -e orbstack sync                      # Official images on OrbStack
USE_LOCAL_IMAGES=true helmfile -e orbstack sync   # Local images on OrbStack
PGSKIPPER_TAG=v1.2.3 helmfile -e orbstack sync    # Specific version on OrbStack
```

### 2. Clarity
```bash
# Environment = deployment target
-e local     # Deploy to OrbStack
-e rancher   # Deploy to Rancher Desktop
(default)    # Deploy to cloud/remote

# Variables = what to deploy
USE_LOCAL_IMAGES=true    # Local build
PGSKIPPER_TAG=v1.2.3     # Specific version
```

### 3. Extensibility

Easy to add new environments without duplicating image configuration:

```yaml
# environments/kind.yaml
storageClass: standard
dockerContext: kind-kind
```

All image options (local build, specific versions, custom registries) automatically work with new environment.

### 4. Composability

Mix and match environments and image configurations:

```bash
# Test matrix
for env in local rancher kind; do
  for tag in v1.2.3 v1.2.4 main; do
    PGSKIPPER_TAG=$tag helmfile -e $env sync
  done
done
```

## Usage Examples

### Local Development Workflow

**With direnv:**
```bash
# .envrc
export USE_LOCAL_IMAGES=true

# Now simply:
helmfile -e orbstack sync
```

**Without direnv:**
```bash
USE_LOCAL_IMAGES=true helmfile -e orbstack sync
```

### CI/CD Pipeline

```bash
# Deploy specific version to staging (OrbStack)
PGSKIPPER_TAG=${CI_COMMIT_TAG} helmfile -e orbstack sync

# Deploy to production (cloud)
PGSKIPPER_TAG=${CI_COMMIT_TAG} helmfile sync
```

### Testing

```bash
# Test local build on multiple environments
for env in local rancher kind; do
  USE_LOCAL_IMAGES=true helmfile -e $env sync
  ./scripts/run-tests.sh
  helmfile -e $env destroy
done
```

## Files Changed

1. ✏️ `helmfile.yaml.gotmpl` - Added env var support, refactored image config
2. ✏️ `environments/default.yaml` - Removed image config, kept deployment config
3. ✏️ `environments/local.yaml` - Removed image config, kept deployment config
4. ✏️ `environments/rancher.yaml` - Created (deployment config only)
5. ✏️ `.gitignore` - Added `.envrc`
6. ➕ `.envrc.example` - Created
7. ➕ `DEPLOYMENT.md` - Created
8. ➕ `REFACTORING_SUMMARY.md` - Created

## Backward Compatibility

The refactoring maintains backward compatibility with one exception:

**Removed:** Auto-detection of local environment triggering local build

**Reason:** Mixing deployment target with image selection creates inflexibility

**Mitigation:** Use direnv or set `USE_LOCAL_IMAGES=true` explicitly

## Next Steps

1. Review `DEPLOYMENT.md` for complete usage guide
2. Copy `.envrc.example` to `.envrc` if using direnv
3. Update CI/CD pipelines to use environment variables
4. Consider adding more environments (kind, k3d, etc.)
