# GraphQL Federation Connectors

# GraphQL Server with Subscriptions
# Federation v2 is enabled automatically on every GraphQL server —
# gateways (Apollo Router, Cosmo) can discover and compose this subgraph
# without any extra configuration.

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
