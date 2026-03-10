# Message Queues

Produce and consume messages with RabbitMQ and Kafka. Both use `type = "mq"` with a `driver` to select the backend.

## RabbitMQ

You can connect with either a full `url` or individual fields (`host`, `port`, `username`, `password`, `vhost`). If `url` is set, it takes precedence.

```hcl
# Option A: Full URL
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")   # amqp://user:pass@host:5672/vhost

  consumer {
    queue       = "my-queue"
    prefetch    = 10
    auto_ack    = false
  }
}

# Option B: Individual fields
connector "rabbit" {
  type     = "mq"
  driver   = "rabbitmq"
  host     = env("RABBITMQ_HOST")
  port     = 5672
  username = env("RABBITMQ_USER")
  password = env("RABBITMQ_PASS")
  vhost    = "/my-vhost"

  consumer {
    queue       = "my-queue"
    prefetch    = 10
    auto_ack    = false
  }

  publisher {
    exchange    = "my-exchange"
    routing_key = "my.routing.key"
  }
}
```

### Connection Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `url` | string | optional | — | Full AMQP URL (overrides host/port/username/password/vhost) |
| `host` | string | optional | `localhost` | RabbitMQ host |
| `port` | int | optional | `5672` | RabbitMQ port |
| `username` | string | optional | `guest` | Username |
| `password` | string | optional | `guest` | Password |
| `vhost` | string | optional | `/` | Virtual host |
| `heartbeat` | duration | optional | `10s` | Heartbeat interval |
| `connection_name` | string | optional | connector name | Connection name visible in RabbitMQ management |
| `reconnect_delay` | duration | optional | `5s` | Delay between reconnection attempts |
| `max_reconnects` | int | optional | `10` | Max reconnection attempts |

### TLS Options

```hcl
connector "rabbit_secure" {
  type   = "mq"
  driver = "rabbitmq"
  host   = "rabbit.example.com"
  port   = 5671

  tls {
    enabled             = true
    cert                = "./client.pem"
    key                 = "./client-key.pem"
    ca_cert             = "./ca.pem"
    insecure_skip_verify = false
  }
}
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `tls.enabled` | bool | **yes** | `false` | Enable TLS (switches to `amqps://`) |
| `tls.cert` | string | optional | — | Client certificate file |
| `tls.key` | string | optional | — | Client key file |
| `tls.ca_cert` | string | optional | — | CA certificate file |
| `tls.insecure_skip_verify` | bool | optional | `false` | Skip server certificate verification |

### Queue Options

Declare a queue explicitly. If omitted, the `consumer.queue` shorthand is used instead.

```hcl
queue {
  name        = "orders"
  durable     = true
  auto_delete = false
  exclusive   = false
  no_wait     = false
}
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `queue.name` | string | **yes** | — | Queue name |
| `queue.durable` | bool | optional | `true` | Survive broker restart |
| `queue.auto_delete` | bool | optional | `false` | Delete when last consumer disconnects |
| `queue.exclusive` | bool | optional | `false` | Exclusive to this connection |
| `queue.no_wait` | bool | optional | `false` | Do not wait for server confirmation |

### Exchange Options

Declare an exchange and optionally bind it to a queue.

```hcl
exchange {
  name        = "orders_exchange"
  type        = "topic"       # direct, fanout, topic, headers
  durable     = true
  auto_delete = false
  routing_key = "order.*"     # Binding pattern
}
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `exchange.name` | string | **yes** | — | Exchange name |
| `exchange.type` | string | optional | `direct` | Exchange type: `direct`, `fanout`, `topic`, `headers` |
| `exchange.durable` | bool | optional | `true` | Survive broker restart |
| `exchange.auto_delete` | bool | optional | `false` | Delete when no queues are bound |
| `exchange.internal` | bool | optional | `false` | Internal exchange (not directly publishable) |
| `exchange.no_wait` | bool | optional | `false` | Do not wait for server confirmation |
| `exchange.routing_key` | string | optional | — | Binding routing key pattern |

### Consumer Options

```hcl
consumer {
  queue       = "my-queue"     # Shorthand (creates queue block if not set)
  prefetch    = 10
  auto_ack    = false
  concurrency = 2              # Alias: workers
  tag         = "mycel"
  retry_count = 3              # Shorthand for DLQ with max_retries

  dlq {
    enabled     = true
    exchange    = "orders.dlx"
    queue       = "orders.dlq"
    max_retries = 3
    retry_delay = "5s"
  }
}
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `consumer.queue` | string | **yes**\* | — | Queue to consume from (\*creates a `queue {}` block if not set separately) |
| `consumer.prefetch` | int | optional | `10` | Prefetch count (QoS) |
| `consumer.auto_ack` | bool | optional | `false` | Auto-acknowledge messages |
| `consumer.concurrency` | int | optional | `1` | Number of consumer workers (alias: `workers`) |
| `consumer.tag` | string | optional | — | Consumer tag |
| `consumer.exclusive` | bool | optional | `false` | Exclusive consumer |
| `consumer.no_local` | bool | optional | `false` | Do not deliver own publications |
| `consumer.no_wait` | bool | optional | `false` | Do not wait for server confirmation |
| `consumer.retry_count` | int | optional | — | Shorthand: creates DLQ with `max_retries` set to this value |

### Dead Letter Queue (DLQ)

Configure automatic retry and dead-lettering for failed messages. Nest inside the `consumer` block.

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `consumer.dlq.enabled` | bool | optional | `true` | Enable DLQ processing |
| `consumer.dlq.exchange` | string | optional | `<exchange>.dlx` | DLQ exchange name |
| `consumer.dlq.queue` | string | optional | `<queue>.dlq` | DLQ queue name |
| `consumer.dlq.routing_key` | string | optional | — | Routing key for DLQ messages |
| `consumer.dlq.max_retries` | int | optional | `3` | Max retry attempts before dead-lettering |
| `consumer.dlq.retry_delay` | duration | optional | `0` | Delay before requeuing for retry |
| `consumer.dlq.retry_header` | string | optional | `x-retry-count` | Header name to track retry count |

### Publisher Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `publisher.exchange` | string | optional | — | Target exchange |
| `publisher.routing_key` | string | optional | — | Routing key |
| `publisher.mandatory` | bool | optional | `false` | Mandatory delivery |
| `publisher.immediate` | bool | optional | `false` | Immediate delivery |
| `publisher.persistent` | bool | optional | `true` | Persistent messages (survive broker restart) |
| `publisher.content_type` | string | optional | `application/json` | Message content type |
| `publisher.confirms` | bool | optional | `false` | Enable publisher confirms |

---

## Kafka

```hcl
connector "kafka" {
  type      = "mq"
  driver    = "kafka"
  brokers   = ["kafka1:9092", "kafka2:9092"]
  client_id = "my-service"

  consumer {
    group_id          = "my-consumer-group"
    topics            = ["my-topic"]
    auto_offset_reset = "earliest"   # "earliest", "latest"
    auto_commit       = true
    concurrency       = 2
  }

  producer {
    topic       = "my-topic"
    acks        = "all"         # "none", "one", "all"
    retries     = 3
    batch_size  = 16384
    linger_ms   = 5
    compression = "gzip"        # "none", "gzip", "snappy", "lz4", "zstd"
  }

  sasl {
    mechanism = "PLAIN"
    username  = env("KAFKA_USER")
    password  = env("KAFKA_PASS")
  }

  tls {
    enabled = true
    ca_cert = "./ca.pem"
  }
}
```

### Connection Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `brokers` | list | **yes** | `["localhost:9092"]` | Kafka broker addresses |
| `client_id` | string | optional | connector name | Client identifier |

### TLS Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `tls.enabled` | bool | **yes** | `false` | Enable TLS |
| `tls.cert` | string | optional | — | Client certificate file |
| `tls.key` | string | optional | — | Client key file |
| `tls.ca_cert` | string | optional | — | CA certificate file |
| `tls.insecure_skip_verify` | bool | optional | `false` | Skip server certificate verification |

### SASL Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `sasl.mechanism` | string | optional | `PLAIN` | Auth mechanism: `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512` |
| `sasl.username` | string | **yes** | — | SASL username |
| `sasl.password` | string | **yes** | — | SASL password |

### Consumer Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `consumer.group_id` | string | **yes** | — | Consumer group ID |
| `consumer.topics` | list | **yes** | — | Topics to subscribe |
| `consumer.auto_offset_reset` | string | optional | `earliest` | Start offset: `earliest`, `latest` |
| `consumer.auto_commit` | bool | optional | `true` | Auto-commit offsets |
| `consumer.min_bytes` | int | optional | `1` | Minimum bytes to fetch per request |
| `consumer.max_bytes` | int | optional | `10485760` | Maximum bytes to fetch (10 MB) |
| `consumer.max_wait_time` | duration | optional | `500ms` | Maximum time to wait for new data |
| `consumer.concurrency` | int | optional | `1` | Number of consumer workers |

### Producer Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `producer.topic` | string | **yes** | — | Default target topic |
| `producer.acks` | string | optional | `all` | Acknowledgment level: `none`, `one`, `all` |
| `producer.retries` | int | optional | `3` | Max delivery retries |
| `producer.batch_size` | int | optional | `16384` | Max batch size in bytes |
| `producer.linger_ms` | int | optional | `5` | Time (ms) to wait for batch to fill |
| `producer.compression` | string | optional | `none` | Compression: `none`, `gzip`, `snappy`, `lz4`, `zstd` |

### Schema Registry

Integrate with Confluent Schema Registry for schema validation and evolution.

```hcl
connector "kafka_avro" {
  type   = "mq"
  driver = "kafka"
  brokers = ["localhost:9092"]

  schema_registry {
    url                   = "http://localhost:8081"
    username              = env("SR_USER")
    password              = env("SR_PASS")
    subject_name_strategy = "topic"      # topic, record, topic_record
    auto_register         = false
    format                = "avro"       # avro, json, protobuf

    schemas {
      orders {
        key_schema      = "{\"type\":\"string\"}"
        value_schema    = "{\"type\":\"record\",\"name\":\"Order\",\"fields\":[...]}"
        value_schema_id = 1
      }
    }
  }

  consumer {
    group_id = "avro-consumer"
    topics   = ["orders"]
  }
}
```

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `schema_registry.url` | string | **yes** | — | Schema Registry endpoint |
| `schema_registry.username` | string | optional | — | Authentication username |
| `schema_registry.password` | string | optional | — | Authentication password |
| `schema_registry.subject_name_strategy` | string | optional | `topic` | Strategy: `topic`, `record`, `topic_record` |
| `schema_registry.auto_register` | bool | optional | `false` | Auto-register new schemas |
| `schema_registry.format` | string | optional | `avro` | Schema format: `avro`, `json`, `protobuf` |

Per-topic schemas (nested under `schema_registry.schemas.<topic>`):

| Option | Type | Description |
|--------|------|-------------|
| `key_schema` | string | Key schema definition |
| `key_schema_id` | int | Key schema ID in registry |
| `value_schema` | string | Value schema definition |
| `value_schema_id` | int | Value schema ID in registry |

---

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| Queue/topic name | source | Consume messages |
| `PUBLISH` | target | Publish a message |

## Filter Rejection Policy

When a flow consumes from a queue and has a `filter`, messages that don't match the condition are ACKed and **lost forever** by default. This is a problem when multiple consumers share the same queue with different filters — rejected messages should go back to the queue so other consumers can process them.

The `on_reject` policy controls what happens to filtered-out messages.

### When to use each policy

- **`ack`** (default) — You're the only consumer, or you don't care about unmatched messages.
- **`reject`** — Unmatched messages should go to a Dead Letter Queue for inspection or later processing.
- **`requeue`** — Multiple consumers share a queue with different filters. Unmatched messages go back to the queue for another consumer to pick up.

### Syntax

Two syntaxes are supported. Use the **string** form when you just want to skip messages (equivalent to `on_reject = "ack"`). Use the **block** form when you need a rejection policy.

```hcl
# String syntax — filtered messages are ACKed and discarded
flow "process_orders" {
  from {
    connector = "rabbit"
    operation = "orders.new"
    filter    = "input.body.status == 'pending'"
  }
  to { connector = "db", target = "orders" }
}

# Block syntax — filtered messages are requeued for other consumers
flow "process_sales" {
  from {
    connector = "rabbit"
    operation = "events"

    filter {
      condition   = "input.headers.elementType == 'sales-associate'"
      on_reject   = "requeue"
      id_field    = "input.properties.message_id"
      max_requeue = 5
    }
  }
  to { connector = "db", target = "sales" }
}

# Block syntax — filtered messages go to DLQ
flow "process_payments" {
  from {
    connector = "kafka"
    operation = "transactions"

    filter {
      condition = "input.body.type == 'payment'"
      on_reject = "reject"
    }
  }
  to { connector = "db", target = "payments" }
}
```

### Filter Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `condition` | string | **yes** | — | CEL expression to evaluate |
| `on_reject` | string | optional | `ack` | What to do with non-matching messages: `ack`, `reject`, `requeue` |
| `id_field` | string | optional | — | CEL expression to extract a unique message ID (used for requeue dedup) |
| `max_requeue` | int | optional | `3` | Max requeue attempts before giving up and ACKing silently |

### Policies

| Policy | RabbitMQ behavior | Kafka behavior |
|--------|-------------------|----------------|
| `ack` | ACK and discard | No-op (offset auto-committed) |
| `reject` | NACK without requeue — goes to DLX/DLQ if configured | Republish to `<topic>.dlq` |
| `requeue` | NACK with requeue — message returns to the queue | Republish to same topic |

### Requeue dedup

When using `requeue`, Mycel tracks how many times each message has been requeued to prevent infinite loops:

1. A message is filtered out and requeued.
2. If the same message comes back and is filtered again, the counter increments.
3. After `max_requeue` attempts (default 3), the message is ACKed silently — it won't loop forever.
4. Tracker entries expire after 10 minutes of inactivity.

**Message ID resolution:** The tracker needs a unique ID per message. It's resolved in this order:
1. `id_field` CEL expression (if configured) — e.g., `input.properties.message_id`
2. Native message ID — `MessageId` (RabbitMQ) or message key (Kafka)
3. If no ID is available, the message is ACKed immediately (RabbitMQ) or skipped (Kafka) to avoid untraceable loops.

---

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

---

> **Full configuration reference:** See [Message Queue](../reference/configuration.md#message-queue) in the Configuration Reference.
