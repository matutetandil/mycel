#!/bin/bash
# Test: CEL transform functions
source "$(dirname "$0")/lib.sh"

echo "=== Transforms ==="

# POST triggers transform and writes to DB
status=$(http_status POST "$BASE/test/transforms" '{"text":"HELLO World","first":"John","last":"Doe"}')
assert_status "Transform POST returns 200" "200" "$status"

# Read back the stored results to verify transforms worked
body=$(http_body GET "$BASE/test/transforms/results")

# Verify transform functions produced correct output
assert_contains "uuid() generates ID" "generated_id" "$body"
assert_contains "lower() works" "hello world" "$body"
assert_contains "upper() works" "HELLO WORLD" "$body"
assert_contains "now() generates timestamp" "timestamp" "$body"
assert_contains "concat() works" "John Doe" "$body"

report
