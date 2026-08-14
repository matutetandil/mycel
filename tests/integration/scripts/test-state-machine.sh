#!/bin/bash
# Test: an entity whose lifecycle is a state machine
#
# The engine reads the entity's current state from its status column, refuses
# an event the state does not allow, evaluates the guard, and writes the new
# state back. Every one of those is a round trip through a real database, so a
# unit test with a fake connector proves none of it — and until this suite
# existed, nothing exercised a state machine against one at all.
source "$(dirname "$0")/lib.sh"

echo "=== State machines ==="

# --- A transition the machine allows ----------------------------------------

body=$(http_body POST "$BASE/orders/order-pending/status" '{"event":"pay"}')
assert_contains "An allowed event is accepted" "paid" "$body"

# And it reached the database, which is the point: the next request has to see
# the state this one left behind.
body=$(http_body GET "$BASE/machine-orders")
if echo "$body" | jq -e '.[] | select(.id == "order-pending") | select(.status == "paid")' > /dev/null 2>&1; then
  echo "  ✓ The new state was written to the database"
  ((PASS++))
else
  echo "  ✗ The new state was written to the database"
  echo "    rows: $body"
  ((FAIL++))
fi

# --- One it does not --------------------------------------------------------

# order-paid is paid, and a paid order cannot be paid again. Accepting it would
# let an entity move anywhere from anywhere, which is the whole thing a state
# machine is there to prevent.
status=$(http_status POST "$BASE/orders/order-paid/status" '{"event":"pay"}')
assert_not_contains "An event the current state does not allow is refused" "^200$" "$status"

# --- A guard ----------------------------------------------------------------

# Shipping needs a tracking number. Without one the transition is refused, and
# the entity stays where it was.
status=$(http_status POST "$BASE/orders/order-paid/status" '{"event":"ship","tracking_number":""}')
assert_not_contains "A transition whose guard rejects is refused" "^200$" "$status"

body=$(http_body GET "$BASE/machine-orders")
if echo "$body" | jq -e '.[] | select(.id == "order-paid") | select(.status == "paid")' > /dev/null 2>&1; then
  echo "  ✓ A refused transition left the entity where it was"
  ((PASS++))
else
  echo "  ✗ A refused transition left the entity where it was"
  echo "    rows: $body"
  ((FAIL++))
fi

# With one, it goes through.
body=$(http_body POST "$BASE/orders/order-paid/status" '{"event":"ship","tracking_number":"NZ-9911"}')
assert_contains "A transition whose guard passes goes through" "shipped" "$body"

# --- A final state ----------------------------------------------------------

# Nothing leaves a final state, whatever event arrives.
status=$(http_status POST "$BASE/orders/order-delivered/status" '{"event":"pay"}')
assert_not_contains "Nothing leaves a final state" "^200$" "$status"

# --- An entity nobody has seen ----------------------------------------------

# An id with no row starts at the machine's initial state rather than failing:
# that is how the first transition of a new entity works.
status=$(http_status POST "$BASE/orders/order-brand-new/status" '{"event":"cancel"}')
assert_status "An entity with no row yet starts at the initial state" "200" "$status"

report
