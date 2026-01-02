# Connector Definitions for Sync Example

# REST API for testing
connector "api" {
  type = "rest"
  port = 3000
}

# PostgreSQL for data storage
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("POSTGRES_HOST", "localhost")
  port     = 5432
  database = env("POSTGRES_DB", "mycel")
  user     = env("POSTGRES_USER", "mycel")
  password = env("POSTGRES_PASSWORD", "mycel")
}

# Redis for sync primitives (locks, semaphores, coordinate)
connector "redis" {
  type   = "cache"
  driver = "redis"
  url    = env("REDIS_URL", "redis://localhost:6379")
}

# RabbitMQ for message processing examples
connector "rabbitmq" {
  type     = "mq"
  driver   = "rabbitmq"
  url      = env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
  exchange = "mycel"
}

# External API (mock for semaphore example)
connector "external_api" {
  type     = "http"
  base_url = env("EXTERNAL_API_URL", "https://httpbin.org")
  timeout  = "30s"
}

# Monitoring endpoint (for health ping example)
connector "monitoring" {
  type     = "http"
  base_url = env("MONITORING_URL", "https://httpbin.org")
}
