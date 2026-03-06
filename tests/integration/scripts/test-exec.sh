#!/bin/bash
# Test: Exec connector
source "$(dirname "$0")/lib.sh"

echo "=== Exec ==="

# GET triggers exec step and returns transform output directly
body=$(http_body GET "$BASE/test/exec")
assert_contains "Exec returns output" "hello-exec|output|result" "$body"

status=$(http_status GET "$BASE/test/exec")
assert_status "Exec returns 200" "200" "$status"

report
