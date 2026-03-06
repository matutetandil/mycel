#!/bin/bash
# Test: Health endpoints
# Note: When a REST connector exists, health/metrics are served on the REST port.
# The standalone admin server only starts when there is no REST connector.
source "$(dirname "$0")/lib.sh"

echo "=== Health Checks ==="

# /health on REST port (admin endpoints are registered here when REST exists)
status=$(http_status GET "$BASE/health")
assert_status "GET /health returns 200" "200" "$status"

body=$(http_body GET "$BASE/health")
assert_contains "Health response has status" "healthy|ok|status" "$body"

# /health/live
status=$(http_status GET "$BASE/health/live")
assert_contains "GET /health/live responds" "200|404" "$status"

# /health/ready
status=$(http_status GET "$BASE/health/ready")
assert_contains "GET /health/ready responds" "200|404" "$status"

report
