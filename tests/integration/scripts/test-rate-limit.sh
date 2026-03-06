#!/bin/bash
# Test: Rate limiting
# Rate limit is 50 req/s burst=200. We send concurrent requests to exceed the burst.
source "$(dirname "$0")/lib.sh"

echo "=== Rate Limit ==="

# Send 300 concurrent requests to overwhelm burst=200
TMPFILE=$(mktemp)
for i in $(seq 1 300); do
  curl -so /dev/null -w "%{http_code}\n" "$BASE/pg/users" >> "$TMPFILE" 2>/dev/null &
done
wait

GOT_429=false
if grep -q "429" "$TMPFILE"; then
  GOT_429=true
fi
rm -f "$TMPFILE"

if $GOT_429; then
  echo "  ✓ Rate limit triggered (429)"
  ((PASS++))
else
  echo "  ✗ Rate limit not triggered after 300 concurrent requests"
  ((FAIL++))
fi

# Wait for rate limit window to reset
sleep 5

# Verify service recovers
status=$(http_status GET "$BASE/pg/users")
assert_status "Service recovers after rate limit window" "200" "$status"

report
