# GraphQL Federation Connectors

# GraphQL Server with Federation v2 and Subscriptions
# This connector exposes a federated subgraph that can be composed
# with other subgraphs via Apollo Router, Cosmo, or similar gateways.

connector "api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
    headers = ["Content-Type", "Authorization"]
  }

  # Enable Apollo Federation v2
  federation {
    enabled = true
    version = 2
  }

  # Enable GraphQL Subscriptions over WebSocket
  subscriptions {
    enabled = true
    path    = "/subscriptions"
  }
}

# SQLite Database for product and review storage
connector "db" {
  type   = "database"
  driver = "sqlite"

  database = ":memory:"
}

# RabbitMQ for event-driven subscriptions
# In production, price updates arrive via a message queue and are pushed
# to subscribers in real time.

connector "events" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = 5672
  username = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")
  vhost    = "/"

  queue {
    name    = "product_events"
    durable = true
  }

  exchange {
    name        = "product_exchange"
    type        = "topic"
    durable     = true
    routing_key = "product.*"
  }

  consumer {
    auto_ack    = false
    concurrency = 1
    prefetch    = 10
  }
}
