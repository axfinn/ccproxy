#!/bin/bash
# Check account authentication status

ADMIN_KEY="${CCPROXY_ADMIN_KEY:-your-admin-key}"
BASE_URL="${1:-http://localhost:8080}"

echo "=== Account Authentication Status ==="
echo

# List accounts with full details
echo "Listing all accounts:"
ACCOUNTS=$(curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list")
echo "$ACCOUNTS" | jq '.[] | {
  id,
  name,
  type,
  is_active,
  organization_id,
  expires_at,
  health_status,
  error_count,
  success_count,
  last_check_at,
  last_used_at
}' 2>/dev/null || echo "Failed to parse account data"

echo
echo "=== Analysis ==="
echo "$ACCOUNTS" | jq -r '.[] | select(.health_status == "unhealthy" or .error_count > 0) |
  "⚠️  Account: \(.name) (\(.id))
  Status: \(.health_status // "unknown")
  Type: \(.type)
  Errors: \(.error_count) | Success: \(.success_count)
  Last Check: \(.last_check_at // "never")
  Expires At: \(.expires_at // "never")
  "' 2>/dev/null

echo
echo "=== Next Steps ==="
echo "If you see 403 errors, your token/session has expired or been revoked."
echo
echo "For OAuth accounts:"
echo "  1. Manually refresh token:"
echo "     curl -X POST -H \"X-Admin-Key: \$ADMIN_KEY\" \\"
echo "       $BASE_URL/api/account/{account_id}/refresh"
echo
echo "For SessionKey accounts:"
echo "  1. Get new session key from claude.ai browser"
echo "  2. Update account:"
echo "     curl -X PUT -H \"X-Admin-Key: \$ADMIN_KEY\" \\"
echo "       -H \"Content-Type: application/json\" \\"
echo "       -d '{\"session_key\": \"sk-ant-sid01-...\"}' \\"
echo "       $BASE_URL/api/account/{account_id}"
echo
echo "To reset circuit breaker:"
echo "  - Restart the service (circuit breaker state is in-memory)"
echo "  - Or wait 30 seconds for half-open state"
