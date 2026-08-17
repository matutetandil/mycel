#!/bin/bash
# Test: a connector made of profiles
#
# A profile is a connector chosen at runtime, and it is what the documentation
# recommends for configuring per environment. It used to be parsed against a
# second hand-written list — forty attributes against the connector's hundred
# and fifty-nine, seven nested blocks against nineteen — so the settings that
# actually differ between environments were the ones a profile could not hold.
source "$(dirname "$0")/lib.sh"

echo "=== Profiles ==="

http_body POST "$BASE/test/profile/init" '{}' > /dev/null 2>&1

TITLE="profile-$$"

status=$(http_status POST "$BASE/test/profile" "{\"title\":\"$TITLE\",\"status\":\"active\"}")
assert_status "A write through the selected profile is accepted" "200" "$status"

sleep 1
body=$(http_body GET "$BASE/test/profile")
assert_contains "And it landed in the profile's own store" "$TITLE" "$body"

report
