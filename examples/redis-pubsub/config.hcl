service {
  name    = "event-processor"
  version = "1.0.0"
}

# Redis Pub/Sub for real-time events
connector "events" {
  type     = "mq"
  driver   = "redis"
  host     = "localhost"
  port     = 6379
  channels = ["orders", "payments"]
}

# REST API
connector "api" {
  type = "rest"
  port = 3000
}

# Database
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = "localhost"
  database = "events"
  user     = "admin"
  password = env("DB_PASS")
}

# Process order events from Redis
flow "process_order" {
  from {
    connector = "events"
    operation = "orders"
  }
  transform {
    order_id   = "input.order_id"
    status     = "input.status"
    channel    = "input._channel"
    processed_at = "now()"
  }
  to {
    connector = "db"
    target    = "order_events"
  }
}

# Publish events via REST
flow "publish_event" {
  from {
    connector = "api"
    operation = "POST /events/:channel"
  }
  to {
    connector = "events"
    target    = "input.channel"
  }
}
