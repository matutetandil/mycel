# Message Queue flows

# Publish to RabbitMQ via REST
flow "rabbit_publish" {
  from {
    connector = "api"
    operation = "POST /mq/rabbit/publish"
  }
  to {
    connector = "rabbit_pub"
    target    = "test_queue"
  }
}

# Consume from RabbitMQ and write to DB
flow "rabbit_consume" {
  from {
    connector = "rabbit_sub"
    operation = "test_queue"
  }
  transform {
    source = "'rabbitmq'"
    data   = "'received'"
  }
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}

# List RabbitMQ results
flow "rabbit_results" {
  from {
    connector = "api"
    operation = "GET /mq/rabbit/results"
  }
  step "results" {
    connector = "postgres"
    query     = "SELECT * FROM mq_results WHERE source = 'rabbitmq' ORDER BY id DESC"
  }
  transform {
    output = "step.results"
  }
  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}

# Publish to Kafka via REST
flow "kafka_publish" {
  from {
    connector = "api"
    operation = "POST /mq/kafka/publish"
  }
  to {
    connector = "kafka_pub"
    target    = "test_topic"
  }
}

# Consume from Kafka and write to DB
flow "kafka_consume" {
  from {
    connector = "kafka_sub"
    operation = "test_topic"
  }
  transform {
    source = "'kafka'"
    data   = "'received'"
  }
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}

# List Kafka results
flow "kafka_results" {
  from {
    connector = "api"
    operation = "GET /mq/kafka/results"
  }
  step "results" {
    connector = "postgres"
    query     = "SELECT * FROM mq_results WHERE source = 'kafka' ORDER BY id DESC"
  }
  transform {
    output = "step.results"
  }
  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "postgres"
    target    = "mq_results"
  }
}
