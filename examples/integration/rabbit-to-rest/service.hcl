# Integration Pattern: RabbitMQ -> REST
#
# Use case: Consume messages from a queue and call an external REST API
# Common scenarios:
#   - Process orders and notify external fulfillment service
#   - Sync data to external CRM/ERP systems
#   - Trigger webhooks based on events

connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"

  host     = env("RABBIT_HOST", "localhost")
  port     = env("RABBIT_PORT", 5672)
  username = env("RABBIT_USER", "guest")
  password = env("RABBIT_PASS", "guest")
  vhost    = "/"

  prefetch = 10

  reconnect {
    enabled      = true
    interval     = "5s"
    max_attempts = 0
  }
}

connector "fulfillment_api" {
  type     = "rest"
  mode     = "client"
  base_url = env("FULFILLMENT_API_URL", "https://api.fulfillment.example.com")

  timeout = "30s"

  auth {
    type = "bearer"
    bearer {
      token = env("FULFILLMENT_API_TOKEN")
    }
  }

  headers {
    "Content-Type" = "application/json"
    "Accept"       = "application/json"
  }

  retry {
    attempts = 3
    backoff  = "exponential"
    initial  = "1s"
    max      = "30s"
  }

  circuit_breaker {
    threshold         = 5
    timeout           = "30s"
    success_threshold = 2
  }
}

connector "notification_api" {
  type     = "rest"
  mode     = "client"
  base_url = env("NOTIFICATION_API_URL", "https://api.notifications.example.com")

  timeout = "10s"

  auth {
    type = "api_key"
    api_key {
      header = "X-API-Key"
      value  = env("NOTIFICATION_API_KEY")
    }
  }
}
