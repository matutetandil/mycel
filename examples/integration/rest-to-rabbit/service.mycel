# Integration Pattern: REST -> RabbitMQ
#
# Use case: Receive HTTP requests and publish messages to a queue
# Common scenarios:
#   - API Gateway that decouples request handling from processing
#   - Webhook receivers that queue events for async processing
#   - Command endpoints that trigger background jobs
#   - Event ingestion APIs

connector "api" {
  type = "rest"
  mode = "server"
  port = env("API_PORT", 8080)
}

connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"

  host     = env("RABBIT_HOST", "localhost")
  port     = env("RABBIT_PORT", 5672)
  username = env("RABBIT_USER", "guest")
  password = env("RABBIT_PASS", "guest")
  vhost    = "/"
}
