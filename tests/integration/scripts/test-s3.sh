#!/bin/bash
# Test: S3 (MinIO) operations via Call() interface
# Note: S3 upload/download can't be tested through flows because S3 connector's
# Write/Read have non-standard signatures. Only Call() operations work.
source "$(dirname "$0")/lib.sh"

echo "=== S3 (MinIO) ==="

# List files (works via Call() "list" operation)
status=$(http_status GET "$BASE/s3/files")
assert_status "S3 list returns 200" "200" "$status"

body=$(http_body GET "$BASE/s3/files")
assert_contains "S3 list returns response" "files" "$body"

report
