#!/bin/bash
# Test: a function a plugin adds to the expression language
#
# Registering one declared two overloads CEL considers the same shape — a
# variadic taking list(dyn) beside a single-argument one taking dyn, which
# matches a list — so building the expression environment failed outright:
#
#   overload signature collision in function shout: shout_variadic collides
#   with shout_1
#
# Not the call, the environment. Every transform in that flow went down with it,
# whether or not anything referred to the plugin. Nothing here used a functions
# block, so the whole feature was broken with the suite green.
source "$(dirname "$0")/lib.sh"

echo "=== Plugin functions ==="

status=$(http_status POST "$BASE/test/function" '{"name":"ada"}')
assert_status "A flow whose transform calls a plugin function runs" "200" "$status"

body=$(http_body POST "$BASE/test/function" '{"name":"ada"}')
assert_contains "The plugin's answer reaches the field" "PLUGIN" "$body"

# The rest of the transform still works, which is what the collision took down:
# the environment, not the one expression.
assert_contains "The other fields in the same transform are intact" "ada" "$body"

report
