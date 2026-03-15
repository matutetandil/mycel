#!/bin/bash
# Test: v1.13.0 features — echo flows, response block, status codes,
#       file upload, request headers, idempotency, async, header stripping
source "$(dirname "$0")/lib.sh"

echo "=== New Features (v1.13.0) ==="

# --- Echo flows (no "to" block, response block) ---

body=$(http_body GET "$BASE/echo/ping")
assert_json "echo GET: status field" ".status" "ok" "$body"
assert_json "echo GET: service field" ".service" "mycel" "$body"

body=$(http_body POST "$BASE/echo/mirror" '{"name":"World"}')
assert_json "echo POST: greeting" ".greeting" "Hello, World" "$body"
assert_json "echo POST: original" ".original" "World" "$body"

# --- Status code overrides ---

status=$(http_status POST "$BASE/echo/created" '{}')
assert_status "status override: 201 Created" "201" "$status"

body=$(curl -s -X POST -H "Content-Type: application/json" -d '{}' "$BASE/echo/created")
assert_json "status override 201: message field" ".message" "resource created" "$body"

status=$(http_status GET "$BASE/echo/not-implemented")
assert_status "status override: 501 Not Implemented" "501" "$status"

# --- Request headers accessible in CEL ---

HEADERS_RESPONSE=$(curl -s -H "X-Tenant-Id: acme" "$BASE/echo/headers")
assert_json "headers: has_headers is true" ".has_headers" "true" "$HEADERS_RESPONSE"

# --- File upload (multipart/form-data) ---

UPLOAD_BODY=$(curl -s -X POST \
  -F "document=@$(dirname "$0")/lib.sh;type=text/plain" \
  -F "name=testfile" \
  "$BASE/echo/upload")
assert_json "upload: has_file is true" ".has_file" "true" "$UPLOAD_BODY"
assert_json "upload: name preserved" ".name" "testfile" "$UPLOAD_BODY"

# --- Idempotency (duplicate prevention) ---

IDEMP_BODY='{"name":"Widget","price":9.99,"sku":"IDEMP-001"}'

# First request should succeed
status1=$(http_status POST "$BASE/idempotent/products" "$IDEMP_BODY")
assert_status "idempotency: first request 200" "200" "$status1"

# Second request with same SKU should return cached result (still 200, not a new row)
body2=$(http_body POST "$BASE/idempotent/products" "$IDEMP_BODY")
status2=$(http_status POST "$BASE/idempotent/products" "$IDEMP_BODY")
assert_status "idempotency: duplicate request 200" "200" "$status2"

# --- Async execution (HTTP 202 + polling) ---

ASYNC_BODY='{"name":"AsyncItem","price":19.99,"sku":"ASYNC-001"}'

# Should return 202 with job_id
ASYNC_RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  -H "Content-Type: application/json" -d "$ASYNC_BODY" "$BASE/async/products")
ASYNC_STATUS=$(echo "$ASYNC_RESPONSE" | tail -1)
ASYNC_JSON=$(echo "$ASYNC_RESPONSE" | sed '$d')
assert_status "async: returns 202 Accepted" "202" "$ASYNC_STATUS"
assert_contains "async: response has job_id" "job_id" "$ASYNC_JSON"

# Poll job status (wait briefly for completion)
JOB_ID=$(echo "$ASYNC_JSON" | jq -r '.job_id')
if [[ -n "$JOB_ID" && "$JOB_ID" != "null" ]]; then
  sleep 2
  JOB_BODY=$(http_body GET "$BASE/jobs/$JOB_ID")
  assert_contains "async: job status available" "status" "$JOB_BODY"
fi

# --- Headers stripped from DB writes ---

HDR_BODY='{"name":"HeaderItem","price":5.00}'
hdr_status=$(curl -so /dev/null -w "%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -H "X-Tenant-Id: tenant-abc" \
  -d "$HDR_BODY" "$BASE/headers/products")
assert_status "headers write: succeeds (headers stripped)" "200" "$hdr_status"

report
