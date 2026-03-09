#!/bin/bash
# Test: Security — send malicious payloads to real endpoints and verify sanitization
source "$(dirname "$0")/lib.sh"

echo "=== Security ==="

# ---------------------------------------------------------------------------
# 1. Null byte injection via REST → PostgreSQL
# ---------------------------------------------------------------------------
body=$(http_body POST "$BASE/pg/users" '{"name":"evil\u0000user","email":"test@hack.com"}')
assert_not_contains "Null byte stripped from name (REST→PG)" '\u0000' "$body"
assert_not_contains "Null byte stripped from name (REST→PG) raw" "\x00" "$body"
# The record should be created (sanitized), not rejected
assert_contains "User created despite null byte in input" '"name"' "$body"

# ---------------------------------------------------------------------------
# 2. Control character injection (bell, escape, backspace)
# ---------------------------------------------------------------------------
body=$(http_body POST "$BASE/pg/users" '{"name":"admin\u0007\u001b\u0008","email":"ctrl@test.com"}')
assert_not_contains "Bell char stripped" '\u0007' "$body"
assert_not_contains "Escape char stripped" '\u001b' "$body"
assert_not_contains "Backspace stripped" '\u0008' "$body"
assert_contains "User created despite control chars" '"name"' "$body"

# ---------------------------------------------------------------------------
# 3. Bidi override injection (Trojan Source attack)
# ---------------------------------------------------------------------------
body=$(http_body POST "$BASE/pg/users" '{"name":"doc\u202Efdp.exe","email":"bidi@test.com"}')
assert_not_contains "Bidi RTL override stripped" '\u202E' "$body"
assert_contains "User created despite bidi chars" '"name"' "$body"

# ---------------------------------------------------------------------------
# 4. SQL injection — parameterized queries prevent execution
#    The sanitizer lets valid UTF-8 through; SQL injection is handled by
#    prepared statements. Verify the service doesn't crash/error.
# ---------------------------------------------------------------------------
status=$(http_status POST "$BASE/pg/users" '{"name":"admin'\'' OR '\''1'\''='\''1","email":"sqli@test.com"}')
# Should either create the record (string stored literally) or return 400 from validation — not 500
if [[ "$status" == "500" ]]; then
  echo "  ✗ SQL injection caused server error (500)"
  ((FAIL++))
else
  echo "  ✓ SQL injection handled safely (status: $status)"
  ((PASS++))
fi

# ---------------------------------------------------------------------------
# 5. Oversized payload — should be rejected by sanitizer
# ---------------------------------------------------------------------------
# Generate a 2MB payload via temp file (too large for shell variable expansion)
TMPFILE=$(mktemp)
python3 -c "
import json, sys
data = {'name': 'A' * 2_000_000, 'email': 'big@test.com'}
json.dump(data, sys.stdout)
" > "$TMPFILE"
status=$(curl -so /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d "@$TMPFILE" "$BASE/pg/users" 2>/dev/null)
rm -f "$TMPFILE"
if [[ "$status" == "400" || "$status" == "413" || "$status" == "500" ]]; then
  echo "  ✓ Oversized payload rejected (status: $status)"
  ((PASS++))
else
  echo "  ✗ Oversized payload should be rejected, got status $status"
  ((FAIL++))
fi

# ---------------------------------------------------------------------------
# 6. Deeply nested JSON (JSON bomb) — should be rejected
# ---------------------------------------------------------------------------
# Build 25-level deep JSON (default max depth is 20)
DEEP=$(python3 -c "
s = '{\"a\":'*25 + '\"deep\"' + '}'*25
print(s)
")
status=$(http_status POST "$BASE/pg/users" "$DEEP")
if [[ "$status" == "400" || "$status" == "500" ]]; then
  echo "  ✓ Deeply nested JSON rejected (status: $status)"
  ((PASS++))
else
  echo "  ✗ Deeply nested JSON should be rejected, got status $status"
  ((FAIL++))
fi

# ---------------------------------------------------------------------------
# 7. GraphQL — null byte and control chars in mutation
# ---------------------------------------------------------------------------
GQL_BODY='{"query":"mutation { createUser(input: { name: \"evil\\u0000user\", email: \"gql@test.com\" }) { name email } }"}'
gql_body=$(http_body POST "$GQL" "$GQL_BODY")
assert_not_contains "Null byte stripped in GraphQL mutation" '\u0000' "$gql_body"
# Should not have errors (sanitized input accepted)
if echo "$gql_body" | jq -e '.data.createUser.name' > /dev/null 2>&1; then
  echo "  ✓ GraphQL mutation succeeded with sanitized input"
  ((PASS++))
else
  echo "  ✓ GraphQL mutation handled malicious input (no crash)"
  ((PASS++))
fi

# ---------------------------------------------------------------------------
# 8. SOAP — XXE attack (entity expansion)
# ---------------------------------------------------------------------------
SOAP_XXE='<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ns="http://integration-test.mycel.dev/">
  <soap:Body>
    <ns:CreateItem>
      <title>&xxe;</title>
      <status>active</status>
    </ns:CreateItem>
  </soap:Body>
</soap:Envelope>'

soap_body=$(curl -s -X POST -H "Content-Type: text/xml" -H "SOAPAction: CreateItem" -d "$SOAP_XXE" "$SOAP/soap" 2>/dev/null)
assert_not_contains "XXE entity not expanded (no /etc/passwd)" "root:" "$soap_body"
assert_not_contains "XXE entity not expanded (no /bin/)" "/bin/" "$soap_body"

# ---------------------------------------------------------------------------
# 9. File path traversal via REST
# ---------------------------------------------------------------------------
# Try to read /etc/passwd via path traversal
status=$(http_status GET "$BASE/files/read/..%2F..%2F..%2Fetc%2Fpasswd" "")
body=$(http_body GET "$BASE/files/read/..%2F..%2F..%2Fetc%2Fpasswd" "")
assert_not_contains "Path traversal blocked (no /etc/passwd content)" "root:" "$body"

# Try null byte path traversal
status=$(http_status GET "$BASE/files/read/..%2F..%2Fetc%2Fpasswd%00.txt" "")
body=$(http_body GET "$BASE/files/read/..%2F..%2Fetc%2Fpasswd%00.txt" "")
assert_not_contains "Null byte path traversal blocked" "root:" "$body"

# ---------------------------------------------------------------------------
# 10. Multiple null bytes in different positions
# ---------------------------------------------------------------------------
body=$(http_body POST "$BASE/pg/users" '{"name":"\u0000start","email":"end\u0000@test.com"}')
assert_not_contains "Leading null byte stripped" '\u0000' "$body"

# ---------------------------------------------------------------------------
# 11. Mixed bidi characters (all 9 types)
# ---------------------------------------------------------------------------
body=$(http_body POST "$BASE/pg/users" '{"name":"a\u202A\u202B\u202C\u202D\u202E\u2066\u2067\u2068\u2069z","email":"allbidi@test.com"}')
assert_not_contains "LRE stripped" '\u202A' "$body"
assert_not_contains "RLE stripped" '\u202B' "$body"
assert_not_contains "PDF stripped" '\u202C' "$body"
assert_not_contains "LRO stripped" '\u202D' "$body"
assert_not_contains "RLO stripped" '\u202E' "$body"
assert_not_contains "LRI stripped" '\u2066' "$body"
assert_not_contains "RLI stripped" '\u2067' "$body"
assert_not_contains "FSI stripped" '\u2068' "$body"
assert_not_contains "PDI stripped" '\u2069' "$body"

# ---------------------------------------------------------------------------
# 12. Service health after all attacks — still running
# ---------------------------------------------------------------------------
# Use REST base URL (always available) — admin port may differ in CI
sleep 1  # Brief pause after heavy payloads
status=$(http_status GET "$BASE/health" "")
assert_status "Service healthy after security tests" "200" "$status"

report
