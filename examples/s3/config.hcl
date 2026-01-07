# S3 Example Configuration
# This example shows how to work with S3/MinIO storage
#
# NOTE: S3-specific attributes (bucket, region, access_key, etc.) are
# supported by the connector factory but not yet in the parser schema.
# See internal/connector/s3/factory.go for configuration options.
# This example demonstrates the intended configuration pattern.

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
# NOTE: S3-specific attributes need parser schema support.
# The connector factory reads these from Properties map.
connector "s3" {
  type   = "s3"
  driver = "s3"

  # These attributes are documented but need parser support:
  # bucket     = env("S3_BUCKET")
  # region     = env("AWS_REGION")
  # access_key = env("AWS_ACCESS_KEY_ID")
  # secret_key = env("AWS_SECRET_ACCESS_KEY")
}

# MinIO connector (S3-compatible)
# NOTE: Same as above - attributes need parser support.
connector "minio" {
  type   = "s3"
  driver = "s3"

  # These attributes are documented but need parser support:
  # bucket           = env("MINIO_BUCKET")
  # endpoint         = env("MINIO_ENDPOINT")
  # access_key       = env("MINIO_ACCESS_KEY")
  # secret_key       = env("MINIO_SECRET_KEY")
  # force_path_style = true
  # use_ssl          = false
}

# Database for file metadata
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/s3.db"
}
