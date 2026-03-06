#!/bin/bash
# Test: MongoDB CRUD via REST
source "$(dirname "$0")/lib.sh"

echo "=== MongoDB ==="

# Create user
body=$(http_body POST "$BASE/mongo/users" '{"name":"Charlie","email":"charlie@test.com"}')
assert_json_not_null "POST creates user with id" ".id // ._id" "$body"
USER_ID=$(echo "$body" | jq -r '.id // ._id')

# List users
status=$(http_status GET "$BASE/mongo/users")
assert_status "GET /mongo/users returns 200" "200" "$status"

body=$(http_body GET "$BASE/mongo/users")
assert_contains "List contains Charlie" "charlie@test.com" "$body"

# Get by ID
body=$(http_body GET "$BASE/mongo/users/$USER_ID")
assert_contains "Get returns correct name" "Charlie" "$body"

# Delete
status=$(http_status DELETE "$BASE/mongo/users/$USER_ID")
assert_contains "DELETE /mongo/users/:id returns 200/204" "200|204" "$status"

report
