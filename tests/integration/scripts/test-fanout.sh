#!/bin/bash
# Test: Fan-out from source — multiple flows sharing the same from connector+operation
source "$(dirname "$0")/lib.sh"

echo "=== Fan-Out ==="

# ---- REST Fan-Out ----
# Two flows share POST /fanout/create:
#   fanout_primary   → writes to fanout_primary table (returns HTTP response)
#   fanout_secondary → writes to fanout_secondary table (fire-and-forget)

status=$(http_status POST "$BASE/fanout/create" '{"name":"fanout-test"}')
assert_status "POST /fanout/create returns 200" "200" "$status"

# Wait for fire-and-forget goroutine to complete
sleep 2

# Verify primary flow executed (returns the response)
body=$(http_body GET "$BASE/fanout/primary")
assert_contains "Primary flow stored data" "fanout-test" "$body"
assert_contains "Primary flow has correct target" "primary" "$body"

# Verify secondary flow executed (fire-and-forget)
body=$(http_body GET "$BASE/fanout/secondary")
assert_contains "Secondary flow stored data (fire-and-forget)" "fanout-test" "$body"
assert_contains "Secondary flow has correct target" "secondary" "$body"

# ---- MQ Fan-Out ----
# Two consumer flows share the same RabbitMQ queue:
#   fanout_mq_consumer_a → writes with source='flow_a'
#   fanout_mq_consumer_b → writes with source='flow_b'
# Both execute concurrently, message ACKed after both complete.

status=$(http_status POST "$BASE/fanout/mq/publish" '{"message":"fanout-mq-test"}')
assert_status "Publish to fanout queue returns 200" "200" "$status"

# Wait for consumers to process
echo "  Waiting for MQ consumers..."
sleep 4

# Verify both consumer flows processed the message
body=$(http_body GET "$BASE/fanout/mq/results")
assert_contains "Consumer A processed message" "flow_a" "$body"
assert_contains "Consumer B processed message" "flow_b" "$body"

report
