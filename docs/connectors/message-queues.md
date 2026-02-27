# Message Queues

Produce and consume messages with RabbitMQ and Kafka. Both use `type = "mq"` with a `driver` to select the backend.

## RabbitMQ

```hcl
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")

  consumer {
    queue       = "my-queue"
    prefetch    = 10
    auto_ack    = false
    workers     = 5
    retry_count = 3
  }

  publisher {
    exchange    = "my-exchange"
    routing_key = "my.routing.key"
    mandatory   = false
    immediate   = false
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | — | AMQP connection URL |
| `consumer.queue` | string | — | Queue to consume from |
| `consumer.prefetch` | int | `10` | Prefetch count |
| `consumer.auto_ack` | bool | `false` | Auto-acknowledge messages |
| `consumer.workers` | int | `1` | Number of consumer workers |
| `publisher.exchange` | string | — | Target exchange |
| `publisher.routing_key` | string | — | Routing key |

## Kafka

```hcl
connector "kafka" {
  type    = "mq"
  driver  = "kafka"
  brokers = ["kafka1:9092", "kafka2:9092"]

  consumer {
    group_id = "my-consumer-group"
    topics   = ["my-topic"]
    offset   = "latest"    # "earliest", "latest"
  }

  producer {
    topic       = "my-topic"
    acks        = "all"    # "none", "leader", "all"
    compression = "gzip"   # "none", "gzip", "snappy", "lz4"
  }

  sasl {
    mechanism = "PLAIN"
    username  = env("KAFKA_USER")
    password  = env("KAFKA_PASS")
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `brokers` | list | — | Kafka broker addresses |
| `consumer.group_id` | string | — | Consumer group ID |
| `consumer.topics` | list | — | Topics to subscribe |
| `consumer.offset` | string | `"latest"` | Start offset |
| `producer.topic` | string | — | Target topic |
| `producer.acks` | string | `"all"` | Acknowledgment level |
| `producer.compression` | string | `"none"` | Compression codec |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| Queue/topic name | source | Consume messages |
| `PUBLISH` | target | Publish a message |

## Example

```hcl
# Consume from queue, write to DB
flow "process_order" {
  from { connector = "rabbit", operation = "orders.new" }
  to   { connector = "db", target = "orders" }
}

# API call publishes to queue
flow "enqueue_order" {
  from { connector = "api", operation = "POST /orders" }
  to   { connector = "rabbit", operation = "PUBLISH", target = "orders.new" }
}
```

See the [mq example](../../examples/mq/) for a complete working setup.
