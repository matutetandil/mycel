# Redis Pub/Sub flows for integration tests

# Publish to Redis channel via REST
flow "redis_pubsub_publish" {
  from {
    connector = "api"
    operation = "POST /mq/redis/publish"
  }
  to {
    connector = "redis_pub"
    target    = "test_events"
  }
}

# Subscribe to Redis channel and write to DB
flow "redis_pubsub_consume" {
  from {
    connector = "redis_sub"
    operation = "test_events"
  }
  transform {
    source = "'redis-pubsub'"
    data   = "'received'"
  }
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}

# List Redis Pub/Sub results
flow "redis_pubsub_results" {
  from {
    connector = "api"
    operation = "GET /mq/redis/results"
  }
  step "results" {
    connector = "postgres"
    query     = "SELECT * FROM mq_results WHERE source = 'redis-pubsub' ORDER BY id DESC"
  }
  transform {
    output = "step.results"
  }
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}
