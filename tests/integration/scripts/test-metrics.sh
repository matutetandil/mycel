#!/bin/bash
# Test: Prometheus metrics
# Metrics are served on the REST port when a REST connector exists.
source "$(dirname "$0")/lib.sh"

echo "=== Metrics ==="

# Warm up: make a request to a flow endpoint so metrics are populated
# (health endpoints may not go through the metrics middleware)
http_body GET "$BASE/pg/users" > /dev/null 2>&1

status=$(http_status GET "$BASE/metrics")
assert_status "GET /metrics returns 200" "200" "$status"

body=$(http_body GET "$BASE/metrics")
assert_contains "Has request_duration" "mycel_request_duration" "$body"
assert_contains "Has mycel_version" "mycel_service_info|mycel_version" "$body"

report
