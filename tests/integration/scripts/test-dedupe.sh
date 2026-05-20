#!/bin/bash
# Test: dedupe primitive end-to-end through the REST → dedupe → mock pipeline.
# Validates that duplicate content is dropped before reaching the downstream,
# while real changes and distinct keys go through.
source "$(dirname "$0")/lib.sh"

echo "=== Dedupe ==="

# Clear any captured mock requests so the test starts from a known state.
mock_clear

# POST #1 — first message with SKU=A, price=10. Cache is empty → must reach
# the mock.
status=$(http_status POST "$BASE/test/dedupe" '{"sku":"A","name":"Widget","price":10}')
assert_status "POST #1 (new SKU=A, price=10) returns 200" "200" "$status"

# POST #2 — identical to #1. Phase A finds the just-stored fingerprint and
# drops without calling the downstream.
status=$(http_status POST "$BASE/test/dedupe" '{"sku":"A","name":"Widget","price":10}')
assert_status "POST #2 (duplicate of #1) returns 200" "200" "$status"

# POST #3 — SKU=A but different price. Fingerprint differs from stored →
# must reach the mock.
status=$(http_status POST "$BASE/test/dedupe" '{"sku":"A","name":"Widget","price":11}')
assert_status "POST #3 (SKU=A with changed price) returns 200" "200" "$status"

# POST #4 — different SKU (B) with same content shape as A. Different key,
# so the dedupe cache is separate. Must reach the mock.
status=$(http_status POST "$BASE/test/dedupe" '{"sku":"B","name":"Widget","price":10}')
assert_status "POST #4 (different SKU=B) returns 200" "200" "$status"

# POST #5 — duplicate of #4. Should be dropped.
status=$(http_status POST "$BASE/test/dedupe" '{"sku":"B","name":"Widget","price":10}')
assert_status "POST #5 (duplicate of #4) returns 200" "200" "$status"

# Give the writes a moment to land in the mock's request log.
sleep 1

# Count mock captures at /dedupe-target. Expected: exactly 3 (POSTs 1, 3, 4).
# POSTs 2 and 5 should have been dropped by dedupe before the to-block ran.
captures=$(mock_requests "/dedupe-target")
count=$(echo "$captures" | jq 'length' 2>/dev/null)

if [[ "$count" == "3" ]]; then
  echo "  ✓ Mock received exactly 3 downstream calls (2 of 5 deduped)"
  ((PASS++))
else
  echo "  ✗ Mock received $count downstream calls; expected 3 (POSTs 1, 3, 4)"
  echo "    Full capture:"
  echo "$captures" | jq -r '.[] | "      \(.method) \(.path) body=\(.body)"' 2>/dev/null || echo "$captures"
  ((FAIL++))
fi

# Verify the three captured bodies are the right ones — POSTs 1, 3, 4 in
# arrival order. The mock stores body as a JSON string; parse it with a
# second jq pass so key ordering is irrelevant.
expected_skus=("A" "A" "B")
expected_prices=("10" "11" "10")
labels=(
  "POST #1 (SKU=A price=10)"
  "POST #3 (SKU=A price=11)"
  "POST #4 (SKU=B price=10)"
)
for i in 0 1 2; do
  body=$(echo "$captures" | jq -r ".[$i].body" 2>/dev/null)
  sku=$(echo "$body" | jq -r '.sku' 2>/dev/null)
  price=$(echo "$body" | jq -r '.price' 2>/dev/null)
  if [[ "$sku" == "${expected_skus[$i]}" && "$price" == "${expected_prices[$i]}" ]]; then
    echo "  ✓ Mock capture #$((i+1)) is ${labels[$i]}"
    ((PASS++))
  else
    echo "  ✗ Mock capture #$((i+1)) expected ${labels[$i]} but body was: $body"
    ((FAIL++))
  fi
done

# Cleanup so a re-run starts from a clean mock state.
mock_clear

report
