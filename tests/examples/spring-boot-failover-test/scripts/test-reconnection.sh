#!/bin/bash

# PostgreSQL Failover Test - Test Reconnection Script
# This script monitors the application during failover and tests reconnection

set -e

NAMESPACE="default"
APP_NAMESPACE="default"
PG_NAMESPACE="postgres"

echo "===================================="
echo "PostgreSQL Reconnection Test"
echo "===================================="
echo ""

# Check if kubectl is available
command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl is not installed" >&2; exit 1; }

# Get application pod
APP_POD=$(kubectl get pods -n "$APP_NAMESPACE" -l app.kubernetes.io/name=postgresql-failover-test -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$APP_POD" ]; then
    echo "Error: Could not find application pod"
    exit 1
fi

echo "Monitoring application pod: $APP_POD"
echo ""

# Function to call API endpoint
call_api() {
    local endpoint=$1
    kubectl exec -n "$APP_NAMESPACE" "$APP_POD" -- wget -q -O - "http://localhost:8080/api/$endpoint" 2>/dev/null || echo "ERROR"
}

# Function to test database connectivity
test_connectivity() {
    echo "Testing database connectivity..."

    # Get database info
    DB_INFO=$(call_api "db-info")

    if echo "$DB_INFO" | grep -q "PRIMARY\|REPLICA"; then
        echo "✓ Database is connected"
        echo "$DB_INFO" | grep -o '"role":"[^"]*"' | sed 's/"role":"//;s/"//'
        echo "$DB_INFO" | grep -o '"serverAddress":"[^"]*"' | sed 's/"serverAddress":"//;s/"//'
    else
        echo "✗ Database is not connected"
        return 1
    fi
}

# Function to perform write test
test_write() {
    echo "Testing write operation..."

    WRITE_RESULT=$(call_api "write-test?message=Failover+test+$(date +%s)")

    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        echo "✓ Write operation successful"
    else
        echo "✗ Write operation failed"
        return 1
    fi
}

# Function to get monitoring stats
get_stats() {
    echo "Monitoring Statistics:"

    STATS=$(call_api "monitor-stats")

    if [ "$STATS" != "ERROR" ]; then
        echo "$STATS" | python3 -m json.tool 2>/dev/null || echo "$STATS"
    else
        echo "Failed to get stats"
    fi
}

# Initial connectivity test
echo "=== Initial State ==="
test_connectivity
echo ""
get_stats
echo ""

# Start monitoring in background
echo "Starting continuous monitoring..."
echo "Press Ctrl+C to stop"
echo ""
echo "Timestamp          | Status       | Role    | Server IP        | Failures"
echo "-------------------|--------------|---------|------------------|----------"

CONSECUTIVE_FAILURES=0
TOTAL_CHECKS=0

# Continuous monitoring loop
while true; do
    TOTAL_CHECKS=$((TOTAL_CHECKS + 1))
    TIMESTAMP=$(date +"%Y-%m-%d %H:%M:%S")

    # Get database info
    DB_INFO=$(call_api "db-info")

    if echo "$DB_INFO" | grep -q '"status":"CONNECTED"'; then
        STATUS="CONNECTED"
        ROLE=$(echo "$DB_INFO" | grep -o '"role":"[^"]*"' | sed 's/"role":"//;s/"//')
        SERVER=$(echo "$DB_INFO" | grep -o '"serverAddress":"[^"]*"' | sed 's/"serverAddress":"//;s/"//')
        CONSECUTIVE_FAILURES=0
    else
        STATUS="DISCONNECTED"
        ROLE="UNKNOWN"
        SERVER="N/A"
        CONSECUTIVE_FAILURES=$((CONSECUTIVE_FAILURES + 1))
    fi

    # Get monitoring stats
    STATS=$(call_api "monitor-stats")
    TOTAL_FAILURES=$(echo "$STATS" | grep -o '"totalFailures":[0-9]*' | grep -o '[0-9]*' || echo "0")

    # Print status line
    printf "%s | %-12s | %-7s | %-16s | %s\n" \
        "$TIMESTAMP" "$STATUS" "$ROLE" "$SERVER" "$TOTAL_FAILURES"

    # Alert on failures
    if [ "$CONSECUTIVE_FAILURES" -eq 1 ]; then
        echo "⚠ CONNECTION LOST! Failover may be in progress..."
    elif [ "$CONSECUTIVE_FAILURES" -eq 10 ]; then
        echo "⚠ Connection has been down for 50 seconds"
    fi

    # Wait 5 seconds before next check
    sleep 5
done
