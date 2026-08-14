#!/bin/bash
# Test: gRPC via grpcurl
source "$(dirname "$0")/lib.sh"

echo "=== gRPC ==="

# Check if grpcurl is available
if ! command -v grpcurl &> /dev/null; then
  echo "  ⚠ grpcurl not installed, skipping gRPC tests"
  echo "  Install: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"
  # --- Authentication -------------------------------------------------------
#
# The server hands its chained interceptor to each method handler and expects
# the handler to invoke it. Mycel's handlers took the argument and ignored it,
# so a service configured with auth answered calls carrying no credentials at
# all — while logging that authentication was enabled.

body=$(grpcurl -plaintext -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
assert_contains "A call with no credentials is refused" "Unauthenticated|unauthenticated|auth" "$body"

body=$(grpcurl -plaintext -H "api-key: not-the-key" -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
assert_contains "A key nobody issued is refused" "Unauthenticated|unauthenticated|auth" "$body"

body=$(grpcurl -plaintext -H "api-key: grpc-test-key" -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
assert_not_contains "A call carrying the key is answered" "Unauthenticated|unauthenticated" "$body"

# A method the configuration lists as public answers without credentials, which
# is how a health check works before anything has a key to send.
body=$(grpcurl -plaintext -H "api-key: grpc-test-key" "$GRPC" integration.UserService/ListUsers 2>&1)
assert_not_contains "A public method needs no credentials" "Unauthenticated|unauthenticated" "$body"

report
  exit 0
fi

# List services via reflection
body=$(grpcurl -plaintext -H "api-key: grpc-test-key" "$GRPC" list 2>&1)
assert_contains "List services returns UserService" "UserService" "$body"

# Create user
body=$(grpcurl -plaintext -H "api-key: grpc-test-key" -d '{"name":"Frank","email":"frank@test.com"}' "$GRPC" integration.UserService/CreateUser 2>&1)
assert_contains "CreateUser returns name" "Frank" "$body"

# List users
body=$(grpcurl -plaintext -H "api-key: grpc-test-key" "$GRPC" integration.UserService/ListUsers 2>&1)
assert_contains "ListUsers returns data" "Frank|users" "$body"

# Get user
body=$(grpcurl -plaintext -H "api-key: grpc-test-key" -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
status=$?
assert_status "GetUser call succeeds" "0" "$status"

# --- Authentication -------------------------------------------------------
#
# The server hands its chained interceptor to each method handler and expects
# the handler to invoke it. Mycel's handlers took the argument and ignored it,
# so a service configured with auth answered calls carrying no credentials at
# all — while logging that authentication was enabled.

body=$(grpcurl -plaintext -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
assert_contains "A call with no credentials is refused" "Unauthenticated|unauthenticated|auth" "$body"

body=$(grpcurl -plaintext -H "api-key: not-the-key" -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
assert_contains "A key nobody issued is refused" "Unauthenticated|unauthenticated|auth" "$body"

body=$(grpcurl -plaintext -H "api-key: grpc-test-key" -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
assert_not_contains "A call carrying the key is answered" "Unauthenticated|unauthenticated" "$body"

# A method the configuration lists as public answers without credentials, which
# is how a health check works before anything has a key to send.
body=$(grpcurl -plaintext -H "api-key: grpc-test-key" "$GRPC" integration.UserService/ListUsers 2>&1)
assert_not_contains "A public method needs no credentials" "Unauthenticated|unauthenticated" "$body"

report
