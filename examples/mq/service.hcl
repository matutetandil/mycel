// Mycel Message Queue Example Configuration
// This example demonstrates RabbitMQ integration with publish/subscribe patterns.

service {
  name    = "mq-demo"
  version = "1.0.0"
}

// REST API for triggering message publishing
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}

// RabbitMQ Consumer - Listens for order events
connector "order_events" {
  type   = "mq"
  driver = "rabbitmq"

  // Connection settings
  host     = env("RABBITMQ_HOST", "localhost")
  port     = 5672
  username = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")
  vhost    = "/"

  // Queue configuration (for consuming)
  queue {
    name        = "orders"
    durable     = true
    auto_delete = false
  }

  // Exchange binding (optional - for topic routing)
  exchange {
    name        = "orders_exchange"
    type        = "topic"
    durable     = true
    routing_key = "order.*"
  }

  // Consumer settings
  consumer {
    auto_ack    = false    // Manual acknowledgment for reliability
    concurrency = 2        // Number of parallel consumers
    prefetch    = 10       // QoS prefetch count
  }
}

// RabbitMQ Publisher - Sends notifications
connector "notifications" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = 5672
  username = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")
  vhost    = "/"

  // Publisher settings
  publisher {
    exchange     = "notifications_exchange"
    routing_key  = "notification.email"
    persistent   = true
    content_type = "application/json"
  }
}

// SQLite database for persistence
connector "db" {
  type   = "database"
  driver = "sqlite"

  database = "./data/mq_demo.db"
}
