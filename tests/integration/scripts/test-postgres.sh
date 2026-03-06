#!/bin/bash
# Test: PostgreSQL CRUD via REST
source "$(dirname "$0")/lib.sh"

echo "=== PostgreSQL ==="

# Create user
body=$(http_body POST "$BASE/pg/users" '{"name":"Alice","email":"alice@test.com"}')
assert_contains "POST creates user" "Alice" "$body"

# List users
status=$(http_status GET "$BASE/pg/users")
assert_status "GET /pg/users returns 200" "200" "$status"

body=$(http_body GET "$BASE/pg/users")
assert_contains "List contains Alice" "alice@test.com" "$body"

# Extract actual ID from listing (postgres POST returns id:0)
USER_ID=$(echo "$body" | jq -r '[.[] | select(.name=="Alice")][0].id // empty' 2>/dev/null)
if [[ -z "$USER_ID" || "$USER_ID" == "null" ]]; then
  USER_ID=$(echo "$body" | jq -r '.[-1].id' 2>/dev/null)
fi

# Get by ID
body=$(http_body GET "$BASE/pg/users/$USER_ID")
assert_json "Get returns correct name" ".[0].name" "Alice" "$body"

# Delete
status=$(http_status DELETE "$BASE/pg/users/$USER_ID")
assert_contains "DELETE /pg/users/:id returns 200/204" "200|204" "$status"

report
