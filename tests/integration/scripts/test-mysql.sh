#!/bin/bash
# Test: MySQL CRUD via REST
source "$(dirname "$0")/lib.sh"

echo "=== MySQL ==="

# Create user
body=$(http_body POST "$BASE/mysql/users" '{"name":"Bob","email":"bob@test.com"}')
assert_json_not_null "POST creates user with id" ".id" "$body"
USER_ID=$(echo "$body" | jq -r '.id')

# List users
status=$(http_status GET "$BASE/mysql/users")
assert_status "GET /mysql/users returns 200" "200" "$status"

body=$(http_body GET "$BASE/mysql/users")
assert_contains "List contains Bob" "bob@test.com" "$body"

# Get by ID
body=$(http_body GET "$BASE/mysql/users/$USER_ID")
assert_json "Get returns correct name" ".[0].name // .name" "Bob" "$body"

# Delete
status=$(http_status DELETE "$BASE/mysql/users/$USER_ID")
assert_contains "DELETE /mysql/users/:id returns 200/204" "200|204" "$status"

report
