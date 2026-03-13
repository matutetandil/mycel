# Common Use Cases

Complete, copy-paste ready examples for things you'll want to do in almost every project. Each example includes all the HCL files needed to run it.

## Table of Contents

1. [REST API + Database + Slack notification on create](#1-rest-api--database--slack-notification-on-create)
2. [Send welcome email when a user registers](#2-send-welcome-email-when-a-user-registers)
3. [Audit log for all write operations](#3-audit-log-for-all-write-operations)
4. [Cache reads, invalidate on writes](#4-cache-reads-invalidate-on-writes)
5. [Publish event to a queue after database write](#5-publish-event-to-a-queue-after-database-write)
6. [Error alerting to Slack for all API flows](#6-error-alerting-to-slack-for-all-api-flows)
7. [REST API with input validation](#7-rest-api-with-input-validation)
8. [Enrich response with data from another service](#8-enrich-response-with-data-from-another-service)
9. [Webhook relay with transform](#9-webhook-relay-with-transform)
10. [Rate-limited public API](#10-rate-limited-public-api)

---

## 1. REST API + Database + Slack notification on create

**Use case:** A POST endpoint creates a user in PostgreSQL, then sends a Slack message with the new user's name and ID.

```hcl
# config.hcl
service {
  name    = "user-service"
  version = "1.0.0"
}
```

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}

connector "slack" {
  type    = "slack"
  webhook = env("SLACK_WEBHOOK_URL")
}
```

```hcl
# flows.hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    id    = "uuid()"
    name  = "input.name"
    email = "lower(input.email)"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

```hcl
# aspects.hcl
aspect "notify_new_user" {
  when = "after"
  on   = ["create_user"]

  action {
    connector = "slack"
    transform {
      text = "'New user created: ' + output.name + ' (ID: ' + output.id + ')'"
    }
  }
}
```

**Test:**

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "Alice@Example.com"}'
```

The user is created in the database, and Slack receives: `New user created: Alice (ID: a1b2c3...)`.

---

## 2. Send welcome email when a user registers

**Use case:** After a user is created via the API, send them a welcome email via SMTP.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}

connector "mailer" {
  type     = "email"
  host     = env("SMTP_HOST")
  port     = 587
  username = env("SMTP_USER")
  password = env("SMTP_PASSWORD")
  from     = "noreply@myapp.com"
  tls      = true
}
```

```hcl
# flows.hcl
flow "register_user" {
  from {
    connector = "api"
    operation = "POST /register"
  }

  transform {
    id         = "uuid()"
    name       = "input.name"
    email      = "lower(input.email)"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

```hcl
# aspects.hcl
aspect "welcome_email" {
  when = "after"
  on   = ["register_user"]

  action {
    connector = "mailer"
    operation = "send"
    transform {
      to      = "output.email"
      subject = "'Welcome to MyApp, ' + output.name + '!'"
      body    = "'Hello ' + output.name + ',\n\nYour account is ready. Your user ID is ' + output.id + '.'"
    }
  }
}
```

**Test:**

```bash
curl -X POST http://localhost:3000/register \
  -H "Content-Type: application/json" \
  -d '{"name": "Bob", "email": "bob@gmail.com"}'
```

---

## 3. Audit log for all write operations

**Use case:** Automatically log every create, update, and delete operation to an audit table with the flow name, user, and timestamp.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}
```

```hcl
# flows.hcl
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  transform {
    id    = "uuid()"
    name  = "input.name"
    price = "input.price"
  }

  to {
    connector = "db"
    target    = "products"
  }
}

flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }

  transform {
    name  = "input.name"
    price = "input.price"
  }

  to {
    connector = "db"
    target    = "products"
  }
}

flow "delete_product" {
  from {
    connector = "api"
    operation = "DELETE /products/:id"
  }

  to {
    connector = "db"
    target    = "products"
  }
}
```

```hcl
# aspects.hcl
aspect "audit_log" {
  when = "after"
  on   = ["create_*", "update_*", "delete_*"]

  action {
    connector = "db"
    target    = "audit_logs"
    transform {
      id        = "uuid()"
      flow      = "_flow"
      operation = "_operation"
      timestamp = "now()"
    }
  }
}
```

Every write operation across the entire service is logged automatically. Add new flows like `create_order` or `delete_user` and they're audited without touching the aspect.

---

## 4. Cache reads, invalidate on writes

**Use case:** Cache GET responses in Redis. When a write happens, invalidate the relevant cache entries.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}

connector "cache" {
  type   = "cache"
  driver = "redis"
  host   = env("REDIS_HOST")
  port   = 6379
}
```

```hcl
# flows.hcl
flow "get_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  to {
    connector = "db"
    target    = "products"
  }
}

flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  transform {
    id    = "uuid()"
    name  = "input.name"
    price = "input.price"
  }

  to {
    connector = "db"
    target    = "products"
  }
}
```

```hcl
# aspects.hcl
aspect "cache_reads" {
  when = "around"
  on   = ["get_*"]

  cache {
    storage = "cache"
    ttl     = "5m"
    key     = "'products:list'"
  }
}

aspect "invalidate_on_write" {
  when = "after"
  on   = ["create_*", "update_*", "delete_*"]

  invalidate {
    storage  = "cache"
    patterns = ["products:*"]
  }
}
```

GET requests are served from cache for 5 minutes. Any write operation clears the cache automatically.

---

## 5. Publish event to a queue after database write

**Use case:** After creating an order in the database, publish an event to RabbitMQ so other services can react (send confirmation, update inventory, etc.).

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}

connector "rabbit" {
  type     = "mq"
  driver   = "rabbitmq"
  url      = env("RABBITMQ_URL")
  exchange = "events"
}
```

```hcl
# flows.hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  transform {
    id         = "uuid()"
    product_id = "input.product_id"
    quantity   = "input.quantity"
    status     = "'pending'"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
```

```hcl
# aspects.hcl
aspect "publish_order_event" {
  when = "after"
  on   = ["create_order"]

  action {
    connector = "rabbit"
    operation = "order.created"
    transform {
      order_id   = "output.id"
      product_id = "output.product_id"
      quantity   = "output.quantity"
      timestamp  = "now()"
    }
  }
}
```

The API returns the created order immediately. The event is published asynchronously to RabbitMQ, where other services can consume it.

---

## 6. Error alerting to Slack for all API flows

**Use case:** Whenever any flow fails, send an alert to a Slack channel with the error details.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}

connector "slack_alerts" {
  type    = "slack"
  webhook = env("SLACK_ALERTS_WEBHOOK")
}
```

```hcl
# aspects.hcl
aspect "alert_server_errors" {
  when = "on_error"
  on   = ["*"]
  if   = "error.code >= 500"

  action {
    connector = "slack_alerts"
    transform {
      text = "':rotating_light: *Server error in ' + _flow + '*\n>Code: ' + string(error.code) + '\n>Error: ' + error.message + '\n>Type: ' + error.type"
    }
  }
}

aspect "log_client_errors" {
  when = "on_error"
  on   = ["*"]
  if   = "error.code >= 400 && error.code < 500"

  action {
    connector = "db"
    target    = "client_error_logs"
    transform {
      id        = "uuid()"
      flow      = "_flow"
      code      = "error.code"
      message   = "error.message"
      timestamp = "now()"
    }
  }
}
```

Two aspects handle errors differently: 5xx errors go to Slack as critical alerts, 4xx errors are logged to a database table for analytics. The `error.code`, `error.message`, and `error.type` fields let you route errors precisely.

---

## 7. REST API with input validation

**Use case:** Validate request data before it reaches the database using type definitions.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}
```

```hcl
# types.hcl
type "create_user_input" {
  name  = string { min_length = 2, max_length = 100 }
  email = string { format = "email" }
  age   = number { min = 18, max = 150 }
}
```

```hcl
# flows.hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  validate {
    input = "create_user_input"
  }

  transform {
    id    = "uuid()"
    name  = "input.name"
    email = "lower(input.email)"
    age   = "input.age"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

Invalid requests get a 400 response with details before touching the database:

```bash
# This fails validation (age < 18)
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "A", "email": "bad", "age": 5}'
```

---

## 8. Enrich response with data from another service

**Use case:** A GET endpoint reads from the database, then enriches the response with data from an external API using a step block.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}

connector "weather_api" {
  type     = "http"
  base_url = "https://api.weatherapi.com/v1"
}
```

```hcl
# flows.hcl
flow "get_user_with_weather" {
  from {
    connector = "api"
    operation = "GET /users/:id/dashboard"
  }

  to {
    connector = "db"
    target    = "users"
  }

  step "weather" {
    connector = "weather_api"
    operation = "GET /current.json"
    params {
      key = env("WEATHER_API_KEY")
      q   = "output.city"
    }
  }

  response {
    name    = "output.name"
    email   = "output.email"
    city    = "output.city"
    weather = "step.weather.current.condition.text"
    temp_c  = "step.weather.current.temp_c"
  }
}
```

**Test:**

```bash
curl http://localhost:3000/users/abc-123/dashboard
```

Returns the user from the database plus live weather data for their city.

---

## 9. Webhook relay with transform

**Use case:** Receive a webhook from Stripe, transform the payload, and forward it to your internal system and a Discord channel.

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "internal_api" {
  type     = "http"
  base_url = env("INTERNAL_API_URL")
}

connector "discord" {
  type    = "discord"
  webhook = env("DISCORD_WEBHOOK_URL")
}
```

```hcl
# flows.hcl
flow "stripe_webhook" {
  from {
    connector = "api"
    operation = "POST /webhooks/stripe"
  }

  transform {
    event_type = "input.type"
    amount     = "input.data.object.amount / 100"
    currency   = "upper(input.data.object.currency)"
    customer   = "input.data.object.customer"
    timestamp  = "now()"
  }

  to {
    connector = "internal_api"
    operation = "POST /events/payments"
  }
}
```

```hcl
# aspects.hcl
aspect "notify_payments" {
  when = "after"
  on   = ["stripe_webhook"]

  action {
    connector = "discord"
    transform {
      content = "':moneybag: Payment received: $' + string(output.amount) + ' ' + output.currency + ' from customer ' + output.customer"
    }
  }
}
```

Stripe sends the webhook, Mycel transforms and forwards it to your internal API, and Discord gets a notification.

---

## 10. Rate-limited public API

**Use case:** A public API with rate limiting and custom error responses for rate-limited requests.

```hcl
# config.hcl
service {
  name    = "public-api"
  version = "1.0.0"

  rate_limit {
    requests_per_second = 10
    burst               = 20
  }
}
```

```hcl
# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASS")
}
```

```hcl
# flows.hcl
flow "search_products" {
  from {
    connector = "api"
    operation = "GET /products/search"
  }

  to {
    connector = "db"
    target    = "products"
    query     = "SELECT * FROM products WHERE name ILIKE '%' || $1 || '%' LIMIT 20"
    params    = ["input.q"]
  }
}

flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  to {
    connector = "db"
    target    = "products"
  }
}
```

The rate limit applies globally. Clients exceeding 10 req/s get a 429 response. The `burst` allows short spikes up to 20.

---

## 11. Flow orchestration via aspects

Chain flows together using aspects. An internal flow (no `from` block) handles welcome emails, triggered automatically after user creation.

### connectors.hcl

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "sqlite"
  dsn    = "./users.db"
}

connector "mailer" {
  type     = "email"
  host     = env("SMTP_HOST")
  port     = 587
  username = env("SMTP_USER")
  password = env("SMTP_PASSWORD")
  from     = "noreply@example.com"
  tls      = true
}
```

### flows.hcl

```hcl
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  to {
    connector = "db"
    operation = "INSERT users"
  }
}

# Internal flow — no "from" block, only invocable from aspects
flow "send_welcome_email" {
  transform {
    to      = "input.email"
    subject = "'Welcome, ' + input.name + '!'"
    body    = "'Hello ' + input.name + ', your account is ready.'"
  }
  to {
    connector = "mailer"
    operation = "send"
  }
}
```

### aspects.hcl

```hcl
aspect "welcome_email" {
  when = "after"
  on   = ["create_user"]

  action {
    flow = "send_welcome_email"
    transform {
      email = "input.email"
      name  = "input.name"
    }
  }
}
```

### How it works

1. `POST /users` creates a user in the database via `create_user`
2. The `welcome_email` aspect matches the flow name and fires after success
3. The aspect's transform builds the input for `send_welcome_email`
4. The internal flow sends the email — if it fails, the user creation still succeeds (soft failure)

---

## 12. Error recovery flow

When an order fails, an `on_error` aspect invokes a recovery flow that logs the failure and enqueues a retry message for later processing.

### connectors.hcl

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")
}
```

### flows.hcl

```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "db"
    operation = "INSERT orders"
  }
}

# Internal flow — logs failure and enqueues retry
flow "handle_order_failure" {
  step "log_failure" {
    connector = "db"
    operation = "INSERT failed_orders"
    body {
      order_data   = "input.order_data"
      error_reason = "input.error_reason"
      failed_at    = "now()"
      retry_count  = "0"
    }
  }

  step "enqueue_retry" {
    connector = "rabbit"
    operation = "orders.retry"
    body {
      order_data = "input.order_data"
      attempt    = "1"
    }
  }
}
```

### aspects.hcl

```hcl
aspect "order_failure_recovery" {
  when = "on_error"
  on   = ["create_order"]

  action {
    flow = "handle_order_failure"
    transform {
      order_data   = "input"
      error_reason = "error.message"
    }
  }
}
```

### How it works

1. `POST /orders` attempts to create an order in the database
2. If it fails, the `order_failure_recovery` aspect fires
3. The aspect invokes `handle_order_failure` with the original input and error details
4. The recovery flow logs the failure to `failed_orders` and publishes a retry message to RabbitMQ
5. A separate consumer (another Mycel service or the same one) processes the retry queue

---

## 13. Notification hub — route by event type

A single internal flow decides where to notify (Slack, email, or SMS) based on the event severity passed by the aspect.

### connectors.hcl

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

connector "slack_alerts" {
  type  = "slack"
  token = env("SLACK_BOT_TOKEN")
}

connector "mailer" {
  type     = "email"
  host     = env("SMTP_HOST")
  port     = 587
  username = env("SMTP_USER")
  password = env("SMTP_PASSWORD")
  from     = "alerts@example.com"
  tls      = true
}

connector "sms_service" {
  type        = "sms"
  account_sid = env("TWILIO_ACCOUNT_SID")
  auth_token  = env("TWILIO_AUTH_TOKEN")
  from        = env("TWILIO_FROM_NUMBER")
}
```

### flows.hcl

```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "db"
    operation = "INSERT orders"
  }
}

flow "update_order" {
  from {
    connector = "api"
    operation = "PUT /orders/:id"
  }
  to {
    connector = "db"
    operation = "UPDATE orders"
  }
}

flow "delete_order" {
  from {
    connector = "api"
    operation = "DELETE /orders/:id"
  }
  to {
    connector = "db"
    operation = "DELETE orders"
  }
}

# Internal flow — notify Slack on every event
flow "notify_slack" {
  transform {
    channel = "'#operations'"
    text    = "input.message"
  }
  to {
    connector = "slack_alerts"
    operation = "chat.postMessage"
  }
}

# Internal flow — email for important events
flow "notify_email" {
  transform {
    to      = "input.recipient"
    subject = "input.subject"
    body    = "input.body"
  }
  to {
    connector = "mailer"
    operation = "send"
  }
}

# Internal flow — SMS for critical events
flow "notify_sms" {
  transform {
    to   = "input.phone"
    body = "input.message"
  }
  to {
    connector = "sms_service"
    operation = "send"
  }
}
```

### aspects.hcl

```hcl
# All write operations → Slack
aspect "slack_on_writes" {
  when = "after"
  on   = ["create_*", "update_*", "delete_*"]

  action {
    flow = "notify_slack"
    transform {
      message = "':white_check_mark: ' + _flow + ' completed successfully'"
    }
  }
}

# Deletions → email to admin
aspect "email_on_delete" {
  when = "after"
  on   = ["delete_*"]

  action {
    flow = "notify_email"
    transform {
      recipient = "'admin@example.com'"
      subject   = "'Deletion alert: ' + _flow"
      body      = "'A delete operation was performed: ' + _flow + ' at ' + string(_timestamp)"
    }
  }
}

# Critical errors → SMS to on-call
aspect "sms_on_critical" {
  when = "on_error"
  on   = ["create_order", "delete_*"]
  if   = "error.code >= 500"

  action {
    flow = "notify_sms"
    transform {
      phone   = "'+1234567890'"
      message = "'CRITICAL: ' + _flow + ' failed with ' + string(error.code)"
    }
  }
}
```

### How it works

1. Every write operation sends a Slack message via the `notify_slack` flow
2. Delete operations additionally email the admin via `notify_email`
3. Server errors (5xx) on critical flows send SMS via `notify_sms`
4. Each notification channel is an independent internal flow — easy to test, reuse, or replace
5. Aspect failures are soft — the main flow always succeeds regardless of notification errors

---

## 14. Data sync to external system

After any product change, an aspect invokes a sync flow that pushes the updated data to an external search index.

### connectors.hcl

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

connector "search" {
  type = "elasticsearch"
  urls = [env("ELASTICSEARCH_URL")]
}
```

### flows.hcl

```hcl
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }
  to {
    connector = "db"
    operation = "INSERT products"
  }
}

flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  to {
    connector = "db"
    operation = "UPDATE products"
  }
}

flow "delete_product" {
  from {
    connector = "api"
    operation = "DELETE /products/:id"
  }
  to {
    connector = "db"
    operation = "DELETE products"
  }
}

# Internal flow — index product in Elasticsearch
flow "sync_product_to_search" {
  to {
    connector = "search"
    operation = "index"
    target    = "products"
  }
}

# Internal flow — remove product from Elasticsearch
flow "remove_product_from_search" {
  to {
    connector = "search"
    operation = "delete"
    target    = "products"
  }
}
```

### aspects.hcl

```hcl
# Sync to search index after create/update
aspect "sync_search_index" {
  when = "after"
  on   = ["create_product", "update_product"]

  action {
    flow = "sync_product_to_search"
    transform {
      id          = "input.id"
      name        = "input.name"
      description = "input.description"
      price       = "input.price"
      updated_at  = "string(_timestamp)"
    }
  }
}

# Remove from search index after delete
aspect "remove_from_search" {
  when = "after"
  on   = ["delete_product"]

  action {
    flow = "remove_product_from_search"
    transform {
      id = "input.id"
    }
  }
}
```

### How it works

1. `create_product` / `update_product` write to PostgreSQL, then the aspect invokes `sync_product_to_search` to index the product in Elasticsearch
2. `delete_product` removes from PostgreSQL, then the aspect invokes `remove_product_from_search` to delete from the index
3. The sync is decoupled from the main flow — if Elasticsearch is down, the database write still succeeds
4. Adding more sync targets (e.g., Redis cache, analytics) is just another aspect — no changes to the original flows

---

## See Also

- [Integration Patterns](../advanced/integration-patterns.md) -- advanced patterns (GraphQL, gRPC, message queues)
- [Notifications Guide](notifications.md) -- all notification connectors in detail
- [Aspects / Extending](extending.md) -- full aspect reference
- [Error Handling](error-handling.md) -- retry, DLQ, fallback patterns
- [Multi-Step Flows](multi-step-flows.md) -- step blocks, conditional logic, fan-out
