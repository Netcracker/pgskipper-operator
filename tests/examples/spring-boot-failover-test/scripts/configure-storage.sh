#!/bin/bash

# Storage Configuration Script for pgskipper-operator
# This script configures the Kubernetes cluster storage for pgskipper
#
# Usage:
#   ./configure-storage.sh           # Interactive mode
#   ./configure-storage.sh --auto    # Automatic mode (no prompts)

set -e

# Parse arguments
AUTO_MODE=false
if [[ "$1" == "--auto" ]]; then
    AUTO_MODE=true
fi

if [ "$AUTO_MODE" = false ]; then
    echo "===================================="
    echo "pgskipper Storage Configuration"
    echo "===================================="
    echo ""
fi

# Check Kubernetes connectivity
command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl is not installed" >&2; exit 1; }
kubectl cluster-info >/dev/null 2>&1 || { echo "Error: Cannot connect to Kubernetes cluster" >&2; exit 1; }

if [ "$AUTO_MODE" = false ]; then
    echo "Current Storage Classes:"
    kubectl get storageclass
    echo ""
fi

# Check if there's a default storage class
DEFAULT_SC=$(kubectl get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}')

if [ -n "$DEFAULT_SC" ]; then
    if [ "$AUTO_MODE" = false ]; then
        echo "✓ Default storage class found: ${DEFAULT_SC}"
        echo ""
        echo "No action needed. pgskipper will use the default storage class."
    fi
    exit 0
fi

if [ "$AUTO_MODE" = false ]; then
    echo "⚠ No default storage class configured"
    echo ""
fi

# Get available storage classes
STORAGE_CLASSES=$(kubectl get storageclass -o jsonpath='{.items[*].metadata.name}')

if [ -z "$STORAGE_CLASSES" ]; then
    echo "Error: No storage classes available in the cluster"
    echo ""
    echo "Solutions:"
    echo ""
    echo "For local development (Docker Desktop, OrbStack, Minikube, Kind):"
    echo "  Install local-path provisioner:"
    echo "  kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/master/deploy/local-path-storage.yaml"
    echo ""
    echo "For cloud providers:"
    echo "  GKE: Storage classes are pre-configured"
    echo "  EKS: Install EBS CSI driver"
    echo "  AKS: Storage classes are pre-configured"
    echo ""
    exit 1
fi

if [ "$AUTO_MODE" = false ]; then
    echo "Available storage classes:"
    for sc in $STORAGE_CLASSES; do
        echo "  - $sc"
    done
    echo ""
fi

# Try to auto-detect the best storage class
SELECTED_SC=""

# Preference order for local development
for sc in local-path hostpath standard default; do
    if echo "$STORAGE_CLASSES" | grep -qw "$sc"; then
        SELECTED_SC="$sc"
        break
    fi
done

# If still not found, use the first available
if [ -z "$SELECTED_SC" ]; then
    SELECTED_SC=$(echo "$STORAGE_CLASSES" | awk '{print $1}')
fi

if [ "$AUTO_MODE" = false ]; then
    echo "Recommended storage class: ${SELECTED_SC}"
    echo ""

    # Ask for confirmation
    read -p "Set '$SELECTED_SC' as the default storage class? (y/n) " -n 1 -r
    echo ""

    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Storage configuration cancelled"
        echo ""
        echo "To manually set a default storage class:"
        echo "  kubectl patch storageclass <name> -p '{\"metadata\": {\"annotations\":{\"storageclass.kubernetes.io/is-default-class\":\"true\"}}}'"
        exit 0
    fi
fi

# Set the storage class as default
if [ "$AUTO_MODE" = false ]; then
    echo "Setting '${SELECTED_SC}' as default storage class..."
else
    echo "Auto-configuring storage class: ${SELECTED_SC}"
fi
kubectl patch storageclass "$SELECTED_SC" -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'

echo "✓ Storage class configured"
echo ""

# Verify
echo "Updated Storage Classes:"
kubectl get storageclass
echo ""

# Test with a temporary PVC
echo "Testing storage configuration..."

cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: storage-test-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Mi
EOF

# Wait a few seconds
sleep 3

# Check status
PVC_STATUS=$(kubectl get pvc storage-test-pvc -o jsonpath='{.status.phase}')

if [ "$PVC_STATUS" = "Bound" ]; then
    echo "✓ Storage test successful - PVC bound to volume"
elif [ "$PVC_STATUS" = "Pending" ]; then
    echo "⚠ Storage test: PVC is pending (volume binding may be waiting for first consumer)"
    echo "  This is normal for 'WaitForFirstConsumer' binding mode"
else
    echo "✗ Storage test failed - PVC status: ${PVC_STATUS}"
fi

# Clean up test PVC
kubectl delete pvc storage-test-pvc >/dev/null 2>&1 || true

echo ""
echo "===================================="
echo "Storage Configuration Complete"
echo "===================================="
echo ""
echo "Next steps:"
echo "1. Deploy pgskipper: ./scripts/setup.sh"
echo "2. Verify PostgreSQL cluster: kubectl get pods -n postgres"
echo ""
echo "Note: If PVCs remain pending after setup, check:"
echo "  kubectl describe pvc <pvc-name> -n postgres"
echo ""
