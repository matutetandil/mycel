#!/bin/bash
# Test: Redis Pub/Sub publish/subscribe
source "$(dirname "$0")/lib.sh"

echo "=== Redis Pub/Sub ==="

# Publish message via REST
status=$(http_status POST "$BASE/mq/redis/publish" '{"message":"hello-redis","source":"integration-test"}')
assert_status "Publish to Redis Pub/Sub returns 200" "200" "$status"

# Wait for subscriber to process
echo "  Waiting for subscriber to process message..."
found=false
for i in $(seq 1 10); do
  body=$(http_body GET "$BASE/mq/redis/results")
  if echo "$body" | grep -qE "redis-pubsub|received"; then
    found=true
    break
  fi
  sleep 2
done

if $found; then
  echo "  ✓ Message consumed and stored"
  ((PASS++))
else
  echo "  ✗ Message consumed and stored (missing 'redis-pubsub|received')"
  ((FAIL++))
fi

report
