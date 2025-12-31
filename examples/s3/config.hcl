# S3 Example Configuration
# This example shows how to work with S3/MinIO storage

service {
  name    = "s3-example"
  version = "1.0.0"
}

# REST API to expose file operations
connector "api" {
  type = "rest"
  port = 3000
}

# AWS S3 connector
connector "s3" {
  type   = "file"
  driver = "s3"

  bucket     = env("S3_BUCKET")
  region     = env("AWS_REGION")
  access_key = env("AWS_ACCESS_KEY_ID")
  secret_key = env("AWS_SECRET_ACCESS_KEY")
}

# MinIO connector (S3-compatible)
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

# Database for file metadata
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/s3.db"
}
