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

## See Also

- [Integration Patterns](../advanced/integration-patterns.md) -- advanced patterns (GraphQL, gRPC, message queues)
- [Notifications Guide](notifications.md) -- all notification connectors in detail
- [Aspects / Extending](extending.md) -- full aspect reference
- [Error Handling](error-handling.md) -- retry, DLQ, fallback patterns
- [Multi-Step Flows](multi-step-flows.md) -- step blocks, conditional logic, fan-out
