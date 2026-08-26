#!/bin/bash
# Mycel Benchmark Runner (Parallel Architecture)
#
# Usage:
#   ./run.sh cloud                     Full auto: deploy 5 VPS -> test from attacker VPS -> collect -> destroy
#   ./run.sh cloud-local               Full auto: deploy 4 VPS -> test from your machine -> collect -> destroy
#   ./run.sh local <target-ip>         Manual: run k6 locally against an existing target
#   ./run.sh docker <target-ip>        Manual: run k6 via Docker against an existing target
#   ./run.sh remote <target-ip>        Manual: run k6 from attacker VPS against an existing target
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
RESULTS_BASE="${SCRIPT_DIR}/results/${RUN_ID}"

# SSH options: skip host key checking entirely (IPs get reused across deploys)
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR"
MODE="${1:-}"
TARGET_IP="${2:-}"
TEST_MODE="standard"  # standard | realistic | stress | full

# Parse flags from any position
for arg in "$@"; do
  case "$arg" in
    --realistic) TEST_MODE="realistic" ;;
    --stress)    TEST_MODE="stress" ;;
    --full)      TEST_MODE="full" ;;
  esac
done

# Fix TARGET_IP if a flag was in $2
if [[ "$TARGET_IP" == "--stress" || "$TARGET_IP" == "--full" || "$TARGET_IP" == "--realistic" ]]; then
  TARGET_IP="${3:-}"
fi

# Cloud mode: per-target IPs (set after terraform apply)
TARGET_STANDARD_IP=""
TARGET_REALISTIC_IP=""
TARGET_STRESS_IP=""
DATABASE_IP=""
ATTACKER_IP=""

# =========================================================================
# Helpers
# =========================================================================

TF_CMD=""
find_tf() {
  if command -v tofu &> /dev/null; then
    TF_CMD="tofu"
  elif command -v terraform &> /dev/null; then
    TF_CMD="terraform"
  else
    echo "ERROR: Neither 'tofu' nor 'terraform' found. Install with: brew install opentofu"
    exit 1
  fi
}

infra_up() {
  local deploy_attacker="${1:-false}"
  find_tf
  echo "Deploying infrastructure (5 VPS: 3 targets + 1 database + attacker=${deploy_attacker})..."
  $TF_CMD -chdir="${SCRIPT_DIR}" init -input=false -no-color 2>&1 | tail -1
  $TF_CMD -chdir="${SCRIPT_DIR}" apply -auto-approve -var "deploy_attacker=${deploy_attacker}" -no-color
  echo ""
}

infra_down() {
  find_tf
  if [[ -f "${SCRIPT_DIR}/terraform.tfstate" ]]; then
    echo ""
    echo "============================================"
    echo "  TEARING DOWN INFRASTRUCTURE"
    echo "============================================"
    echo ""
    $TF_CMD -chdir="${SCRIPT_DIR}" destroy -auto-approve -no-color
    echo "All VPS instances destroyed."
  fi
}

get_output() {
  $TF_CMD -chdir="${SCRIPT_DIR}" output -raw "$1" 2>/dev/null
}

wait_for_target() {
  local ip="$1"
  local label="${2:-Mycel}"
  local url="http://${ip}:3000"
  echo "Waiting for ${label} at ${url} (up to 6 minutes)..."
  for i in $(seq 1 72); do
    if curl -sf --max-time 5 "${url}/health" > /dev/null 2>&1; then
      echo ""
      echo "${label} is ready!"
      return 0
    fi
    printf "."
    sleep 5
  done
  echo ""
  echo "ERROR: ${label} did not become healthy after 6 minutes."
  echo "Check logs: ssh root@${ip} 'docker logs mycel'"
  return 1
}

wait_for_pg() {
  local ip="$1"
  echo "Waiting for PostgreSQL at ${ip}:5432..."
  for i in $(seq 1 60); do
    if ssh $SSH_OPTS -o ConnectTimeout=5 "root@${ip}" 'docker exec postgres pg_isready -U bench' > /dev/null 2>&1; then
      echo "PostgreSQL is ready!"
      return 0
    fi
    printf "."
    sleep 3
  done
  echo ""
  echo "WARNING: PostgreSQL did not become ready in time."
  return 1
}

wait_for_attacker() {
  local ip="$1"
  echo "Waiting for attacker VPS to finish provisioning (up to 4 minutes)..."
  for i in $(seq 1 48); do
    if ssh $SSH_OPTS -o ConnectTimeout=5 "root@${ip}" 'command -v k6' > /dev/null 2>&1; then
      echo "Attacker is ready!"
      return 0
    fi
    printf "."
    sleep 5
  done
  echo ""
  echo "WARNING: Attacker VPS did not become ready in time."
  return 1
}

# =========================================================================
# k6 runner function
# =========================================================================

# run_k6 <test_file> <results_dir> <label> <target_url> [extra_env_args...]
# Each call targets a specific URL (supports parallel execution against different targets)
run_k6() {
  local test_file="$1"
  local results_dir="$2"
  local label="$3"
  local target_url="$4"
  shift 4
  local extra_envs=()
  if [[ $# -gt 0 ]]; then
    extra_envs=("$@")
  fi

  mkdir -p "$results_dir"
  local k6_output="${results_dir}/k6-output.txt"

  # Build extra --env flags
  local env_flags=""
  if [[ ${#extra_envs[@]} -gt 0 ]]; then
    for env_pair in "${extra_envs[@]}"; do
      if [[ -n "$env_pair" ]]; then
        env_flags="${env_flags} --env ${env_pair}"
      fi
    done
  fi

  # k6 exits non-zero when thresholds are crossed -- that's expected, not fatal
  set +e
  case "$K6_MODE" in
    local)
      if ! command -v k6 &> /dev/null; then
        echo "ERROR: k6 not installed. Install with: brew install k6"
        set -e
        exit 1
      fi
      echo "[${label}] Running k6 against ${target_url}..." > "$k6_output"
      k6 run \
        --env TARGET_URL="${target_url}" \
        ${env_flags} \
        "${SCRIPT_DIR}/${test_file}" 2>&1 | tee -a "$k6_output"
      ;;

    docker|cloud-local)
      echo "[${label}] Running k6 (Docker) against ${target_url}..." > "$k6_output"
      docker run --rm \
        -v "${SCRIPT_DIR}:/benchmark" \
        -v "${results_dir}:/benchmark/results" \
        grafana/k6 run \
          --env TARGET_URL="${target_url}" \
          ${env_flags} \
          /benchmark/${test_file} 2>&1 | tee -a "$k6_output"
      ;;

    remote|cloud)
      local attacker_ip="${ATTACKER_IP:-$(get_output attacker_ip 2>/dev/null || echo "")}"
      if [[ -z "$attacker_ip" || "$attacker_ip" == "local" ]]; then
        echo "ERROR: No attacker VPS available."
        set -e
        exit 1
      fi
      ssh $SSH_OPTS "root@${attacker_ip}" "mkdir -p /opt/benchmark/results"
      scp $SSH_OPTS "${SCRIPT_DIR}/${test_file}" "root@${attacker_ip}:/opt/benchmark/${test_file}"
      ssh $SSH_OPTS "root@${attacker_ip}" \
        "cd /opt/benchmark && k6 run --env TARGET_URL=${target_url} ${env_flags} /opt/benchmark/${test_file}" 2>&1 | tee "$k6_output"
      scp $SSH_OPTS "root@${attacker_ip}:/opt/benchmark/results/summary.json" "$results_dir/summary.json" 2>/dev/null || true
      scp $SSH_OPTS "root@${attacker_ip}:/opt/benchmark/results/calibration.json" "$results_dir/calibration.json" 2>/dev/null || true
      ;;

    *)
      echo "Unknown mode: $K6_MODE"
      set -e
      exit 1
      ;;
  esac
  set -e
}

# run_k6_bg <test_file> <results_dir> <label> <target_url> [extra_env_args...]
# Same as run_k6 but output goes only to file (no tee to stdout) for parallel use
run_k6_bg() {
  local test_file="$1"
  local results_dir="$2"
  local label="$3"
  local target_url="$4"
  shift 4
  local extra_envs=()
  if [[ $# -gt 0 ]]; then
    extra_envs=("$@")
  fi

  mkdir -p "$results_dir"
  local k6_output="${results_dir}/k6-output.txt"

  # Build extra --env flags
  local env_flags=""
  if [[ ${#extra_envs[@]} -gt 0 ]]; then
    for env_pair in "${extra_envs[@]}"; do
      if [[ -n "$env_pair" ]]; then
        env_flags="${env_flags} --env ${env_pair}"
      fi
    done
  fi

  # k6 exits non-zero when thresholds are crossed -- that's expected, not fatal
  set +e
  case "$K6_MODE" in
    local)
      k6 run \
        --env TARGET_URL="${target_url}" \
        ${env_flags} \
        "${SCRIPT_DIR}/${test_file}" > "$k6_output" 2>&1
      ;;

    docker|cloud-local)
      docker run --rm \
        -v "${SCRIPT_DIR}:/benchmark" \
        -v "${results_dir}:/benchmark/results" \
        grafana/k6 run \
          --env TARGET_URL="${target_url}" \
          ${env_flags} \
          /benchmark/${test_file} > "$k6_output" 2>&1
      ;;

    remote|cloud)
      local attacker_ip="${ATTACKER_IP:-$(get_output attacker_ip 2>/dev/null || echo "")}"
      if [[ -z "$attacker_ip" || "$attacker_ip" == "local" ]]; then
        echo "ERROR: No attacker VPS available for ${label}." >> "$k6_output"
        set -e
        return 1
      fi
      ssh $SSH_OPTS "root@${attacker_ip}" "mkdir -p /opt/benchmark/results-${label}"
      scp $SSH_OPTS "${SCRIPT_DIR}/${test_file}" "root@${attacker_ip}:/opt/benchmark/${test_file}"
      ssh $SSH_OPTS "root@${attacker_ip}" \
        "cd /opt/benchmark && k6 run --env TARGET_URL=${target_url} ${env_flags} /opt/benchmark/${test_file}" > "$k6_output" 2>&1
      scp $SSH_OPTS "root@${attacker_ip}:/opt/benchmark/results/summary.json" "$results_dir/summary.json" 2>/dev/null || true
      scp $SSH_OPTS "root@${attacker_ip}:/opt/benchmark/results/calibration.json" "$results_dir/calibration.json" 2>/dev/null || true
      ;;

    *)
      echo "Unknown mode: $K6_MODE" >> "$k6_output"
      set -e
      return 1
      ;;
  esac
  local exit_code=$?
  set -e
  return $exit_code
}

# =========================================================================
# Calibration function -- discovers hardware limits before running tests
# =========================================================================

MAX_VUS=""

run_calibration() {
  local cal_target_url="$1"
  local cal_dir="${RESULTS_BASE}/00-calibration"
  mkdir -p "$cal_dir"

  echo ""
  echo "============================================"
  echo "  HARDWARE CALIBRATION"
  echo "  Discovering sustainable VU limits..."
  echo "  Target: ${cal_target_url}"
  echo "============================================"
  echo ""

  run_k6 "calibrate.js" "$cal_dir" "CALIBRATION" "$cal_target_url"

  # Parse calibration results
  local cal_json=""

  # Try local results directory first (for local/docker modes)
  if [[ -f "${cal_dir}/calibration.json" ]]; then
    cal_json="${cal_dir}/calibration.json"
  elif [[ -f "${SCRIPT_DIR}/results/calibration.json" ]]; then
    # k6 writes relative to its working dir
    cp "${SCRIPT_DIR}/results/calibration.json" "${cal_dir}/calibration.json" 2>/dev/null || true
    cal_json="${cal_dir}/calibration.json"
  fi

  if [[ -n "$cal_json" && -f "$cal_json" ]]; then
    # Extract max_vus using basic JSON parsing (no jq dependency)
    MAX_VUS=$(grep -o '"max_vus"[[:space:]]*:[[:space:]]*[0-9]*' "$cal_json" | grep -o '[0-9]*$')

    if [[ -n "$MAX_VUS" && "$MAX_VUS" -gt 0 ]]; then
      echo ""
      echo "============================================"
      echo "  CALIBRATION COMPLETE: MAX_VUS = ${MAX_VUS}"
      echo "============================================"
      echo ""
    else
      echo ""
      echo "WARNING: Could not parse max_vus from calibration. Using default (200)."
      MAX_VUS="200"
    fi
  else
    echo ""
    echo "WARNING: No calibration.json found. Using default MAX_VUS=200."
    MAX_VUS="200"
  fi

  # Build the calibration report
  build_report "$cal_dir" "HARDWARE CALIBRATION RESULTS"
}

# =========================================================================
# Report builder function
# =========================================================================

# build_report <results_dir> <report_title_pattern>
build_report() {
  local results_dir="$1"
  local k6_output="${results_dir}/k6-output.txt"
  local title_pattern="$2"
  local report_file="${results_dir}/report.txt"

  {
    # Extract the full report box from k6 output
    if [[ -f "$k6_output" ]]; then
      awk "/${title_pattern}/{found=1; if(prev) print prev} found{print} /╚.*╝/{if(found) exit} {prev=\$0}" "$k6_output" 2>/dev/null || true
    fi
  } > "$report_file"
}

# =========================================================================
# Usage
# =========================================================================

usage() {
  echo "Mycel Benchmark Runner (Parallel Architecture)"
  echo ""
  echo "Usage:"
  echo "  ./run.sh cloud             Full auto: deploy 5 VPS -> test from cloud -> collect -> destroy"
  echo "  ./run.sh cloud-local       Full auto: deploy 4 VPS -> test from your Mac -> collect -> destroy"
  echo "  ./run.sh local <ip>        Run k6 locally against an existing target"
  echo "  ./run.sh docker <ip>       Run k6 via Docker against an existing target"
  echo "  ./run.sh remote <ip>       Run k6 from attacker VPS against an existing target"
  echo ""
  echo "Options:"
  echo "  --realistic                Run realistic benchmark (adaptive VUs via calibration)"
  echo "  --stress                   Run stress test (adaptive VUs via calibration)"
  echo "  --full                     Run calibration + all 3 tests in parallel"
  echo ""
  echo "Cloud architecture (--full):"
  echo "                      +-- target-standard  --- loadtest.js"
  echo "  attacker -----------+-- target-realistic --- realistictest.js"
  echo "                      +-- target-stress    --- stresstest.js"
  echo "                                 |"
  echo "                            database (PG)"
  echo ""
  echo "Examples:"
  echo "  ./run.sh cloud                  # Standard benchmark (transforms only)"
  echo "  ./run.sh cloud --realistic      # Calibrate + realistic benchmark (PostgreSQL CRUD)"
  echo "  ./run.sh cloud --stress         # Calibrate + stress test (adaptive scaling)"
  echo "  ./run.sh cloud --full           # Calibrate + all 3 tests in parallel"
  echo "  ./run.sh docker 45.33.100.50    # Manual mode against existing VPS"
  exit 1
}

if [[ -z "$MODE" ]]; then
  usage
fi

# For manual modes, require target IP
if [[ "$MODE" != "cloud" && "$MODE" != "cloud-local" && -z "$TARGET_IP" ]]; then
  usage
fi

mkdir -p "$RESULTS_BASE"

# =========================================================================
# Cloud modes: deploy -> test -> collect -> destroy
# =========================================================================

if [[ "$MODE" == "cloud" || "$MODE" == "cloud-local" ]]; then
  DEPLOY_ATTACKER="false"
  K6_MODE="docker"

  if [[ "$MODE" == "cloud" ]]; then
    DEPLOY_ATTACKER="true"
    K6_MODE="remote"
  fi

  # Ensure teardown on any exit (success, error, ctrl+c)
  trap 'infra_down' EXIT

  echo ""
  echo "========================================================================"
  MODE_UPPER=$(echo "$MODE" | tr '[:lower:]' '[:upper:]')
  case "$TEST_MODE" in
    standard)  TEST_LABEL="BENCHMARK" ;;
    realistic) TEST_LABEL="REALISTIC" ;;
    stress)    TEST_LABEL="STRESS TEST" ;;
    full)      TEST_LABEL="FULL SUITE" ;;
  esac
  echo "  MYCEL ${TEST_LABEL} -- ${MODE_UPPER}"
  echo "========================================================================"
  if [[ "$TEST_MODE" == "full" ]]; then
  echo "  1. Deploy infrastructure (5 VPS: 3 targets + database + attacker)"
  echo "  2. Wait for all targets to be ready"
  echo "  3. Calibrate hardware limits"
  echo "  4. Run all 3 tests in PARALLEL:"
  echo "     - Standard  -> target-standard"
  echo "     - Realistic -> target-realistic (adaptive)"
  echo "     - Stress    -> target-stress (adaptive)"
  echo "  5. Collect results"
  echo "  6. Destroy infrastructure"
  elif [[ "$TEST_MODE" == "realistic" || "$TEST_MODE" == "stress" ]]; then
  echo "  1. Deploy infrastructure (5 VPS)"
  echo "  2. Wait for targets to be ready"
  echo "  3. Calibrate hardware limits"
  echo "  4. Run load test (adaptive)"
  echo "  5. Collect results"
  echo "  6. Destroy infrastructure"
  else
  echo "  1. Deploy infrastructure (5 VPS)"
  echo "  2. Wait for target to be ready"
  echo "  3. Run load test"
  echo "  4. Collect results"
  echo "  5. Destroy infrastructure"
  fi
  echo "========================================================================"
  echo ""

  # Step 1: Deploy all 5 VPS (terraform handles dependency ordering)
  infra_up "$DEPLOY_ATTACKER"

  TARGET_STANDARD_IP=$(get_output "target_standard_ip")
  TARGET_REALISTIC_IP=$(get_output "target_realistic_ip")
  TARGET_STRESS_IP=$(get_output "target_stress_ip")
  DATABASE_IP=$(get_output "database_ip")

  if [[ "$MODE" == "cloud" ]]; then
    ATTACKER_IP=$(get_output "attacker_ip")
    echo "Target (standard):  root@${TARGET_STANDARD_IP}"
    echo "Target (realistic): root@${TARGET_REALISTIC_IP}"
    echo "Target (stress):    root@${TARGET_STRESS_IP}"
    echo "Database:           root@${DATABASE_IP}"
    echo "Attacker:           root@${ATTACKER_IP}"
  else
    echo "Target (standard):  root@${TARGET_STANDARD_IP}"
    echo "Target (realistic): root@${TARGET_REALISTIC_IP}"
    echo "Target (stress):    root@${TARGET_STRESS_IP}"
    echo "Database:           root@${DATABASE_IP}"
    echo "Attacker:           local (this machine)"
  fi

  # Step 2: Wait for PostgreSQL first, then all targets in parallel
  echo ""
  wait_for_pg "$DATABASE_IP" || true

  # Ensure schema exists (StackScript may not have created it due to timing)
  echo "Ensuring database schema exists..."
  ssh $SSH_OPTS -o ConnectTimeout=10 "root@${DATABASE_IP}" '
    docker exec postgres psql -U bench -d bench -c "
      CREATE TABLE IF NOT EXISTS users (
        id TEXT PRIMARY KEY,
        email TEXT NOT NULL,
        name TEXT NOT NULL,
        created_at TIMESTAMPTZ DEFAULT NOW()
      );
      CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
    "
  ' 2>/dev/null && echo "Schema verified!" || echo "WARNING: Could not verify schema"

  # Wait for all 3 targets in parallel
  echo ""
  echo "Waiting for all targets in parallel..."
  wait_for_target "$TARGET_STANDARD_IP" "target-standard" &
  WAIT_STD=$!
  wait_for_target "$TARGET_REALISTIC_IP" "target-realistic" &
  WAIT_REAL=$!
  wait_for_target "$TARGET_STRESS_IP" "target-stress" &
  WAIT_STRESS=$!

  WAIT_FAILED=0
  wait $WAIT_STD || WAIT_FAILED=1
  wait $WAIT_REAL || WAIT_FAILED=1
  wait $WAIT_STRESS || WAIT_FAILED=1

  if [[ "$WAIT_FAILED" -eq 1 ]]; then
    echo ""
    echo "WARNING: One or more targets did not become healthy."
    echo "Continuing with available targets..."
  fi

  if [[ "$MODE" == "cloud" ]]; then
    wait_for_attacker "$ATTACKER_IP" || true
  fi

  # Brief pause to let VPS settle after provisioning
  echo "Waiting 15s for all VPS to stabilize..."
  sleep 15

  # Pre-flight smoke tests
  echo ""
  echo "=== PRE-FLIGHT: Endpoint smoke tests ==="

  echo -n "  target-standard  GET /ping:  "
  curl -s -w "HTTP %{http_code}" "http://${TARGET_STANDARD_IP}:3000/ping" 2>/dev/null | tail -c 20
  echo ""

  echo -n "  target-realistic POST /users: "
  SMOKE_RESULT=$(curl -s -w "%{http_code}" -X POST "http://${TARGET_REALISTIC_IP}:3000/users" \
    -H "Content-Type: application/json" \
    -d '{"name":"smoke test","email":"SMOKE@Test.COM"}' 2>/dev/null)
  SMOKE_STATUS="${SMOKE_RESULT: -3}"
  echo "HTTP ${SMOKE_STATUS}"

  echo -n "  target-stress    POST /users: "
  SMOKE_RESULT2=$(curl -s -w "%{http_code}" -X POST "http://${TARGET_STRESS_IP}:3000/users" \
    -H "Content-Type: application/json" \
    -d '{"name":"smoke test","email":"SMOKE2@Test.COM"}' 2>/dev/null)
  SMOKE_STATUS2="${SMOKE_RESULT2: -3}"
  echo "HTTP ${SMOKE_STATUS2}"

  if [[ "$SMOKE_STATUS" != "200" && "$SMOKE_STATUS" != "201" ]]; then
    echo ""
    echo "WARNING: realistic target CRUD endpoints returning errors! DB may not be connected."
    echo "Mycel logs:"
    ssh $SSH_OPTS -o ConnectTimeout=10 "root@${TARGET_REALISTIC_IP}" 'docker logs mycel --tail 10' 2>/dev/null || true
    echo ""
    echo "Continue anyway? Press Ctrl+C to abort, or wait 10s to continue..."
    sleep 10
  fi

  # Clean up smoke test data
  if [[ -n "${DATABASE_IP:-}" ]]; then
    ssh $SSH_OPTS -o ConnectTimeout=10 "root@${DATABASE_IP}" \
      'docker exec postgres psql -U bench -d bench -c "TRUNCATE TABLE users;"' 2>/dev/null || true
  fi

  # Steps 3+: Run benchmark (falls through to the common benchmark logic below)
  # Teardown happens via the EXIT trap
fi

# =========================================================================
# Resolve target URLs
# =========================================================================

# For manual (non-cloud) modes, all tests target the same IP
if [[ -z "$TARGET_STANDARD_IP" ]]; then
  TARGET_STANDARD_IP="$TARGET_IP"
  TARGET_REALISTIC_IP="$TARGET_IP"
  TARGET_STRESS_IP="$TARGET_IP"
fi

TARGET_STANDARD_URL="http://${TARGET_STANDARD_IP}:3000"
TARGET_REALISTIC_URL="http://${TARGET_REALISTIC_IP}:3000"
TARGET_STRESS_URL="http://${TARGET_STRESS_IP}:3000"

# =========================================================================
# Pre-flight: reachable, and answering correctly
# =========================================================================

if [[ "$MODE" != "cloud" && "$MODE" != "cloud-local" ]]; then
  echo "Checking target at ${TARGET_STANDARD_URL}..."
  if ! curl -sf --max-time 5 "${TARGET_STANDARD_URL}/health" > /dev/null 2>&1; then
    echo "ERROR: Cannot reach ${TARGET_STANDARD_URL}/health"
    exit 1
  fi
  echo "Target is healthy!"
fi

# Healthy is not the same as correct. Every k6 check in this suite is a status
# check, so a target that is up and answering 200 with the wrong body — or with
# no configuration loaded at all — would be measured as if it were working.
echo ""
run_preflight() {
  local url="$1"
  shift
  if ! "${SCRIPT_DIR}/scripts/preflight.sh" "$url" "$@"; then
    echo ""
    echo "Refusing to benchmark a target that is not answering correctly."
    echo "Run './scripts/preflight.sh ${url}' to see it again, then fix the target."
    exit 1
  fi
}

case "$TEST_MODE" in
  standard)  run_preflight "$TARGET_STANDARD_URL" ;;
  realistic) run_preflight "$TARGET_REALISTIC_URL" --db ;;
  stress)    run_preflight "$TARGET_STRESS_URL" --db ;;
  full)
    run_preflight "$TARGET_STANDARD_URL"
    run_preflight "$TARGET_REALISTIC_URL" --db
    run_preflight "$TARGET_STRESS_URL" --db
    ;;
esac

# =========================================================================
# Capture pre-test metrics (from realistic target -- most interesting)
# =========================================================================

# Pick the primary monitoring target based on test mode
MONITOR_TARGET_IP="$TARGET_REALISTIC_IP"
if [[ "$TEST_MODE" == "standard" ]]; then
  MONITOR_TARGET_IP="$TARGET_STANDARD_IP"
fi

echo ""
echo "Capturing pre-test metrics from ${MONITOR_TARGET_IP}..."
for attempt in 1 2 3; do
  if ssh $SSH_OPTS -o ConnectTimeout=10 -o ServerAliveInterval=5 "root@${MONITOR_TARGET_IP}" '
    echo "=== SYSTEM ==="
    echo "CPU: $(nproc) cores"
    echo "RAM: $(free -m | awk "/Mem:/ {print \$2}")MB total"
    echo ""
    echo "=== DOCKER ==="
    docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}" mycel 2>/dev/null || echo "(no containers)"
  ' | tee "$RESULTS_BASE/pre-test.txt"; then
    break
  fi
  echo "SSH connection failed (attempt ${attempt}/3), retrying in 10s..."
  sleep 10
done

# =========================================================================
# Start resource monitoring on target (background)
# =========================================================================

echo ""
echo "Starting resource monitor on ${MONITOR_TARGET_IP}..."
ssh $SSH_OPTS -o ConnectTimeout=10 -o ServerAliveInterval=5 "root@${MONITOR_TARGET_IP}" '
  rm -f /tmp/resource-log.csv
  echo "timestamp,cpu_pct,mem_usage_mb,mem_pct" > /tmp/resource-log.csv
  while true; do
    stats=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}}" mycel 2>/dev/null || echo "0%,0MiB / 0MiB")
    cpu=$(echo "$stats" | cut -d, -f1 | tr -d "%")
    mem_raw=$(echo "$stats" | cut -d, -f2 | cut -d/ -f1 | xargs)
    mem_mb=$(echo "$mem_raw" | sed "s/MiB//;s/GiB/*1024/" | bc 2>/dev/null || echo "0")
    mem_pct=$(echo "$stats" | cut -d, -f3 | tr -d "%")
    echo "$(date +%s),$cpu,$mem_mb,$mem_pct" >> /tmp/resource-log.csv
    sleep 2
  done
' &
MONITOR_PID=$!

cleanup_monitor() {
  kill $MONITOR_PID 2>/dev/null || true
  wait $MONITOR_PID 2>/dev/null || true
}

FINAL_REPORT_FILE="${RESULTS_BASE}/final-report.txt"

show_final_report() {
  if [[ -f "$FINAL_REPORT_FILE" ]]; then
    echo ""
    cat "$FINAL_REPORT_FILE"
  fi
}

# For cloud modes the EXIT trap is already set (infra_down), so chain them
if [[ "$MODE" == "cloud" || "$MODE" == "cloud-local" ]]; then
  trap 'cleanup_monitor; infra_down; show_final_report' EXIT
else
  trap 'cleanup_monitor; show_final_report' EXIT
fi

sleep 3

# =========================================================================
# Run tests
# =========================================================================

# Determine effective k6 mode
K6_MODE="${K6_MODE:-$MODE}"

# =========================================================================
# Run calibration for modes that need it (realistic, stress, full)
# Calibration runs against the realistic target (has DB endpoints)
# =========================================================================

if [[ "$TEST_MODE" == "realistic" || "$TEST_MODE" == "stress" || "$TEST_MODE" == "full" ]]; then
  run_calibration "$TARGET_REALISTIC_URL"
  # Clean up data created during calibration
  if [[ -n "${DATABASE_IP:-}" ]]; then
    echo "Cleaning database after calibration..."
    ssh $SSH_OPTS -o ConnectTimeout=10 "root@${DATABASE_IP}" \
      'docker exec postgres psql -U bench -d bench -c "TRUNCATE TABLE users;"' 2>/dev/null || true
  fi
  echo "Pausing 10s after calibration to let servers stabilize..."
  sleep 10
fi

# =========================================================================
# Execute tests
# =========================================================================

case "$TEST_MODE" in
  standard)
    echo ""
    echo "============================================"
    echo "  STANDARD BENCHMARK -> ${TARGET_STANDARD_URL}"
    echo "============================================"
    echo ""
    run_k6 "loadtest.js" "$RESULTS_BASE" "STANDARD" "$TARGET_STANDARD_URL"
    build_report "$RESULTS_BASE" "MYCEL BENCHMARK REPORT"
    ;;

  realistic)
    echo ""
    echo "============================================"
    echo "  REALISTIC BENCHMARK (adaptive, MAX_VUS=${MAX_VUS:-200}) -> ${TARGET_REALISTIC_URL}"
    echo "============================================"
    echo ""
    run_k6 "realistictest.js" "$RESULTS_BASE" "REALISTIC" "$TARGET_REALISTIC_URL" "MAX_VUS=${MAX_VUS:-200}"
    build_report "$RESULTS_BASE" "MYCEL REALISTIC BENCHMARK REPORT"
    ;;

  stress)
    echo ""
    echo "============================================"
    echo "  STRESS TEST (adaptive, MAX_VUS=${MAX_VUS:-200}) -> ${TARGET_STRESS_URL}"
    echo "============================================"
    echo ""
    run_k6 "stresstest.js" "$RESULTS_BASE" "STRESS" "$TARGET_STRESS_URL" "MAX_VUS=${MAX_VUS:-200}"
    build_report "$RESULTS_BASE" "MYCEL STRESS TEST REPORT"
    ;;

  full)
    echo ""
    echo "============================================"
    echo "  RUNNING ALL 3 TESTS IN PARALLEL"
    echo "============================================"
    echo "  Standard  -> ${TARGET_STANDARD_URL}"
    echo "  Realistic -> ${TARGET_REALISTIC_URL} (MAX_VUS=${MAX_VUS:-200})"
    echo "  Stress    -> ${TARGET_STRESS_URL} (MAX_VUS=${MAX_VUS:-200})"
    echo "============================================"
    echo ""

    # Truncate DB before parallel tests
    if [[ -n "${DATABASE_IP:-}" ]]; then
      echo "Truncating database before parallel tests..."
      ssh $SSH_OPTS -o ConnectTimeout=10 "root@${DATABASE_IP}" \
        'docker exec postgres psql -U bench -d bench -c "TRUNCATE TABLE users;"' 2>/dev/null || true
    fi

    # Launch all 3 tests in parallel (output goes to files only)
    echo "Launching standard test in background..."
    run_k6_bg "loadtest.js" "${RESULTS_BASE}/01-standard" "STANDARD" "$TARGET_STANDARD_URL" &
    PID_STD=$!

    echo "Launching realistic test in background..."
    run_k6_bg "realistictest.js" "${RESULTS_BASE}/02-realistic" "REALISTIC" "$TARGET_REALISTIC_URL" "MAX_VUS=${MAX_VUS:-200}" &
    PID_REAL=$!

    echo "Launching stress test in background..."
    run_k6_bg "stresstest.js" "${RESULTS_BASE}/03-stress" "STRESS" "$TARGET_STRESS_URL" "MAX_VUS=${MAX_VUS:-200}" &
    PID_STRESS=$!

    echo ""
    echo "All 3 tests running in parallel. Waiting for completion..."
    echo "  PID ${PID_STD}  = standard"
    echo "  PID ${PID_REAL} = realistic"
    echo "  PID ${PID_STRESS} = stress"
    echo ""

    # Wait for all to complete (set +e so we don't exit on k6 threshold failures)
    set +e
    wait $PID_STD
    STD_EXIT=$?
    echo "  [DONE] Standard test finished (exit=$STD_EXIT)"

    wait $PID_REAL
    REAL_EXIT=$?
    echo "  [DONE] Realistic test finished (exit=$REAL_EXIT)"

    wait $PID_STRESS
    STRESS_EXIT=$?
    echo "  [DONE] Stress test finished (exit=$STRESS_EXIT)"
    set -e

    echo ""
    echo "All parallel tests completed."

    # Build reports from captured output
    build_report "${RESULTS_BASE}/01-standard" "MYCEL BENCHMARK REPORT"
    build_report "${RESULTS_BASE}/02-realistic" "MYCEL REALISTIC BENCHMARK REPORT"
    build_report "${RESULTS_BASE}/03-stress" "MYCEL STRESS TEST REPORT"
    ;;
esac

# =========================================================================
# Stop monitor and fetch resource data
# =========================================================================

cleanup_monitor
sleep 2

echo ""
echo "Fetching resource usage data..."
scp $SSH_OPTS "root@${MONITOR_TARGET_IP}:/tmp/resource-log.csv" "$RESULTS_BASE/resources.csv" 2>/dev/null || true

# =========================================================================
# Post-test metrics
# =========================================================================

echo ""
echo "Capturing post-test metrics..."
ssh $SSH_OPTS "root@${MONITOR_TARGET_IP}" '
  echo "=== DOCKER ==="
  docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}" mycel 2>/dev/null || echo "(no containers)"
  echo ""
  echo "=== CONTAINER STATUS ==="
  docker inspect mycel --format "mycel: Status: {{.State.Status}}, OOMKilled: {{.State.OOMKilled}}, RestartCount: {{.RestartCount}}" 2>/dev/null || true
' > "$RESULTS_BASE/post-test.txt" 2>&1

# =========================================================================
# Provenance: which build produced these numbers
# =========================================================================
#
# The suite used to deploy whatever :latest happened to be, and the results
# file said "v1.12.0" because somebody typed it. A number nobody can attribute
# to a build is not a measurement.

echo ""
echo "Recording which build was measured..."
{
  echo "run_id: ${RUN_ID}"
  echo "test_mode: ${TEST_MODE}"
  echo "targets:"
  echo "  standard:  ${TARGET_STANDARD_URL}"
  echo "  realistic: ${TARGET_REALISTIC_URL}"
  echo "  stress:    ${TARGET_STRESS_URL}"
  echo ""
  echo "=== VERSION UNDER TEST ==="
  ssh $SSH_OPTS -o ConnectTimeout=10 "root@${MONITOR_TARGET_IP}" 'cat /opt/mycel/version.txt' 2>/dev/null \
    || echo "(target did not record a version — old StackScript?)"
} > "$RESULTS_BASE/provenance.txt" 2>&1
cat "$RESULTS_BASE/provenance.txt"

# =========================================================================
# Mycel's own counters, as a second opinion on the load
# =========================================================================
#
# k6 counts what it sent. mycel_flow_executions_total counts what Mycel ran.
# When the two disagree the difference is requests that never reached a flow —
# rejected at the edge, or answered by a route that is not the one under test.

echo ""
echo "Capturing Mycel flow counters..."
for label in standard realistic stress; do
  case "$label" in
    standard)  metrics_ip="$TARGET_STANDARD_IP" ;;
    realistic) metrics_ip="$TARGET_REALISTIC_IP" ;;
    stress)    metrics_ip="$TARGET_STRESS_IP" ;;
  esac
  [[ -z "$metrics_ip" ]] && continue
  curl -sf --max-time 15 "http://${metrics_ip}:9090/metrics" 2>/dev/null \
    | grep -E '^mycel_flow_(executions_total|errors_total)|^go_goroutines |^process_open_fds ' \
    > "${RESULTS_BASE}/flow-metrics-${label}.txt" || true
done

# =========================================================================
# Build final report (saved to file, printed after teardown)
# =========================================================================

{
  echo ""
  echo "========================================================================"
  if [[ "$TEST_MODE" == "full" ]]; then
  echo "  MYCEL FULL SUITE RESULTS (PARALLEL)"
  else
  echo "  BENCHMARK RESULTS"
  fi
  echo "========================================================================"

  # Show calibration results if available
  cal_report="${RESULTS_BASE}/00-calibration/report.txt"
  if [[ -f "$cal_report" ]]; then
    echo ""
    echo "  -- CALIBRATION --"
    cat "$cal_report"
    if [[ -n "$MAX_VUS" ]]; then
      echo "  Adaptive scaling: MAX_VUS=${MAX_VUS}"
    fi
  fi

  if [[ "$TEST_MODE" == "full" ]]; then
    # Full mode: show each sub-test report
    declare -a FULL_DIRS=("${RESULTS_BASE}/01-standard" "${RESULTS_BASE}/02-realistic" "${RESULTS_BASE}/03-stress")
    declare -a FULL_LABELS=("STANDARD" "REALISTIC" "STRESS")
    declare -a FULL_URLS=("${TARGET_STANDARD_URL}" "${TARGET_REALISTIC_URL}" "${TARGET_STRESS_URL}")

    for i in "${!FULL_DIRS[@]}"; do
      local_report="${FULL_DIRS[$i]}/report.txt"
      echo ""
      echo "  -- ${FULL_LABELS[$i]} (${FULL_URLS[$i]}) --"
      if [[ -f "$local_report" && -s "$local_report" ]]; then
        cat "$local_report"
      else
        echo "  (no report data)"
      fi
    done
  else
    # Single test mode
    local_report="${RESULTS_BASE}/report.txt"
    if [[ -f "$local_report" && -s "$local_report" ]]; then
      echo ""
      cat "$local_report"
    fi
  fi

  echo ""
  echo "  RESOURCE USAGE (monitored: ${MONITOR_TARGET_IP})"
  echo "  -------------------------------------------------------"

  if [[ -f "$RESULTS_BASE/resources.csv" ]]; then
    awk -F, 'NR>1 && $2+0 > 0 {
      cpu+=$2; mem+=$3; n++
      if($2+0 > max_cpu) max_cpu=$2+0
      if($3+0 > max_mem) max_mem=$3+0
    } END {
      if(n>0) {
        printf "  Samples:    %d\n", n
        printf "  CPU avg:    %.1f%%\n", cpu/n
        printf "  CPU max:    %.1f%%\n", max_cpu
        printf "  RAM avg:    %.1f MB\n", mem/n
        printf "  RAM max:    %.1f MB\n", max_mem
      } else {
        print "  No samples collected"
      }
    }' "$RESULTS_BASE/resources.csv"
  fi

  echo ""
  echo "  POST-TEST CONTAINER STATUS"
  echo "  -------------------------------------------------------"
  if [[ -f "$RESULTS_BASE/post-test.txt" ]]; then
    sed 's/^/  /' "$RESULTS_BASE/post-test.txt"
  fi

  echo ""
  echo "  Results saved to: $RESULTS_BASE/"
  find "$RESULTS_BASE" -type f | sort | while read -r f; do
    echo "    ${f#$RESULTS_BASE/}"
  done
  echo ""
} > "$FINAL_REPORT_FILE"

echo ""
echo "All tests complete. Results will be shown after cleanup."

# Teardown happens automatically via EXIT trap for cloud modes
# For manual modes, nothing to tear down
