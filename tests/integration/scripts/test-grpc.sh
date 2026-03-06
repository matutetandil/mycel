#!/bin/bash
# Test: gRPC via grpcurl
source "$(dirname "$0")/lib.sh"

echo "=== gRPC ==="

# Check if grpcurl is available
if ! command -v grpcurl &> /dev/null; then
  echo "  ⚠ grpcurl not installed, skipping gRPC tests"
  echo "  Install: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"
  report
  exit 0
fi

# List services via reflection
body=$(grpcurl -plaintext "$GRPC" list 2>&1)
assert_contains "List services returns UserService" "UserService" "$body"

# Create user
body=$(grpcurl -plaintext -d '{"name":"Frank","email":"frank@test.com"}' "$GRPC" integration.UserService/CreateUser 2>&1)
assert_contains "CreateUser returns name" "Frank" "$body"

# List users
body=$(grpcurl -plaintext "$GRPC" integration.UserService/ListUsers 2>&1)
assert_contains "ListUsers returns data" "Frank|users" "$body"

# Get user
body=$(grpcurl -plaintext -d '{"id":1}' "$GRPC" integration.UserService/GetUser 2>&1)
status=$?
assert_status "GetUser call succeeds" "0" "$status"

report
