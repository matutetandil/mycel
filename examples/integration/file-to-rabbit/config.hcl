# Configuration for File -> RabbitMQ integration example

service {
  name    = "file-to-rabbit-example"
  version = "1.0.0"
}

# Environment variables are configured at runtime via:
# - FILES_BASE_PATH
# - RABBIT_HOST, RABBIT_PORT, RABBIT_USER, RABBIT_PASS
# - AWS_REGION, S3_BUCKET, AWS_ACCESS_KEY, AWS_SECRET_KEY
