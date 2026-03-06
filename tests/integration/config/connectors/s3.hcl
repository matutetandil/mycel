# MinIO (S3-compatible)
connector "s3" {
  type   = "s3"
  driver = "s3"

  bucket         = env("S3_BUCKET", "test-bucket")
  region         = "us-east-1"
  endpoint       = env("S3_ENDPOINT", "http://localhost:9000")
  access_key     = env("S3_ACCESS_KEY", "minioadmin")
  secret_key     = env("S3_SECRET_KEY", "minioadmin")
  use_path_style = true
  prefix         = "integration/"
}
