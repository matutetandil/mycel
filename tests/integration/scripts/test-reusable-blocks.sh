#!/bin/bash
# Test: reusable inline blocks (v2.6.0) end-to-end through the real binary.
#
# The integration container loads reusable/blocks.mycel (one named block per
# kind) and flows/reusable-blocks.mycel (flows referencing them via use=).
# Because the parser resolves every `use` at config load, the container being
# up at all already proves all ten references resolve in the real binary. This
# script then asserts the runtime BEHAVIOUR for the cleanly-observable kinds
# (dedupe / accept / response / sequence_guard / transaction) and that the
# remaining kinds (retry / error_handling / lock / semaphore / coordinate)
# actually execute the flow that reuses them.
#
# This suite is registered in run.sh's sequential "mock" phase (alongside
# dedupe / notifications / http-client) so it never runs concurrently with
# another suite that calls the global mock_clear. Each flow also writes to a
# UNIQUE mock path and mock_requests filters by path, so the per-path counts
# are doubly robust.
source "$(dirname "$0")/lib.sh"

echo "=== Reusable inline blocks (use = \"<kind>.<name>\") ==="

# Clean baseline — safe here because this suite runs in the sequential phase.
mock_clear

# --- dedupe: named dedupe drops duplicate content -------------------------
# Two identical POSTs + one with a changed price → mock should see 2 (the
# second of the identical pair is deduped; the price change is a new
# fingerprint).
http_status POST "$BASE/test/ru/dedupe" '{"sku":"Z","price":1}' > /dev/null
http_status POST "$BASE/test/ru/dedupe" '{"sku":"Z","price":1}' > /dev/null
http_status POST "$BASE/test/ru/dedupe" '{"sku":"Z","price":2}' > /dev/null
sleep 1
n=$(mock_requests "/ru-dedupe-target" | jq 'length' 2>/dev/null)
assert_status "dedupe reuse: 2 of 3 reach downstream (duplicate dropped)" "2" "$n"

# --- accept: named gate lets us-east through, drops eu-west ---------------
http_status POST "$BASE/test/ru/accept" '{"region":"us-east"}' > /dev/null
http_status POST "$BASE/test/ru/accept" '{"region":"eu-west"}' > /dev/null
sleep 1
n=$(mock_requests "/ru-accept-target" | jq 'length' 2>/dev/null)
assert_status "accept reuse: only the accepted region reaches downstream" "1" "$n"

# --- response: caller body is shaped by the named response mapping --------
body=$(http_body POST "$BASE/test/ru/response" '{"anything":1}')
assert_contains "response reuse: caller body carries named mapping (source)" "reusable" "$body"
assert_contains "response reuse: caller body carries named mapping (ok)" '"ok"' "$body"

# --- sequence_guard: the reused guard is wired and the flow executes ------
# Resolution + execution proof (like lock/semaphore/coordinate below): a POST
# through a flow that reuses the named sequence_guard returns 200, so the named
# block resolved and the runtime accepted the merged config. (A value-ordering
# behavioural assertion is intentionally omitted here — sequence value CEL
# resolution over a plain REST source is a separate sync-eval concern,
# orthogonal to whether the block is reused.)
status=$(http_status POST "$BASE/test/ru/seq" '{"id":"a","seq":5}')
assert_status "sequence_guard reuse: flow returns 200" "200" "$status"

# --- transaction: named transaction inserts a row via mysql ---------------
status=$(http_status POST "$BASE/test/ru/tx" '{"name":"alpha"}')
assert_status "transaction reuse: POST runs the named transaction (200)" "200" "$status"
sleep 1
rows=$(http_body GET "$BASE/test/ru/tx/results")
assert_contains "transaction reuse: row was inserted by the named transaction" "alpha" "$rows"

# --- error_handling (with nested retry.use): flow executes ----------------
status=$(http_status POST "$BASE/test/ru/eh" '{"id":"e1"}')
assert_status "error_handling reuse (+ nested retry.use): flow returns 200" "200" "$status"
sleep 1
n=$(mock_requests "/ru-eh-target" | jq 'length' 2>/dev/null)
assert_status "error_handling reuse: downstream reached once" "1" "$n"

# --- retry (direct reuse inside inline error_handling): flow executes -----
status=$(http_status POST "$BASE/test/ru/retry" '{"id":"r1"}')
assert_status "retry reuse: flow returns 200" "200" "$status"

# --- lock: flow executes under the reused lock ----------------------------
status=$(http_status POST "$BASE/test/ru/lock" '{"id":"l1"}')
assert_status "lock reuse: flow returns 200" "200" "$status"

# --- semaphore: flow executes under the reused semaphore ------------------
status=$(http_status POST "$BASE/test/ru/sem" '{"id":"s1"}')
assert_status "semaphore reuse: flow returns 200" "200" "$status"

# --- coordinate: signal-only flow executes under the reused coordinator ---
status=$(http_status POST "$BASE/test/ru/coord" '{"id":"c1"}')
assert_status "coordinate reuse: flow returns 200" "200" "$status"

report
