#!/bin/bash
# Test: Exec connector
source "$(dirname "$0")/lib.sh"

echo "=== Exec ==="

# GET triggers exec step and returns transform output directly
body=$(http_body GET "$BASE/test/exec")
assert_contains "Exec returns output" "hello-exec|output|result" "$body"

status=$(http_status GET "$BASE/test/exec")
assert_status "Exec returns 200" "200" "$status"

# The same command, run over SSH on another container. Without a server that
# will run one, the ssh block could be parsed into a client nothing ever used —
# the stack's SFTP server is restricted to the sftp subsystem on purpose.
body=$(http_body GET "$BASE/test/exec/remote")
assert_contains "Remote exec returns what ran on the other machine" "hello-from-the-other-side" "$body"

status=$(http_status GET "$BASE/test/exec/remote")
assert_status "Remote exec returns 200" "200" "$status"

report
