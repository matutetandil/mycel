#!/bin/bash
# Test: Kafka publish/consume
source "$(dirname "$0")/lib.sh"

echo "=== Kafka ==="

# Publish message via REST
status=$(http_status POST "$BASE/mq/kafka/publish" '{"message":"hello-kafka","source":"integration-test"}')
assert_status "Publish to Kafka returns 200" "200" "$status"

# Wait for consumer to process (Kafka consumer groups need time to rebalance)
echo "  Waiting for consumer to process message..."
found=false
for i in $(seq 1 20); do
  body=$(http_body GET "$BASE/mq/kafka/results")
  if echo "$body" | grep -qE "kafka|received"; then
    found=true
    break
  fi
  sleep 3
done

if $found; then
  echo "  ✓ Message consumed and stored"
  ((PASS++))
else
  echo "  ✗ Message consumed and stored (missing 'kafka|received')"
  ((FAIL++))
fi

report
