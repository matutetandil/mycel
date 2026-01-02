# Semaphore Example - External API Rate Limiting
# Limits concurrent requests to an external API

flow "call_external_api" {
  from {
    connector = "rabbitmq"
    operation = "queue:api_requests"
  }

  # Allow max 10 concurrent requests to external API
  semaphore {
    storage     = "redis"
    key         = "'external_api'"
    max_permits = 10
    timeout     = "30s"
    lease       = "60s"  # Auto-release after 60s (crash protection)
  }

  to {
    connector = "external_api"
    target    = "POST /post"
  }
}

# REST endpoint to trigger external API call (for testing)
flow "call_external_api_rest" {
  from {
    connector = "api"
    operation = "POST /external"
  }

  semaphore {
    storage     = "redis"
    key         = "'external_api'"
    max_permits = 10
    timeout     = "30s"
    lease       = "60s"
  }

  to {
    connector = "external_api"
    target    = "POST /post"
  }
}
