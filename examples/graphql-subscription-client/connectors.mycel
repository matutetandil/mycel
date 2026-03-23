# GraphQL Subscription Client Connectors

# External GraphQL server to subscribe to.
# Mycel connects via WebSocket and receives real-time events,
# just like a message queue consumer.

connector "products_api" {
  type     = "graphql"
  driver   = "client"
  endpoint = env("PRODUCTS_API_URL", "http://localhost:4000/graphql")

  # Authentication (optional)
  auth {
    type  = "bearer"
    token = env("PRODUCTS_API_TOKEN", "")
  }

  # Enable subscription support — this makes the connector act as
  # a WebSocket client using the graphql-ws protocol.
  subscriptions {
    enabled = true
    path    = "/subscriptions"    # WebSocket endpoint on the remote server
  }
}

# Local database for storing received events
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/price-tracker.db"
}

# REST API to expose the stored data
connector "api" {
  type = "rest"
  port = 3000
}
