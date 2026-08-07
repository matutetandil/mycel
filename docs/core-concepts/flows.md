# Flows

A flow is the unit of work in Mycel. It defines where data comes **from**, what happens to it, and where it goes **to**. When the source connector receives an event — an HTTP request, a queue message, a cron tick, a CDC database change — the flow executes.

## Minimal Flow

```hcl
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
```

A flow needs only `from` and `to`. Everything else is optional.

## Flow Anatomy

Everything a flow can contain, in the order it runs. Each block is optional
except `from`; a flow with no `to` is an [echo flow](#response) that answers from
its own `response` block.

```
        from ─── filter ─── accept ─── validate ─── enrich ─── transform ─── dedupe ─── to
       source     drop      business    schema     fetch more   reshape     skip     write
       event    non-matching  gate       check     data/steps    payload  duplicates
```

`lock`, `semaphore`, `coordinate` and `sequence_guard` wrap the whole pipeline;
`error_handling`, `cache`, `batch`, `async` and `idempotency` change how it is
scheduled and retried; `after` runs once the write succeeds.

### Blocks

| Block | What it does | Where it's documented |
|-------|--------------|-----------------------|
| `from` | The trigger: which connector fires the flow, and for what event | [below](#the-from-block) · [source properties](../reference/source-properties.md) |
| `to` | Where the flow writes. Repeat it to fan out, nest `transaction` for all-or-nothing writes | [below](#the-to-block) · [destination properties](../reference/destination-properties.md) |
| `accept` | Business-level gate — "is this message mine?" — with an ack/reject/requeue policy | [below](#the-accept-block) |
| `validate` | Check the payload against a `type` before and/or after transforming | [below](#validate-block) · [types](types.md) |
| `require` | Demand that specific fields be present, with a custom error | [below](#require-block) |
| `enrich` | Pull extra data from another connector and merge it into the payload | [below](#enrich-block) · [multi-step flows](../guides/multi-step-flows.md) |
| `step` | Named intermediate connector calls, referenced later as `step.<name>.<field>` | [below](#step-block) · [multi-step flows](../guides/multi-step-flows.md) |
| `transform` | Reshape the payload with CEL | [below](#transform) · [transforms](transforms.md) |
| `response` | Shape what the caller gets back, independently of what was written | [below](#response) |
| `after` | Fire-and-forget work once the write succeeds | [below](#after-block) |
| `cache` | Serve reads from cache and invalidate on write | [below](#cache-block) · [caching](../guides/caching.md) |
| `dedupe` | Skip a message whose transformed payload matches the last one processed | [below](#dedupe-block) · [caching](../guides/caching.md) |
| `idempotency` | Replay the stored result when the same idempotency key arrives twice | [configuration reference](../reference/configuration.md#idempotency-block) |
| `error_handling` | Retry with backoff, dead-letter queues, circuit breakers, per-class dispositions | [below](#error-handling-block) · [error handling](../guides/error-handling.md) |
| `lock` | Distributed mutex — one flow instance per key at a time | [below](#lock-mutex) · [synchronization](../guides/synchronization.md) |
| `semaphore` | Allow at most N concurrent instances | [below](#semaphore-n-concurrent) · [synchronization](../guides/synchronization.md) |
| `coordinate` | Signal/wait between flows | [below](#coordinate-signalwait) · [synchronization](../guides/synchronization.md) |
| `sequence_guard` | Reject messages that arrive out of order, by monotonic sequence number | [below](#sequence-guard-ordering) · [synchronization](../guides/synchronization.md) |
| `batch` | Process large datasets in chunks | [batch processing](../guides/batch-processing.md) |
| `async` | Answer `202 Accepted` immediately and run the flow in the background | [configuration reference](../reference/configuration.md#async-block) |
| `state_transition` | Drive a state machine as part of the flow | [below](#state-transition-block) · [sagas & state machines](../guides/sagas.md) |

Any of these blocks can also be declared once at the top level and reused across
flows with `use = "<kind>.<name>"` — see [Reusable Blocks](reusable-blocks.md).

### Flow-level attributes

| Attribute | What it does | Where it's documented |
|-----------|--------------|-----------------------|
| `when` | Run on a cron schedule instead of waiting for a source event | [below](#scheduled-flows-cron) |
| `cache` | Reference a named cache — `cache = "cache.<name>"` | [caching](../guides/caching.md) |
| `returns` | GraphQL return type for HCL-first schemas | [below](#returns-graphql-type) |
| `entity` | Mark the flow as a GraphQL Federation entity resolver | [below](#federation-entity-resolver) |

> Looking for the bare syntax of every block, without the prose? See the
> [Configuration Reference](../reference/configuration.md#flow).

## The `from` Block

`from` defines the trigger: which connector fires the flow and for what event.

```hcl
from {
  connector = "api"           # Required: connector name
  operation = "GET /users"    # Event type or endpoint — required for some connectors (see below)
  format    = "json"          # Optional: expected input format ("json", "xml", "csv", "tsv")
}
```

### `from` attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `connector` | string | yes | Name of the source connector |
| `operation` | string | **depends on the connector** | Operation or event to listen for — see below |
| `format` | string | no | Input format: `json`, `xml`, `csv` (default: `json`) |
| `filter` | string/block | no | CEL condition to skip non-matching events |

### Is `operation` required?

It depends on the source connector. Request/RPC-style sources address a specific
endpoint, so `operation` is mandatory. Stream-style sources subscribe to whatever
the connector is already configured to consume, so `operation` merely *narrows*
what this flow handles — omit it and the flow receives everything.

| Source connector | `operation` | If omitted |
|------------------|-------------|------------|
| `rest` | **required** | Startup error |
| `graphql` | **required** | Startup error |
| `grpc` | **required** | Startup error |
| `soap` | **required** | Startup error |
| `tcp` | **required** | Startup error |
| `sse` | **required** | Startup error |
| `mq` (RabbitMQ, Kafka, Redis Pub/Sub) | optional | Defaults to `"*"` — catch-all |
| `mqtt` | optional | Defaults to `"*"` — catch-all |
| `cdc` | optional | Defaults to `"*"` — every trigger, every table |
| `websocket` | optional | Defaults to `"*"` — every event type |
| `file` (watch) | optional | Defaults to `"*"` — every matched file |

A missing required `operation` is caught by `mycel validate` and at startup:

```
flow "create_user": from block is missing attribute "operation" required by connector "api" (rest)
```

So a queue consumer that handles every message on its queue needs no `operation`
at all:

```hcl
flow "process_all_orders" {
  from {
    connector = "rabbit"    # consumes whatever the connector's consumer{} block is bound to
  }
  to {
    connector = "db"
    target    = "orders"
  }
}
```

> **What does `operation` mean for each connector, and what `input.*` variables are available?** See the [Source Properties by Connector](../reference/source-properties.md) reference.

### Filter (simple)

Skip events that don't match a condition:

```hcl
from {
  connector = "rabbit"
  operation = "orders"
  filter    = "input.country == 'US'"
}
```

### Filter (block — with rejection policy)

For message queues, control what happens to rejected messages:

```hcl
from {
  connector = "rabbit"
  operation = "payments"

  filter {
    condition   = "input.amount > 0"
    on_reject   = "requeue"  # "ack" (discard), "reject" (DLQ), "requeue" (retry)
    id_field    = "input.payment_id"   # For deduplication tracking
    max_requeue = 3          # Maximum times a message can be requeued
  }
}
```

`on_reject` options:
- `ack` (default) — acknowledge and discard the message
- `reject` — send to the dead-letter queue
- `requeue` — put back in the queue (up to `max_requeue` times)

## The `accept` Block

`accept` is a business-level gate that runs **after** `filter` but **before** `transform`. While `filter` determines whether a message belongs to this flow (structural match), `accept` determines whether this flow should actually process it (business decision).

This is useful when multiple flows consume from the same queue: a message passes the filter for several flows, but only one should process it. The others can requeue it.

```hcl
accept {
  when      = "input.payload.type == 'A1'"
  on_reject = "requeue"
}
```

### `accept` attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `when` | string | — | **Required.** CEL expression that must return `true` to proceed |
| `on_reject` | string | `"ack"` | What to do when condition is false: `"ack"`, `"reject"`, `"requeue"` |

`on_reject` options (same as filter):
- `ack` (default) — acknowledge and discard the message
- `reject` — send to the dead-letter queue
- `requeue` — put back in the queue for another consumer

### Example: Multiple flows, one queue

```hcl
# Flow A: only processes type A1
flow "handle_type_a1" {
  from {
    connector = "rabbit"
    operation = "events"
    filter    = "has(input.metadata) && input.metadata.operation == 'upsert'"
    on_reject = "ack"
  }

  accept {
    when      = "input.payload.type == 'A1'"
    on_reject = "requeue"  # Not for me — put it back
  }

  transform { ... }
  to {
    connector = "db"
    target    = "type_a1_table"
  }
}

# Flow B: only processes type B2
flow "handle_type_b2" {
  from {
    connector = "rabbit"
    operation = "events"
    filter    = "has(input.metadata) && input.metadata.operation == 'upsert'"
    on_reject = "ack"
  }

  accept {
    when      = "input.payload.type == 'B2'"
    on_reject = "requeue"
  }

  transform { ... }
  to {
    connector = "db"
    target    = "type_b2_table"
  }
}
```

### Pipeline position

```
from → filter → accept → validate → enrich/steps → transform → dedupe → to
```

Since v2.1.0 the `dedupe` block runs **after** `transform` — the content-based primitive needs the transformed payload (`output.*`) to compute its fingerprint. Earlier versions ran the (key-based) dedupe before transform.

## The `to` Block

`to` defines where the flow writes its output.

```hcl
to {
  connector = "db"
  target    = "users"
}
```

### `to` attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `connector` | string | required | Target connector name |
| `target` | string | — | Table, topic, file path, etc. |
| `operation` | string | — | Override operation type (`INSERT`, `UPDATE`, `DELETE`, named operation) |
| `format` | string | `json` | Output format: `json`, `xml` |
| `filter` | string | — | CEL condition for per-user filtering (WebSocket, SSE, subscriptions) |
| `query` | string | — | SQL query (for database writes with custom SQL) |
| `query_filter` | map | — | NoSQL filter document (MongoDB) |
| `update` | map | — | NoSQL update document (MongoDB) |
| `params` | map | — | Extra parameters (e.g., for S3 COPY operations) |
| `when` | string | — | CEL condition: only write if this evaluates to true |
| `parallel` | bool | `true` | Whether multi-to targets run in parallel |

> **What does `target`, `operation`, `query`, and `params` mean for each connector?** See the [Destination Properties by Connector](../reference/destination-properties.md) reference.

### Conditional write

Only write to the target if a condition is met:

```hcl
to {
  connector = "db"
  target    = "high_value_orders"
  when      = "input.amount > 1000"
}
```

### Per-destination transform

Apply a transform only for this destination (useful with multi-to):

```hcl
to {
  connector = "db"
  target    = "orders"
  transform {
    id         = "uuid()"
    created_at = "now()"
    status     = "'pending'"
  }
}
```

### Transactional write (`transaction`)

For a database connector, a `transaction` block runs an **ordered list of SQL
statements inside a single pinned connection wrapped in one `BEGIN`/`COMMIT`** —
all-or-nothing. Use it when one logical write spans several statements that must
be atomic and need to pass values between them (a captured `LAST_INSERT_ID`, a
looked-up id), which a single-statement `to` write cannot express.

```hcl
to {
  connector = "db"            # must be a database connector

  transaction {
    exec {
      query  = "DELETE FROM child WHERE owner_id = :owner"
      params = { owner = "output.owner_id" }
      when   = "output.owner_id > 0"        # optional CEL gate; false = skip
    }

    exec {
      query   = "INSERT INTO parent (owner_id, name) VALUES (:owner, :name)"
      params  = { owner = "output.owner_id", name = "output.name" }
      capture = "parent_id"                 # INSERT → captured.parent_id = last insert id
    }

    each "child" in "output.children" {     # iterate a list from the payload
      exec {
        query   = "INSERT INTO child (parent_id, label, position) VALUES (:pid, :label, :pos)"
        params  = {
          pid   = "captured.parent_id"       # value captured above
          label = "child.label"              # current element
          pos   = "child_index"              # 0-based each index
        }
        capture = "child_id"
      }

      each "store" in "child.stores" {       # each is nestable
        exec {
          query  = "INSERT INTO child_value (child_id, store_id) VALUES (:cid, :sid)"
          params = { cid = "captured.child_id", sid = "store.id" }
        }
      }
    }

    exec {
      query   = "SELECT option_id FROM lookup WHERE code = :c LIMIT 1"
      params  = { c = "output.code" }
      capture = "option_id"                 # SELECT → first column of first row (null if 0 rows)
    }
  }
}
```

**Statements (textual order is significant — captured values flow forward):**

- **`exec`** runs one SQL statement.
  - `query` — SQL with `:named` placeholders. *(required)*
  - `params` — map of placeholder name → CEL expression.
  - `when` — optional CEL gate; the statement is skipped (not an error) when false.
  - `capture` — optional name stored under `captured.<name>`: the **last insert id**
    for `INSERT`/`UPDATE`/`DELETE`, or the **first column of the first row** for a
    `SELECT` (`null` when there are no rows).
- **`each "<var>" in "<listExpr>"`** evaluates a CEL list and runs its body once
  per element, binding the element to `<var>` and its 0-based index to
  `<var>_index`. A non-list or empty result runs nothing. Nestable.

**CEL scope inside a transaction:** `input`, `output` (the transform result),
`step`, `captured` (values captured so far), plus the active `each` bindings.

**Atomicity & error handling:** commit on success; any error — a failing
statement, an unresolved `when`/param expression, or a panic — rolls back the
**entire** transaction. The error then propagates to the flow's
`error_handling` (retry / `on_timeout` / `on_error` dispositions) exactly like a
failed single-statement write. The transaction is also wrapped by `dedupe` and
`after`/`on_error` aspects as a single unit.

**Rules:** the `to` connector must be of type `database`; `transaction` is
mutually exclusive with `query` / `target` / `operation` / `envelope` in the
same `to` block (`mycel validate` enforces both). See the
[transactional-write example](https://github.com/matutetandil/mycel/tree/main/examples/transactional-write).

### Multi-to (fan-out)

Write to multiple targets by declaring multiple `to` blocks:

```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  to {
    connector = "db"
    target    = "orders"
  }
  to {
    connector = "rabbit"
    target    = "order.created"
    when      = "input.amount > 500"  # Only for large orders
  }
  to {
    connector = "cache"
    target    = "order_counts"
    operation = "INCR"
  }
}
```

By default, multiple `to` blocks execute in parallel. Set `parallel = false` on a `to` block to force sequential execution.

### Source Fan-Out (Multiple Flows from Same Source)

Multiple flows can share the same `from` connector and operation. When a request or message arrives, **all registered flows execute concurrently**:

```hcl
# Flow 1: Save order to database
flow "save_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}

# Flow 2: Send notification (same source, runs concurrently)
flow "notify_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  transform {
    channel = "'#orders'"
    text    = "'New order received: ' + input.customer"
  }
  to {
    connector = "slack"
    target    = "message"
  }
}
```

The behavior depends on the connector type:

| Connector type | Behavior |
|---|---|
| **Request-response** (REST, gRPC, TCP, WebSocket, SOAP, SSE, GraphQL) | First registered flow returns the response. Additional flows run as fire-and-forget in background goroutines. |
| **Event-driven** (RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch) | All flows execute in parallel. The message is acknowledged only after **all** flows complete successfully. |

Errors in fire-and-forget flows (request-response) are logged but don't affect the primary response. Errors in event-driven flows cause the message to be NACKed/retried according to the connector's error handling policy.

This differs from [multi-to](#multi-to-fan-out) which sends the _same flow's_ output to multiple destinations. Source fan-out runs _independent flows_ with their own transforms, validation, and error handling.

## Scheduled Flows (Cron)

Run a flow on a schedule instead of from a connector event:

```hcl
flow "daily_cleanup" {
  when = "0 3 * * *"  # Cron: every day at 3 AM

  to {
    connector = "db"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

flow "health_ping" {
  when = "@every 5m"
  to {
    connector = "monitoring"
    operation = "POST /heartbeat"
  }
}
```

Shortcuts: `@hourly`, `@daily`, `@weekly`, `@monthly`. Combine with `lock` to prevent duplicate execution across instances.

## Transform

Transform data between source and target using CEL expressions:

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    created_at = "now()"
    status     = "'active'"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

Reference a named (reusable) transform:

```hcl
transform {
  use = "transform.normalize_user"
  # Override or add fields
  source = "'api'"
}
```

See [Transforms](transforms.md) for all CEL functions and patterns.

## Response

Transform the output **after** receiving the result from the destination (or define the response directly for echo flows without `to`):

```hcl
# With destination — transform what the DB returns before sending to the client
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  to {
    connector = "db"
    target    = "users"
  }
  response {
    full_name = "output.first_name + ' ' + output.last_name"
    email     = "lower(output.email)"
  }
}

# Without destination — define the response directly (echo flow)
flow "process" {
  from {
    connector = "api"
    operation = "POST /process"
  }
  response {
    id    = "uuid()"
    email = "lower(input.email)"
    name  = "upper(input.name)"
  }
}
```

Available variables:
- `input.*` — original request data
- `output.*` — destination result (only when `to` is present)

### Status Code Override

Control the HTTP status code from the response block:

```hcl
flow "not_implemented" {
  from {
    connector = "api"
    operation = "DELETE /users/:id"
  }
  response {
    http_status_code = "501"
    error            = "'Not yet implemented'"
  }
}
```

Supported status code fields by connector:
- **REST / SOAP**: `http_status_code`
- **gRPC**: `grpc_status_code` (maps to gRPC status codes)

### Transform vs Response

| Block | When it runs | Available variables | Purpose |
|-------|-------------|---------------------|---------|
| `transform` | **Before** sending to destination | `input.*`, `enriched.*`, `step.*` | Reshape input data |
| `response` | **After** receiving from destination | `input.*`, `output.*` | Reshape output data |

Both blocks are optional and can be used together in the same flow.

## Validate Block

Validate input or output against a type schema:

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  validate {
    input  = "user_input"   # Validates request body
    output = "user"         # Validates transform result before writing
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

Both `input` and `output` accept either a type name string or a `type.name` reference. Validation failure returns HTTP 422 with field-level error details.

## Require Block

Enforce role-based or permission-based access control on a flow:

```hcl
flow "delete_user" {
  from {
    connector = "api"
    operation = "DELETE /users/:id"
  }

  require {
    roles       = ["admin"]
    permissions = ["users:delete"]
  }

  to {
    connector = "db"
    operation = "DELETE users"
  }
}
```

`roles` and `permissions` are checked against the authenticated user's JWT claims. Requires the [auth](../guides/auth.md) system to be configured.

## Step Block

Steps call intermediate connectors and make their results available to subsequent steps and transforms. Use them when a flow needs data from multiple sources.

```hcl
flow "get_order_detail" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  step "order" {
    connector = "db"
    operation = "query"
    query     = "SELECT * FROM orders WHERE id = ?"
    params    = [input.params.id]
  }

  step "customer" {
    connector = "customers_api"
    operation = "GET /customers/${step.order.customer_id}"
    when      = "step.order.customer_id != ''"  # Skip if no customer
    on_error  = "skip"
    default   = {}
  }

  transform {
    output = merge(step.order, { "customer": step.customer })
  }

  to {
    connector = "api"
    target    = "response"
  }
}
```

### Step attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `connector` | string | Required: connector to call |
| `operation` | string | Operation or endpoint |
| `query` | string | SQL query (database connectors) |
| `target` | string | Table or resource |
| `params` | map/list | Query parameters |
| `body` | map | Request body (HTTP connectors) |
| `when` | string | CEL condition — skip step if false |
| `timeout` | string | Step timeout: `"5s"`, `"30s"` |
| `on_error` | string | `"skip"` — continue flow if step fails |
| `default` | any | Value to use when step is skipped or fails |
| `format` | string | Data format for this step |

Step results are available as `step.NAME` in subsequent steps and in the transform block.

## Enrich Block

Enrich data by fetching from external services before transforming:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  enrich "inventory" {
    connector = "inventory_api"
    operation = "GET /stock"
    params {
      sku = "input.sku"
    }
  }

  transform {
    id       = "input.id"
    name     = "input.name"
    price    = "enriched.pricing.price"
    in_stock = "enriched.inventory.available > 0"
  }

  to {
    connector = "db"
    target    = "products"
  }
}
```

Enriched data is available as `enriched.NAME` in CEL expressions.

## Cache Block

Cache flow responses to avoid repeated connector calls:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  cache {
    storage      = "redis_cache"
    ttl          = "5m"
    key          = "'product:' + input.params.id"
    invalidate_on = ["product.updated", "product.deleted"]
  }

  to {
    connector = "db"
    target    = "products WHERE id = :id"
  }
}
```

See [Caching Guide](../guides/caching.md) for details.

## After Block

Run cache invalidation or side effects after the flow completes:

```hcl
flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  to {
    connector = "db"
    target    = "UPDATE products"
  }

  after {
    invalidate {
      storage  = "redis_cache"
      keys     = ["product:${input.params.id}"]
      patterns = ["products:list:*"]
    }
  }
}
```

## Dedupe Block

Drop **no-op** messages before reaching the downstream by comparing a canonical fingerprint of the persisted projection against the last stored fingerprint for the same key. Phase A runs after `transform` and before `to`; Phase B stores the new fingerprint only if `to` succeeds, so a failed-then-retried message does not self-discard. The primitive self-locks per key so two workers cannot double-call the downstream with identical content.

Useful for MQ consumers where the upstream re-sends update messages even when nothing relevant changed.

```hcl
connector "fp_cache" {
  type   = "cache"
  driver = "redis"   # or "memory" for tests
}

flow "process_payment" {
  from {
    connector = "rabbit"
    operation = "payments"
  }

  transform {
    payment_id = "input.payment_id"
    account_id = "input.account_id"
    amount     = "input.amount"
  }

  dedupe {
    cache        = "fp_cache"
    key          = "'payment:' + input.payment_id"
    ttl          = "24h"
    on_duplicate = "ack"      # ack | reject | requeue (sequence_guard vocabulary)
    fingerprint {
      payment_id = "output.payment_id"
      account_id = "output.account_id"
      amount     = "output.amount"
    }
  }

  to {
    connector = "db"
    target    = "payments"
  }
}
```

| Attribute | Required | Description |
|---|---|---|
| `cache` | yes | Name of a `connector { type = "cache" }` used to store fingerprints. Pool is initialized once at startup; the hot path does not pay a registry lookup per message |
| `key` | yes | CEL expression for the per-resource fingerprint key (evaluated against `input.*`) |
| `fingerprint {}` | yes | Block of named CEL expressions whose values form the projection; must list every persisted field — omitting one would silently drop real changes |
| `ttl` | no | How long to keep stored fingerprints. Supports `"30d"` and `"2w"` plus stdlib units; malformed values fail the parse |
| `on_duplicate` | no | Behavior on match: `"ack"` (default), `"reject"`, `"requeue"` |

**Pipeline order:** `dedupe` runs **after** `transform` because the fingerprint references `output.*` (the transformed payload). Earlier versions ran the (key-based) dedupe before transform — see CHANGELOG v2.1.0 for migration.

**Array order-insensitivity:** the canonical encoder sorts array elements before serialization, treating them as order-insensitive sets. Reshape order-sensitive arrays into delimited strings in `transform` before dedupe sees them.

## Error Handling Block

Configure retry, fallback, and custom error responses:

```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  error_handling {
    retry {
      attempts  = 3
      delay     = "1s"
      max_delay = "30s"
      backoff   = "exponential"
    }

    fallback {
      connector     = "rabbit"
      target        = "orders.failed"
      include_error = true
    }

    error_response {
      status = 422
      body {
        error = "'Order creation failed'"
        code  = "'ORDER_ERROR'"
      }
    }
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
```

See [Error Handling Guide](../guides/error-handling.md).

## State Transition Block

Trigger a state machine transition as part of a flow:

```hcl
flow "update_order_status" {
  from {
    connector = "api"
    operation = "POST /orders/:id/events"
  }

  state_transition {
    machine = "order_status"
    entity  = "orders"
    id      = "input.params.id"
    event   = "input.event"
    data    = "input.data"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
```

## Synchronization Blocks

Prevent concurrent access to shared resources:

### Lock (mutex)

```hcl
flow "process_payment" {
  from {
    connector = "rabbit"
    operation = "payments"
  }

  lock {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'account:' + input.account_id"
    timeout = "30s"
    wait    = true
    retry   = "100ms"
  }

  to {
    connector = "db"
    target    = "UPDATE accounts"
  }
}
```

### Semaphore (N concurrent)

```hcl
flow "call_external_api" {
  from {
    connector = "api"
    operation = "POST /enrich"
  }

  semaphore {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'api_quota'"
    limit   = 10        # Max 10 concurrent flows
    timeout = "5s"
  }

  to {
    connector = "external_api"
    operation = "POST /enrich"
  }
}
```

### Coordinate (signal/wait)

```hcl
# Flow A signals when data is ready
flow "produce_data" {
  from {
    connector = "api"
    operation = "POST /data"
  }
  to {
    connector = "db"
    target    = "data"
  }

  coordinate {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }

    signal {
      when = "true"
      emit = "'data_ready:' + input.batch_id"
      ttl  = "1h"
    }
  }
}

# Flow B waits for the signal before proceeding
flow "consume_data" {
  from {
    connector = "api"
    operation = "POST /process"
  }

  coordinate {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    timeout    = "60s"
    on_timeout = "fail"

    wait {
      when = "true"
      for  = "'data_ready:' + input.batch_id"
    }
  }

  to {
    connector = "db"
    target    = "results"
  }
}
```

### Sequence guard (ordering)

Reject a message whose sequence number is not strictly greater than the last one
already processed for the same key. This is the guard against *out-of-order*
replays — redelivery, requeue, multiple workers — where an older update would
otherwise overwrite a newer one already applied.

```hcl
flow "apply_product_update" {
  from {
    connector = "rabbit"
    operation = "products.updated"
  }

  sequence_guard {
    key      = "'sku_seq:' + input.body.sku"
    sequence = "input.body.version"
    on_older = "ack"          # ack | reject | requeue
    ttl      = "30d"

    storage {
      driver = "redis"
      url    = env("REDIS_URL")
    }
  }

  to {
    connector = "db"
    target    = "products"
  }
}
```

`sequence_guard` is comparative (rejects by *ordering*), while
[`dedupe`](#dedupe-block) is content-based (rejects by *sameness*). They compose:
`sequence_guard` drops stale replays, then `dedupe` drops no-op rewrites.

Ordering inside a flow is `lock → coordinate → sequence_guard → transform → to`.

See [Synchronization Guide](../guides/synchronization.md) for details.

## Federation Entity Resolver

Mark a flow as a GraphQL Federation entity resolver:

```hcl
flow "resolve_product" {
  entity = "Product"

  from {
    connector = "gql_api"
    operation = "Query.product"
  }
  to {
    connector = "db"
    operation = "find_by_sku"
  }
}
```

## Returns (GraphQL Type)

Specify the GraphQL return type for flows used in GraphQL schema auto-generation:

```hcl
flow "get_users" {
  returns = "[User]"
  from {
    connector = "gql"
    operation = "Query.users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
```

## Complete Example

```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  require {
    roles = ["customer", "admin"]
  }

  validate {
    input = "order_input"
  }

  step "check_inventory" {
    connector = "inventory_api"
    operation = "GET /check"
    params    = { product_id = "input.product_id" }
    timeout   = "5s"
    on_error  = "skip"
    default   = { available = true }
  }

  transform {
    id          = "uuid()"
    user_id     = "input.user_id"
    product_id  = "input.product_id"
    quantity    = "input.quantity"
    can_fulfill = "step.check_inventory.available"
    status      = "'pending'"
    created_at  = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }

  to {
    connector = "rabbit"
    target    = "order.created"
    when      = "step.check_inventory.available == true"
  }

  error_handling {
    retry {
      attempts = 3
      delay    = "1s"
      backoff  = "exponential"
    }
  }
}
```
