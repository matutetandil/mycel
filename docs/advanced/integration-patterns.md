# Integration Patterns

This guide shows common integration patterns with complete, copy-paste ready examples.

Each pattern includes:
- Use case description
- Complete HCL configuration
- Test commands

---

## Table of Contents

1. [GraphQL API → Database](#1-graphql-api--database)
2. [REST → GraphQL Passthrough](#2-rest--graphql-passthrough)
3. [GraphQL → REST Passthrough](#3-graphql--rest-passthrough)
4. [RabbitMQ → Database](#4-rabbitmq--database)
5. [REST → RabbitMQ](#5-rest--rabbitmq)
6. [GraphQL → RabbitMQ](#6-graphql--rabbitmq)
7. [Raw SQL Queries (JOINs)](#7-raw-sql-queries-joins)

---

## 1. GraphQL API → Database

**Use case:** Expose a GraphQL API that reads/writes to a database.

### Configuration

```hcl
# config.mycel
service {
  name    = "users-graphql-api"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

# GraphQL Server
connector "api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  cors {
    origins = ["*"]
  }

  schema {
    path = "./schema.graphql"
  }
}

# Database
connector "db" {
  type   = "database"
  driver = "sqlite"
  path   = "./data/users.db"
}
```

```graphql
# schema.graphql
type User {
  id: ID!
  email: String!
  name: String!
  createdAt: String
}

input CreateUserInput {
  email: String!
  name: String!
}

type Query {
  users: [User!]!
  user(id: ID!): User
}

type Mutation {
  createUser(input: CreateUserInput!): User!
  deleteUser(id: ID!): Boolean!
}
```

```hcl
# flows.mycel

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

flow "get_user" {
  from {
    connector = "api"
    operation = "Query.user"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "Mutation.createUser"
  }
  transform {
    email      = "lower(input.email)"
    name       = "input.name"
    created_at = "now()"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

flow "delete_user" {
  from {
    connector = "api"
    operation = "Mutation.deleteUser"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
```

### Test

```bash
# Start service
mycel start --config .

# Query all users
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ users { id email name } }"}'

# Create user
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { createUser(input: {email: \"john@example.com\", name: \"John\"}) { id email } }"}'

# Open playground
open http://localhost:4000/playground
```

---

## 2. REST → GraphQL Passthrough

**Use case:** Receive REST requests and forward them to an external GraphQL API.

### Configuration

```hcl
# config.mycel
service {
  name    = "rest-to-graphql-gateway"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

# REST Server (receives requests)
connector "api" {
  type = "rest"
  port = 3000
}

# External GraphQL API (forwards to)
connector "products_api" {
  type     = "graphql"
  driver   = "client"
  endpoint = "https://api.example.com/graphql"

  auth {
    type  = "bearer"
    token = env("PRODUCTS_API_TOKEN")
  }

  timeout     = "30s"
  retry_count = 3
}
```

```hcl
# flows.mycel

# GET /products -> GraphQL query
flow "get_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  enrich "products" {
    connector = "products_api"
    operation = "query { products { id name price } }"
  }

  transform {
    products = "enriched.products.products"
  }

  to {
    connector = "api"  # Returns response
  }
}

# GET /products/:id -> GraphQL query with variable
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  enrich "product" {
    connector = "products_api"
    operation = "query GetProduct($id: ID!) { product(id: $id) { id name price description } }"
    params {
      id = "input.id"
    }
  }

  transform {
    id          = "enriched.product.product.id"
    name        = "enriched.product.product.name"
    price       = "enriched.product.product.price"
    description = "enriched.product.product.description"
  }

  to {
    connector = "api"
  }
}

# POST /products -> GraphQL mutation
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  enrich "created" {
    connector = "products_api"
    operation = "mutation CreateProduct($input: ProductInput!) { createProduct(input: $input) { id name } }"
    params {
      input = "input"
    }
  }

  transform {
    id   = "enriched.created.createProduct.id"
    name = "enriched.created.createProduct.name"
  }

  to {
    connector = "api"
  }
}
```

### Test

```bash
# Start service
PRODUCTS_API_TOKEN=your-token mycel start --config .

# Get all products (REST -> GraphQL)
curl http://localhost:3000/products

# Get single product
curl http://localhost:3000/products/123

# Create product
curl -X POST http://localhost:3000/products \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 29.99}'
```

---

## 3. GraphQL → REST Passthrough

**Use case:** Expose a GraphQL API that internally calls REST endpoints.

### Configuration

```hcl
# config.mycel
service {
  name    = "graphql-to-rest-gateway"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

# GraphQL Server (receives requests)
connector "api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  schema {
    path = "./schema.graphql"
  }
}

# External REST API (forwards to)
connector "users_rest" {
  type     = "http"
  base_url = "https://api.example.com"

  auth {
    type   = "api_key"
    header = "X-API-Key"
    key    = env("USERS_API_KEY")
  }

  timeout = "30s"
}
```

```graphql
# schema.graphql
type User {
  id: ID!
  email: String!
  name: String!
  avatar: String
}

type Query {
  users: [User!]!
  user(id: ID!): User
}

type Mutation {
  createUser(email: String!, name: String!): User!
}
```

```hcl
# flows.mycel

# Query.users -> GET /users
flow "get_users" {
  from {
    connector = "api"
    operation = "Query.users"
  }

  enrich "users" {
    connector = "users_rest"
    operation = "GET /users"
  }

  transform {
    result = "enriched.users"
  }

  to {
    connector = "api"
  }
}

# Query.user(id) -> GET /users/:id
flow "get_user" {
  from {
    connector = "api"
    operation = "Query.user"
  }

  enrich "user" {
    connector = "users_rest"
    operation = "GET /users/${input.id}"
  }

  transform {
    id     = "enriched.user.id"
    email  = "enriched.user.email"
    name   = "enriched.user.name"
    avatar = "enriched.user.avatar"
  }

  to {
    connector = "api"
  }
}

# Mutation.createUser -> POST /users
flow "create_user" {
  from {
    connector = "api"
    operation = "Mutation.createUser"
  }

  enrich "created" {
    connector = "users_rest"
    operation = "POST /users"
    params {
      email = "input.email"
      name  = "input.name"
    }
  }

  transform {
    id    = "enriched.created.id"
    email = "enriched.created.email"
    name  = "enriched.created.name"
  }

  to {
    connector = "api"
  }
}
```

### Test

```bash
# Start service
USERS_API_KEY=your-key mycel start --config .

# Query users via GraphQL (calls REST internally)
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ users { id email name } }"}'

# Query single user
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ user(id: \"123\") { id email name avatar } }"}'

# Create user via GraphQL mutation
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { createUser(email: \"john@example.com\", name: \"John\") { id email } }"}'
```

---

## 4. RabbitMQ → Database

**Use case:** Consume messages from a queue and store them in a database.

### Configuration

```hcl
# config.mycel
service {
  name    = "order-processor"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

# RabbitMQ Consumer
connector "orders_queue" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = 5672
  user     = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")

  queue {
    name    = "orders"
    durable = true
  }

  exchange {
    name        = "orders_exchange"
    type        = "topic"
    durable     = true
    routing_key = "order.#"
  }

  consumer {
    auto_ack    = false
    concurrency = 2
    prefetch    = 10
  }
}

# Database
connector "db" {
  type   = "database"
  driver = "postgres"

  host     = env("DB_HOST", "localhost")
  port     = 5432
  database = "orders"
  user     = env("DB_USER", "postgres")
  password = env("DB_PASS", "postgres")
}
```

```hcl
# flows.mycel

# Process all order events
flow "process_order" {
  from {
    connector = "orders_queue"
    operation = "order.*"  # Matches order.created, order.updated, etc.
  }

  transform {
    id           = "input.order_id"
    product      = "input.product"
    quantity     = "input.quantity"
    customer     = "input.customer.email"
    status       = "'pending'"
    received_at  = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

# Process only order.created events
flow "new_order_notification" {
  from {
    connector = "orders_queue"
    operation = "order.created"
  }

  transform {
    order_id   = "input.order_id"
    email      = "input.customer.email"
    subject    = "'New Order Received'"
    message    = "'Your order ' + input.order_id + ' has been received.'"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "notifications"
  }
}
```

### Test

```bash
# Start RabbitMQ
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# Start service
mycel start --config .

# Publish a test message (using rabbitmqadmin or your app)
rabbitmqadmin publish exchange=orders_exchange routing_key=order.created \
  payload='{"order_id":"ORD-001","product":"Widget","quantity":5,"customer":{"email":"john@example.com"}}'
```

---

## 5. REST → RabbitMQ

**Use case:** Receive REST requests and publish messages to a queue for async processing.

### Configuration

```hcl
# config.mycel
service {
  name    = "order-api"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

# REST API
connector "api" {
  type = "rest"
  port = 3000
}

# RabbitMQ Publisher
connector "order_events" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = 5672
  user     = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")

  publisher {
    exchange    = "orders_exchange"
    routing_key = "order.created"
    persistent  = true
  }
}

# Optional: Database to store order locally
connector "db" {
  type   = "database"
  driver = "sqlite"
  path   = "./data/orders.db"
}
```

```hcl
# flows.mycel

# POST /orders -> Publish to queue
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  transform {
    order_id   = "uuid()"
    product    = "input.product"
    quantity   = "input.quantity"
    customer   = "input.customer"
    status     = "'queued'"
    created_at = "now()"
  }

  to {
    connector   = "order_events"
    routing_key = "order.created"
  }
}

# POST /orders/:id/cancel -> Publish cancel event
flow "cancel_order" {
  from {
    connector = "api"
    operation = "POST /orders/:id/cancel"
  }

  transform {
    order_id     = "input.id"
    reason       = "input.reason"
    cancelled_at = "now()"
  }

  to {
    connector   = "order_events"
    routing_key = "order.cancelled"
  }
}
```

### Test

```bash
# Start RabbitMQ
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# Start service
mycel start --config .

# Create order (publishes to queue)
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{
    "product": "Widget Pro",
    "quantity": 5,
    "customer": {
      "name": "John Doe",
      "email": "john@example.com"
    }
  }'

# Cancel order
curl -X POST http://localhost:3000/orders/ORD-001/cancel \
  -H "Content-Type: application/json" \
  -d '{"reason": "Customer request"}'

# Check RabbitMQ management UI
open http://localhost:15672  # guest/guest
```

---

## 6. GraphQL → RabbitMQ

**Use case:** Expose GraphQL mutations that publish messages to a queue.

### Configuration

```hcl
# config.mycel
service {
  name    = "graphql-order-api"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

# GraphQL Server
connector "api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  schema {
    path = "./schema.graphql"
  }
}

# RabbitMQ Publisher
connector "events" {
  type   = "mq"
  driver = "rabbitmq"

  host     = env("RABBITMQ_HOST", "localhost")
  port     = 5672
  user     = env("RABBITMQ_USER", "guest")
  password = env("RABBITMQ_PASS", "guest")

  publisher {
    exchange   = "events_exchange"
    persistent = true
  }
}

# Database for queries
connector "db" {
  type   = "database"
  driver = "sqlite"
  path   = "./data/orders.db"
}
```

```graphql
# schema.graphql
type Order {
  id: ID!
  product: String!
  quantity: Int!
  status: String!
  createdAt: String!
}

input CreateOrderInput {
  product: String!
  quantity: Int!
  customerEmail: String!
}

type OrderResult {
  id: ID!
  status: String!
  message: String!
}

type Query {
  orders: [Order!]!
  order(id: ID!): Order
}

type Mutation {
  createOrder(input: CreateOrderInput!): OrderResult!
  cancelOrder(id: ID!, reason: String): OrderResult!
}
```

```hcl
# flows.mycel

# Query orders from database
flow "get_orders" {
  from {
    connector = "api"
    operation = "Query.orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}

# Mutation.createOrder -> Publish to queue
flow "create_order" {
  from {
    connector = "api"
    operation = "Mutation.createOrder"
  }

  transform {
    order_id       = "uuid()"
    product        = "input.product"
    quantity       = "input.quantity"
    customer_email = "input.customerEmail"
    status         = "'queued'"
    created_at     = "now()"
  }

  to {
    connector   = "events"
    routing_key = "order.created"
  }

  # Return confirmation to client
  returns = "OrderResult"
}

# Mutation.cancelOrder -> Publish cancel event
flow "cancel_order" {
  from {
    connector = "api"
    operation = "Mutation.cancelOrder"
  }

  transform {
    order_id     = "input.id"
    reason       = "default(input.reason, 'No reason provided')"
    cancelled_at = "now()"
  }

  to {
    connector   = "events"
    routing_key = "order.cancelled"
  }

  returns = "OrderResult"
}
```

### Test

```bash
# Start RabbitMQ
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# Start service
mycel start --config .

# Create order via GraphQL (publishes to RabbitMQ)
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation { createOrder(input: {product: \"Widget\", quantity: 5, customerEmail: \"john@example.com\"}) { id status message } }"
  }'

# Cancel order via GraphQL
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation { cancelOrder(id: \"ORD-001\", reason: \"Changed mind\") { id status } }"
  }'

# Open GraphQL Playground
open http://localhost:4000/playground
```

---

## 7. Raw SQL Queries (JOINs)

**Use case:** Execute complex SQL queries with JOINs, subqueries, or multi-table operations.

### Configuration

```hcl
# config.mycel
service {
  name    = "orders-api"
  version = "1.0.0"
}
```

```hcl
# connectors.mycel

connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "sqlite"  # or "postgres"
  path   = "./data/orders.db"
}
```

```hcl
# flows.mycel

# Simple JOIN: Get order with user info
flow "get_order_with_user" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  to {
    connector = "db"
    query     = <<-SQL
      SELECT
        o.id,
        o.product,
        o.quantity,
        o.status,
        o.created_at,
        u.name as user_name,
        u.email as user_email
      FROM orders o
      JOIN users u ON u.id = o.user_id
      WHERE o.id = :id
    SQL
  }
}

# Multiple named parameters
flow "get_orders_filtered" {
  from {
    connector = "api"
    operation = "GET /orders"
  }

  to {
    connector = "db"
    query     = <<-SQL
      SELECT o.*, u.name as user_name
      FROM orders o
      JOIN users u ON u.id = o.user_id
      WHERE o.status = :status
        AND o.created_at >= :from_date
      ORDER BY o.created_at DESC
      LIMIT :limit
    SQL
  }
}

# Aggregation query
flow "get_user_order_stats" {
  from {
    connector = "api"
    operation = "GET /users/:user_id/stats"
  }

  to {
    connector = "db"
    query     = <<-SQL
      SELECT
        u.id,
        u.name,
        u.email,
        COUNT(o.id) as total_orders,
        SUM(o.quantity) as total_items,
        MAX(o.created_at) as last_order_date
      FROM users u
      LEFT JOIN orders o ON o.user_id = u.id
      WHERE u.id = :user_id
      GROUP BY u.id, u.name, u.email
    SQL
  }
}

# Subquery example
flow "get_top_customers" {
  from {
    connector = "api"
    operation = "GET /reports/top-customers"
  }

  to {
    connector = "db"
    query     = <<-SQL
      SELECT
        u.id,
        u.name,
        u.email,
        order_stats.order_count,
        order_stats.total_spent
      FROM users u
      JOIN (
        SELECT
          user_id,
          COUNT(*) as order_count,
          SUM(total) as total_spent
        FROM orders
        WHERE status = 'completed'
        GROUP BY user_id
        HAVING COUNT(*) >= 5
      ) order_stats ON order_stats.user_id = u.id
      ORDER BY order_stats.total_spent DESC
      LIMIT 10
    SQL
  }
}

# INSERT with raw SQL
flow "create_order_with_audit" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  to {
    connector = "db"
    query     = <<-SQL
      INSERT INTO orders (id, user_id, product, quantity, status, created_at)
      VALUES (:id, :user_id, :product, :quantity, 'pending', datetime('now'))
    SQL
  }

  transform {
    id       = "uuid()"
    user_id  = "input.user_id"
    product  = "input.product"
    quantity = "input.quantity"
  }
}

# UPDATE with JOIN (PostgreSQL)
flow "update_order_status" {
  from {
    connector = "api"
    operation = "PUT /orders/:id/status"
  }

  to {
    connector = "db"
    query     = <<-SQL
      UPDATE orders
      SET status = :status,
          updated_at = NOW(),
          updated_by = :updated_by
      WHERE id = :id
      RETURNING id, status, updated_at
    SQL
  }

  transform {
    id         = "input.id"
    status     = "input.status"
    updated_by = "input.user_id"
  }
}
```

### Named Parameters

Use `:param_name` syntax for parameters. Values come from:
- Path parameters (`:id` from `/orders/:id`)
- Query parameters (`?status=active`)
- Request body (POST/PUT)
- Transform output

```hcl
# Parameter sources:
# - Path: GET /orders/:id        -> :id
# - Query: ?status=active        -> :status
# - Body: {"user_id": 123}       -> :user_id
# - Transform: id = "uuid()"     -> :id
```

### Test

```bash
# Start service
mycel start --config .

# Get order with user info (JOIN)
curl http://localhost:3000/orders/1

# Get filtered orders with multiple params
curl "http://localhost:3000/orders?status=pending&from_date=2024-01-01&limit=10"

# Get user stats (aggregation)
curl http://localhost:3000/users/1/stats

# Get top customers (subquery)
curl http://localhost:3000/reports/top-customers

# Create order with raw SQL
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{"user_id": 1, "product": "Widget", "quantity": 5}'
```

---

## Quick Reference

### Connector Types

| Type | Drivers | Use as Source | Use as Target |
|------|---------|---------------|---------------|
| `rest` | - | HTTP Server | HTTP Response |
| `graphql` | `server`, `client` | GraphQL Server | GraphQL Client |
| `database` | `sqlite`, `postgres` | Query | Insert/Update |
| `mq` | `rabbitmq`, `kafka` | Consumer | Publisher |
| `http` | - | - | HTTP Client |
| `tcp` | - | TCP Server | TCP Client |

### Flow Structure

```hcl
flow "name" {
  from {
    connector = "source_connector"
    operation = "operation"  # REST: "GET /path", GraphQL: "Query.field", MQ: "routing.key"
  }

  # Optional: validate input
  validate {
    input = "type.name"
  }

  # Optional: enrich from external services
  enrich "name" {
    connector = "other_connector"
    operation = "..."
    params { ... }
  }

  # Optional: transform data
  transform {
    field = "expression"
  }

  to {
    connector = "target_connector"
    target    = "table_or_topic"      # For database/mq
    query     = "raw SQL"             # For complex queries
  }

  # Optional: specify return type (GraphQL)
  returns = "Type"
}
```

### Common CEL Functions

```hcl
transform {
  # String functions
  id         = "uuid()"
  email      = "lower(trim(input.email))"
  slug       = "replace(lower(input.name), ' ', '-')"

  # Date/time
  created_at = "now()"
  timestamp  = "now_unix()"

  # Conditionals
  status     = "input.age >= 18 ? 'adult' : 'minor'"
  role       = "default(input.role, 'user')"

  # Enriched data
  price      = "enriched.pricing.price"

  # Math
  total      = "input.quantity * enriched.product.price"
}
```

---

## More Examples

- [GraphQL Full Example](../examples/graphql/README.md)
- [Message Queue Example](../examples/mq/README.md)
- [Data Enrichment](../examples/enrich/README.md)
- [TCP Connector](../examples/tcp/README.md)
- [Transformations Guide](../core-concepts/transforms.md)

---

## Event-Driven Integration Patterns

The following patterns show complete, production-ready examples for event-driven architectures with RabbitMQ as the central message broker. All examples are available in `examples/integration/`.

### Pattern: RabbitMQ → REST

**Use case:** Consume messages from a queue and call external REST APIs.

**Common scenarios:**
- Process orders and notify fulfillment service
- Sync data to external CRM/ERP systems
- Trigger webhooks based on events

```hcl
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
  host   = env("RABBIT_HOST")
  # ...
}

connector "fulfillment_api" {
  type     = "rest"
  mode     = "client"
  base_url = env("FULFILLMENT_API_URL")

  auth {
    type = "bearer"
    bearer { token = env("API_TOKEN") }
  }

  retry {
    attempts = 3
    backoff  = "exponential"
  }

  circuit_breaker {
    threshold = 5
    timeout   = "30s"
  }
}

flow "process_order" {
  from {
    connector.rabbit = {
      queue   = "orders.pending"
      durable = true

      bind {
        exchange    = "orders"
        routing_key = "order.created"
      }

      dlq {
        enabled     = true
        queue       = "orders.pending.dlq"
        max_retries = 3
      }
    }
  }

  transform {
    output.external_id = "input.body.order_id"
    output.customer    = "input.body.customer"
    output.items       = "input.body.items"
  }

  to {
    connector.fulfillment_api = "POST /v1/shipments"
  }
}
```

📁 Full example: `examples/integration/rabbit-to-rest/`

---

### Pattern: RabbitMQ → GraphQL

**Use case:** Consume messages from a queue and call GraphQL APIs.

**Common scenarios:**
- Update inventory in a GraphQL-based product service
- Sync user data to Hasura/Apollo backend
- Trigger mutations based on domain events

```hcl
connector "inventory_graphql" {
  type     = "graphql"
  mode     = "client"
  endpoint = env("INVENTORY_GRAPHQL_URL")

  auth {
    type = "bearer"
    bearer { token = env("GRAPHQL_TOKEN") }
  }
}

flow "update_inventory" {
  from {
    connector.rabbit = {
      queue   = "inventory.updates"
      durable = true

      bind {
        exchange    = "inventory"
        routing_key = "stock.changed"
      }
    }
  }

  to {
    connector.inventory_graphql = {
      query = <<GRAPHQL
        mutation UpdateStock($sku: String!, $quantity: Int!) {
          updateInventory(input: { sku: $sku, quantity: $quantity }) {
            success
            inventory { id, sku, quantity }
          }
        }
      GRAPHQL
      variables {
        sku      = "${input.body.sku}"
        quantity = "${input.body.new_quantity}"
      }
    }
  }
}
```

📁 Full example: `examples/integration/rabbit-to-graphql/`

---

### Pattern: RabbitMQ → Exec

**Use case:** Consume messages from a queue and execute local processes/scripts.

**Common scenarios:**
- PDF generation, image processing, video transcoding
- Run data processing scripts (Python, R, shell)
- Execute legacy system integrations
- Trigger batch jobs

```hcl
connector "exec" {
  type        = "exec"
  working_dir = "/app/scripts"
  timeout     = "5m"
  shell       = "/bin/bash"
}

flow "process_image" {
  # Limit concurrent image processing
  semaphore {
    key     = "image_processing"
    permits = 3
    storage = "memory"
    on_fail = "wait"
  }

  from {
    connector.rabbit = {
      queue = "images.processing"
      bind {
        exchange    = "images"
        routing_key = "image.*"
      }
    }
  }

  to {
    connector.exec = {
      command = "./process_image.sh"
      args    = [
        "${input.body.source_path}",
        "${input.body.dest_path}",
        "${input.body.operation}"
      ]
      timeout = "3m"
    }
  }
}
```

📁 Full example: `examples/integration/rabbit-to-exec/`

---

### Pattern: REST → RabbitMQ (API Gateway)

**Use case:** Receive HTTP requests and publish messages to a queue.

**Common scenarios:**
- API Gateway that decouples request handling from processing
- Webhook receivers that queue events for async processing
- Command endpoints that trigger background jobs

```hcl
connector "api" {
  type = "rest"
  mode = "server"
  port = 8080

  rate_limit {
    requests = 1000
    window   = "1m"
    by       = "ip"
  }
}

connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"

  exchange {
    name    = "events"
    type    = "topic"
    durable = true
  }
}

flow "create_order" {
  from {
    connector.api = "POST /orders"
  }

  transform {
    output.order_id   = "input.order_id ?? uuid()"
    output.customer   = "input.customer"
    output.items      = "input.items"
    output.created_at = "now()"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "order.created"
      persistent  = true

      headers {
        "x-request-id" = "${context.request_id}"
      }
    }
  }

  response {
    status = 202
    body = {
      message  = "Order received"
      order_id = "${output.order_id}"
    }
  }
}

flow "receive_webhook" {
  from {
    connector.api = "POST /webhooks/:provider"
  }

  transform {
    output.provider   = "input.params.provider"
    output.event_type = "input.headers['x-event-type']"
    output.payload    = "input.body"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "'webhook.' + output.provider + '.' + output.event_type"
      persistent  = true
    }
  }

  response {
    status = 200
    body   = { received = true }
  }
}
```

📁 Full example: `examples/integration/rest-to-rabbit/`

---

### Pattern: File → RabbitMQ (Scheduled Import)

**Use case:** Read files periodically and publish content to queue.

**Common scenarios:**
- Process drop folders (CSV imports, data feeds)
- Watch for new files and trigger processing
- Batch file processing on schedule
- Log file tailing and event streaming

```hcl
connector "files" {
  type      = "file"
  base_path = "/data"
}

connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"

  exchange {
    name    = "imports"
    type    = "topic"
    durable = true
  }
}

flow "process_daily_import" {
  when = "0 6 * * *"  # Every day at 6am

  from {
    connector.files = {
      path   = "imports/daily/*.csv"
      format = "csv"
      glob   = true

      csv {
        delimiter = ","
        header    = true
      }

      on_success { move_to = "imports/archive/" }
      on_error   { move_to = "imports/failed/" }
    }
  }

  foreach "row" in "input.rows" {
    transform {
      output.record_id   = "row.id ?? uuid()"
      output.data        = "row"
      output.source      = "input.file_name"
      output.imported_at = "now()"
    }

    to {
      connector.rabbit = {
        exchange    = "imports"
        routing_key = "import.daily.record"
        persistent  = true
      }
    }
  }
}

flow "watch_drop_folder" {
  when = "@every 30s"

  from {
    connector.files = {
      path = "dropbox/*.json"
      glob = true

      filter {
        newer_than = "30s"
      }
    }
  }

  to {
    connector.rabbit = {
      exchange    = "imports"
      routing_key = "file.dropped"
      persistent  = true
    }
  }
}
```

📁 Full example: `examples/integration/file-to-rabbit/`

---

## Complete Event-Driven Architecture

Real-world systems combine multiple patterns:

```
┌─────────────────────────────────────────────────────────────┐
│                        Mycel Service                         │
│                                                              │
│  ┌──────────┐     ┌──────────┐     ┌──────────────────┐     │
│  │ REST API │────▶│ RabbitMQ │────▶│ External REST API │     │
│  └──────────┘     └──────────┘     └──────────────────┘     │
│                         │                                    │
│                         ├─────────▶ GraphQL Backend          │
│                         │                                    │
│                         └─────────▶ Exec (Scripts)           │
│                                                              │
│  ┌──────────┐                                                │
│  │ Files/S3 │─────────────────────▶ RabbitMQ                 │
│  └──────────┘                                                │
│     (cron)                                                   │
└─────────────────────────────────────────────────────────────┘
```

### Example: Order Processing Pipeline

```hcl
# 1. Receive order via API
flow "receive_order" {
  from { connector.api = "POST /orders" }
  to   { connector.rabbit = { exchange = "orders", routing_key = "order.received" } }
}

# 2. Validate inventory
flow "validate_inventory" {
  from { connector.rabbit = { queue = "orders.validation" } }
  to   { connector.inventory_graphql = { query = "..." } }
}

# 3. Process payment
flow "process_payment" {
  from { connector.rabbit = { queue = "orders.payment" } }
  to   { connector.payment_api = "POST /v1/charges" }
}

# 4. Generate invoice PDF
flow "generate_invoice" {
  from { connector.rabbit = { queue = "orders.invoice" } }
  to   { connector.exec = { command = "./generate_invoice.py" } }
}

# 5. Notify customer
flow "send_notification" {
  from { connector.rabbit = { queue = "orders.notify" } }
  to   { connector.email = { to = "${input.body.customer.email}" } }
}
```

---

## Best Practices

### 1. Always Use DLQ for Critical Flows

```hcl
from {
  connector.rabbit = {
    queue = "critical.queue"

    dlq {
      enabled     = true
      queue       = "critical.queue.dlq"
      max_retries = 3
    }
  }
}
```

### 2. Use Semaphores for Rate-Limited APIs

```hcl
flow "call_rate_limited_api" {
  semaphore {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "external_api"
    permits = 5  # Max 5 concurrent calls
  }
  # ...
}
```

### 3. Use Locks for Non-Idempotent Operations

```hcl
flow "process_payment" {
  lock {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'payment:' + input.body.payment_id"
    timeout = "5m"
  }
  # ...
}
```

### 4. Configure Circuit Breakers for External Services

```hcl
connector "external_api" {
  type = "rest"

  circuit_breaker {
    threshold         = 5
    timeout           = "30s"
    success_threshold = 2
  }
}
```

### 5. Use Appropriate Message Persistence

```hcl
# Critical messages - persistent
to {
  connector.rabbit = {
    persistent = true  # Survives broker restart
  }
}

# Ephemeral messages - non-persistent
to {
  connector.rabbit = {
    persistent = false  # Faster, but lost on restart
  }
}
```
