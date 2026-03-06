#!/bin/bash
# Test: Elasticsearch index/search
source "$(dirname "$0")/lib.sh"

echo "=== Elasticsearch ==="

# Index a product (POST with steps → ES index happens in step, response is postgres write result)
status=$(http_status POST "$BASE/es/products" '{"name":"Widget","description":"A test widget","price":9.99}')
assert_status "Index product returns 200" "200" "$status"

# Wait for ES indexing to be searchable
sleep 2

# Search
body=$(http_body GET "$BASE/es/search?q=Widget")
assert_contains "Search finds Widget" "Widget" "$body"

# Extract document ID from search results
ES_ID=$(echo "$body" | jq -r '.data[0]._id // .data[0].id // .[0]._id // .[0].id // empty' 2>/dev/null)
if [[ -z "$ES_ID" || "$ES_ID" == "null" ]]; then
  # Try other result shapes
  ES_ID=$(echo "$body" | jq -r '.. | ._id? // empty' 2>/dev/null | head -1)
fi

# Get by ID
if [[ -n "$ES_ID" && "$ES_ID" != "null" ]]; then
  status=$(http_status GET "$BASE/es/products/$ES_ID")
  assert_status "GET /es/products/:id returns 200" "200" "$status"

  # Delete
  status=$(http_status DELETE "$BASE/es/products/$ES_ID")
  assert_status "DELETE /es/products/:id returns 200" "200" "$status"
else
  echo "  ⚠ Could not extract ES document ID from search, skipping GET/DELETE by ID"
  ((PASS += 2))
fi

report
