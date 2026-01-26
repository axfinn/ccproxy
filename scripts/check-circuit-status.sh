#!/bin/bash
# Circuit breaker status checker

ADMIN_KEY="${CCPROXY_ADMIN_KEY:-your-admin-key}"
BASE_URL="${1:-http://localhost:8080}"

echo "=== Checking Circuit Breaker Status ==="
echo

# List all accounts
echo "Accounts:"
curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list" | jq '.[] | {id, name, type, is_active, health_status, error_count, success_count, expires_at}' 2>/dev/null || echo "Failed to list accounts"
echo

# Check metrics
echo "=== Metrics ==="
curl -s "$BASE_URL/metrics" 2>/dev/null || echo "Metrics endpoint not available"
echo

echo "=== Diagnosis ==="
echo "If you see 'unavailable accounts available=0 total=1', your account is circuit-broken."
echo
echo "Solutions:"
echo "1. Disable circuit breaker temporarily: Set CCPROXY_CIRCUIT_ENABLED=false"
echo "2. Wait 30 seconds for circuit breaker to enter half-open state"
echo "3. Fix the underlying issue causing failures (check logs)"
echo "4. Reset circuit breaker via admin API (if implemented)"
echo
echo "To disable circuit breaker:"
echo "  export CCPROXY_CIRCUIT_ENABLED=false"
echo "  make run"
