#!/bin/bash
# Master integration test runner
# Usage: bash run.sh [--keep] [--skip-build] [--sequential] [test-name...]
#   --keep        Don't tear down services after tests
#   --skip-build  Skip docker compose build
#   --sequential  Run tests one by one (default: parallel)
#   test-name     Run only specific test(s), e.g. "postgres graphql"

set -uo pipefail

cd "$(dirname "$0")"

KEEP=false
SKIP_BUILD=false
SEQUENTIAL=false
SPECIFIC_TESTS=()

for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=true ;;
    --skip-build) SKIP_BUILD=true ;;
    --sequential) SEQUENTIAL=true ;;
    *) SPECIFIC_TESTS+=("$arg") ;;
  esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[2m'
NC='\033[0m'

TOTAL_PASS=0
TOTAL_FAIL=0
FAILED_SUITES=()

# Always clean up on exit (Ctrl+C, errors, etc.) unless --keep
cleanup() {
  if ! $KEEP; then
    echo ""
    echo "Cleaning up..."
    docker compose down -v 2>&1 | tail -3
  fi
  # Clean up temp dir
  [[ -n "${TMPDIR_TESTS:-}" ]] && rm -rf "$TMPDIR_TESTS"
}
trap cleanup EXIT

echo "======================================"
echo "  Mycel Integration Test Suite"
echo "======================================"
echo ""

# ---------------------------------------------------------------------------
# Port auto-configuration: scan required ports and remap if busy
# ---------------------------------------------------------------------------
find_available_port() {
  local port=$1
  while lsof -i :"$port" -sTCP:LISTEN >/dev/null 2>&1; do
    ((port++))
  done
  echo "$port"
}

# VAR_NAME:DEFAULT_PORT pairs (only ports exposed to host)
# Infrastructure services (postgres, mysql, redis, etc.) use Docker internal
# networking and don't need host ports.
PORT_DEFS=(
  PORT_REST:3000
  PORT_GQL:4000
  PORT_SOAP:8081
  PORT_GRPC:50051
  PORT_ADMIN:9090
  PORT_COSMO:5000
  PORT_MOCK:8888
)

echo "Checking ports..."
REMAPPED=0
for entry in "${PORT_DEFS[@]}"; do
  var="${entry%%:*}"
  default="${entry##*:}"
  actual=$(find_available_port "$default")
  export "$var=$actual"
  if [[ "$actual" != "$default" ]]; then
    echo -e "  ${YELLOW}:$default busy → remapped to :$actual ($var)${NC}"
    ((REMAPPED++))
  fi
done

if [[ $REMAPPED -eq 0 ]]; then
  echo -e "  ${GREEN}All ports available${NC}"
else
  echo -e "  ${CYAN}$REMAPPED port(s) remapped${NC}"
fi
echo ""

# Step 1: Start services
echo "Starting services..."
BUILD_FLAG=""
if ! $SKIP_BUILD; then
  BUILD_FLAG="--build"
fi
docker compose up -d $BUILD_FLAG --wait 2>&1 | tail -5

# Step 2: Wait for services
echo ""
bash scripts/wait-for-services.sh 120
echo ""

# Step 3: Determine which tests to run
if [[ ${#SPECIFIC_TESTS[@]} -gt 0 ]]; then
  TEST_FILES=()
  for name in "${SPECIFIC_TESTS[@]}"; do
    TEST_FILES+=("scripts/test-${name}.sh")
  done
else
  TEST_FILES=(
    scripts/test-health.sh
    scripts/test-metrics.sh
    scripts/test-postgres.sh
    scripts/test-mysql.sh
    scripts/test-mongodb.sh
    scripts/test-sqlite.sh
    scripts/test-graphql.sh
    scripts/test-grpc.sh
    scripts/test-soap.sh
    scripts/test-cache.sh
    scripts/test-rabbitmq.sh
    scripts/test-kafka.sh
    scripts/test-elasticsearch.sh
    scripts/test-s3.sh
    scripts/test-files.sh
    scripts/test-http-client.sh
    scripts/test-transforms.sh
    scripts/test-validation.sh
    scripts/test-steps.sh
    scripts/test-error-handling.sh
    scripts/test-rate-limit.sh
    scripts/test-notifications.sh
    scripts/test-exec.sh
    scripts/test-filter.sh
    scripts/test-federation.sh
  )
fi

# ---------------------------------------------------------------------------
# Helper: parse pass/fail counts from test output
# ---------------------------------------------------------------------------
parse_results() {
  local output="$1"
  pass=$(echo "$output" | grep "Results:" | sed -n 's/.*\([0-9][0-9]*\) passed.*/\1/p')
  fail=$(echo "$output" | grep "Results:" | sed -n 's/.*\([0-9][0-9]*\) failed.*/\1/p')
  pass=${pass:-0}
  fail=${fail:-0}

  TOTAL_PASS=$((TOTAL_PASS + pass))
  TOTAL_FAIL=$((TOTAL_FAIL + fail))

  if [[ "$fail" -gt 0 ]]; then
    local suite_name
    suite_name=$(basename "$2" .sh | sed 's/test-//')
    FAILED_SUITES+=("$suite_name")
  fi
}

# ---------------------------------------------------------------------------
# Step 4: Run tests
# ---------------------------------------------------------------------------
if $SEQUENTIAL; then
  # Sequential mode: run one by one (original behavior)
  for test_file in "${TEST_FILES[@]}"; do
    if [[ ! -f "$test_file" ]]; then
      echo -e "${YELLOW}warning: $test_file not found, skipping${NC}"
      continue
    fi

    echo "--------------------------------------"
    output=$(bash "$test_file" 2>&1)
    echo "$output"
    parse_results "$output" "$test_file"
  done
else
  # Parallel mode (default)
  # Tests are split into three phases:
  #   Phase 1 — parallel:    all independent tests
  #   Phase 2 — sequential:  mock-dependent tests (share mock_clear)
  #   Phase 3 — solo:        rate-limit (triggers 429 server-wide)
  TMPDIR_TESTS=$(mktemp -d)
  START_TIME=$(date +%s)

  PREFLIGHT_FILES=()
  PARALLEL_FILES=()
  MOCK_FILES=()
  SOLO_FILES=()
  ALL_NAMES_ORDERED=()

  for test_file in "${TEST_FILES[@]}"; do
    name=$(basename "$test_file" .sh | sed 's/test-//')
    ALL_NAMES_ORDERED+=("$name")

    if [[ ! -f "$test_file" ]]; then
      echo -e "${YELLOW}warning: $test_file not found, skipping${NC}"
      continue
    fi

    case "$name" in
      health|metrics)
        PREFLIGHT_FILES+=("$test_file")
        ;;
      rate-limit)
        SOLO_FILES+=("$test_file")
        ;;
      http-client|notifications)
        MOCK_FILES+=("$test_file")
        ;;
      *)
        PARALLEL_FILES+=("$test_file")
        ;;
    esac
  done

  # Phase 1: Preflight — health/metrics run first on a clean server
  for test_file in "${PREFLIGHT_FILES[@]}"; do
    name=$(basename "$test_file" .sh | sed 's/test-//')
    echo "--------------------------------------"
    output=$(bash "$test_file" 2>&1)
    echo "$output"
    parse_results "$output" "$test_file"
  done

  # Phase 2: Parallel — all independent tests + mock group
  PIDS=()

  for test_file in "${PARALLEL_FILES[@]}"; do
    name=$(basename "$test_file" .sh | sed 's/test-//')
    bash "$test_file" > "$TMPDIR_TESTS/$name.out" 2>&1 &
    PIDS+=($!)
  done

  # Mock-dependent tests run sequentially within a single background job
  if [[ ${#MOCK_FILES[@]} -gt 0 ]]; then
    (
      for test_file in "${MOCK_FILES[@]}"; do
        name=$(basename "$test_file" .sh | sed 's/test-//')
        bash "$test_file" > "$TMPDIR_TESTS/$name.out" 2>&1
      done
    ) &
    PIDS+=($!)
  fi

  PARALLEL_COUNT=$(( ${#PARALLEL_FILES[@]} + ${#MOCK_FILES[@]} ))
  echo -e "${CYAN}Running $PARALLEL_COUNT test suites in parallel...${NC}"

  # Wait for all parallel tests
  for pid in "${PIDS[@]}"; do
    wait "$pid" 2>/dev/null
  done

  PHASE1_TIME=$(( $(date +%s) - START_TIME ))

  # Display parallel results in original order
  for name in "${ALL_NAMES_ORDERED[@]}"; do
    # Skip tests already displayed (preflight) or deferred (solo)
    [[ "$name" == "health" || "$name" == "metrics" || "$name" == "rate-limit" ]] && continue
    [[ -f "$TMPDIR_TESTS/$name.out" ]] || continue

    echo "--------------------------------------"
    output=$(cat "$TMPDIR_TESTS/$name.out")
    echo "$output"
    parse_results "$output" "test-$name.sh"
  done

  # Phase 3: Solo — rate-limit must run alone (triggers 429 server-wide)
  for test_file in "${SOLO_FILES[@]}"; do
    echo "--------------------------------------"
    output=$(bash "$test_file" 2>&1)
    echo "$output"
    parse_results "$output" "$test_file"
  done

  TOTAL_TIME=$(( $(date +%s) - START_TIME ))
fi

# Step 5: Summary
echo ""
echo "======================================"
echo "  FINAL RESULTS"
echo "======================================"
echo -e "  Passed: ${GREEN}${TOTAL_PASS}${NC}"
echo -e "  Failed: ${RED}${TOTAL_FAIL}${NC}"

if [[ ${#FAILED_SUITES[@]} -gt 0 ]]; then
  echo -e "  Failed suites: ${RED}${FAILED_SUITES[*]}${NC}"
fi

if ! $SEQUENTIAL && [[ -n "${TOTAL_TIME:-}" ]]; then
  echo -e "  ${DIM}Parallel: ${PHASE1_TIME}s  |  Total: ${TOTAL_TIME}s${NC}"
fi

echo "======================================"

exit "$TOTAL_FAIL"
