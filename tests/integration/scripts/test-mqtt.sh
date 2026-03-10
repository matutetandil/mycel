#!/bin/bash
# Test: MQTT publish/subscribe
source "$(dirname "$0")/lib.sh"

echo "=== MQTT ==="

# Publish message via REST
status=$(http_status POST "$BASE/mqtt/publish" '{"message":"hello-mqtt","source":"integration-test"}')
assert_status "Publish to MQTT returns 200" "200" "$status"

# Wait for subscriber to process
echo "  Waiting for subscriber to process message..."
found=false
for i in $(seq 1 10); do
  body=$(http_body GET "$BASE/mqtt/results")
  if echo "$body" | grep -qE "mqtt|received"; then
    found=true
    break
  fi
  sleep 2
done

if $found; then
  echo "  ✓ Message subscribed and stored"
  ((PASS++))
else
  echo "  ✗ Message subscribed and stored (missing 'mqtt|received')"
  ((FAIL++))
fi

report
