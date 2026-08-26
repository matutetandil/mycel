#!/bin/bash
# Test: Flow filter
source "$(dirname "$0")/lib.sh"

echo "=== Filter ==="

# Active status should pass filter and write to DB
status=$(http_status POST "$BASE/test/filter" '{"status":"active","data":"test"}')
assert_status "Active status passes filter (200)" "200" "$status"

# Verify the data was stored
body=$(http_body GET "$BASE/test/filter/results")
assert_contains "Filter result stored correctly" "passed" "$body"

# Inactive status should be rejected by filter (200, and the body says which
# gate decided rather than spilling the internal struct)
body=$(http_body POST "$BASE/test/filter" '{"status":"inactive","data":"test"}')
assert_contains "Inactive status filtered" '"status":"dropped"' "$body"
assert_contains "Drop names the gate that decided" '"reason":"filter"' "$body"

report
