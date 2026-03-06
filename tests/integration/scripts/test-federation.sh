#!/bin/bash
# Test: GraphQL Federation via Cosmo Router
source "$(dirname "$0")/lib.sh"

COSMO="http://localhost:${PORT_COSMO:-5000}"

echo "=== Federation (Cosmo Router) ==="

# Check if Cosmo Router is available
status=$(curl -so /dev/null -w "%{http_code}" "$COSMO/health" 2>/dev/null)
if [[ "$status" != "200" ]]; then
  echo "  ⚠ Cosmo Router not available (health=$status), skipping federation tests"
  report
  exit 0
fi

# Query through Cosmo with typed field selection (federated gateway → Mycel subgraph)
body=$(http_body POST "$COSMO/graphql" '{"query":"{ users { id name email } }"}')
assert_contains "Federated query returns users" "users" "$body"
assert_not_contains "No errors in federated query" "errors" "$body"

# Mutation through Cosmo with typed input and field selection
body=$(http_body POST "$COSMO/graphql" '{"query":"mutation { createUser(input: {name: \"Cosmo\", email: \"cosmo@test.com\"}) { id name email } }"}')
assert_contains "Federated mutation returns data" "createUser" "$body"
assert_not_contains "No errors in federated mutation" "errors" "$body"

# Verify the mutation persisted (query back through Cosmo with field selection)
body=$(http_body POST "$COSMO/graphql" '{"query":"{ users { name email } }"}')
assert_contains "Federated query finds created user" "Cosmo" "$body"

report
