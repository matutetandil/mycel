# Integration Pattern: RabbitMQ -> GraphQL
#
# Use case: Consume messages from a queue and call an external GraphQL API
# Common scenarios:
#   - Update inventory in a GraphQL-based product service
#   - Sync user data to a Hasura/Apollo backend
#   - Trigger mutations based on domain events

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

connector "inventory_graphql" {
  type     = "graphql"
  mode     = "client"
  endpoint = env("INVENTORY_GRAPHQL_URL", "https://inventory.example.com/graphql")

  timeout = "30s"

  auth {
    type = "bearer"
    bearer {
      token = env("INVENTORY_GRAPHQL_TOKEN")
    }
  }

  headers {
    "X-Hasura-Admin-Secret" = env("HASURA_ADMIN_SECRET", "")
  }
}

connector "users_graphql" {
  type     = "graphql"
  mode     = "client"
  endpoint = env("USERS_GRAPHQL_URL", "https://users.example.com/graphql")

  timeout = "15s"

  auth {
    type = "api_key"
    api_key {
      header = "X-API-Key"
      value  = env("USERS_GRAPHQL_API_KEY")
    }
  }
}
