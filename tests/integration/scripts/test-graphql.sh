#!/bin/bash
# Test: GraphQL queries and mutations with typed input/output
source "$(dirname "$0")/lib.sh"

echo "=== GraphQL ==="

# Create user via mutation (typed input: UserInput, returns User type with field selection)
body=$(http_body POST "$GQL/graphql" '{"query":"mutation { createUser(input: {name: \"Eve\", email: \"eve@test.com\"}) { id name email } }"}')
assert_contains "Mutation returns user name" "Eve" "$body"
assert_contains "Mutation returns user email" "eve@test.com" "$body"
assert_not_contains "No errors in mutation" "errors" "$body"

# Query all users with field selection (returns [User] typed array)
body=$(http_body POST "$GQL/graphql" '{"query":"{ users { id name email } }"}')
assert_contains "Query returns user data" "Eve" "$body"
assert_not_contains "No errors in query" "errors" "$body"

# GraphQL endpoint returns 200
status=$(http_status POST "$GQL/graphql" '{"query":"{ users { name } }"}')
assert_status "GraphQL endpoint returns 200" "200" "$status"

# Introspection: verify User type exists with typed fields
body=$(http_body POST "$GQL/graphql" '{"query":"{ __type(name: \"user\") { name fields { name type { name kind } } } }"}')
assert_contains "User type exists" "user" "$body"
assert_contains "User type has name field" "name" "$body"

# Introspection: verify UserInput type exists (typed DTO for mutations)
body=$(http_body POST "$GQL/graphql" '{"query":"{ __type(name: \"userInput\") { name inputFields { name type { name kind } } } }"}')
assert_contains "UserInput type exists" "userInput" "$body"

# Federation: _service { sdl }
body=$(http_body POST "$GQL/graphql" '{"query":"{ _service { sdl } }"}')
assert_contains "Federation SDL available" "sdl|type" "$body"

# Invalid query returns error
body=$(http_body POST "$GQL/graphql" '{"query":"{ nonExistent { id } }"}')
assert_contains "Invalid query returns error" "error|Error" "$body"

report
