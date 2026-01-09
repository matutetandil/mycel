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

}

connector "inventory_graphql" {
  type     = "graphql"
  mode     = "client"
  endpoint = env("INVENTORY_GRAPHQL_URL", "https://inventory.example.com/graphql")

  timeout = "30s"
}

connector "users_graphql" {
  type     = "graphql"
  mode     = "client"
  endpoint = env("USERS_GRAPHQL_URL", "https://users.example.com/graphql")

  timeout = "15s"
}
