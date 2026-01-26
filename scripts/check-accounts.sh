#!/bin/bash
# Check account status via API

BASE_URL="${1:-http://localhost:8080}"
ADMIN_KEY="${CCPROXY_ADMIN_KEY}"

if [ -z "$ADMIN_KEY" ]; then
  echo "Error: CCPROXY_ADMIN_KEY not set"
  echo "Usage: export CCPROXY_ADMIN_KEY=your-key && $0"
  exit 1
fi

echo "=== Checking Accounts Status ==="
echo

curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list" | jq -r '
  if length == 0 then
    "❌ No accounts found. Please add an account first."
  else
    "Total accounts: \(length)\n",
    "Schedulable accounts: \([.[] | select(.status == \"active\" and .schedulable == true)] | length)\n",
    "\nAccount Details:",
    "================",
    (.[] |
      "ID: \(.id)",
      "Name: \(.name)",
      "Type: \(.type)",
      "Status: \(.status // "unknown")",
      "Schedulable: \(.schedulable // false)",
      "IsActive: \(.is_active)",
      "RateLimitResetAt: \(.rate_limit_reset_at // "none")",
      "TempUnschedulableUntil: \(.temp_unschedulable_until // "none")",
      "ErrorMessage: \(.error_message // "none")",
      "Priority: \(.priority // 0)",
      "---"
    )
  end
'

echo
echo "=== Quick Fix ==="
echo "If all accounts are unschedulable, run:"
echo "  curl -X POST -H \"X-Admin-Key: \$CCPROXY_ADMIN_KEY\" \\"
echo "    $BASE_URL/api/account/\$(curl -s -H \"X-Admin-Key: \$CCPROXY_ADMIN_KEY\" $BASE_URL/api/account/list | jq -r '.[0].id')/refresh"
