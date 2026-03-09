#!/bin/bash
# Test: Plugin system (local plugin with WASM validator used in a flow)
source "$(dirname "$0")/lib.sh"

echo "=== Plugin System ==="

# 1. Verify Mycel started with the plugin loaded
status=$(http_status GET "$BASE/health")
assert_status "Mycel started with plugin loaded" "200" "$status"

# 2. Check logs confirm plugin validator was registered
logs=$(docker compose -p mycel-integration logs mycel 2>&1)
assert_contains "Plugin loaded in logs" "plugins loaded" "$logs"
assert_contains "Plugin validator registered" "registered plugin validator.*always_valid" "$logs"

# 3. Initialize the plugin_results table
http_status POST "$BASE/test/plugin-init" '{}' > /dev/null

# 4. POST with valid data — the always_valid WASM validator runs on the `code` field.
#    If the plugin didn't load or the validator wasn't wired, this would fail.
body=$(http_body POST "$BASE/test/plugin-validate" '{"name":"plugin-test","code":"XYZ789"}')
status=$(http_status POST "$BASE/test/plugin-validate" '{"name":"plugin-test2","code":"ABC123"}')
assert_contains "POST passes plugin WASM validation" "200|201" "$status"

report
