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
  force_path_style = true
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
