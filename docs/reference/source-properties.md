# Source Properties by Connector

Reference for all properties available in the `from` block when reading from each connector type. Every `from` block shares a set of [universal attributes](#universal-attributes); the sections below document what `operation` means for each connector and what `input.*` variables are available in transforms.

---

## Universal Attributes

Available on every `from` block regardless of connector type:

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `connector` | string | yes | Name of the source connector |
| `operation` | string | yes | Event type or endpoint (meaning varies per connector — see below) |
| `format` | string | no | Input format: `json`, `xml`, `csv` (default: `json`) |
| `filter` | string/block | no | CEL condition to skip non-matching events |

### Filter (block form — message queues)

```hcl
from {
  connector = "rabbit"
  operation = "payments"

  filter {
    condition   = "input.amount > 0"
    on_reject   = "requeue"    # "ack" (discard), "reject" (DLQ), "requeue" (retry)
    id_field    = "input.payment_id"
    max_requeue = 3
  }
}
```

### Schedule trigger (`when`)

Any flow can add a `when` block (cron) instead of an event-based `from`:

```hcl
flow "nightly_sync" {
  when {
    schedule = "0 0 * * *"    # Standard cron expression
    timezone = "UTC"
  }
  to { ... }
}
```

---

## REST Server

| Property | Value |
|----------|-------|
| **Connector type** | `rest` |
| **`operation` format** | `"METHOD /path"` — e.g., `"GET /users"`, `"POST /orders"`, `"GET /users/:id"` |

Path parameters use colon syntax (`:id`, `:user_id`).

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.<param>` | Path | Path parameters by name (`input.id`, `input.user_id`) |
| `input.<param>` | Query | Query string parameters by name (`input.page`, `input.limit`) |
| `input.<field>` | Body | JSON/XML body fields merged directly (POST/PUT/PATCH) |
| `input.headers` | Headers | Map of all request headers (lowercased keys) |
| `input.<field>` | Multipart | File uploads: `{filename, size, content_type, data}` (base64) |

```hcl
from {
  connector = "api"
  operation = "POST /users/:id/upload"
}

# Available: input.id (path), input.name (body), input.headers (map), input.avatar (file)
```

---

## GraphQL Server

| Property | Value |
|----------|-------|
| **Connector type** | `graphql` |
| **`operation` format** | `"Query.fieldName"`, `"Mutation.fieldName"`, `"Subscription.fieldName"` |

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.<arg>` | Arguments | GraphQL arguments passed to the field resolver |

```hcl
from {
  connector = "gql"
  operation = "Mutation.createUser"
}

# Available: input.name, input.email (from mutation arguments)
```

---

## gRPC Server

| Property | Value |
|----------|-------|
| **Connector type** | `grpc` |
| **`operation` format** | `"Service/Method"` or `"package.Service/Method"` |

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.<field>` | Proto message | All protobuf message fields (decoded via JSON) |

```hcl
from {
  connector = "grpc_server"
  operation = "UserService/CreateUser"
}

# Available: input.name, input.email (from proto request message)
```

---

## SOAP Server

| Property | Value |
|----------|-------|
| **Connector type** | `soap` (with `driver = "server"`) |
| **`operation` format** | SOAP operation name — e.g., `"CreateOrder"`, `"GetUser"` |

Extracted from the SOAP envelope body element name.

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.<field>` | SOAP body | Parameters parsed from the SOAP envelope body |

```hcl
from {
  connector = "soap_server"
  operation = "CreateOrder"
}

# Available: input.customer_id, input.items (from SOAP body elements)
```

---

## TCP Server

| Property | Value |
|----------|-------|
| **Connector type** | `tcp` |
| **`operation` format** | Message type string (json/msgpack) or NestJS pattern string |

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.<field>` | Message data | All fields from `msg.Data` merged directly |

```hcl
from {
  connector = "tcp_server"
  operation = "create_order"
}

# Available: input.product_id, input.quantity (from message data)
```

---

## RabbitMQ

| Property | Value |
|----------|-------|
| **Connector type** | `queue` (with `driver = "rabbitmq"`) |
| **`operation` format** | Routing key — e.g., `"orders.created"`, `"user.*"`, `"#"` |

Supports AMQP topic exchange patterns: `*` matches one word, `#` matches zero or more.

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.body` | Payload | Parsed JSON (or raw string) |
| `input.headers` | AMQP | AMQP headers as map |
| `input.properties` | AMQP | Message properties (see below) |
| `input.routing_key` | AMQP | The routing key |
| `input.exchange` | AMQP | The exchange name |

**`input.properties` fields:** `message_id`, `correlation_id`, `content_type`, `content_encoding`, `delivery_mode`, `priority`, `reply_to`, `expiration`, `type`, `user_id`, `app_id`, `timestamp`, `delivery_tag`, `redelivered`.

```hcl
from {
  connector = "rabbit"
  operation = "orders.created"
}

# Available: input.body.order_id, input.routing_key, input.properties.correlation_id
```

---

## Kafka

| Property | Value |
|----------|-------|
| **Connector type** | `queue` (with `driver = "kafka"`) |
| **`operation` format** | Topic name — e.g., `"orders"`, `"user-events"` |

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.body` | Payload | Parsed JSON (or raw string) |
| `input.headers` | Kafka | Kafka headers as map |
| `input.topic` | Kafka | Topic name |
| `input.partition` | Kafka | Partition number |
| `input.offset` | Kafka | Message offset |
| `input.key` | Kafka | Message key (string) |
| `input.timestamp` | Kafka | Unix timestamp |

```hcl
from {
  connector = "kafka"
  operation = "order-events"
}

# Available: input.body.event_type, input.key, input.partition, input.offset
```

---

## Redis Pub/Sub

| Property | Value |
|----------|-------|
| **Connector type** | `queue` (with `driver = "redis"`) |
| **`operation` format** | Channel name or glob pattern — e.g., `"orders"`, `"user.*"`, `"*"` |

Exact channel match first, then pattern match (from PSubscribe), then wildcard `"*"`.

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input._channel` | Redis | Channel the message was published to |
| `input._pattern` | Redis | Pattern (if matched via PSubscribe), omitted for exact subscriptions |
| `input.<field>` | Payload | JSON payload fields merged directly |
| `input.raw` | Payload | Raw string payload (if not valid JSON) |

```hcl
from {
  connector = "redis_events"
  operation = "orders.*"
}

# Available: input._channel ("orders.created"), input._pattern ("orders.*"), input.order_id
```

---

## MQTT

| Property | Value |
|----------|-------|
| **Connector type** | `mqtt` |
| **`operation` format** | MQTT topic pattern — e.g., `"sensors/+/temperature"`, `"home/#"` |

Supports MQTT wildcards: `+` matches single level, `#` matches multi-level.

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input._topic` | MQTT | Topic the message was received on |
| `input._message_id` | MQTT | MQTT message ID |
| `input._qos` | MQTT | QoS level (0, 1, or 2) |
| `input._retained` | MQTT | Whether the message was retained |
| `input.<field>` | Payload | JSON payload fields merged directly |
| `input._raw` | Payload | Raw string payload (if not valid JSON) |

```hcl
from {
  connector = "mqtt_broker"
  operation = "sensors/+/temperature"
}

# Available: input._topic ("sensors/room1/temperature"), input._qos, input.value, input.unit
```

---

## WebSocket

| Property | Value |
|----------|-------|
| **Connector type** | `websocket` |
| **`operation` format** | Event type: `"connect"`, `"disconnect"`, `"message"`, or custom type string |

### `input.*` variables

| Event | Variables |
|-------|-----------|
| `"connect"` | `input.event`, `input.remote_addr` |
| `"disconnect"` | `input.event` |
| `"message"` | `input.event`, data fields merged into `input`, `input.user_id` |
| custom type | `input.event`, `input.data`, `input.room` |

```hcl
from {
  connector = "ws"
  operation = "message"
}

# Available: input.event ("message"), input.user_id, input.text (from message data)
```

---

## SSE (Server-Sent Events)

| Property | Value |
|----------|-------|
| **Connector type** | `sse` |
| **`operation` format** | `"connect"` or `"disconnect"` |

SSE is unidirectional (server-to-client push). The `from` block only fires on lifecycle events.

### `input.*` variables

| Event | Variables |
|-------|-----------|
| `"connect"` | `input.event`, `input.client_id`, `input.remote_addr` |
| `"disconnect"` | `input.event`, `input.client_id` |

```hcl
from {
  connector = "sse"
  operation = "connect"
}

# Available: input.event, input.client_id, input.remote_addr
```

---

## CDC (Change Data Capture)

| Property | Value |
|----------|-------|
| **Connector type** | `cdc` |
| **`operation` format** | `"TRIGGER:table"` — e.g., `"INSERT:users"`, `"UPDATE:orders"`, `"*:*"` |

Trigger is uppercase (`INSERT`, `UPDATE`, `DELETE`). Wildcards: `"*:users"` (any trigger), `"INSERT:*"` (any table), `"*:*"` or `"*"` (all).

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.trigger` | CDC | `"INSERT"`, `"UPDATE"`, or `"DELETE"` |
| `input.table` | CDC | Table name (lowercase) |
| `input.schema` | CDC | Schema name (e.g., `"public"`) |
| `input.timestamp` | CDC | RFC3339 timestamp |
| `input.new` | CDC | New row data (INSERT/UPDATE) |
| `input.old` | CDC | Old row data (UPDATE/DELETE) |

```hcl
from {
  connector = "cdc"
  operation = "INSERT:users"
}

# Available: input.trigger, input.table, input.new.email, input.new.id
```

---

## File Watch

| Property | Value |
|----------|-------|
| **Connector type** | `file` (with `watch = true`) |
| **`operation` format** | Glob pattern — e.g., `"*.csv"`, `"reports/*.json"`, `"**/*.csv"` |

Matches against filename, relative path, or `**/` prefix with filename suffix.

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input._path` | File | Relative path from `base_path` |
| `input._name` | File | Filename only |
| `input._size` | File | File size in bytes |
| `input._mod_time` | File | RFC3339 modification time |
| `input._event` | File | `"created"` or `"modified"` |
| `input._error` | File | Error string (if file could not be read) |
| `input.<field>` | Content | Single-row file fields merged directly |
| `input.rows` | Content | Multi-row file content as array of maps |

```hcl
from {
  connector = "data_files"
  operation = "*.csv"
}

# Available: input._path, input._name, input._event, input.rows (array of CSV rows)
```

---

## Summary

| Connector | `operation` format | Key `input.*` fields |
|-----------|--------------------|----------------------|
| REST | `"METHOD /path"` (e.g., `"GET /users/:id"`) | path params, query params, body fields, `headers` |
| GraphQL | `"Query.field"` / `"Mutation.field"` / `"Subscription.field"` | argument fields |
| gRPC | `"Service/Method"` | proto message fields |
| SOAP | `"OperationName"` | SOAP body element children |
| TCP | message type/pattern string | `msg.Data` fields |
| RabbitMQ | routing key (`*` / `#` wildcards) | `body`, `headers`, `properties`, `routing_key`, `exchange` |
| Kafka | topic name | `body`, `headers`, `topic`, `partition`, `offset`, `key`, `timestamp` |
| Redis Pub/Sub | channel name or glob pattern | `_channel`, `_pattern`, payload fields |
| MQTT | topic pattern (`+` / `#` wildcards) | `_topic`, `_message_id`, `_qos`, `_retained`, payload fields |
| WebSocket | `"connect"` / `"disconnect"` / `"message"` / custom type | `event`, data fields, `user_id`, `room` |
| SSE | `"connect"` / `"disconnect"` | `event`, `client_id`, `remote_addr` |
| CDC | `"TRIGGER:table"` (e.g., `"INSERT:users"`) | `trigger`, `table`, `schema`, `new`, `old`, `timestamp` |
| File watch | glob pattern (e.g., `"*.csv"`) | `_path`, `_name`, `_size`, `_mod_time`, `_event`, content fields |

---

> **See also:** [Flows](../core-concepts/flows.md) for `from` block syntax, [Destination Properties](destination-properties.md) for `to` block properties, [Configuration Reference](configuration.md) for all HCL blocks.
