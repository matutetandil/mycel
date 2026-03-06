#!/bin/bash
# Test: Notification connectors (all route to mock server)
source "$(dirname "$0")/lib.sh"

echo "=== Notifications ==="

# Clear mock
mock_clear

# Send Slack notification
status=$(http_status POST "$BASE/notify/slack" '{"message":"Hello from Slack test"}')
assert_status "Slack notification returns 200" "200" "$status"

# Send Discord notification
status=$(http_status POST "$BASE/notify/discord" '{"message":"Hello from Discord test"}')
assert_status "Discord notification returns 200" "200" "$status"

# Send SMS notification
status=$(http_status POST "$BASE/notify/sms" '{"phone":"+15559876543","message":"Hello from SMS test"}')
assert_status "SMS notification returns 200" "200" "$status"

# Send Push notification
status=$(http_status POST "$BASE/notify/push" '{"device_token":"test-device-123","title":"Test Push","message":"Hello from Push test"}')
assert_status "Push notification returns 200" "200" "$status"

# Wait for all notifications to reach mock
sleep 2

# Verify mock captured all outbound calls
body=$(mock_requests)
count=$(echo "$body" | jq 'length')
if [[ "$count" -ge 4 ]]; then
  echo "  ✓ Mock captured $count notification requests"
  ((PASS++))
else
  echo "  ✗ Expected at least 4 captured requests, got $count"
  ((FAIL++))
fi

report
