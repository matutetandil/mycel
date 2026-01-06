# Configuration for RabbitMQ -> GraphQL integration example

service {
  name        = "rabbit-to-graphql-example"
  version     = "1.0.0"
  description = "Integration pattern: Consume from RabbitMQ and call GraphQL APIs"
}

environment "development" {
  variables {
    RABBIT_HOST             = "localhost"
    RABBIT_PORT             = "5672"
    RABBIT_USER             = "guest"
    RABBIT_PASS             = "guest"
    INVENTORY_GRAPHQL_URL   = "http://localhost:4000/graphql"
    INVENTORY_GRAPHQL_TOKEN = "dev-token"
    USERS_GRAPHQL_URL       = "http://localhost:4001/graphql"
    USERS_GRAPHQL_API_KEY   = "dev-api-key"
    HASURA_ADMIN_SECRET     = "dev-secret"
  }
}

environment "production" {
  variables {
    RABBIT_HOST             = "${RABBIT_HOST}"
    RABBIT_PORT             = "${RABBIT_PORT}"
    RABBIT_USER             = "${RABBIT_USER}"
    RABBIT_PASS             = "${RABBIT_PASS}"
    INVENTORY_GRAPHQL_URL   = "${INVENTORY_GRAPHQL_URL}"
    INVENTORY_GRAPHQL_TOKEN = "${INVENTORY_GRAPHQL_TOKEN}"
    USERS_GRAPHQL_URL       = "${USERS_GRAPHQL_URL}"
    USERS_GRAPHQL_API_KEY   = "${USERS_GRAPHQL_API_KEY}"
    HASURA_ADMIN_SECRET     = "${HASURA_ADMIN_SECRET}"
  }
}
