#!/bin/bash
# Test: Multi-step orchestration
source "$(dirname "$0")/lib.sh"

echo "=== Steps ==="

# First create a user in postgres for steps to reference
http_body POST "$BASE/pg/users" '{"name":"Step User","email":"step@test.com"}' > /dev/null 2>&1

# Get the user ID from listing (postgres POST returns id:0)
body=$(http_body GET "$BASE/pg/users")
USER_ID=$(echo "$body" | jq -r '[.[] | select(.name=="Step User")][0].id // .[-1].id' 2>/dev/null)
if [[ -z "$USER_ID" || "$USER_ID" == "null" ]]; then
  USER_ID=1
fi

# Create an item directly in postgres
http_body POST "$BASE/pg/items" '{"title":"Step Item","status":"active"}' > /dev/null 2>&1

# Call multi-step flow (GET with path param returns transform output)
status=$(http_status GET "$BASE/test/steps/$USER_ID")
assert_status "Multi-step flow returns 200" "200" "$status"

body=$(http_body GET "$BASE/test/steps/$USER_ID")
assert_contains "Step result has user data" "Step User|step@test.com|user_name|user_email" "$body"

report
