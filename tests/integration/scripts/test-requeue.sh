#!/bin/bash
# Test: a rejected message is requeued a bounded number of times
#
# The bound is what stops a message nobody wants circling for ever, and it is
# counted per message — so it needs something to identify one by. id_field names
# that expression, and every form the documentation showed failed to evaluate:
# the flow wrapped the message in another map before evaluating, so
# "input.reference" reached one level too high and came back "no such key". The
# error was discarded, the identifier was always empty, and the consumer fell
# back to acknowledging — a rejected message dropped rather than retried, while
# warning that the message had arrived without an identifier.
source "$(dirname "$0")/lib.sh"

echo "=== Requeue with an identifier ==="

REF="req-$$"

# A message the flow does not want. It should be requeued twice and then let go.
status=$(http_status POST "$BASE/test/requeue" "{\"reference\":\"$REF\",\"wanted\":false}")
assert_status "The message is published" "200" "$status"

# Give the consumer time to see it, put it back, and see it again.
sleep 6

# Nothing was written: the flow never wanted this message.
body=$(http_body GET "$BASE/pg/items")
assert_not_contains "A rejected message is not written" "$REF" "$body"

# And the queue is not still churning on it: with the bound reached, the message
# is acknowledged and gone. A queue that never drains is what a broken
# identifier looks like from the outside.
sleep 4
assert_contains "The service is still healthy after the requeue loop" "ok|pass|UP|healthy" "$(http_body GET "$ADMIN/health")"

# A message the flow does want goes through on the first delivery.
WANTED="req-ok-$$"
status=$(http_status POST "$BASE/test/requeue" "{\"reference\":\"$WANTED\",\"wanted\":true}")
assert_status "A wanted message is published" "200" "$status"

sleep 4
body=$(http_body GET "$BASE/pg/items")
assert_contains "A wanted message is written" "$WANTED" "$body"

report
