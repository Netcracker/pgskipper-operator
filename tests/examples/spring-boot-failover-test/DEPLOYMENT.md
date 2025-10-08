# Deployment Configuration Guide

This guide explains how to configure deployments using helmfile environments and environment variables.

## Architecture

The deployment configuration separates two concerns:

1. **Environments** (`-e` flag) - Define **WHERE** to deploy (deployment target)
2. **Environment Variables** - Define **WHAT** to deploy (image selection)

## Environments

Environments configure the deployment target (local vs cloud, storage, Docker context).

### Available Environments

| Environment | Description | Storage Class | Docker Context |
|------------|-------------|---------------|----------------|
| `default` | Cloud/remote Kubernetes | `standard` | `default` |
| `local` | OrbStack (macOS) | `local-path` | `orbstack` |
| `rancher` | Rancher Desktop | `local-path` | `rancher-desktop` |

### Usage

```bash
# Deploy to default (cloud/remote)
helmfile sync

# Deploy to OrbStack
helmfile -e orbstack sync

# Deploy to Rancher Desktop
helmfile -e rancher sync
```

## Image Configuration

Image configuration is controlled by environment variables, allowing flexible image selection regardless of deployment target.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `USE_LOCAL_IMAGES` | Build and use local images | `false` |
| `PGSKIPPER_IMAGE` | Operator image repository | `ghcr.io/netcracker/pgskipper-operator` |
| `PGSKIPPER_TAG` | Operator image tag | `main` |
| `NAMESPACE` | PostgreSQL namespace | `postgres` |
| `APP_NAMESPACE` | Application namespace | `default` |

### Usage Examples

#### 1. Official Images (Default)

Deploy using official images from GitHub Container Registry:

```bash
# To default environment (cloud)
helmfile sync

# To OrbStack
helmfile -e orbstack sync

# To Rancher Desktop
helmfile -e rancher sync
```

#### 2. Local Development Build

Build operator images locally and deploy:

```bash
# To OrbStack with local build
USE_LOCAL_IMAGES=true helmfile -e orbstack sync

# To Rancher Desktop with local build
USE_LOCAL_IMAGES=true helmfile -e rancher sync
```

**What happens:**
1. Switches to appropriate Docker context (`orbstack` or `rancher-desktop`)
2. Builds `pgskipper-operator:local` image locally
3. Deploys with `imagePullPolicy: Never` (uses local image)

#### 3. Specific Version

Deploy a specific version from the registry:

```bash
# Deploy v1.2.3 to OrbStack
PGSKIPPER_TAG=v1.2.3 helmfile -e orbstack sync

# Deploy v1.2.3 to cloud
PGSKIPPER_TAG=v1.2.3 helmfile sync
```

#### 4. Custom Registry

Use images from a custom registry:

```bash
# Deploy from custom registry to OrbStack
PGSKIPPER_IMAGE=myregistry.io/pgskipper PGSKIPPER_TAG=custom helmfile -e orbstack sync

# Deploy from custom registry to cloud
PGSKIPPER_IMAGE=myregistry.io/pgskipper PGSKIPPER_TAG=latest helmfile sync
```

#### 5. Custom Namespaces

Override default namespaces:

```bash
# Deploy to custom namespaces on OrbStack
NAMESPACE=my-postgres APP_NAMESPACE=my-app helmfile -e orbstack sync
```

## Using direnv (Recommended)

For frequent local development, use [direnv](https://direnv.net/) to automatically set environment variables:

```bash
# Install direnv
brew install direnv

# Copy example configuration
cp .envrc.example .envrc

# Edit .envrc with your preferences
vim .envrc

# Allow direnv to load the file
direnv allow
```

Example `.envrc` for local development:

```bash
# .envrc
export USE_LOCAL_IMAGES=true
export NAMESPACE=postgres
export APP_NAMESPACE=default
```

Now simply:

```bash
cd postgresql-stability  # direnv automatically loads .envrc
helmfile -e orbstack sync   # Uses local images automatically
```

## Common Workflows

### Workflow 1: Local Development Cycle

```bash
# Initial setup with local images
USE_LOCAL_IMAGES=true helmfile -e orbstack sync

# Make changes to operator code
cd ../../../
vim pkg/reconciler/patroni.go

# Rebuild and redeploy
cd tests/examples/spring-boot-failover-test
USE_LOCAL_IMAGES=true helmfile -e orbstack sync
```

### Workflow 2: Test Specific Version Locally

```bash
# Test version v1.2.3 on OrbStack before production deployment
PGSKIPPER_TAG=v1.2.3 helmfile -e orbstack sync

# Verify it works, then deploy to production
PGSKIPPER_TAG=v1.2.3 helmfile sync
```

### Workflow 3: Multi-Environment Testing

```bash
# Test local build on OrbStack
USE_LOCAL_IMAGES=true helmfile -e orbstack sync

# Test same build on Rancher Desktop
USE_LOCAL_IMAGES=true helmfile -e rancher sync

# Deploy official image to cloud
helmfile sync
```

## Adding New Environments

To add a new environment (e.g., `kind`):

1. Create `environments/kind.yaml`:

```yaml
# Kind environment - for local development with kind
storageClass: standard
dockerContext: kind-kind
```

2. Register in `helmfile.yaml.gotmpl`:

```yaml
environments:
  # ... existing environments ...
  kind:
    values:
      - environments/kind.yaml
```

3. Use it:

```bash
USE_LOCAL_IMAGES=true helmfile -e kind sync
```

## Troubleshooting

### Issue: Images not found in local Kubernetes

**Symptom:** `ImagePullBackOff` with local images

**Solution:** Verify Docker context matches environment:

```bash
# Check current context
docker context ls

# Should match environment's dockerContext setting
# OrbStack: orbstack
# Rancher: rancher-desktop
# Kind: kind-kind

# Switch if needed
docker context use orbstack
```

### Issue: Wrong image version deployed

**Symptom:** Deployed version doesn't match expectation

**Solution:** Check environment variables:

```bash
# Show current configuration
env | grep -E '(PGSKIPPER|USE_LOCAL)'

# Clear variables if needed
unset USE_LOCAL_IMAGES PGSKIPPER_TAG PGSKIPPER_IMAGE

# Redeploy
helmfile sync
```

### Issue: Build fails with "context not found"

**Symptom:** Docker build fails with context error

**Solution:** Ensure Docker context exists:

```bash
# List available contexts
docker context ls

# Create missing context (example for Rancher)
docker context create rancher-desktop
```

## Best Practices

1. **Use environments for infrastructure** - Storage classes, Docker contexts, cluster-specific settings
2. **Use env vars for images** - Image repositories, tags, build flags
3. **Use direnv for local dev** - Automate common settings
4. **Version control environment files** - Commit to git
5. **Don't version .envrc** - Add to `.gitignore` (personal settings)

## Summary

```bash
# Quick reference
helmfile sync                              # Official images → default (cloud)
helmfile -e orbstack sync                     # Official images → OrbStack
USE_LOCAL_IMAGES=true helmfile -e orbstack sync  # Local build → OrbStack
PGSKIPPER_TAG=v1.2.3 helmfile sync         # Specific version → cloud
```
