#!/bin/bash
# Test: Accept block (business-level gate after filter)
source "$(dirname "$0")/lib.sh"

echo "=== Accept Block ==="

# Request with matching region should be accepted and written to DB
status=$(http_status POST "$BASE/test/accept" '{"action":"create","region":"us-east"}')
assert_status "Matching region accepted (200)" "200" "$status"

# Verify the data was stored
body=$(http_body GET "$BASE/test/accept/results")
assert_contains "Accepted request stored in DB" "create" "$body"
assert_contains "Region stored correctly" "us-east" "$body"

# Request with non-matching region should be rejected by accept gate
body=$(http_body POST "$BASE/test/accept" '{"action":"update","region":"eu-west"}')
assert_contains "Non-matching region rejected" "Filtered|filtered" "$body"

# Verify rejected request was NOT stored
body=$(http_body GET "$BASE/test/accept/results")
assert_not_contains "Rejected request not in DB" "update" "$body"

# Request that fails filter (no action field) should also not pass
body=$(http_body POST "$BASE/test/accept" '{"region":"us-east"}')
assert_contains "Missing action filtered out" "Filtered|filtered" "$body"

report
