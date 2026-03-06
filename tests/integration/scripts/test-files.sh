#!/bin/bash
# Test: File read/write
source "$(dirname "$0")/lib.sh"

echo "=== Files ==="

# Write file (send content as JSON object so it writes valid JSON)
status=$(http_status POST "$BASE/files/write" '{"filename":"test.json","content":{"hello":"world"}}')
assert_status "File write returns 200" "200" "$status"

# Read file
body=$(http_body GET "$BASE/files/read/test.json")
assert_contains "File read returns content" "hello|world" "$body"

report
