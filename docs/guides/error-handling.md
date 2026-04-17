# Error Handling

Mycel provides multiple layers of error handling — from automatic retries and fallback queues at the flow level, to circuit breakers and rate limiting at the infrastructure level. This guide covers every mechanism and when to use each.

## Overview

| Layer | Mechanism | Scope | Purpose |
|-------|-----------|-------|---------|
| [Flow](#flow-level-error-handling) | `error_handling` block | Per flow | Retry, DLQ, custom error response |
| [Step](#step-level-error-handling) | `on_error` attribute | Per step | Skip, fail, or default on step failure |
| [Batch](#batch-error-handling) | `on_error` attribute | Per batch | Continue or stop on chunk failure |
| [Circuit Breaker](#circuit-breaker) | Aspect | Per connector/pattern | Stop calling failing services |
| [Rate Limiting](#rate-limiting) | Aspect | Per connector/pattern | Prevent overload |
| [On-Error Aspects](#on-error-aspects) | Aspect | Per flow/pattern | React to flow failures (log, alert) |
| [DLQ (RabbitMQ)](#rabbitmq-dead-letter-queue) | Connector config | Per queue | Native dead letter queue |
| [Connector Profiles](#connector-profiles) | Profile config | Per connector | Fallback to alternate backends |
| [Connector Timeout/Retry](#connector-level-timeout-and-retry) | Connector config | Per connector | Timeout and retry for HTTP clients |
| [Health Checks](#health-checks) | Automatic | Per service | Detect and report failures |

## Flow-Level Error Handling

The `error_handling` block inside a flow configures retry behavior and a fallback destination (DLQ) for when all retries are exhausted.

```hcl
flow "process_order" {
  from {
    connector = "rabbit"
    operation = "consume"
    queue     = "orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }

  error_handling {
    retry {
      attempts  = 5
      delay     = "1s"
      max_delay = "30s"
      backoff   = "exponential"
    }

    fallback {
      connector     = "rabbit_dlq"
      target        = "orders.failed"
      include_error = true

      transform {
        original_order = "input"
        error_message  = "error.message"
        failed_at      = "now()"
      }
    }
  }
}
```

### Retry

Automatically retries the entire flow when it fails.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `attempts` | int | `1` | Maximum number of attempts (1 = no retry) |
| `delay` | string | `"1s"` | Initial delay between retries |
| `max_delay` | string | `"30s"` | Maximum delay (caps exponential growth) |
| `backoff` | string | `"constant"` | Strategy: `constant`, `linear`, `exponential` |

**Backoff strategies:**

```
constant:     1s → 1s → 1s → 1s
linear:       1s → 2s → 3s → 4s
exponential:  1s → 2s → 4s → 8s → 16s → 30s (capped by max_delay)
```

### Fallback (DLQ)

When all retries are exhausted, the original message is sent to a fallback connector — typically a dead letter queue, a database table, or a log file.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `connector` | string | **required** | Fallback connector name |
| `target` | string | **required** | Destination (queue, table, file) |
| `include_error` | bool | `false` | Include error details in the message |
| `transform` | block | — | Optional transformation before sending |

**Message sent to fallback:**

```json
{
  "original_input": { "order_id": "123", "amount": 99.99 },
  "error": {
    "message": "connection refused",
    "flow_name": "process_order",
    "timestamp": "2026-03-04T12:00:00Z"
  }
}
```

The `error` field is only included when `include_error = true`.

### Custom Error Response

Define a custom HTTP error response for when a flow fails. Instead of the default `{"error": "..."}` with a 500 status, you control the status code, headers, and response body.

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

  error_handling {
    error_response {
      status = 422

      body {
        code    = "'VALIDATION_ERROR'"
        message = "error.message"
        details = "'Check the request payload'"
      }
    }
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `status` | int | `500` | HTTP status code |
| `headers` | map | — | Custom response headers |
| `body` | block | — | CEL expressions that build the response body |

The `body` block uses CEL expressions. Available variables:
- `error.message` — the error message string
- `input.*` — the original flow input

**Response sent to client:**

```json
{
  "code": "VALIDATION_ERROR",
  "message": "duplicate key: order_id",
  "details": "Check the request payload"
}
```

Custom error responses work with retry — the custom response is only sent after all retries are exhausted.

## Step-Level Error Handling

Each step in a multi-step flow can define its own error behavior with `on_error`. This is useful when some steps are critical and others are optional.

```hcl
flow "get_order_details" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  # Required — fail the entire flow if order not found
  step "order" {
    connector = "db"
    query     = "SELECT * FROM orders WHERE id = :id"
    params    = { id = "input.id" }
    on_error  = "fail"
  }

  # Optional — use default values if pricing service is down
  step "pricing" {
    connector = "pricing_api"
    operation = "GET /prices/${step.order.product_id}"
    timeout   = "5s"
    on_error  = "default"
    default   = { price = 0, currency = "USD" }
  }

  # Optional — skip entirely if fraud service is unavailable
  step "fraud_check" {
    connector = "fraud_api"
    operation = "GET /score/${step.order.user_id}"
    timeout   = "3s"
    on_error  = "skip"
  }

  transform {
    id          = "step.order.id"
    total       = "step.pricing.price"
    risk_score  = "step.fraud_check.risk_score"
  }

  to { connector = "api" }
}
```

### on_error values

| Value | Behavior |
|-------|----------|
| `"fail"` | Step failure fails the entire flow. This is the default. |
| `"skip"` | Step is silently skipped. Downstream references to `step.<name>.*` will be empty. |
| `"default"` | Step returns the value from `default = { ... }` instead of failing. |

### timeout

Steps support a `timeout` attribute (e.g., `"5s"`, `"30s"`) that limits how long the step waits before failing. Combine with `on_error = "skip"` or `"default"` to gracefully handle slow external services.

## Batch Error Handling

Batch processing supports `on_error` to control behavior when a chunk fails.

```hcl
flow "migrate_users" {
  batch {
    source     = "old_db"
    query      = "SELECT * FROM users"
    chunk_size = 100
    on_error   = "continue"

    to {
      connector = "new_db"
      target    = "users"
    }
  }
}
```

| Value | Behavior |
|-------|----------|
| `"stop"` | Fail the entire batch on the first chunk error. **This is the default.** |
| `"continue"` | Skip the failed chunk and continue processing remaining chunks. |

**Batch result includes error details:**

```json
{
  "processed": 950,
  "failed": 50,
  "chunks": 10,
  "errors": ["chunk 3: connection timeout", "chunk 7: duplicate key"]
}
```

## Circuit Breaker

Circuit breakers prevent cascading failures by stopping calls to a failing service. Applied via [aspects](extending.md#aspects) using pattern matching.

```hcl
aspect "protect_magento" {
  when = "around"
  on   = ["magento_*"]

  circuit_breaker {
    failure_threshold = 5
    success_threshold = 2
    timeout           = "30s"
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `failure_threshold` | int | — | Failures before opening the circuit |
| `success_threshold` | int | — | Successes needed to close from half-open |
| `timeout` | string | — | How long circuit stays open before retrying |

**States:**

```
          failures >= threshold
Closed ──────────────────────────► Open
  ▲                                  │
  │  successes >= threshold          │ timeout elapsed
  │                                  ▼
  └────────────────────────────── Half-Open
                                   (limited requests)
```

- **Closed:** Normal operation. Requests pass through. Failures are counted.
- **Open:** All requests fail immediately (fast fail). No calls to the service.
- **Half-Open:** After the timeout, a limited number of requests are allowed through. If they succeed, the circuit closes. If they fail, it opens again.

## Rate Limiting

Prevents overload by limiting how many requests reach a connector. Applied via aspects.

```hcl
aspect "throttle_api" {
  when = "before"
  on   = ["external_*"]

  rate_limit {
    key                 = "input._client_ip"
    requests_per_second = 10
    burst               = 20
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `key` | string | — | CEL expression for rate limit key (e.g., `input.user_id`, `input._client_ip`) |
| `requests_per_second` | float | — | Sustained request rate |
| `burst` | int | — | Maximum burst above the sustained rate |

When a request is rate limited, the flow returns an error immediately without calling the connector.

## On-Error Aspects

On-error aspects execute only when a flow fails. Use them for cross-cutting error handling like logging errors to a database, sending alerts, or notifying external systems.

```hcl
aspect "log_errors" {
  when = "on_error"
  on   = ["*"]

  action {
    connector = "db"
    target    = "error_logs"

    transform {
      flow_name     = "input._flow"
      operation     = "input._operation"
      error_message = "error.message"
      timestamp     = "now()"
    }
  }
}
```

On-error aspects:
- Only fire when the flow returns an error (never on success)
- Have access to `error.message`, `error.code`, and `error.type` in transform and `if` expressions
- Do not swallow the original error — it is still returned to the caller
- Execute after "after" aspects, in definition order
- Support the `if` condition for selective error handling based on error code or type

The `error` variable is a structured object:

| Field | Type | Description |
|-------|------|-------------|
| `error.message` | string | The error message |
| `error.code` | int | HTTP status code (e.g., 404, 500) or 0 if unknown |
| `error.type` | string | `http`, `flow`, `validation`, `not_found`, `timeout`, `connection`, `auth`, `unknown` |

```hcl
# Alert only on server errors (5xx)
aspect "alert_critical" {
  when = "on_error"
  on   = ["payment_*"]
  if   = "error.code >= 500"

  action {
    connector = "slack"
    transform {
      text = "':rotating_light: Payment flow failed (' + string(error.code) + '): ' + error.message"
    }
  }
}

# Handle timeouts differently
aspect "timeout_handler" {
  when = "on_error"
  on   = ["*"]
  if   = "error.type == 'timeout'"

  action {
    connector = "slack"
    transform {
      text = "':hourglass: Timeout in ' + _flow + ' — check external service health'"
    }
  }
}

# Log 404s to analytics
aspect "not_found_tracker" {
  when = "on_error"
  on   = ["get_*"]
  if   = "error.code == 404"

  action {
    connector = "db"
    target    = "not_found_logs"
    transform {
      flow      = "_flow"
      timestamp = "now()"
    }
  }
}
```

## RabbitMQ Dead Letter Queue

The RabbitMQ connector has native DLQ support — separate from the flow-level fallback. This handles message-level failures at the queue layer.

```hcl
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")

  consumer {
    queue = "orders"

    dlq {
      enabled      = true
      exchange     = "orders.dlx"
      queue        = "orders.dlq"
      routing_key  = ""
      max_retries  = 3
      retry_delay  = "5s"
      retry_header = "x-retry-count"
    }
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable DLQ processing |
| `exchange` | string | `<main>.dlx` | Dead letter exchange name |
| `queue` | string | `<main>.dlq` | Dead letter queue name |
| `routing_key` | string | `""` | Routing key for DLQ messages |
| `max_retries` | int | `3` | Retries before sending to DLQ |
| `retry_delay` | string | — | Delay before requeuing for retry |
| `retry_header` | string | `x-retry-count` | Header tracking retry count |

**How it works:**

1. Consumer picks up a message
2. If processing fails, the retry count header is incremented
3. If retries < max_retries, the message is requeued after `retry_delay`
4. If retries >= max_retries, the message is routed to the DLQ
5. The DLQ exchange and queue are created automatically

**Flow-level fallback vs. RabbitMQ DLQ:** Use both. The RabbitMQ DLQ catches failures at the message layer (consumer crashes, unhandled errors). The flow-level fallback catches failures at the application layer (business logic errors, connector timeouts) after retries.

## Message Rejection (filter and accept)

Before a flow processes a message, two gates can reject it: `filter` (structural match) and `accept` (business logic). Both support `on_reject` to control what happens with rejected messages in MQ connectors.

```hcl
flow "process_order" {
  from {
    connector = "rabbit"
    operation = "orders"

    filter {
      condition = "has(input.metadata) && input.metadata.type == 'order'"
      on_reject = "ack"      # Not my message type — discard
    }
  }

  accept {
    when      = "input.region == 'us-east'"
    on_reject = "requeue"    # My type, but not my region — put it back
  }

  transform { ... }
  to { ... }
}
```

| `on_reject` value | Behavior | Use case |
|-------------------|----------|----------|
| `ack` (default) | Acknowledge and discard | Message is irrelevant to any consumer |
| `reject` | NACK — routed to DLQ if configured | Message is malformed or invalid |
| `requeue` | NACK + requeue — back in the queue | Another consumer should handle it |

**filter vs. accept:** Use `filter` for structural validation ("is this message shaped correctly for me?"). Use `accept` for business decisions ("this message is valid, but should I process it?"). See [flows documentation](../core-concepts/flows.md#the-accept-block) for details.

**Requeue loops:** If using `on_reject = "requeue"` on `accept`, make sure at least one consumer will eventually accept the message. Otherwise it will bounce indefinitely. Use RabbitMQ TTL or `x-delivery-count` limits at the queue level as a safety net.

## Connector Profiles

Profiles provide automatic failover between multiple backends for the same connector.

```hcl
connector "database" {
  type   = "database"
  driver = "postgres"

  profile "primary" {
    dsn     = env("PRIMARY_DB_URL")
    default = true
  }

  profile "replica" {
    dsn = env("REPLICA_DB_URL")
  }

  profile "fallback" {
    dsn = env("FALLBACK_DB_URL")
  }
}
```

When the primary profile fails with a retriable error (5xx, connection refused, timeout), the connector automatically tries the next profile. This works for any connector type — databases, REST APIs, queues, etc.

## Connector-Level Timeout and Retry

HTTP client connectors support `timeout` and `retry` directly in the connector configuration:

```hcl
connector "payment_api" {
  type     = "http"
  base_url = env("PAYMENT_API_URL")
  timeout  = "10s"

  retry {
    attempts = 3
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `timeout` | string | `"30s"` | Connection and request timeout |
| `retry.attempts` | int | `1` | Number of retry attempts on failure |

For other connector types, use step-level `timeout` and flow-level `error_handling { retry }` to achieve the same effect.

## Health Checks

Every Mycel service automatically exposes health check endpoints:

| Endpoint | Purpose | Use Case |
|----------|---------|----------|
| `/health` | Full health with component details | Monitoring dashboards |
| `/health/live` | Liveness probe (always 200 if process is running) | Kubernetes liveness probe |
| `/health/ready` | Readiness probe (checks all connectors) | Kubernetes readiness probe |

**Response format:**

```json
{
  "status": "healthy",
  "timestamp": "2026-03-04T12:00:00Z",
  "version": "1.0.0",
  "uptime": "2h15m30s",
  "components": [
    { "name": "postgres", "status": "healthy", "latency": "3ms" },
    { "name": "rabbitmq", "status": "healthy", "latency": "8ms" },
    { "name": "redis",    "status": "degraded", "latency": "150ms" }
  ]
}
```

**Status values:** `healthy` (200), `degraded` (200), `unhealthy` (503).

Health checks detect connector failures automatically. Kubernetes uses `/health/ready` to stop routing traffic to unhealthy pods, and `/health/live` to restart crashed pods.

## Putting It All Together

A production flow typically combines multiple layers:

```hcl
# Aspect: circuit breaker on all Magento API flows
aspect "magento_circuit_breaker" {
  when = "around"
  on   = ["magento_*"]

  circuit_breaker {
    failure_threshold = 5
    success_threshold = 2
    timeout           = "30s"
  }
}

# Aspect: rate limit external API calls
aspect "magento_rate_limit" {
  when = "before"
  on   = ["magento_*"]

  rate_limit {
    requests_per_second = 10
    burst               = 20
  }
}

# Flow: consume from queue, call API, write to DB
flow "magento_create_product" {
  from {
    connector = "rabbit"
    operation = "consume"
    queue     = "products"
  }

  step "create" {
    connector = "magento_api"
    operation = "POST /rest/V1/products"
    timeout   = "30s"
    on_error  = "fail"

    transform {
      product.sku  = "input.payload.sku"
      product.name = "input.payload.name"
    }
  }

  step "assign_category" {
    connector = "magento_api"
    operation = "POST /rest/V1/categories/${input.payload.category_id}/products"
    timeout   = "10s"
    on_error  = "skip"
  }

  to {
    connector = "rabbit_response"
    operation = "publish"
  }

  error_handling {
    retry {
      attempts  = 3
      delay     = "2s"
      backoff   = "exponential"
      max_delay = "30s"
    }

    fallback {
      connector     = "rabbit_dlq"
      target        = "products.failed"
      include_error = true
    }
  }
}
```

**What happens when the Magento API goes down:**

1. **Rate limit** prevents flooding the API with requests
2. **Step timeout** (30s) prevents the flow from hanging indefinitely
3. **Flow retry** (3 attempts, exponential backoff) retries the whole flow
4. **Fallback** sends the failed message to `products.failed` queue with error details
5. **Circuit breaker** opens after 5 consecutive failures, immediately rejecting subsequent requests for 30s
6. **Health check** reports the service as degraded
7. When the API recovers, the circuit breaker transitions to half-open, then closes
8. The DLQ messages can be replayed manually or automatically

## Summary

| Question | Answer |
|----------|--------|
| API call times out? | Step `timeout` + `on_error = "skip"` or `"default"` |
| External service down? | Circuit breaker (aspect) + retry + fallback to DLQ |
| Occasional failures? | `error_handling { retry { ... } }` with exponential backoff |
| Custom error format? | `error_handling { error_response { status, body } }` |
| Message processing fails? | RabbitMQ DLQ + flow-level fallback |
| Message not for this consumer? | `accept { on_reject = "requeue" }` |
| Database unreachable? | Connector profiles (automatic failover) |
| Too many requests? | Rate limiting (aspect) |
| Log all errors centrally? | On-error aspect with `when = "on_error"` |
| Need to monitor? | `/health`, `/health/ready`, `/metrics` |
| Batch import has bad rows? | `batch { on_error = "continue" }` |
