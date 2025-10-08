#!/bin/bash

# PostgreSQL Failover Test - Trigger Failover Script
# This script triggers a failover by deleting the primary PostgreSQL pod

set -e

NAMESPACE="postgres"

echo "===================================="
echo "PostgreSQL Failover Trigger"
echo "===================================="
echo ""

# Check if kubectl is available
command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl is not installed" >&2; exit 1; }

# Get current primary pod
echo "Identifying current primary PostgreSQL pod..."
PRIMARY_POD=$(kubectl get pods -n "$NAMESPACE" --selector=pgtype=master -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$PRIMARY_POD" ]; then
    echo "Error: Could not find primary PostgreSQL pod"
    echo "Make sure the PostgreSQL cluster is running"
    exit 1
fi

echo "Current primary pod: $PRIMARY_POD"
echo ""

# Get current primary IP
PRIMARY_IP=$(kubectl get pod "$PRIMARY_POD" -n "$NAMESPACE" -o jsonpath='{.status.podIP}')
echo "Primary IP: $PRIMARY_IP"
echo ""

# Ask for confirmation
echo "This will delete the primary pod to trigger failover."
echo "Patroni will automatically promote a replica to primary."
read -p "Do you want to continue? (y/n) " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Failover cancelled"
    exit 0
fi
echo ""

# Get current timestamp
START_TIME=$(date +%s)
echo "Failover started at: $(date)"
echo ""

# Delete primary pod
echo "Deleting primary pod: $PRIMARY_POD"
kubectl delete pod "$PRIMARY_POD" -n "$NAMESPACE" --grace-period=0 --force

echo "✓ Primary pod deleted"
echo ""

# Monitor failover process
echo "Monitoring failover process..."
echo ""

# Wait for new primary to be elected
MAX_WAIT=120  # 2 minutes
ELAPSED=0
NEW_PRIMARY=""

while [ $ELAPSED -lt $MAX_WAIT ]; do
    sleep 5
    ELAPSED=$((ELAPSED + 5))

    # Try to get new primary pod (different from the deleted one)
    NEW_PRIMARY=$(kubectl get pods -n "$NAMESPACE" --selector=pgtype=master -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

    if [ -n "$NEW_PRIMARY" ] && [ "$NEW_PRIMARY" != "$PRIMARY_POD" ]; then
        break
    fi

    echo "Waiting for new primary to be elected... (${ELAPSED}s)"
done

if [ -z "$NEW_PRIMARY" ] || [ "$NEW_PRIMARY" == "$PRIMARY_POD" ]; then
    echo "Error: New primary was not elected within ${MAX_WAIT} seconds"
    exit 1
fi

END_TIME=$(date +%s)
FAILOVER_DURATION=$((END_TIME - START_TIME))

echo ""
echo "===================================="
echo "Failover Complete!"
echo "===================================="
echo ""
echo "Old primary pod: $PRIMARY_POD (IP: $PRIMARY_IP)"
echo "New primary pod: $NEW_PRIMARY"

# Get new primary IP
NEW_PRIMARY_IP=$(kubectl get pod "$NEW_PRIMARY" -n "$NAMESPACE" -o jsonpath='{.status.podIP}')
echo "New primary IP: $NEW_PRIMARY_IP"
echo ""
echo "Failover duration: ${FAILOVER_DURATION} seconds"
echo ""

# Display current cluster status
echo "Current PostgreSQL Cluster Status:"
kubectl get pods -n "$NAMESPACE" -l app=postgres
echo ""

echo "Pod details:"
kubectl get pods -n "$NAMESPACE" -l app=postgres -o wide
echo ""

echo "Next steps:"
echo "1. Monitor application logs to verify reconnection:"
echo "   kubectl logs -f -n default -l app.kubernetes.io/name=postgresql-failover-test"
echo ""
echo "2. Check monitoring stats:"
echo "   kubectl port-forward -n default svc/postgresql-failover-test 8080:8080"
echo "   curl http://localhost:8080/api/monitor-stats"
echo ""
echo "3. Verify database connection:"
echo "   curl http://localhost:8080/api/db-info"
echo ""
