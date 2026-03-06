#!/bin/bash
# Test: Type validation
source "$(dirname "$0")/lib.sh"

echo "=== Validation ==="

# Valid input
status=$(http_status POST "$BASE/test/validate" '{"name":"Valid User","email":"valid@test.com"}')
assert_status "Valid input returns 200" "200" "$status"

# Missing required field
status=$(http_status POST "$BASE/test/validate" '{"name":"No Email"}')
assert_status "Missing email returns 400" "400" "$status"

# Empty body
status=$(http_status POST "$BASE/test/validate" '{}')
assert_status "Empty body returns 400" "400" "$status"

report
