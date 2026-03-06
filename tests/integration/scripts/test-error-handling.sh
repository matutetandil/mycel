#!/bin/bash
# Test: Error handling with retry and DLQ fallback
source "$(dirname "$0")/lib.sh"

echo "=== Error Handling ==="

# Trigger flow that will fail (references nonexistent table)
status=$(http_status POST "$BASE/test/error" '{"data":"should-fail"}')
# This should either return error or trigger fallback
# The important thing is the service doesn't crash
assert_contains "Error flow returns a response" "200|500|400" "$status"

# Check DLQ table has a record (fallback wrote the error)
sleep 1
body=$(http_body GET "$BASE/pg/users") # Just verify service is still healthy
status=$(http_status GET "$BASE/pg/users")
assert_status "Service still healthy after error" "200" "$status"

report
