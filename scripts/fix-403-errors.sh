#!/bin/bash
# Fix 403 authentication errors

ADMIN_KEY="${CCPROXY_ADMIN_KEY:-your-admin-key}"
BASE_URL="${1:-http://localhost:8080}"

echo "=== Fix Account 403 Errors ==="
echo

# Get account list
ACCOUNTS=$(curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list")

# Find accounts with errors
echo "Finding accounts with 403 errors..."
echo "$ACCOUNTS" | jq -r '.[] | select(.error_count > 0 or .health_status == "unhealthy") |
  "Account ID: \(.id)
  Name: \(.name)
  Type: \(.type)
  Status: \(.health_status // "unknown")
  Errors: \(.error_count)
  Expires: \(.expires_at // "never")
  "' || echo "No problematic accounts found"

echo
echo "=== Solution 1: Try Auto Refresh (for OAuth accounts) ==="
echo

# Try to refresh each OAuth account
echo "$ACCOUNTS" | jq -r '.[] | select(.type == "oauth") | .id' | while read -r ACCOUNT_ID; do
  if [ -n "$ACCOUNT_ID" ]; then
    echo "Refreshing OAuth account: $ACCOUNT_ID"
    RESULT=$(curl -s -X POST -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/$ACCOUNT_ID/refresh")

    if echo "$RESULT" | jq -e '.error' >/dev/null 2>&1; then
      echo "  ❌ Failed: $(echo "$RESULT" | jq -r '.error')"
      echo "  → Refresh token may be expired. Need to re-login."
    else
      echo "  ✅ Success!"
    fi
    echo
  fi
done

echo "=== Solution 2: Re-login OAuth Account ==="
echo
echo "If refresh failed, you need to re-do OAuth login:"
echo
echo "curl -X POST -H \"X-Admin-Key: \$ADMIN_KEY\" \\"
echo "  -H \"Content-Type: application/json\" \\"
echo "  -d '{\"name\": \"your-account\", \"session_key\": \"sk-ant-sid01-...\"}' \\"
echo "  $BASE_URL/api/account/oauth"
echo

echo "=== Solution 3: For SessionKey Accounts ==="
echo
echo "SessionKey cannot auto-refresh. Get new sessionKey from browser:"
echo "  1. Open claude.ai in browser"
echo "  2. F12 → Application → Cookies → sessionKey"
echo "  3. Update account:"
echo
echo "$ACCOUNTS" | jq -r '.[] | select(.type == "session_key") | .id' | while read -r ACCOUNT_ID; do
  if [ -n "$ACCOUNT_ID" ]; then
    echo "curl -X PUT -H \"X-Admin-Key: \$ADMIN_KEY\" \\"
    echo "  -H \"Content-Type: application/json\" \\"
    echo "  -d '{\"session_key\": \"NEW_SESSION_KEY\"}' \\"
    echo "  $BASE_URL/api/account/$ACCOUNT_ID"
    echo
  fi
done

echo "=== Solution 4: Disable Circuit Breaker Temporarily ==="
echo
echo "While fixing accounts, disable circuit breaker:"
echo "  export CCPROXY_CIRCUIT_ENABLED=false"
echo "  # Then restart service"
echo
echo "Or increase threshold:"
echo "  export CCPROXY_CIRCUIT_FAILURE_THRESHOLD=100"
echo "  export CCPROXY_CIRCUIT_OPEN_TIMEOUT=10s"
