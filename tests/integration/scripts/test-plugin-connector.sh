#!/bin/bash
# Test: a connector type the runtime does not ship
#
# The plugin system had a connector interface nothing had ever been built
# against, and two things stood in the way of anyone building one: the
# documented calling convention was a multi-value return no toolchain can emit,
# and the parser rejected the attributes a plugin declares for itself, so the
# connector could not be configured even once it loaded.
#
# This runs a real one — Rust, compiled to WebAssembly, committed beside its
# manifest. Everything it does lives in the module: the type name, the
# attributes the connector block takes, what a read answers, and the rule that
# refuses a reservation larger than the stock.
source "$(dirname "$0")/lib.sh"

echo "=== Plugin connector ==="

# --- Reading -----------------------------------------------------------------

body=$(http_body GET "$BASE/plugin/stock")
assert_contains "A read reaches the module" "WIDGET-1" "$body"

# The connector block's attributes reached the module. They are declared in the
# plugin's manifest, not in Mycel, and used to be refused by the parser.
assert_contains "The connector's own configuration reached it" "integration" "$body"

# --- An operation that is neither a read nor a write -------------------------

body=$(http_body POST "$BASE/plugin/stock/reserve" '{"sku":"WIDGET-1","quantity":4}')
assert_contains "A step reaches the module's call" "reserved" "$body"

# The module kept state between calls, which is what a connector does.
body=$(http_body GET "$BASE/plugin/stock")
if echo "$body" | jq -e '.[] | select(.sku == "WIDGET-1") | select(.on_hand == 6)' > /dev/null 2>&1; then
  echo "  ✓ What the module was asked to do, it did"
  ((PASS++))
else
  echo "  ✗ What the module was asked to do, it did"
  echo "    stock: $body"
  ((FAIL++))
fi

# --- The module's own rule ---------------------------------------------------

# Refusing is the reason to write a connector for your own system, so the
# refusal has to reach the caller rather than being read as an empty answer.
body=$(http_body POST "$BASE/plugin/stock/reserve" '{"sku":"WIDGET-2","quantity":999}')
assert_contains "The module's refusal reaches the caller" "on hand" "$body"

# --- Writing -----------------------------------------------------------------

body=$(http_body POST "$BASE/plugin/stock" '{"sku":"WIDGET-2","delta":25}')
assert_contains "A write reaches the module" "28" "$body"

report
