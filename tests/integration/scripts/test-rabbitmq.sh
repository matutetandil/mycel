#!/bin/bash
# Test: RabbitMQ publish/consume
source "$(dirname "$0")/lib.sh"

echo "=== RabbitMQ ==="

# Publish message via REST
status=$(http_status POST "$BASE/mq/rabbit/publish" '{"message":"hello-rabbit","source":"integration-test"}')
assert_status "Publish to RabbitMQ returns 200" "200" "$status"

# Wait for consumer to process
echo "  Waiting for consumer to process message..."
sleep 3

# Check results in DB
body=$(http_body GET "$BASE/mq/rabbit/results")
assert_contains "Message consumed and stored" "rabbitmq|received" "$body"

report
