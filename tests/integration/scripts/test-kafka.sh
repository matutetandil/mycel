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


# --- A message the filter turns away ----------------------------------------
#
# Kafka cannot put a message back: an offset moves forward and does not come
# back. So a rejected message is republished onto <topic>.dlq, and that path
# needs a broker — building a writer and putting a message on another topic is
# precisely what a unit test cannot reach.

echo "=== Kafka dead-lettering ==="

REF="dlq-$$-$RANDOM"

# One the filter accepts, so the run also proves the filter is not simply
# turning everything away.
status=$(http_status POST "$BASE/mq/kafka/dlq" "{\"reference\":\"$REF-kept\",\"wanted\":true}")
assert_status "Publishing a message the filter accepts" "200" "$status"

# And one it does not.
status=$(http_status POST "$BASE/mq/kafka/dlq" "{\"reference\":\"$REF-turned-away\",\"wanted\":false}")
assert_status "Publishing a message the filter turns away" "200" "$status"

echo "  Waiting for the consumers to settle..."
kept=false
dead=false
for i in $(seq 1 25); do
  body=$(http_body GET "$BASE/pg/items")
  echo "$body" | grep -q "$REF-kept" && kept=true
  echo "$body" | grep -q "$REF-turned-away" && dead=true
  $kept && $dead && break
  sleep 3
done

if $kept; then
  echo "  ✓ The message the filter accepts reached the flow"
  ((PASS++))
else
  echo "  ✗ The message the filter accepts reached the flow"
  ((FAIL++))
fi

# The one nobody wanted is not dropped: it is on the dead-letter topic, where
# somebody can look at it. Dropping it silently is how a message disappears
# with nothing to show for it.
if $dead; then
  echo "  ✓ The rejected message was republished to the dead-letter topic"
  ((PASS++))
else
  echo "  ✗ The rejected message was republished to the dead-letter topic"
  ((FAIL++))
fi

report
