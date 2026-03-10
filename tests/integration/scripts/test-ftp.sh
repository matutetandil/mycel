#!/bin/bash
# Test: FTP/SFTP file operations
source "$(dirname "$0")/lib.sh"

echo "=== FTP/SFTP ==="

# Upload a JSON file (flow target is "test-upload.json")
status=$(http_status POST "$BASE/sftp/upload" '{"content":"{\"name\":\"mycel\",\"version\":\"1.0\"}"}')
assert_status "Upload file returns 200" "200" "$status"

body=$(http_body POST "$BASE/sftp/upload" '{"content":"{\"name\":\"mycel\",\"version\":\"1.0\"}"}')
assert_contains "Upload response has path" "path" "$body"

# List files
body=$(http_body GET "$BASE/sftp/files")
assert_contains "List files returns uploaded file" "test-upload" "$body"

# Download the file (flow target is "test-upload.json")
body=$(http_body GET "$BASE/sftp/download")
assert_contains "Download returns file content" "mycel" "$body"

# Delete the file (flow target is "test-upload.json")
status=$(http_status DELETE "$BASE/sftp/files")
assert_status "Delete file returns 200" "200" "$status"

report
