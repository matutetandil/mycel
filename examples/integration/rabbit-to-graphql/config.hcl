# Configuration for RabbitMQ -> GraphQL integration example

service {
  name    = "rabbit-to-graphql-example"
  version = "1.0.0"
}

# Environment variables are configured at runtime via:
# - RABBIT_HOST, RABBIT_PORT, RABBIT_USER, RABBIT_PASS
# - INVENTORY_GRAPHQL_URL, INVENTORY_GRAPHQL_TOKEN
# - USERS_GRAPHQL_URL, USERS_GRAPHQL_API_KEY
