#!/bin/bash
# Test: a sanitiser the runtime does not ship
#
# The built-in pipeline refuses what everybody has to refuse — SQL fragments,
# XML entities, null bytes. What counts as sensitive in a particular business
# is what the WASM extension point is for, and nothing exercised it end to end:
# a plugin declaring a sanitiser, the runtime registering it, and a request
# being masked or turned away before any flow runs.
#
# The module is Rust, compiled and committed beside its manifest. It masks card
# numbers and refuses New Zealand tax numbers.
source "$(dirname "$0")/lib.sh"

echo "=== Custom sanitiser ==="

# --- Masked before the flow sees it -----------------------------------------

body=$(http_body POST "$BASE/sanitized" '{"reference":"ORD-1","card":"4111 1111 1111 1111"}')

if echo "$body" | grep -q '\*\*\*\*1111'; then
  echo "  ✓ A card number is masked before the flow reads it"
  ((PASS++))
else
  echo "  ✗ A card number is masked before the flow reads it"
  echo "    answer: $body"
  ((FAIL++))
fi

# The number itself must not appear anywhere in the answer.
assert_not_contains "The number the caller sent is gone" "4111 1111 1111 1111" "$body"

# What the sanitiser had no reason to touch is untouched.
assert_contains "What it does not care about is left alone" "ORD-1" "$body"

# --- Turned away entirely ---------------------------------------------------

# A refusal happens before any flow runs, which is the difference between this
# and filtering a response: what is refused never reaches a flow or a log.
status=$(http_status POST "$BASE/sanitized" '{"reference":"ORD-2","tax_number":"123-456-789"}')
assert_not_contains "A value the sanitiser refuses never reaches the flow" "^200$" "$status"

report
