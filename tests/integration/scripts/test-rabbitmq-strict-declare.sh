#!/bin/bash
# Test: RabbitMQ strict declare (v2.0.0 contract)
# Drives `go test` against the docker-compose RabbitMQ broker to validate
# that setupTopology fails fast on a missing queue/exchange when
# create_if_missing is false (the new v2.0.0 default), and succeeds when
# the resource exists or when create_if_missing is true.
source "$(dirname "$0")/lib.sh"

echo "=== RabbitMQ Strict Declare (v2.0.0) ==="

# Require Go on the host — the test runs against the broker via TCP from
# outside the docker network.
if ! command -v go >/dev/null 2>&1; then
  echo "  ✗ Go toolchain not found on PATH; cannot run strict-declare integration tests"
  FAIL=$((FAIL + 1))
  report
  return $FAIL 2>/dev/null || exit $FAIL
fi

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
RABBIT_URL="amqp://guest:guest@localhost:${PORT_RABBIT:-5672}/"

output=$(cd "$REPO_ROOT" && \
  MYCEL_TEST_RABBITMQ_URL="$RABBIT_URL" \
  go test -count=1 -v \
    -run '^TestSetupTopologyStrictDeclare_Integration$' \
    ./internal/connector/mq/rabbitmq/... 2>&1)
exit_code=$?

if [[ $exit_code -eq 0 ]]; then
  # Confirm the test actually ran — not just skipped because the broker
  # wasn't reachable. A skipped run still exits 0.
  if echo "$output" | grep -q "^--- SKIP:"; then
    echo "  ✗ Test was SKIPPED (broker unreachable at $RABBIT_URL)"
    echo "$output" | grep -A1 "^=== RUN" | head -6
    FAIL=$((FAIL + 1))
  else
    # Count sub-tests that passed.
    passed=$(echo "$output" | grep -c "^    --- PASS:")
    if [[ $passed -ge 4 ]]; then
      echo "  ✓ Missing queue with default fails fast"
      echo "  ✓ Missing queue with create_if_missing=true is declared"
      echo "  ✓ Existing queue with default boots cleanly"
      echo "  ✓ Missing exchange with default fails fast"
      PASS=$((PASS + 4))
    else
      echo "  ✗ Only $passed of 4 sub-tests passed"
      echo "$output" | tail -30
      FAIL=$((FAIL + 1))
    fi
  fi
else
  echo "  ✗ go test exited with code $exit_code"
  echo "$output" | tail -40
  FAIL=$((FAIL + 1))
fi

report
