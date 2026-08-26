#!/bin/bash
# Check that a benchmark target answers correctly before anyone measures how
# fast it answers.
#
# Every k6 check in this suite is `status === 200`. That makes the load test
# blind to a target that is up and wrong: a 200 carrying an empty body, or a
# transform whose result never made it into the JSON, is counted as a success
# and folded into the throughput number. It is also blind to a target that
# never loaded its configuration at all — Mycel reads .mycel files, and a
# directory of .hcl files starts a service with no flows in it.
#
# Usage: ./preflight.sh <base-url> [--db]
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

BASE_URL="${1:-}"
WITH_DB=false
for arg in "$@"; do
  [[ "$arg" == "--db" ]] && WITH_DB=true
done

if [[ -z "$BASE_URL" ]]; then
  echo "usage: $0 <base-url> [--db]" >&2
  exit 2
fi

FAILURES=0

fail() {
  echo "  FAIL: $*"
  FAILURES=$((FAILURES + 1))
}

pass() {
  echo "  ok:   $*"
}

# assert <label> <method> <path> <body> <python-expression over `r`>
assert() {
  local label="$1" method="$2" path="$3" body="$4" expr="$5"
  local response status

  if [[ "$method" == "GET" ]]; then
    response="$(curl -s -m 15 -w '\n%{http_code}' "${BASE_URL}${path}" 2>/dev/null)"
  else
    response="$(curl -s -m 15 -w '\n%{http_code}' -X "$method" "${BASE_URL}${path}" \
      -H 'Content-Type: application/json' -d "$body" 2>/dev/null)"
  fi
  status="$(printf '%s' "$response" | tail -1)"
  local payload
  payload="$(printf '%s' "$response" | sed '$d')"

  if [[ "$status" != "200" && "$status" != "201" ]]; then
    fail "${label}: ${method} ${path} answered ${status:-no response}"
    return
  fi

  local verdict
  verdict="$(printf '%s' "$payload" | python3 "${SCRIPT_DIR}/preflight_check.py" "$expr" 2>&1)"

  if [[ -n "$verdict" ]]; then
    fail "${label}: ${verdict}"
  else
    pass "${label}"
  fi
}

echo "Preflight against ${BASE_URL}"

# The flows the standard benchmark loads.
assert "ping"    GET  /ping    ''  'isinstance(r, dict)'
assert "echo"    POST /echo    '{"x":1,"nested":{"y":[1,2]}}' \
       'r.get("x") == 1 and r.get("nested", {}).get("y") == [1, 2]'
assert "process" POST /process '{"email":"A@Example.com","name":"bob"}' \
       'r.get("email") == "a@example.com" and r.get("name") == "BOB" and len(r.get("id", "")) == 36 and r.get("created_at")'
assert "heavy"   POST /heavy   '{"email":"A@Example.com","name":" bob smith "}' \
       'r.get("name") == "BOB SMITH" and r.get("slug") == "bob-smith" and r.get("domain") == "Example.com" and len(r.get("hash", "")) == 64 and r.get("name_len") == 11'
assert "array"   POST /array   '{"items":[{"name":"a","price":10},{"name":"b","price":80}]}' \
       'r.get("count") == 2 and r.get("total") == 90 and r.get("average") == 45 and r.get("expensive") == [{"name": "b", "price": 80}] and r.get("sorted") == [{"name": "a", "price": 10}, {"name": "b", "price": 80}] and r.get("names") == ["a", "b"]'

if [[ "$WITH_DB" == true ]]; then
  EMAIL="preflight-$(date +%s)@example.com"
  assert "create user" POST /users "{\"email\":\"${EMAIL}\",\"name\":\"Preflight\"}" \
         'isinstance(r, (dict, list))'
  assert "read users"  GET  /users '' \
         "any(u.get('email') == '${EMAIL}' for u in (r if isinstance(r, list) else r.get('data', [])))"
fi

echo ""
if [[ "$FAILURES" -gt 0 ]]; then
  echo "Preflight failed: ${FAILURES} endpoint(s) are up but not answering correctly."
  echo "Measuring throughput against them would report the speed of a wrong answer."
  exit 1
fi
echo "Preflight passed: every endpoint returned the data it is supposed to."
