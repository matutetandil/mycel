#!/bin/bash
# Wait for all services to be ready before running tests

set -e

TIMEOUT=${1:-120}
INTERVAL=3
ELAPSED=0

echo "Waiting for services to be ready (timeout: ${TIMEOUT}s)..."

wait_for() {
  local name="$1" url="$2"
  while true; do
    local code
    code=$(curl -so /dev/null -w '%{http_code}' "$url" 2>/dev/null) && true
    if [[ ${#code} -eq 3 && "$code" != "000" ]]; then
      echo "  ✓ $name ready (HTTP $code)"
      return 0
    fi
    if (( ELAPSED >= TIMEOUT )); then
      echo "  ✗ $name not ready after ${TIMEOUT}s"
      return 1
    fi
    sleep "$INTERVAL"
    ELAPSED=$((ELAPSED + INTERVAL))
  done
}

# Reset elapsed for each check (they run in sequence)
ELAPSED=0; wait_for "Mock server" "http://localhost:${PORT_MOCK:-8888}/health"
ELAPSED=0; wait_for "Mycel REST" "http://localhost:${PORT_REST:-3000}/health"

# Wait for Cosmo Router (federation gateway)
ELAPSED=0; wait_for "Cosmo Router" "http://localhost:${PORT_COSMO:-5000}/health" || true

echo ""
echo "All services ready!"
