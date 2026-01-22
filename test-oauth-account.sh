#!/bin/bash

# Test OAuth Account Management

BASE_URL="http://localhost:18080"
ADMIN_KEY="admin123"

echo "==========================================
 Testing OAuth Account Management
=========================================="

echo ""
echo "1. Testing account list endpoint..."
ACCOUNTS=$(curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list")
echo "Accounts: $ACCOUNTS"

echo ""
echo "2. Testing create session key account..."
CREATE_RESULT=$(curl -s -X POST -H "X-Admin-Key: $ADMIN_KEY" -H "Content-Type: application/json" \
  -d '{
    "name": "Test Session Account",
    "session_key": "sk-ant-sid01-test123",
    "organization_id": "org-test-123"
  }' \
  "$BASE_URL/api/account/sessionkey")
echo "Create Result: $CREATE_RESULT"

ACCOUNT_ID=$(echo "$CREATE_RESULT" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
echo "Created Account ID: $ACCOUNT_ID"

echo ""
echo "3. Testing get account endpoint..."
if [ -n "$ACCOUNT_ID" ]; then
  ACCOUNT_INFO=$(curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/$ACCOUNT_ID")
  echo "Account Info: $ACCOUNT_INFO"
fi

echo ""
echo "4. Testing account update..."
if [ -n "$ACCOUNT_ID" ]; then
  UPDATE_RESULT=$(curl -s -X PUT -H "X-Admin-Key: $ADMIN_KEY" -H "Content-Type: application/json" \
    -d '{
      "name": "Updated Test Account"
    }' \
    "$BASE_URL/api/account/$ACCOUNT_ID")
  echo "Update Result: $UPDATE_RESULT"
fi

echo ""
echo "5. Testing account health check..."
if [ -n "$ACCOUNT_ID" ]; then
  HEALTH_RESULT=$(curl -s -X POST -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/$ACCOUNT_ID/check")
  echo "Health Check Result: $HEALTH_RESULT"
fi

echo ""
echo "6. Testing list accounts again..."
ACCOUNTS=$(curl -s -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/list")
echo "Accounts: $ACCOUNTS"

echo ""
echo "7. Testing deactivate account..."
if [ -n "$ACCOUNT_ID" ]; then
  DEACTIVATE_RESULT=$(curl -s -X POST -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/$ACCOUNT_ID/deactivate")
  echo "Deactivate Result: $DEACTIVATE_RESULT"
fi

echo ""
echo "8. Testing delete account..."
if [ -n "$ACCOUNT_ID" ]; then
  DELETE_RESULT=$(curl -s -X DELETE -H "X-Admin-Key: $ADMIN_KEY" "$BASE_URL/api/account/$ACCOUNT_ID")
  echo "Delete Result: $DELETE_RESULT"
fi

echo ""
echo "=========================================="
echo " Testing Complete!"
echo "=========================================="
