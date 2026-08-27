# Destination Properties by Connector

Reference for all properties available in the `to` block when writing to each connector type. Every `to` block shares a set of [universal attributes](#universal-attributes); the sections below document what `target`, `operation`, `query`, and `params` mean for each connector.

---

## Universal Attributes

Available on every `to` block regardless of connector type:

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `connector` | string | required | Target connector name |
| `target` | string | — | Resource identifier (meaning varies per connector — see below) |
| `operation` | string | auto | Override operation type (meaning varies per connector) |
| `format` | string | `json` | Output format: `json`, `xml` |
| `query` | string | — | Raw SQL with named parameters (`:name`, `:id`) — resolved from transformed payload |
| `query_filter` | map | — | NoSQL filter document (MongoDB) |
| `update` | map | — | NoSQL update document (MongoDB `$set`, `$inc`, etc.) |
| `params` | map | — | Extra connector-specific parameters (CEL expressions) |
| `when` | string | — | CEL condition — only write if true. Context: `input`, `output` |
| `parallel` | bool | `true` | In multi-to, run this destination in parallel |
| `transform` | block | — | Per-destination CEL transform (overrides flow-level transform) |

### Data mapping

```
to.target        → connector.Data.Target
to.operation     → connector.Data.Operation
to.query         → connector.Data.RawSQL
to.query_filter  → connector.Data.Filters
to.update        → connector.Data.Update
to.params        → connector.Data.Params
transformed data → connector.Data.Payload
```

---

## Database (SQLite, PostgreSQL, MySQL)

| Property | Value |
|----------|-------|
| **`target`** | Table name (e.g., `"users"`, `"orders"`) |
| **`operation`** | `INSERT` (default for POST), `UPDATE`, `DELETE` |
| **`query`** | Raw SQL with named parameters (`:name`, `:email`). Resolved from payload |
| **`params`** | Not used (named params come from payload) |

### Named parameters in `query`

When using `query`, named parameters like `:name` are replaced with the corresponding field from the transformed payload:

```hcl
transform {
  number = "input.payload.associateNumber"
  name   = "input.payload.name"
  emails = "input.payload.emails.join(',')"
}

to {
  connector = "magento_db"
  target    = "sales_associate"
  query     = "INSERT INTO sales_associate (number, name, emails) VALUES (:number, :name, :emails) ON DUPLICATE KEY UPDATE name = :name, emails = :emails"
}
```

A placeholder is only recognised where the statement is code. Colons inside a comment, a string literal or a quoted identifier are left exactly as written, and so are Postgres casts:

```sql
-- ratio:sku is a comment, not a parameter
SELECT id, 'a:b' AS label, :sku::text
FROM catalog
WHERE sku = :sku          -- the item's parent may be promoted
```

Only the two `:sku` in the `SELECT` and `WHERE` bind; `ratio:sku`, `'a:b'`, and the `::text` cast are untouched. Comments are read per driver, so MySQL's `#` and its rule that `--` needs whitespace after it both apply where they should.

!!! note "Fixed in 3.3.0"
    Before 3.3.0 the binder knew about string literals and nothing else, so an apostrophe in a comment — `-- the item's parent` — opened a literal that never closed, and **every placeholder after it reached the driver unbound**. The statement failed with `missing named argument`, naming the parameter rather than the comment. In the other direction, `-- ratio:sku` was bound as if it were a placeholder and consumed one of the statement's arguments. `mycel validate` does not execute SQL, so neither showed up there.

A placeholder with no matching value is left as written rather than bound to nothing, so the driver rejects the statement you wrote instead of one with an argument silently missing.

### Standard operations (no `query`)

Without `query`, the operation is inferred from the HTTP method or set explicitly:

```hcl
# INSERT — payload fields become columns
to {
  connector = "db"
  target    = "users"
}

# UPDATE — filters from URL params, payload = SET clause
to {
  connector = "db"
  target    = "users"
  operation = "UPDATE"
}

# DELETE — filters from URL params
to {
  connector = "db"
  target    = "users"
  operation = "DELETE"
}
```

### PostgreSQL specifics

- `INSERT ... RETURNING *` returns the full created row (including `id`, `created_at`, etc.)

---

## MongoDB

| Property | Value |
|----------|-------|
| **`target`** | Collection name (e.g., `"users"`) |
| **`operation`** | `INSERT_ONE`, `INSERT_MANY`, `UPDATE_ONE`, `UPDATE_MANY`, `DELETE_ONE`, `DELETE_MANY`, `REPLACE_ONE` |
| **`query_filter`** | MongoDB filter document (WHERE equivalent) |
| **`update`** | MongoDB update document (`$set`, `$inc`, `$push`, etc.) |
| **`params`** | `{ upsert = true }`, `{ documents = [...] }` for INSERT_MANY |

```hcl
to {
  connector    = "mongodb"
  target       = "orders"
  operation    = "UPDATE_ONE"
  query_filter = { order_id = "input.order_id" }
  update       = { "$set" = { status = "completed", updated_at = "now()" } }
}
```

---

## Message Queues (RabbitMQ, Kafka, Redis Pub/Sub, MQTT)

| Property | RabbitMQ | Kafka | Redis Pub/Sub | MQTT |
|----------|----------|-------|---------------|------|
| **`target`** | Routing key | Topic | Channel | Topic |
| **`operation`** | `PUBLISH` (implicit) | `PUBLISH` | `PUBLISH` | `PUBLISH` |
| **`params`** | `{ exchange = "..." }` | — | — | `{ qos = 1, retain = true }` |

```hcl
# RabbitMQ
to {
  connector = "rabbit"
  target    = "order.created"
}

# Kafka
to {
  connector = "kafka"
  target    = "orders"
}

# MQTT with QoS
to {
  connector = "mqtt"
  target    = "sensors/temperature"
  params    = { qos = 1, retain = true }
}
```

The transformed payload becomes the message body (JSON).

---

## HTTP Client

| Property | Value |
|----------|-------|
| **`target`** | Endpoint path (e.g., `"/api/users"`, `"POST /api/notify"`) |
| **`operation`** | HTTP method override: `GET`, `POST`, `PUT`, `PATCH`, `DELETE` |

```hcl
to {
  connector = "external_api"
  target    = "/webhooks/order-created"
  operation = "POST"
}
```

The transformed payload becomes the request body. For read verbs, which carry no body, the payload's fields become query string parameters instead.

---

## GraphQL Client

| Property | Value |
|----------|-------|
| **`target`** | Full GraphQL query/mutation string |
| **`operation`** | Not used (embedded in the query string) |

```hcl
to {
  connector = "graphql_api"
  target    = <<-EOF
    mutation CreateUser($input: UserInput!) {
      createUser(input: $input) { id name email }
    }
  EOF
}
```

The transformed payload becomes the GraphQL variables.

---

## gRPC Client

| Property | Value |
|----------|-------|
| **`target`** | RPC method name (e.g., `"CreateUser"`, `"users.UserService/CreateUser"`) |
| **`operation`** | Alternative to `target` for the method name |

```hcl
to {
  connector = "grpc_service"
  target    = "CreateUser"
}
```

The transformed payload becomes the protobuf message fields.

---

## SOAP Client

| Property | Value |
|----------|-------|
| **`target`** | SOAP operation name (e.g., `"CreateItem"`, `"GetOrder"`) |
| **`operation`** | Alternative to `target` for the operation name |

```hcl
to {
  connector = "soap_service"
  target    = "CreateItem"
}
```

The transformed payload becomes the SOAP body parameters.

---

## File

| Property | Value |
|----------|-------|
| **`target`** | File path (relative to connector `base_path`) |
| **`operation`** | `WRITE` (default), `DELETE`, `COPY`, `MOVE` |
| **`params`** | `{ format = "csv" }`, `{ append = true }`, `{ sheet = "Data" }` (Excel) |

```hcl
# Write JSON
to {
  connector = "files"
  target    = "output/report.json"
}

# Append to CSV
to {
  connector = "files"
  target    = "logs/access.csv"
  params    = { format = "csv", append = true }
}

# Write to Excel sheet
to {
  connector = "files"
  target    = "reports/monthly.xlsx"
  params    = { sheet = "March" }
}
```

Format is auto-detected from file extension. The transformed payload becomes the file content.

---

## S3

| Property | Value |
|----------|-------|
| **`target`** | S3 object key (e.g., `"uploads/document.pdf"`) |
| **`operation`** | `PUT` (default), `DELETE`, `COPY` |
| **`params`** | `content`, `content_type`, `storage_class`, `acl`, `metadata` |

```hcl
to {
  connector = "s3"
  target    = "'uploads/' + input.user_id + '/avatar.png'"
  params    = {
    content       = "output._binary"
    content_type  = "'image/png'"
    storage_class = "'STANDARD'"
  }
}
```

---

## Exec

| Property | Value |
|----------|-------|
| **`target`** | Command to execute |
| **`operation`** | Not used |
| **`params`** | `{ args = [...] }`, `{ stdin = "..." }` |

```hcl
to {
  connector = "exec"
  target    = "convert"
  params    = { args = ["-resize", "800x600", "input.file_path", "output.jpg"] }
}
```

---

## Elasticsearch

| Property | Value |
|----------|-------|
| **`target`** | Index name (e.g., `"products"`) |
| **`operation`** | `index` (default), `update`, `delete`, `bulk` |

```hcl
to {
  connector = "search"
  target    = "products"
  operation = "index"
}
```

The transformed payload becomes the document to index. Returns `_id`, status.

---

## PDF

| Property | Value |
|----------|-------|
| **`target`** | Template path fallback (if not in connector config or payload) |
| **`operation`** | `generate` (returns binary for HTTP) or `save` (writes file) |

```hcl
to {
  connector = "invoice_pdf"
  operation = "generate"
}
```

Template is resolved: payload `template` field > connector config `template` > `target` fallback. All other payload fields become template variables (`{{.field_name}}`). Special payload fields: `filename` (for Content-Disposition).

---

## WebSocket

| Property | Value |
|----------|-------|
| **`target`** | Room name (for `send_to_room`) |
| **`operation`** | `broadcast`, `send_to_room`, `send_to_user` |

```hcl
# Broadcast to all clients
to {
  connector = "ws"
  operation = "broadcast"
}

# Send to specific room
to {
  connector = "ws"
  operation = "send_to_room"
  target    = "order-updates"
}

# Send to specific user (user_id from payload)
to {
  connector = "ws"
  operation = "send_to_user"
}
```

For `send_to_user`, the payload or filters must include `user_id`.

---

## SSE (Server-Sent Events)

| Property | Value |
|----------|-------|
| **`target`** | Room name |
| **`operation`** | `broadcast`, `send_to_room` |

```hcl
to {
  connector = "sse"
  operation = "send_to_room"
  target    = "dashboard"
}
```

---

## TCP Client

| Property | Value |
|----------|-------|
| **`target`** | Connection identifier |
| **`operation`** | `SEND` (implicit) |

The payload is serialized according to the connector's protocol setting (`json`, `msgpack`, `nestjs`).

---

## Notification Connectors

Notification connectors receive the transformed payload as the message. The payload fields map to the notification's properties.

### Email

| Payload field | Type | Description |
|---------------|------|-------------|
| `to` | array | `[{email, name}]` recipients |
| `subject` | string | Email subject |
| `text_body` | string | Plain text body |
| `html_body` | string | HTML body |
| `template` | string | Override connector-level template path |
| `template_data` | map | Variables for template rendering |
| `cc`, `bcc` | array | CC/BCC recipients |
| `attachments` | array | `[{filename, content, content_type}]` |

```hcl
to {
  connector = "email_smtp"
  operation = "send"
}
```

Template resolution: payload `template` > connector config `template`.

### Slack

| Payload field | Type | Description |
|---------------|------|-------------|
| `text` | string | Message text |
| `channel` | string | Channel name or ID |
| `blocks` | array | Slack Block Kit blocks |
| `thread_ts` | string | For threaded replies |

### Discord

| Payload field | Type | Description |
|---------------|------|-------------|
| `content` | string | Message text |
| `channel_id` | string | Channel ID |
| `embeds` | array | Discord embed objects |

### SMS

| Payload field | Type | Description |
|---------------|------|-------------|
| `to` | string | Phone number (E.164) |
| `message` | string | SMS body |

### Push

One of `token`, `tokens`, `topic` or `condition` says who receives it.

| Payload field | Type | Description |
|---------------|------|-------------|
| `token` | string | The device to send to |
| `tokens` | list | Several devices at once |
| `topic` | string | Everyone subscribed to a topic |
| `condition` | string | Everyone matching a topic condition |
| `title` | string | Notification title |
| `body` | string | Notification body |
| `data` | map | Custom data payload |
| `priority` | string | `normal` or `high` |
| `collapse_key` | string | Replaces an earlier undelivered notification with the same key |
| `ttl` | int | Seconds the service should keep trying |

### Webhook

| Payload field | Type | Description |
|---------------|------|-------------|
| `url` | string | Override connector URL |
| `method` | string | HTTP method |
| `headers` | map | Extra headers |
| `body` | any | Request body |

---

## Special Response Fields

These fields in the flow result have special meaning when returned through the REST connector:

| Field | Type | Recognized by | Purpose |
|-------|------|---------------|---------|
| `_binary` | string (base64) | REST | Serve as binary download |
| `_content_type` | string | REST | MIME type for binary response |
| `_filename` | string | REST | Content-Disposition filename |
| `http_status_code` | int/string | REST, SOAP | Override HTTP status code |
| `grpc_status_code` | int/string | gRPC | Override gRPC status code |
| `_response_headers` | map | REST (aspects) | Extra HTTP response headers |

---

> **See also:** [Flows](../core-concepts/flows.md) for `to` block syntax, [Configuration Reference](configuration.md) for all HCL blocks.
