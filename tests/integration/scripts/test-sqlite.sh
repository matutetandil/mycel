#!/bin/bash
# Test: SQLite CRUD via REST
source "$(dirname "$0")/lib.sh"

echo "=== SQLite ==="

# Initialize SQLite table
http_body POST "$BASE/sqlite/init" '{}' > /dev/null 2>&1
sleep 1

# Create user
body=$(http_body POST "$BASE/sqlite/users" '{"name":"Dave","email":"dave@test.com"}')
assert_json_not_null "POST creates user with id" ".id" "$body"
USER_ID=$(echo "$body" | jq -r '.id')

# List users
status=$(http_status GET "$BASE/sqlite/users")
assert_status "GET /sqlite/users returns 200" "200" "$status"

body=$(http_body GET "$BASE/sqlite/users")
assert_contains "List contains Dave" "dave@test.com" "$body"

# Get by ID
body=$(http_body GET "$BASE/sqlite/users/$USER_ID")
assert_json "Get returns correct name" ".[0].name // .name" "Dave" "$body"

# Delete
status=$(http_status DELETE "$BASE/sqlite/users/$USER_ID")
assert_contains "DELETE /sqlite/users/:id returns 200/204" "200|204" "$status"

report
