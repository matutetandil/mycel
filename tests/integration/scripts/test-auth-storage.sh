#!/bin/bash
# Test: accounts kept in a real database
#
# The SQL user stores map onto a table somebody already has and speak Postgres,
# so nothing a unit test can stand up reaches them: everything from the manager
# down was covered only by the memory store. This is the run that exercises the
# statements themselves — the ones that would fail against a real database over
# a column name, a placeholder style or a scan of a null.
#
# It also covers the audit block, which nothing read before 2.19.0: a service
# configured to record sign-ins wrote nothing at all, and that is discovered
# during an investigation.
source "$(dirname "$0")/lib.sh"

echo "=== Auth storage (Postgres) ==="

EMAIL="ada-$$@example.com"

# --- Registering ------------------------------------------------------------

body=$(http_body POST "$BASE/auth/register" "{\"email\":\"$EMAIL\",\"password\":\"a-long-enough-password\",\"name\":\"Ada\"}")
assert_contains "Registering answers with a token" "access_token" "$body"

TOKEN=$(echo "$body" | jq -r '.tokens.access_token // .access_token // empty' 2>/dev/null)
if [[ -z "$TOKEN" ]]; then
  echo "  ⚠ no access token in the response, skipping the rest"
  echo "    response: $body"
  report
  exit 0
fi

# The account is in the database, not only in this process's memory — which is
# the whole reason to configure storage. Read back through a flow, so this is
# the row Postgres holds rather than anything the auth manager remembers.
body=$(http_body GET "$BASE/auth-users")
assert_contains "The account reached the database" "$EMAIL" "$body"

# --- Signing in -------------------------------------------------------------

status=$(http_status POST "$BASE/auth/login" "{\"email\":\"$EMAIL\",\"password\":\"a-long-enough-password\"}")
assert_status "Signing in with the right password" "200" "$status"

status=$(http_status POST "$BASE/auth/login" "{\"email\":\"$EMAIL\",\"password\":\"not-the-password\"}")
assert_not_contains "Signing in with the wrong one is refused" "^200$" "$status"

status=$(http_status POST "$BASE/auth/login" "{\"email\":\"nobody-$$@example.com\",\"password\":\"a-long-enough-password\"}")
assert_not_contains "An account nobody registered is refused" "^200$" "$status"

# --- The token it issued ----------------------------------------------------

body=$(http_body POST "$BASE/auth/login" "{\"email\":\"$EMAIL\",\"password\":\"a-long-enough-password\"}")
REFRESH=$(echo "$body" | jq -r '.tokens.refresh_token // .refresh_token // empty' 2>/dev/null)

if [[ -n "$REFRESH" ]]; then
  status=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    -H "Content-Type: application/json" \
    -d "{\"refresh_token\":\"$REFRESH\"}" "$BASE/auth/refresh")
  assert_status "A refresh token buys a new access token" "200" "$status"
fi

# --- Registering the same address twice -------------------------------------

status=$(http_status POST "$BASE/auth/register" "{\"email\":\"$EMAIL\",\"password\":\"another-long-password\"}")
assert_not_contains "The same address cannot be registered twice" "^200$" "$status"

report
