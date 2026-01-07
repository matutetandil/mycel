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
  type   = "s3"
  driver = "s3"

  bucket     = "my-bucket"
  region     = "us-east-1"
  access_key = "your-access-key"
  secret_key = "your-secret-key"
  prefix     = "uploads/"
  format     = "json"
}

# MinIO connector (S3-compatible)
connector "minio" {
  type   = "s3"
  driver = "s3"

  bucket         = "local-bucket"
  endpoint       = "http://localhost:9000"
  access_key     = "minioadmin"
  secret_key     = "minioadmin"
  use_path_style = true  # Required for MinIO
  prefix         = "data/"
}

# Database for file metadata
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/s3.db"
}
