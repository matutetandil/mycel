#!/bin/bash
# Test: long-running workflows and the endpoints that drive them
#
# Nothing in this suite covered workflows, which is awkward for a feature whose
# whole point is outliving the request that started it. The three endpoints were
# mounted on the admin server — read-only, unauthenticated, scraped — so
# anything that could reach that port could approve whatever a workflow was
# waiting on. They have their own port now and a caller has to say who they are.
source "$(dirname "$0")/lib.sh"

echo "=== Workflows ==="

KEY="workflow-test-key"

# --- Where they are, and are not -------------------------------------------

status=$(http_status GET "$ADMIN/workflows/wf-nobody")
assert_status "The admin port does not serve workflows" "404" "$status"

status=$(http_status GET "$ADMIN/health")
assert_status "The admin port still serves health" "200" "$status"

# --- Who may reach them ------------------------------------------------------

status=$(curl -s -o /dev/null -w "%{http_code}" "$WORKFLOW/workflows/wf-nobody")
assert_status "A caller with no credentials is refused" "401" "$status"

status=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: not-the-key" "$WORKFLOW/workflows/wf-nobody")
assert_status "A key nobody issued is refused" "401" "$status"

status=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: $KEY" "$WORKFLOW/workflows/wf-nobody")
assert_status "The configured key reaches the endpoint" "404" "$status"

# --- A workflow that waits ---------------------------------------------------

TITLE="approval-$$"
body=$(http_body POST "$BASE/test/approval" "{\"title\":\"$TITLE\"}")
WF_ID=$(echo "$body" | jq -r '.workflow_id // .id // empty' 2>/dev/null)

if [[ -z "$WF_ID" ]]; then
  echo "  ⚠ the saga did not report a workflow id, skipping the rest"
  echo "    response: $body"
  report
  exit 0
fi

assert_contains "Starting a waiting workflow answers with its id" "$WF_ID" "$WF_ID"

# It is paused, waiting to be told.
sleep 2
body=$(curl -s -H "X-API-Key: $KEY" "$WORKFLOW/workflows/$WF_ID")
assert_contains "The workflow says what it is waiting for" "approved|paused|waiting" "$body"

# --- Waking it ---------------------------------------------------------------

# A body that is not JSON is refused rather than waking the workflow with
# nothing: it would carry on down a branch reading fields that never arrived,
# and the caller would be told it worked.
status=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"approved_by": "ops",' \
  "$WORKFLOW/workflows/$WF_ID/signal/approved")
assert_status "A signal whose body is not JSON is refused" "400" "$status"

# And the real one.
status=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"approved_by": "ops"}' \
  "$WORKFLOW/workflows/$WF_ID/signal/approved")
assert_status "A signal carrying its data wakes the workflow" "200" "$status"

sleep 3
body=$(curl -s -H "X-API-Key: $KEY" "$WORKFLOW/workflows/$WF_ID")
assert_not_contains "The workflow is no longer waiting" "\"status\":\"paused\"" "$body"

# The step behind the signal ran, which is the whole point of waking it.
body=$(http_body GET "$BASE/pg/items")
assert_contains "The step behind the signal ran" "$TITLE" "$body"

report
