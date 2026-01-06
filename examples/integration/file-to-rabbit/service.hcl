# Integration Pattern: File -> RabbitMQ
#
# Use case: Read files periodically and publish content to queue
# Common scenarios:
#   - Process drop folders (CSV imports, data feeds)
#   - Watch for new files and trigger processing
#   - Batch file processing on schedule
#   - Log file tailing and event streaming

connector "files" {
  type = "file"

  base_path = env("FILES_BASE_PATH", "/data")

  file_mode = "0644"
  dir_mode  = "0755"
}

connector "s3" {
  type = "s3"

  region          = env("AWS_REGION", "us-east-1")
  bucket          = env("S3_BUCKET", "data-imports")
  access_key      = env("AWS_ACCESS_KEY")
  secret_key      = env("AWS_SECRET_KEY")
  endpoint        = env("S3_ENDPOINT", "")  # For MinIO
  force_path_style = env("S3_FORCE_PATH_STYLE", "false") == "true"
}

connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"

  host     = env("RABBIT_HOST", "localhost")
  port     = env("RABBIT_PORT", 5672)
  username = env("RABBIT_USER", "guest")
  password = env("RABBIT_PASS", "guest")
  vhost    = "/"

  exchange {
    name        = "imports"
    type        = "topic"
    durable     = true
    auto_delete = false
  }

  reconnect {
    enabled      = true
    interval     = "5s"
    max_attempts = 0
  }
}
