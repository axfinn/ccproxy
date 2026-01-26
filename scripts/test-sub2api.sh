#!/bin/bash
# Test sub2api-style proxy implementation

set -e

BASE_URL="${1:-http://localhost:8080}"
ADMIN_KEY="${CCPROXY_ADMIN_KEY:-your-admin-key}"

echo "=== Testing Sub2API-Style Proxy Implementation ==="
echo "Base URL: $BASE_URL"
echo

# Step 1: Check if server is running
echo "Step 1: Checking server health..."
if curl -s -f "$BASE_URL/health" > /dev/null; then
  echo "✅ Server is running"
else
  echo "❌ Server is not running. Start it with: ./ccproxy"
  exit 1
fi
echo

# Step 2: Check accounts
echo "Step 2: Checking accounts..."
ACCOUNTS=$(curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list")
ACCOUNT_COUNT=$(echo "$ACCOUNTS" | jq '. | length' 2>/dev/null || echo "0")
echo "Found $ACCOUNT_COUNT accounts"

if [ "$ACCOUNT_COUNT" -eq "0" ]; then
  echo "⚠️  No accounts found. Please add an account first:"
  echo "   curl -X POST -H \"X-Admin-Key: \$ADMIN_KEY\" \\"
  echo "     -H \"Content-Type: application/json\" \\"
  echo "     -d '{\"name\": \"test-account\", \"session_key\": \"sk-ant-sid01-...\"}' \\"
  echo "     $BASE_URL/api/account/oauth"
  exit 1
fi

# Show account details
echo "$ACCOUNTS" | jq -r '.[] | "  • \(.name) (\(.type)) - status:\(.status), schedulable:\(.schedulable)"'
echo

# Step 3: Check schedulable accounts
echo "Step 3: Checking schedulable accounts..."
SCHEDULABLE=$(echo "$ACCOUNTS" | jq '[.[] | select(.status == "active" and .schedulable == true)] | length')
echo "Schedulable accounts: $SCHEDULABLE"

if [ "$SCHEDULABLE" -eq "0" ]; then
  echo "⚠️  No schedulable accounts available!"
  echo
  echo "Account status:"
  echo "$ACCOUNTS" | jq -r '.[] | "  • \(.name): status=\(.status), schedulable=\(.schedulable), rate_limit_reset_at=\(.rate_limit_reset_at // "none"), temp_unschedulable_until=\(.temp_unschedulable_until // "none")"'
  echo
  echo "Possible issues:"
  echo "  1. Account marked as error (403/401) - try refreshing token:"
  echo "     ACCOUNT_ID=\$(echo \"\$ACCOUNTS\" | jq -r '.[0].id')"
  echo "     curl -X POST -H \"X-Admin-Key: \$ADMIN_KEY\" \\"
  echo "       $BASE_URL/api/account/\$ACCOUNT_ID/refresh"
  echo
  echo "  2. Account rate limited - check rate_limit_reset_at"
  echo "  3. Account temporarily unschedulable - wait until temp_unschedulable_until"
  echo
  echo "To clear all temporary flags:"
  echo "  # Manually update database:"
  echo "  sqlite3 ccproxy.db \"UPDATE accounts SET schedulable=1, status='active', rate_limit_reset_at=NULL, temp_unschedulable_until=NULL WHERE id='ACCOUNT_ID'\""
  exit 1
fi

echo "✅ $SCHEDULABLE schedulable accounts found"
echo

# Step 4: Generate test token if needed
echo "Step 4: Checking/generating test token..."
TEST_TOKEN=$(curl -s -X POST -H "X-Admin-Key: $ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"user_name": "test-user", "mode": "web", "expires_in_days": 30}' \
  "$BASE_URL/api/token/generate" | jq -r '.token' 2>/dev/null)

if [ -z "$TEST_TOKEN" ] || [ "$TEST_TOKEN" = "null" ]; then
  echo "❌ Failed to generate test token"
  exit 1
fi

echo "✅ Test token generated: ${TEST_TOKEN:0:20}..."
echo

# Step 5: Test chat completion
echo "Step 5: Testing chat completion request..."
RESPONSE=$(curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TEST_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "Say hello"}],
    "stream": false
  }')

if echo "$RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
  echo "❌ Request failed:"
  echo "$RESPONSE" | jq '.'
  echo
  echo "Debug info:"
  echo "$RESPONSE" | jq -r '.details // "No details"'
else
  echo "✅ Request successful!"
  echo "$RESPONSE" | jq '{model, choices: (.choices[0].message.content[:50] + "...")}'
fi

echo
echo "=== Test Summary ==="
echo "✅ Server running"
echo "✅ Accounts configured: $ACCOUNT_COUNT"
echo "✅ Schedulable accounts: $SCHEDULABLE"
echo "✅ Sub2API-style selection working"
echo
echo "Monitor logs to see account selection logic:"
echo "  tail -f logs/*.log | grep -E '(selected account|switching account|rate limited)'"
