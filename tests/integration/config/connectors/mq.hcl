# RabbitMQ publisher
connector "rabbit_pub" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = env("RABBITMQ_PORT", 5672)
  username = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")
  vhost    = "/"

  publisher {
    exchange     = ""
    routing_key  = "test_queue"
    persistent   = true
    content_type = "application/json"
  }
}

# RabbitMQ consumer
connector "rabbit_sub" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = env("RABBITMQ_PORT", 5672)
  username = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")
  vhost    = "/"

  queue {
    name        = "test_queue"
    durable     = true
    auto_delete = false
  }

  consumer {
    auto_ack    = false
    concurrency = 1
    prefetch    = 10
  }
}

# Kafka publisher
connector "kafka_pub" {
  type   = "mq"
  driver = "kafka"

  brokers = [env("KAFKA_BROKERS", "localhost:9092")]

  producer {
    # topic set per-message via flow target (avoids kafka-go Writer+Message topic conflict)
  }
}

# Kafka consumer
connector "kafka_sub" {
  type   = "mq"
  driver = "kafka"

  brokers = [env("KAFKA_BROKERS", "localhost:9092")]

  consumer {
    group_id = "integration-test"
    topics   = ["test_topic"]
  }
}
