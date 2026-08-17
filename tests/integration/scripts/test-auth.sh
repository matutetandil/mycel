#!/bin/bash
# Test: a connector behind authentication
#
# Nothing in this suite exercised a Mycel service with authentication turned on,
# which is why an auth type compared exactly — "API_KEY" against "api_key" —
# could leave the settings underneath unparsed and turn away every request while
# the configuration file looked correct.
source "$(dirname "$0")/lib.sh"

echo "=== Auth ==="

# A request with no credentials at all.
status=$(http_status GET "$AUTHED/secure/users")
assert_status "A request with no credentials is refused" "401" "$status"

# A key nobody issued.
status=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: not-the-key" "$AUTHED/secure/users")
assert_status "A key nobody issued is refused" "401" "$status"

# The key the connector was configured with. The type is written in capitals in
# that configuration, so this also pins that the word is read however it is
# spelt.
status=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: integration-key" "$AUTHED/secure/users")
assert_status "The configured key is served" "200" "$status"

body=$(curl -s -H "X-API-Key: integration-key" "$AUTHED/secure/users")
assert_contains "And it answers with data" "\[|id|name" "$body"

# A path listed as public answers without credentials, which is what a health
# check or a webhook receiver needs.
status=$(http_status GET "$AUTHED/public/ping")
assert_status "A public path needs no credentials" "200" "$status"

# The unauthenticated connector is untouched by any of this.
status=$(http_status GET "$BASE/pg/users")
assert_status "The connector with no auth block still serves everyone" "200" "$status"

report
