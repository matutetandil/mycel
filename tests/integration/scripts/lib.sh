#!/bin/bash
# Shared test library for integration tests

PASS=0
FAIL=0
BASE="http://localhost:${PORT_REST:-3000}"
GQL="http://localhost:${PORT_GQL:-4000}"
SOAP="http://localhost:${PORT_SOAP:-8081}"
GRPC="localhost:${PORT_GRPC:-50051}"
ADMIN="http://localhost:${PORT_ADMIN:-9090}"
MOCK="http://localhost:${PORT_MOCK:-8888}"

assert_status() {
  local name="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    echo "  ✓ $name"
    ((PASS++))
  else
    echo "  ✗ $name (expected $expected, got $actual)"
    ((FAIL++))
  fi
}

assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -qE "$needle"; then
    echo "  ✓ $name"
    ((PASS++))
  else
    echo "  ✗ $name (missing '$needle')"
    ((FAIL++))
  fi
}

assert_not_contains() {
  local name="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -qE "$needle"; then
    echo "  ✗ $name (found '$needle' but shouldn't)"
    ((FAIL++))
  else
    echo "  ✓ $name"
    ((PASS++))
  fi
}

assert_json() {
  local name="$1" path="$2" expected="$3" json="$4"
  local actual
  actual=$(echo "$json" | jq -r "$path" 2>/dev/null)
  if [[ "$actual" == "$expected" ]]; then
    echo "  ✓ $name"
    ((PASS++))
  else
    echo "  ✗ $name (expected $expected, got $actual)"
    ((FAIL++))
  fi
}

assert_json_not_null() {
  local name="$1" path="$2" json="$3"
  local actual
  actual=$(echo "$json" | jq -r "$path" 2>/dev/null)
  if [[ -n "$actual" && "$actual" != "null" ]]; then
    echo "  ✓ $name"
    ((PASS++))
  else
    echo "  ✗ $name (expected non-null, got $actual)"
    ((FAIL++))
  fi
}

report() {
  echo ""
  echo "Results: $PASS passed, $FAIL failed"
  return "$FAIL"
}

# HTTP helper: returns "STATUS_CODE\nBODY"
http() {
  local method="$1" url="$2" data="$3"
  if [[ -n "$data" ]]; then
    curl -sf -w "\n%{http_code}" -X "$method" -H "Content-Type: application/json" -d "$data" "$url" 2>/dev/null
  else
    curl -sf -w "\n%{http_code}" -X "$method" -H "Content-Type: application/json" "$url" 2>/dev/null
  fi
}

# Returns just the HTTP status code
http_status() {
  local method="$1" url="$2" data="$3"
  if [[ -n "$data" ]]; then
    curl -so /dev/null -w "%{http_code}" -X "$method" -H "Content-Type: application/json" -d "$data" "$url" 2>/dev/null
  else
    curl -so /dev/null -w "%{http_code}" -X "$method" "$url" 2>/dev/null
  fi
}

# Returns just the body
http_body() {
  local method="$1" url="$2" data="$3"
  if [[ -n "$data" ]]; then
    curl -s -X "$method" -H "Content-Type: application/json" -d "$data" "$url" 2>/dev/null
  else
    curl -s -X "$method" "$url" 2>/dev/null
  fi
}

# Clear mock server requests
mock_clear() {
  curl -sf -X DELETE "$MOCK/requests" > /dev/null 2>&1
}

# Get mock server captured requests
mock_requests() {
  local path_filter="$1"
  if [[ -n "$path_filter" ]]; then
    curl -sf "$MOCK/requests?path=$path_filter" 2>/dev/null
  else
    curl -sf "$MOCK/requests" 2>/dev/null
  fi
}
