#!/bin/bash
# Run a group of test scripts in parallel, aggregate results.
# Usage: bash run-group.sh <test-script> [test-script...]
# Exit code = total number of failures across all scripts.

set -uo pipefail

cd "$(dirname "$0")/.."

TOTAL_PASS=0
TOTAL_FAIL=0
FAILED_SUITES=()
TMPDIR_GROUP=$(mktemp -d)
trap 'rm -rf "$TMPDIR_GROUP"' EXIT

NAMES=()

# Launch all test scripts in parallel
for test_file in "$@"; do
  if [[ ! -f "$test_file" ]]; then
    echo "warning: $test_file not found, skipping"
    continue
  fi
  name=$(basename "$test_file" .sh | sed 's/test-//')
  NAMES+=("$name")
  bash "$test_file" > "$TMPDIR_GROUP/$name.out" 2>&1 &
done

wait

# Display results in argument order and aggregate counts
for name in "${NAMES[@]}"; do
  [[ -f "$TMPDIR_GROUP/$name.out" ]] || continue

  echo "--------------------------------------"
  output=$(cat "$TMPDIR_GROUP/$name.out")
  echo "$output"

  pass=$(echo "$output" | grep "Results:" | sed -n 's/.*\([0-9][0-9]*\) passed.*/\1/p')
  fail=$(echo "$output" | grep "Results:" | sed -n 's/.*\([0-9][0-9]*\) failed.*/\1/p')
  pass=${pass:-0}
  fail=${fail:-0}

  TOTAL_PASS=$((TOTAL_PASS + pass))
  TOTAL_FAIL=$((TOTAL_FAIL + fail))

  if [[ "$fail" -gt 0 ]]; then
    FAILED_SUITES+=("$name")
  fi
done

echo ""
echo "======================================"
echo "  Group: $TOTAL_PASS passed, $TOTAL_FAIL failed"
if [[ ${#FAILED_SUITES[@]} -gt 0 ]]; then
  echo "  Failed: ${FAILED_SUITES[*]}"
fi
echo "======================================"

exit "$TOTAL_FAIL"
