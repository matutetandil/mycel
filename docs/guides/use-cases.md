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
22. [API versioning with deprecation warnings](#22-api-versioning-with-deprecation-warnings)
23. [Idempotent payment processing](#23-idempotent-payment-processing)
24. [Async long-running export with polling](#24-async-long-running-export-with-polling)
25. [Database migrations](#25-database-migrations)
26. [Distributed rate limiting with Redis](#26-distributed-rate-limiting-with-redis)
27. [Multi-tenancy via request headers](#27-multi-tenancy-via-request-headers)
28. [Fan-out from source (multiple flows, same trigger)](#28-fan-out-from-source-multiple-flows-same-trigger)

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

## 15. Queue consumer to database

Process messages from RabbitMQ and store them in PostgreSQL. One of the most common microservice patterns — zero code required.

### connectors.hcl

```hcl
connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}
```

### flows.hcl

```hcl
# Consume order events and persist them
flow "process_order_event" {
  from {
    connector = "rabbit"
    operation = "orders.created"
  }

  transform {
    order_id   = "input.order_id"
    customer   = "input.customer_name"
    total      = "input.total_amount"
    status     = "'pending'"
    created_at = "now()"
  }

  to {
    connector = "db"
    operation = "INSERT orders"
  }
}

# Consume payment confirmations and update orders
flow "process_payment" {
  from {
    connector = "rabbit"
    operation = "payments.confirmed"
  }

  to {
    connector = "db"
    operation = "UPDATE orders"
    filter    = "id = input.order_id"
  }
}
```

### How it works

1. Mycel subscribes to `orders.created` and `payments.confirmed` queues on startup
2. Each message is transformed and written to PostgreSQL
3. If the database write fails, the message is nacked (RabbitMQ requeues it)
4. No HTTP server involved — this is a pure consumer service

---

## 16. Scheduled jobs (cron)

Run flows on a schedule. Clean up old data, generate reports, or ping health endpoints — all via HCL configuration.

### connectors.hcl

```hcl
connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}

connector "slack_alerts" {
  type  = "slack"
  token = env("SLACK_BOT_TOKEN")
}
```

### flows.hcl

```hcl
# Clean up expired sessions every hour
flow "cleanup_sessions" {
  when = "@every 1h"

  to {
    connector = "db"
    operation = "DELETE sessions"
    filter    = "expires_at < now()"
  }
}

# Daily report at 9:00 AM
flow "daily_order_summary" {
  when = "0 9 * * *"

  step "count" {
    connector = "db"
    query     = "SELECT count(*) as total, sum(amount) as revenue FROM orders WHERE created_at > now() - interval '1 day'"
  }

  to {
    connector = "slack_alerts"
    operation = "chat.postMessage"
  }

  transform {
    channel = "'#reports'"
    text    = "':bar_chart: Daily summary — ' + string(step.count.total) + ' orders, $' + string(step.count.revenue) + ' revenue'"
  }
}

# Heartbeat every 5 minutes
flow "health_ping" {
  when = "@every 5m"

  transform {
    service    = "'order-service'"
    status     = "'alive'"
    checked_at = "now()"
  }

  to {
    connector = "db"
    operation = "INSERT heartbeats"
  }
}
```

### How it works

1. Scheduled flows have no `from` block — the `when` attribute defines the trigger
2. Cron expressions use standard 5-field format (`minute hour day month weekday`)
3. Interval format `@every <duration>` supports Go durations (`5m`, `1h`, `30s`)
4. Shortcuts available: `@hourly`, `@daily`, `@weekly`, `@monthly`
5. Scheduled flows can use steps, transforms, and aspects like any other flow

---

## 17. API aggregation (BFF pattern)

Combine data from multiple sources into a single API response using multi-step flows. Perfect for Backend-for-Frontend patterns.

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

connector "inventory_api" {
  type    = "http"
  base_url = env("INVENTORY_SERVICE_URL")
}

connector "reviews_api" {
  type    = "http"
  base_url = env("REVIEWS_SERVICE_URL")
}
```

### flows.hcl

```hcl
flow "get_product_detail" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  # Step 1: Get product from database
  step "product" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :id"
    params    = { id = "input.params.id" }
  }

  # Step 2: Get inventory from external service
  step "inventory" {
    connector = "inventory_api"
    operation = "GET /stock/${step.product.sku}"
    timeout   = "3s"
    on_error  = "default"
    default   = { available = 0, warehouse = "unknown" }
  }

  # Step 3: Get reviews (optional, skip on error)
  step "reviews" {
    connector = "reviews_api"
    operation = "GET /reviews?product_id=${step.product.id}"
    timeout   = "2s"
    on_error  = "skip"
    default   = []
  }

  # Combine everything into one response
  response {
    id          = "step.product.id"
    name        = "step.product.name"
    price       = "step.product.price"
    description = "step.product.description"
    stock       = "step.inventory.available"
    warehouse   = "step.inventory.warehouse"
    reviews     = "step.reviews"
    review_count = "size(step.reviews)"
  }
}
```

### How it works

1. A single `GET /products/:id` request triggers 3 parallel-safe steps
2. `product` step fetches from the local database (required — fails the flow if missing)
3. `inventory` step calls an external service with a 3s timeout — returns defaults if unavailable
4. `reviews` step is fully optional — skipped on error with empty array default
5. The `response` block combines all step results into one clean JSON response
6. External service failures don't break the API — degraded but functional

---

## 18. CDC pipeline — real-time database sync

React to PostgreSQL changes in real-time using Change Data Capture. Automatically sync data to Elasticsearch and publish events to a message queue.

### connectors.hcl

```hcl
connector "pg_cdc" {
  type        = "cdc"
  driver      = "postgres"
  host        = env("DB_HOST")
  port        = 5432
  database    = env("DB_NAME")
  user        = env("DB_REPLICATION_USER")
  password    = env("DB_REPLICATION_PASSWORD")
  slot_name   = "mycel_products_slot"
  publication = "mycel_products_pub"
}

connector "search" {
  type = "elasticsearch"
  urls = [env("ELASTICSEARCH_URL")]
}

connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")
}
```

### flows.hcl

```hcl
# Sync new products to search index
flow "cdc_product_created" {
  from {
    connector = "pg_cdc"
    operation = "INSERT:products"
  }

  transform {
    id          = "input.new.id"
    name        = "input.new.name"
    description = "input.new.description"
    price       = "input.new.price"
    category    = "input.new.category"
  }

  to {
    connector = "search"
    operation = "index"
    target    = "products"
  }
}

# Update search index when product changes
flow "cdc_product_updated" {
  from {
    connector = "pg_cdc"
    operation = "UPDATE:products"
    filter    = "input.new.name != input.old.name || input.new.price != input.old.price"
  }

  transform {
    id          = "input.new.id"
    name        = "input.new.name"
    description = "input.new.description"
    price       = "input.new.price"
    category    = "input.new.category"
  }

  to {
    connector = "search"
    operation = "index"
    target    = "products"
  }
}

# Remove from search on delete
flow "cdc_product_deleted" {
  from {
    connector = "pg_cdc"
    operation = "DELETE:products"
  }

  to {
    connector = "search"
    operation = "delete"
    target    = "products"
  }
}

# Publish all product changes as events
flow "cdc_product_events" {
  from {
    connector = "pg_cdc"
    operation = "*:products"
  }

  transform {
    event     = "'product.' + lower(input.trigger)"
    table     = "input.table"
    data      = "input.new"
    old_data  = "input.old"
    timestamp = "input.timestamp"
  }

  to {
    connector = "rabbit"
    operation = "PUBLISH"
    target    = "product.events"
  }
}
```

### How it works

1. The CDC connector listens to PostgreSQL's WAL (Write-Ahead Log) via logical replication
2. `INSERT:products` fires when a new row is inserted — indexes it in Elasticsearch
3. `UPDATE:products` fires on changes — the `filter` skips updates that don't affect searchable fields
4. `DELETE:products` removes the document from the search index
5. `*:products` catches all changes and publishes them as events to RabbitMQ
6. CDC variables: `input.new` (new row), `input.old` (old row), `input.trigger` (INSERT/UPDATE/DELETE)

---

## 19. GraphQL API over database

Expose a full GraphQL API (queries + mutations) backed by a database — auto-generated schema from HCL types, no resolvers to write.

### connectors.hcl

```hcl
connector "api" {
  type       = "graphql"
  driver     = "server"
  port       = 4000
  endpoint   = "/graphql"
  playground = true
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}
```

### types.hcl

```hcl
type "User" {
  id    = string { required = false }
  name  = string
  email = string { format = "email" }
  role  = string { enum = ["admin", "user", "viewer"] }
}

type "Post" {
  id        = string { required = false }
  title     = string { min_length = 1, max_length = 200 }
  body      = string
  author_id = string
  published = boolean
}
```

### flows.hcl

```hcl
# Query: fetch all users
flow "get_users" {
  from {
    connector = "api"
    operation = "Query.users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

# Query: fetch single user
flow "get_user" {
  from {
    connector = "api"
    operation = "Query.user"
  }
  to {
    connector = "db"
    target    = "users"
    filter    = "id = input.id"
  }
}

# Mutation: create user
flow "create_user" {
  from {
    connector = "api"
    operation = "Mutation.createUser"
  }
  to {
    connector = "db"
    operation = "INSERT users"
  }
}

# Query: fetch posts by author
flow "get_posts" {
  from {
    connector = "api"
    operation = "Query.posts"
  }
  to {
    connector = "db"
    target    = "posts"
    filter    = "author_id = input.author_id"
  }
}

# Mutation: create post
flow "create_post" {
  from {
    connector = "api"
    operation = "Mutation.createPost"
  }
  to {
    connector = "db"
    operation = "INSERT posts"
  }
}
```

### How it works

1. The `graphql` connector with `driver = "server"` exposes a GraphQL endpoint at port 4000
2. Schema is auto-generated from `type` blocks — `User` and `Post` become GraphQL types
3. `Query.users` and `Mutation.createUser` map directly to flow operations
4. GraphiQL playground available at `http://localhost:4000/graphql` for testing
5. Input types are generated automatically (e.g., `CreateUserInput` from the `User` type)
6. Field selection optimization: only requested fields are fetched from the database

---

## 20. Circuit breaker on external APIs

Protect your service from cascading failures when external APIs go down. The circuit breaker aspect wraps flows and short-circuits requests when failures exceed the threshold.

### connectors.hcl

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "payment_api" {
  type     = "http"
  base_url = env("PAYMENT_SERVICE_URL")
  timeout  = "5s"
}

connector "shipping_api" {
  type     = "http"
  base_url = env("SHIPPING_SERVICE_URL")
  timeout  = "5s"
}

connector "db" {
  type   = "database"
  driver = "postgres"
  dsn    = env("DATABASE_URL")
}
```

### flows.hcl

```hcl
flow "charge_payment" {
  from {
    connector = "api"
    operation = "POST /payments"
  }

  step "charge" {
    connector = "payment_api"
    operation = "POST /charge"
    body {
      amount   = "input.amount"
      currency = "input.currency"
      token    = "input.payment_token"
    }
  }

  response {
    transaction_id = "step.charge.transaction_id"
    status         = "step.charge.status"
  }
}

flow "create_shipment" {
  from {
    connector = "api"
    operation = "POST /shipments"
  }

  step "ship" {
    connector = "shipping_api"
    operation = "POST /shipments"
    body {
      order_id = "input.order_id"
      address  = "input.address"
    }
  }

  response {
    tracking_number = "step.ship.tracking_number"
    carrier         = "step.ship.carrier"
  }
}
```

### aspects.hcl

```hcl
# Payment API circuit breaker
aspect "payment_circuit_breaker" {
  when = "around"
  on   = ["charge_payment"]

  circuit_breaker {
    name              = "payment_api"
    failure_threshold = 3
    success_threshold = 2
    timeout           = "30s"
  }
}

# Shipping API circuit breaker
aspect "shipping_circuit_breaker" {
  when = "around"
  on   = ["create_shipment"]

  circuit_breaker {
    name              = "shipping_api"
    failure_threshold = 5
    success_threshold = 2
    timeout           = "60s"
  }
}

# Alert when any circuit opens
aspect "circuit_alert" {
  when = "on_error"
  on   = ["charge_payment", "create_shipment"]
  if   = "error.type == 'connection' || error.type == 'timeout'"

  action {
    flow = "notify_slack"
    transform {
      message = "':warning: Circuit breaker tripped for ' + _flow + ': ' + error.message"
    }
  }
}
```

### How it works

1. The `payment_circuit_breaker` wraps `charge_payment` — after 3 consecutive failures, the circuit opens
2. While open, all requests to `charge_payment` immediately fail with "circuit breaker open" (no external call)
3. After 30s, the circuit enters half-open state — the next request is a probe
4. If the probe succeeds twice, the circuit closes and normal operation resumes
5. The `circuit_alert` aspect sends a Slack message when connection/timeout errors occur
6. Each external API has its own named circuit breaker — they operate independently

---

## 21. PDF generation from HTML template

Generate PDF documents (invoices, reports, receipts) from HTML templates. The PDF connector renders an HTML subset to PDF using pure Go — no external binaries required.

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

connector "pdf" {
  type      = "pdf"
  page_size = "A4"
}
```

### templates/invoice.html

```html
<h1 style="text-align: center; color: #336699">Invoice #{{.number}}</h1>

<p>Date: {{.date}}</p>
<p>Customer: {{.customer_name}}</p>
<p>Email: {{.customer_email}}</p>

<hr>

<table>
  <tr><th>Item</th><th>Qty</th><th>Unit Price</th><th>Total</th></tr>
  {{range .items}}
  <tr>
    <td>{{.name}}</td>
    <td>{{.quantity}}</td>
    <td>${{.unit_price}}</td>
    <td>${{.line_total}}</td>
  </tr>
  {{end}}
</table>

<hr>

<p style="text-align: right; font-size: 18px"><strong>Total: ${{.total}}</strong></p>

<p style="font-size: 10px; color: #999999">Thank you for your business.</p>
```

### flows.hcl

```hcl
# Fetch invoice data and generate PDF
flow "get_invoice_pdf" {
  from {
    connector = "api"
    operation = "GET /invoices/:id/pdf"
  }

  step "invoice" {
    connector = "db"
    query     = "SELECT * FROM invoices WHERE id = :id"
    params    = { id = "input.params.id" }
  }

  step "items" {
    connector = "db"
    query     = "SELECT * FROM invoice_items WHERE invoice_id = :id"
    params    = { id = "input.params.id" }
  }

  transform {
    template       = "'./templates/invoice.html'"
    filename       = "'invoice-' + step.invoice.number + '.pdf'"
    number         = "step.invoice.number"
    date           = "step.invoice.date"
    customer_name  = "step.invoice.customer_name"
    customer_email = "step.invoice.customer_email"
    total          = "string(step.invoice.total)"
    items          = "step.items"
  }

  to {
    connector = "pdf"
    operation = "generate"
  }
}
```

### How it works

1. `GET /invoices/123/pdf` triggers the flow
2. Two steps fetch the invoice header and line items from PostgreSQL
3. The transform builds the template data, including the template file path
4. The PDF connector renders the HTML template with Go template syntax (`{{.field}}`, `{{range}}`)
5. The result is served directly as `application/pdf` with `Content-Disposition` header
6. Supported HTML: headings, paragraphs, tables, bold/italic, lists, images, horizontal rules, basic CSS styles

---

## 22. API versioning with deprecation warnings

Handle API versioning through separate flows per version. Use an `after` aspect with a `response` block to inject deprecation metadata into v1 responses automatically.

### connectors.hcl

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "sqlite"
  dsn    = "./data.db"
}
```

### flows.hcl

```hcl
# v1 — returns flat fields
flow "get_users_v1" {
  from {
    connector = "api"
    operation = "GET /v1/users"
  }

  to {
    connector = "db"
    operation = "SELECT id, first_name, last_name, email FROM users"
  }
}

# v2 — returns combined full_name
flow "get_users_v2" {
  from {
    connector = "api"
    operation = "GET /v2/users"
  }

  to {
    connector = "db"
    operation = "SELECT id, first_name || ' ' || last_name AS full_name, email FROM users"
  }
}
```

### aspects.hcl

```hcl
# Automatically inject deprecation headers and body metadata into all v1 responses
aspect "v1_deprecation" {
  when = "after"
  on   = ["*_v1"]

  response {
    # HTTP headers (standard RFC 8594 deprecation headers)
    headers = {
      Deprecation = "true"
      Sunset      = "Thu, 01 Jun 2026 00:00:00 GMT"
    }

    # Body fields (CEL expressions)
    _warning = "'This API version is deprecated. Please migrate to v2.'"
  }
}
```

### How it works

1. `GET /v1/users` and `GET /v2/users` are separate flows — different queries, different response shapes
2. The `v1_deprecation` aspect matches all flows ending in `_v1` (glob pattern)
3. After the flow executes, the `response` block:
   - Sets `Deprecation` and `Sunset` as actual HTTP headers (RFC 8594)
   - Injects `_warning` into the JSON response body
4. v2 flows are unaffected — the aspect only matches `*_v1`
5. No code changes needed in individual flows — the deprecation policy is centralized in one aspect
6. Headers are connector-agnostic in HCL — the REST connector sets them as HTTP headers, other connectors can map them to their protocol equivalent (e.g., gRPC metadata)

### Example response

```
HTTP/1.1 200 OK
Content-Type: application/json
Deprecation: true
Sunset: Thu, 01 Jun 2026 00:00:00 GMT

// GET /v1/users
[
  {
    "id": 1,
    "first_name": "Alice",
    "last_name": "Smith",
    "email": "alice@example.com",
    "_warning": "This API version is deprecated. Please migrate to v2."
  }
]

// GET /v2/users — no deprecation headers or fields
[
  {
    "id": 1,
    "full_name": "Alice Smith",
    "email": "alice@example.com"
  }
]
```

---

## 23. Idempotent payment processing

Prevent duplicate charges by caching results keyed on the payment ID.

### config

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

connector "redis_cache" {
  type   = "cache"
  driver = "redis"
  url    = env("REDIS_URL")
}
```

### flow

```hcl
flow "process_payment" {
  from { connector.api = "POST /payments" }
  to   { connector.db  = "payments" }

  idempotency {
    storage = "redis_cache"
    key     = "input.payment_id"
    ttl     = "24h"
  }

  transform {
    output.payment_id = input.payment_id
    output.amount     = input.amount
    output.status     = "completed"
    output.created_at = now()
  }
}
```

### How it works

1. First request with `payment_id = "pay_123"` executes the flow normally and caches the result
2. Subsequent requests with the same `payment_id` return the cached result without re-executing the flow
3. Cache entry expires after `ttl` (24 hours)

---

## 24. Async long-running export with polling

Return HTTP 202 immediately and process in background.

### config

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

connector "redis_cache" {
  type   = "cache"
  driver = "redis"
  url    = env("REDIS_URL")
}
```

### flow

```hcl
flow "export_report" {
  from { connector.api = "POST /reports/export" }
  to   { connector.db  = "SELECT * FROM orders WHERE date >= :start_date" }

  async {
    storage = "redis_cache"
    ttl     = "1h"
  }
}
```

### How it works

1. `POST /reports/export` returns `202 Accepted` with `{"job_id": "abc123", "status": "pending", "poll_url": "/jobs/abc123"}`
2. Flow executes in the background
3. Client polls `GET /jobs/abc123` to check status: `{"status": "completed", "result": [...]}`
4. Job results are stored for `ttl` (1 hour), then expire

---

## 25. Database migrations

Run SQL migrations from the `migrations/` directory.

### migrations/001_create_users.sql

```sql
CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Usage

```bash
# Run pending migrations
mycel migrate --config ./my-service

# Show migration status
mycel migrate status --config ./my-service

# Specify a particular database connector
mycel migrate --connector pg_main
```

### How it works

1. Mycel reads all `.sql` files from `migrations/` in alphabetical order
2. A `_mycel_migrations` tracking table records which migrations have been applied
3. Only pending (unapplied) migrations are executed
4. Compatible with SQLite and PostgreSQL

---

## 26. Distributed rate limiting with Redis

Share rate limits across multiple service instances.

### config

```hcl
service {
  name    = "api-gateway"
  version = "1.0.0"

  rate_limit {
    requests_per_second = 50
    burst               = 100
    key_extractor       = "header:X-API-Key"
    storage             = "redis_cache"
    enable_headers      = true
  }
}

connector "api" {
  type = "rest"
  port = 3000
}

connector "redis_cache" {
  type   = "cache"
  driver = "redis"
  url    = env("REDIS_URL")
}
```

### How it works

1. Rate limit counters are stored in Redis instead of in-memory
2. All service instances share the same counters, providing true distributed rate limiting
3. If Redis becomes unavailable, rate limiting falls back to local in-memory counters automatically
4. Uses fixed-window counter algorithm with 1-second windows

---

## 27. Multi-tenancy via request headers

Isolate data per tenant using HTTP request headers. The `X-Tenant-ID` header is read in flow transforms and used to filter database queries, ensuring each tenant only sees their own data.

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
```

### flows.hcl

```hcl
# List products filtered by tenant
flow "list_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  to {
    connector = "db"
    operation = "SELECT * FROM products WHERE tenant_id = :tenant"
    params    = { tenant = "input.headers[\"x-tenant-id\"]" }
  }
}

# Create a product scoped to the tenant
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  transform {
    name       = "input.name"
    price      = "input.price"
    tenant_id  = "input.headers[\"x-tenant-id\"]"
    created_at = "now()"
  }

  to {
    connector = "db"
    operation = "INSERT products"
  }
}
```

### How it works

1. Request headers are available as `input.headers` in flow transforms and CEL expressions
2. Header names are lowercase (e.g., `X-Tenant-ID` becomes `input.headers["x-tenant-id"]`)
3. Headers are automatically stripped from the payload before database writes, so `x-tenant-id` does not end up as a column unless explicitly mapped in a transform (as `tenant_id` in the example above)
4. Use `input.headers["x-tenant-id"]` (bracket syntax) for headers containing hyphens, or `input.headers.x_tenant_id` (dot syntax) if the header name uses underscores
5. This pattern works with any connector as source (REST, GraphQL, gRPC) -- headers or metadata are always exposed via `input.headers`

### Example request

```bash
curl -H "X-Tenant-ID: acme-corp" http://localhost:3000/products
# Returns only products where tenant_id = 'acme-corp'

curl -X POST http://localhost:3000/products \
  -H "X-Tenant-ID: acme-corp" \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 9.99}'
# Inserts with tenant_id = 'acme-corp'
```

---

## 28. Fan-out from source (multiple flows, same trigger)

**Use case:** A single REST endpoint or MQ topic triggers multiple independent workflows — e.g., when an order arrives, save it to the database AND send a notification AND update analytics, each as a separate flow with its own transform and error handling.

### REST: One endpoint, two flows

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "postgres"
  # ...
}

connector "slack" {
  type   = "notification"
  driver = "slack"
  # ...
}

# Flow 1: Save to database (returns the HTTP response)
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

# Flow 2: Notify on Slack (fire-and-forget, same endpoint)
flow "notify_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  transform {
    channel = "'#orders'"
    text    = "'New order from ' + input.customer"
  }
  to {
    connector = "slack"
    target    = "message"
  }
}
```

Both flows share `POST /orders`. The first registered flow returns the HTTP response to the client. The second runs concurrently in the background — if it fails, the client still gets a successful response and the error is logged.

### MQ: One queue, two consumers

```hcl
# Two flows consuming from the same RabbitMQ queue
flow "process_payment" {
  from {
    connector = "orders_queue"
    operation = "orders"
  }
  to {
    connector = "payments_db"
    target    = "payments"
  }
}

flow "update_inventory" {
  from {
    connector = "orders_queue"
    operation = "orders"
  }
  transform {
    sku      = "input.body.sku"
    quantity = "input.body.quantity * -1"
  }
  to {
    connector = "inventory_db"
    target    = "stock"
  }
}
```

For event-driven connectors (RabbitMQ, Kafka, MQTT, etc.), **all flows run in parallel** and the message is acknowledged only after all complete. If any flow fails, the message is NACKed and retried.

---

## See Also

- [Integration Patterns](../advanced/integration-patterns.md) -- advanced patterns (GraphQL, gRPC, message queues)
- [Notifications Guide](notifications.md) -- all notification connectors in detail
- [Aspects / Extending](extending.md) -- full aspect reference
- [Error Handling](error-handling.md) -- retry, DLQ, fallback patterns
- [Multi-Step Flows](multi-step-flows.md) -- step blocks, conditional logic, fan-out
