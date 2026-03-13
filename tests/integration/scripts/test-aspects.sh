#!/bin/bash
# Test: Aspect response enrichment (body fields + HTTP headers)
source "$(dirname "$0")/lib.sh"

echo "=== Aspects ==="

# Seed data
http_body POST "$BASE/aspects/init" '{}' > /dev/null 2>&1
sleep 1

# Add a second product for list tests
http_body POST "$BASE/aspects/products" '{"name":"Gadget","price":25.00}' > /dev/null 2>&1
sleep 0.5

# --- v1 deprecation headers ---

# Capture full headers + body for v1 endpoint
V1_RESPONSE=$(curl -si "$BASE/aspects/v1/products" 2>/dev/null)
V1_HEADERS=$(echo "$V1_RESPONSE" | sed -n '1,/^\r\{0,1\}$/p')
V1_BODY=$(echo "$V1_RESPONSE" | sed '1,/^\r\{0,1\}$/d')

# Check HTTP headers
assert_contains "v1: Deprecation header present" "Deprecation: true" "$V1_HEADERS"
assert_contains "v1: Sunset header present" "Sunset:" "$V1_HEADERS"
assert_contains "v1: X-API-Version header is v1" "X-Api-Version: v1" "$V1_HEADERS"

# Check body enrichment
assert_contains "v1: _warning field in body" "_warning" "$V1_BODY"
assert_contains "v1: deprecation message in body" "deprecated" "$V1_BODY"

# Check original data still present
assert_contains "v1: product data preserved" "Widget" "$V1_BODY"

# --- v2 should NOT have deprecation ---

V2_RESPONSE=$(curl -si "$BASE/aspects/v2/products" 2>/dev/null)
V2_HEADERS=$(echo "$V2_RESPONSE" | sed -n '1,/^\r\{0,1\}$/p')
V2_BODY=$(echo "$V2_RESPONSE" | sed '1,/^\r\{0,1\}$/d')

# v2 must not have deprecation headers
assert_not_contains "v2: no Deprecation header" "Deprecation:" "$V2_HEADERS"
assert_not_contains "v2: no Sunset header" "Sunset:" "$V2_HEADERS"
assert_not_contains "v2: no _warning in body" "_warning" "$V2_BODY"

# v2 data still works
assert_contains "v2: product data present" "Widget" "$V2_BODY"

# --- list_* metadata header ---

LIST_RESPONSE=$(curl -si "$BASE/aspects/products" 2>/dev/null)
LIST_HEADERS=$(echo "$LIST_RESPONSE" | sed -n '1,/^\r\{0,1\}$/p')

assert_contains "list: X-Result-Type header present" "X-Result-Type: list" "$LIST_HEADERS"

# list endpoint should NOT have deprecation headers (it's list_products, not *_v1)
assert_not_contains "list: no Deprecation header" "Deprecation:" "$LIST_HEADERS"

report
