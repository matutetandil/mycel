# S3 / MinIO Example

This example demonstrates S3 and S3-compatible storage (MinIO) operations.

## Features

- Upload and download files to/from S3
- Generate presigned URLs for direct access
- Copy files within the bucket
- List bucket contents
- Support for AWS S3 and MinIO

## Files

- `config.hcl` - S3 and MinIO connector configuration
- `flows.hcl` - File operation flows

## Environment Variables

```bash
# AWS S3
export S3_BUCKET="my-bucket"
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"

# MinIO
export MINIO_ENDPOINT="http://localhost:9000"
export MINIO_BUCKET="my-bucket"
export MINIO_ACCESS_KEY="minioadmin"
export MINIO_SECRET_KEY="minioadmin"
```

## Usage

```bash
# Start the service
mycel start --config ./examples/s3

# Upload a file
curl -X POST http://localhost:3000/upload \
  -H "Content-Type: application/json" \
  -d '{"filename":"test.txt","content":"Hello S3"}'

# Get presigned URL
curl http://localhost:3000/presigned/{id}

# Download
curl http://localhost:3000/download/{id}

# List files
curl http://localhost:3000/files

# Copy a file
curl -X POST http://localhost:3000/copy/{id}

# Delete
curl -X DELETE http://localhost:3000/files/{id}
```

## Configuration

### AWS S3

```hcl
connector "s3" {
  type   = "file"
  driver = "s3"

  bucket     = env("S3_BUCKET")
  region     = env("AWS_REGION")
  access_key = env("AWS_ACCESS_KEY_ID")
  secret_key = env("AWS_SECRET_ACCESS_KEY")
}
```

### MinIO (S3-compatible)

```hcl
connector "minio" {
  type   = "file"
  driver = "s3"

  bucket           = env("MINIO_BUCKET")
  endpoint         = env("MINIO_ENDPOINT")
  access_key       = env("MINIO_ACCESS_KEY")
  secret_key       = env("MINIO_SECRET_KEY")
  use_path_style = true
  use_ssl          = false
}
```

## Operations

| Operation | Description |
|-----------|-------------|
| `READ` | Read file from S3 |
| `WRITE` | Write file to S3 |
| `DELETE` | Delete file from S3 |
| `LIST` | List objects with prefix |
| `COPY` | Copy object within bucket |
| `PRESIGN` | Generate presigned URL |

## Verify It Works

### 1. Start MinIO locally (for testing)

```bash
docker run -d --name minio \
  -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address ":9001"
```

Create a bucket:
```bash
docker exec minio mc mb /data/test-bucket
```

### 2. Start the service

```bash
export MINIO_ENDPOINT="http://localhost:9000"
export MINIO_BUCKET="test-bucket"
export MINIO_ACCESS_KEY="minioadmin"
export MINIO_SECRET_KEY="minioadmin"

mycel start --config ./examples/s3
```

You should see:
```
INFO  Starting service: s3-example
INFO  Loaded 2 connectors: api, s3
INFO    s3: MinIO at http://localhost:9000/test-bucket
INFO  REST server listening on :3000
```

### 3. Upload a file

```bash
curl -X POST http://localhost:3000/upload \
  -H "Content-Type: application/json" \
  -d '{"filename":"test.txt","content":"Hello S3"}'
```

Expected response:
```json
{
  "id": "test.txt",
  "size": 8,
  "etag": "abc123..."
}
```

### 4. List files

```bash
curl http://localhost:3000/files
```

Expected response:
```json
[{"key": "test.txt", "size": 8, "last_modified": "..."}]
```

### 5. Get presigned URL

```bash
curl http://localhost:3000/presigned/test.txt
```

Expected response:
```json
{
  "url": "http://localhost:9000/test-bucket/test.txt?X-Amz-...",
  "expires_in": 3600
}
```

### 6. Verify in MinIO Console

Open http://localhost:9001 (minioadmin/minioadmin) to see uploaded files.

### Common Issues

**"InvalidAccessKeyId"**

Check that AWS_ACCESS_KEY_ID or MINIO_ACCESS_KEY matches your credentials.

**"NoSuchBucket"**

Create the bucket first:
```bash
aws s3 mb s3://my-bucket --endpoint-url http://localhost:9000
```

**"Connection refused"**

Ensure MinIO/S3 endpoint is correct and accessible.
