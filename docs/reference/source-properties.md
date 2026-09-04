# Source Properties by Connector

Reference for all properties available in the `from` block when reading from each connector type. Every `from` block shares a set of [universal attributes](#universal-attributes); the sections below document what `operation` means for each connector and what `input.*` variables are available in transforms.

---

## Universal Attributes

Available on every `from` block regardless of connector type:

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `connector` | string | yes | Name of the source connector |
| `operation` | string | depends — see below | Event type or endpoint (meaning varies per connector) |
| `format` | string | no | Input format: `json`, `xml`, `csv` (default: `json`) |
| `filter` | string/block | no | CEL condition to skip non-matching events |

### Is `operation` required?

Request/RPC-style sources address a specific endpoint, so `operation` is
mandatory. Stream-style sources subscribe to whatever the connector already
consumes, so `operation` only *narrows* what this flow handles — omit it and the
flow receives everything.

| `operation` **required** | `operation` **optional** (defaults to `"*"`, catch-all) |
|---|---|
| REST, GraphQL, gRPC, SOAP, TCP, SSE | RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, WebSocket, File watch |

!!! warning "On a stream source, `operation` is a filter — and it drops what it does not match"

    Optional does not mean inert. When you declare it, the value becomes the
    key this flow's handler is registered under, and every incoming message is
    matched against it. **A message matching no flow's pattern is dropped** —
    nacked without requeue on RabbitMQ, offset committed on Kafka, discarded on
    Redis. It is not an error, and without the startup notice below it is not
    visible either.

    Note this is a *second* filter. The broker has already decided what lands
    in the queue, through the exchange and binding you configured on the
    connector. `operation` narrows again, in-process, after that.

    The failure mode is a value that reads like a name — an endpoint, a queue,
    an entity — but matches no key the publisher actually sends. Everything is
    then discarded silently. If you do not need to split one subscription
    across several flows, **omit `operation`** and take the `"*"` catch-all.

    Since 2.13.0 this is stated at startup, per flow:

    ```
    INF dispatch: flow only accepts matching messages connector=rabbit flow=item_create
        operation=all.in.magento.q meaning="only deliveries whose key matches
        \"all.in.magento.q\" reach this flow"
    WRN dispatch: messages matching no pattern will be DROPPED connector=rabbit
        patterns="\"all.in.magento.q\"" hint="on a message queue source
        `operation` is a subscription pattern, not an operation name; omit it
        to accept every message"
    ```

    The warning appears only when **every** flow on that connector is narrowed,
    since one catch-all among them guarantees a handler for anything that
    arrives. Drops that do happen are counted in
    [`mycel_messages_undispatched_total`](../guides/observability.md#message-queue-metrics)
    and the first one per key is logged at error level.

A missing required `operation` is reported by `mycel validate` and fails startup:

```
flow "create_user": from block is missing attribute "operation" required by connector "api" (rest)
```

Each connector section below states which case it falls into.

### Filter (block form — message queues)

```hcl
from {
  connector = "rabbit"
  operation = "payments"

  filter {
    condition   = "input.amount > 0"
    on_reject   = "requeue"    # "ack" (discard), "reject" (DLQ), "requeue" (retry)
    id_field    = "input.body.payment_id"
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
| **`operation` required?** | **Required** |

Path parameters use colon syntax (`:id`, `:user_id`).

Supported methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, and `QUERY` — the RFC 10008 safe method whose query travels in the request body (GET semantics, POST ergonomics). QUERY runs the read path, its body is decoded like POST's, and a QUERY with content but no `Content-Type` is rejected with `415`. Responses on QUERY-capable paths advertise the accepted media types via the `Accept-Query` header. See [examples/query-method](https://github.com/matutetandil/mycel/tree/main/examples/query-method).

### `input.*` variables

| Variable | Source | Description |
|----------|--------|-------------|
| `input.<param>` | Path | Path parameters by name (`input.id`, `input.user_id`) |
| `input.<param>` | Query | Query string parameters by name (`input.page`, `input.limit`) |
| `input.<field>` | Body | JSON/XML body fields merged directly (POST/PUT/PATCH/QUERY, and DELETE when it carries one) |
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
| **`operation` required?** | **Required** |

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
| **`operation` required?** | **Required** |

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
| **`operation` required?** | **Required** |

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
| **`operation` required?** | **Required** |

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
| **Connector type** | `mq` (with `driver = "rabbitmq"`) |
| **`operation` format** | Routing-key **pattern** — e.g., `"orders.created"`, `"user.*"`, `"#"` |
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

Supports AMQP topic exchange patterns: `*` matches one word, `#` matches zero or more.

`operation` here is matched against `delivery.RoutingKey`, in this order: exact
match, then topic-pattern match, then `"*"`, then `"#"`. **A delivery matching
none of the registered patterns is nacked without requeue** — and discarded by
the broker unless the queue carries a dead-letter exchange. See
[Is `operation` required?](#is-operation-required) for what that means in
practice.

A common trap is setting `operation` to the **queue name**. That only works
while the publisher happens to use the queue name as its routing key, and
breaks silently the day it does not:

```hcl
from {
  connector = "rabbit"
  operation = "all.in.magento.q"   # ← a queue name, not a routing key
}
```

The queue is already selected by the connector's `queue {}` and binding. If one
flow handles everything on this queue, omit `operation`.

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
| **Connector type** | `mq` (with `driver = "kafka"`) |
| **`operation` format** | Topic name — e.g., `"orders"`, `"user-events"` |
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

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
| **Connector type** | `mq` (with `driver = "redis"`) |
| **`operation` format** | Channel name or glob pattern — e.g., `"orders"`, `"user.*"`, `"*"` |
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

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
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

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
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

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
| **`operation` required?** | **Required** |

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
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

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
| **`operation` required?** | Optional — defaults to `"*"` (catch-all) |

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

| Connector | `type` | `operation` | `operation` format | Key `input.*` fields |
|-----------|--------|-------------|--------------------|----------------------|
| REST | `rest` | required | `"METHOD /path"` (e.g., `"GET /users/:id"`) | path params, query params, body fields, `headers` |
| GraphQL | `graphql` | required | `"Query.field"` / `"Mutation.field"` / `"Subscription.field"` | argument fields |
| gRPC | `grpc` | required | `"Service/Method"` | proto message fields |
| SOAP | `soap` | required | `"OperationName"` | SOAP body element children |
| TCP | `tcp` | required | message type/pattern string | `msg.Data` fields |
| SSE | `sse` | required | `"connect"` / `"disconnect"` | `event`, `client_id`, `remote_addr` |
| RabbitMQ | `mq` + `driver = "rabbitmq"` | optional (`"*"`) | routing key (`*` / `#` wildcards) | `body`, `headers`, `properties`, `routing_key`, `exchange` |
| Kafka | `mq` + `driver = "kafka"` | optional (`"*"`) | topic name | `body`, `headers`, `topic`, `partition`, `offset`, `key`, `timestamp` |
| Redis Pub/Sub | `mq` + `driver = "redis"` | optional (`"*"`) | channel name or glob pattern | `_channel`, `_pattern`, payload fields |
| MQTT | `mqtt` | optional (`"*"`) | topic pattern (`+` / `#` wildcards) | `_topic`, `_message_id`, `_qos`, `_retained`, payload fields |
| WebSocket | `websocket` | optional (`"*"`) | `"connect"` / `"disconnect"` / `"message"` / custom type | `event`, data fields, `user_id`, `room` |
| CDC | `cdc` | optional (`"*"`) | `"TRIGGER:table"` (e.g., `"INSERT:users"`) | `trigger`, `table`, `schema`, `new`, `old`, `timestamp` |
| File watch | `file` + `watch = true` | optional (`"*"`) | glob pattern (e.g., `"*.csv"`) | `_path`, `_name`, `_size`, `_mod_time`, `_event`, content fields |

---

> **See also:** [Flows](../core-concepts/flows.md) for `from` block syntax, [Destination Properties](destination-properties.md) for `to` block properties, [Configuration Reference](configuration.md) for all HCL blocks.
