#!/bin/bash
# Test: HTTP client -> mock server
source "$(dirname "$0")/lib.sh"

echo "=== HTTP Client ==="

# Clear mock
mock_clear

# Trigger HTTP client call via REST
status=$(http_status POST "$BASE/test/http-call" '{"data":"test-payload"}')
assert_status "HTTP call returns 200" "200" "$status"

# Verify mock captured the outbound request
sleep 1
body=$(mock_requests "/external")
assert_contains "Mock captured outbound call" "external" "$body"
assert_contains "Mock captured correct payload" "test-payload|mycel" "$body"

report
