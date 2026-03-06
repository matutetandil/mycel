#!/bin/bash
# Wait for MinIO to be ready
until mc alias set local http://minio:9000 minioadmin minioadmin 2>/dev/null; do
  echo "Waiting for MinIO..."
  sleep 1
done

mc mb local/test-bucket --ignore-existing
echo "Bucket 'test-bucket' ready"
