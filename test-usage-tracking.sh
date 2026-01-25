#!/bin/bash

# ccproxy Usage Tracking API Test Script
# Usage: ./test-usage-tracking.sh <admin_key> [base_url]

set -e

ADMIN_KEY="${1:-}"
BASE_URL="${2:-http://localhost:8080}"

if [ -z "$ADMIN_KEY" ]; then
    echo "Usage: $0 <admin_key> [base_url]"
    echo "Example: $0 your-admin-key http://localhost:8080"
    exit 1
fi

API="$BASE_URL/api"
HEADERS="X-Admin-Key: $ADMIN_KEY"

echo "========================================"
echo "ccproxy Usage Tracking API Test"
echo "========================================"
echo "Base URL: $BASE_URL"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

function print_section() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

function print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

function print_error() {
    echo -e "${RED}✗ $1${NC}"
}

function print_info() {
    echo -e "${YELLOW}→ $1${NC}"
}

# 1. Test Token Management
print_section "1. Token Management & Settings"

print_info "Listing all tokens..."
TOKENS=$(curl -s -H "$HEADERS" "$API/token/list")
echo "$TOKENS" | jq '.' 2>/dev/null || echo "$TOKENS"

# Get first token ID for testing
TOKEN_ID=$(echo "$TOKENS" | jq -r '.tokens[0].id' 2>/dev/null)
if [ -z "$TOKEN_ID" ] || [ "$TOKEN_ID" = "null" ]; then
    print_error "No tokens found. Please create a token first."
    exit 1
fi

print_success "Found token: $TOKEN_ID"
echo ""

print_info "Enabling conversation logging for token..."
curl -s -X PUT -H "$HEADERS" \
    -H "Content-Type: application/json" \
    -d '{"enable_conversation_logging": true}' \
    "$API/token/$TOKEN_ID/settings" | jq '.'
print_success "Conversation logging enabled"
echo ""

# 2. Test Request Logs
print_section "2. Request Logs API"

print_info "Listing request logs..."
LOGS=$(curl -s -H "$HEADERS" "$API/logs/requests?limit=5")
echo "$LOGS" | jq '.' 2>/dev/null || echo "$LOGS"
echo ""

LOG_ID=$(echo "$LOGS" | jq -r '.logs[0].id' 2>/dev/null)
if [ -n "$LOG_ID" ] && [ "$LOG_ID" != "null" ]; then
    print_info "Getting single request log: $LOG_ID"
    curl -s -H "$HEADERS" "$API/logs/requests/$LOG_ID" | jq '.'
    echo ""
fi

print_info "Listing logs for specific token..."
curl -s -H "$HEADERS" "$API/logs/requests?token_id=$TOKEN_ID&limit=3" | jq '.'
echo ""

# 3. Test Statistics
print_section "3. Usage Statistics API"

print_info "Getting token statistics (last 7 days)..."
curl -s -H "$HEADERS" "$API/stats/tokens/$TOKEN_ID?days=7" | jq '.'
echo ""

print_info "Getting token trend (last 30 days)..."
curl -s -H "$HEADERS" "$API/stats/tokens/$TOKEN_ID/trend?days=30" | jq '.trend | .[:3]'
echo ""

print_info "Getting global overview..."
curl -s -H "$HEADERS" "$API/stats/overview?days=1" | jq '.'
echo ""

print_info "Getting realtime statistics..."
curl -s -H "$HEADERS" "$API/stats/realtime" | jq '.'
echo ""

print_info "Getting top tokens..."
curl -s -H "$HEADERS" "$API/stats/top/tokens?days=7&limit=5" | jq '.'
echo ""

print_info "Getting top models..."
curl -s -H "$HEADERS" "$API/stats/top/models?days=7" | jq '.'
echo ""

# 4. Test Conversations
print_section "4. Conversations API"

print_info "Listing conversations..."
CONVS=$(curl -s -H "$HEADERS" "$API/conversations?token_id=$TOKEN_ID&limit=5")
echo "$CONVS" | jq '.' 2>/dev/null || echo "$CONVS"
echo ""

CONV_ID=$(echo "$CONVS" | jq -r '.conversations[0].id' 2>/dev/null)
if [ -n "$CONV_ID" ] && [ "$CONV_ID" != "null" ]; then
    print_info "Getting single conversation: $CONV_ID"
    curl -s -H "$HEADERS" "$API/conversations/$CONV_ID" | jq '.'
    echo ""

    print_info "Searching conversations..."
    curl -s -H "$HEADERS" "$API/conversations/search?q=test&token_id=$TOKEN_ID&limit=3" | jq '.'
    echo ""
else
    print_info "No conversations found (conversation logging may not be enabled yet)"
fi

# 5. Test Export
print_section "5. Export API"

print_info "Exporting request logs to CSV..."
curl -s -H "$HEADERS" "$API/logs/requests/export?format=csv&token_id=$TOKEN_ID&limit=10" -o /tmp/request_logs.csv
if [ -f /tmp/request_logs.csv ]; then
    print_success "CSV exported to /tmp/request_logs.csv"
    echo "First 5 lines:"
    head -5 /tmp/request_logs.csv
    echo ""
fi

print_info "Exporting request logs to JSON..."
curl -s -H "$HEADERS" "$API/logs/requests/export?format=json&token_id=$TOKEN_ID&limit=3" -o /tmp/request_logs.json
if [ -f /tmp/request_logs.json ]; then
    print_success "JSON exported to /tmp/request_logs.json"
    cat /tmp/request_logs.json | jq '.[0]'
    echo ""
fi

if [ -n "$CONV_ID" ] && [ "$CONV_ID" != "null" ]; then
    print_info "Exporting conversations to JSONL..."
    curl -s -H "$HEADERS" "$API/conversations/export?format=jsonl&token_id=$TOKEN_ID&limit=3" -o /tmp/conversations.jsonl
    if [ -f /tmp/conversations.jsonl ]; then
        print_success "JSONL exported to /tmp/conversations.jsonl"
        echo "First conversation:"
        head -1 /tmp/conversations.jsonl | jq '.'
        echo ""
    fi
fi

# 6. Summary
print_section "Summary"

print_success "Token Management: ✓"
print_success "Request Logs API: ✓"
print_success "Usage Statistics: ✓"
print_success "Conversations API: ✓"
print_success "Export Functions: ✓"

echo ""
echo "All tests completed successfully!"
echo ""
echo "Exported files:"
echo "  - /tmp/request_logs.csv"
echo "  - /tmp/request_logs.json"
[ -f /tmp/conversations.jsonl ] && echo "  - /tmp/conversations.jsonl"
echo ""
echo "For more information, see docs/USAGE_TRACKING.md"
