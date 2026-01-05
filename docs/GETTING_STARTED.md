# Getting Started with Mycel

This guide will take you from zero to a running microservice in 10 minutes.

## What is Mycel?

Mycel is a **declarative microservice framework**. Instead of writing code, you write configuration files (HCL) that describe:
- **What** data sources to connect to (databases, APIs, queues)
- **How** data flows between them
- **What** transformations to apply

Think of it like nginx for microservices: same binary, different configuration = different service.

## Prerequisites

- Docker (recommended) or Go 1.21+
- A terminal
- 10 minutes

## Step 1: Create Your First Service

Create a new directory for your service:

```bash
mkdir my-first-service
cd my-first-service
```

Create three files:

### `service.hcl` - Service configuration

```hcl
service {
  name = "my-first-api"
  port = 3000
}
```

### `connectors.hcl` - Data sources

```hcl
# REST API endpoint
connector "api" {
  type = "rest"
  port = 3000
}

# SQLite database (file-based, no setup needed)
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data.db"
}
```

### `flows.hcl` - Data flows

```hcl
# List all items
flow "list_items" {
  from {
    connector = "api"
    path      = "GET /items"
  }
  to {
    connector = "db"
    table     = "items"
  }
}

# Create an item
flow "create_item" {
  from {
    connector = "api"
    path      = "POST /items"
  }
  to {
    connector = "db"
    table     = "items"
    operation = "insert"
  }
}

# Get single item
flow "get_item" {
  from {
    connector = "api"
    path      = "GET /items/:id"
  }
  to {
    connector = "db"
    table     = "items"
  }
}
```

## Step 2: Run Your Service

### With Docker (recommended)

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

### From source

```bash
# If you have Go installed
go install github.com/matutetandil/mycel/cmd/mycel@latest
mycel start
```

You should see:

```
  __  __                  _
 |  \/  |_   _  ___ ___  | |
 | |\/| | | | |/ __/ _ \ | |
 | |  | | |_| | (_|  __/ | |
 |_|  |_|\__, |\___\___| |_|
         |___/

 Declarative Microservice Framework

INFO  Starting service: my-first-api
INFO  Loaded 2 connectors
INFO  Registered 3 flows
INFO  REST server listening on :3000
```

## Step 3: Test Your API

Open a new terminal and test:

### Create an item

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"name": "My first item", "description": "Created with Mycel!"}'
```

Expected response:
```json
{"id":1,"name":"My first item","description":"Created with Mycel!"}
```

### List all items

```bash
curl http://localhost:3000/items
```

Expected response:
```json
[{"id":1,"name":"My first item","description":"Created with Mycel!"}]
```

### Get a single item

```bash
curl http://localhost:3000/items/1
```

Expected response:
```json
{"id":1,"name":"My first item","description":"Created with Mycel!"}
```

**Congratulations!** You just created a REST API with a database backend without writing any code.

## Step 4: Add Data Transformation

Let's add automatic timestamps and UUID generation. Update your `flows.hcl`:

```hcl
flow "create_item" {
  from {
    connector = "api"
    path      = "POST /items"
  }
  to {
    connector = "db"
    table     = "items"
    operation = "insert"
  }

  # Transform data before saving
  transform {
    id          = "uuid()"
    name        = "input.name"
    description = "input.description"
    created_at  = "now()"
  }
}
```

Save the file. If hot-reload is enabled, changes apply automatically. Otherwise, restart the service.

Test creating a new item:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"name": "Auto-generated ID", "description": "Has UUID and timestamp"}'
```

Response now includes auto-generated fields:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Auto-generated ID",
  "description": "Has UUID and timestamp",
  "created_at": "2024-01-15T10:30:00Z"
}
```

## Step 5: Add Input Validation

Create a `types.hcl` file to validate incoming data:

```hcl
type "item_input" {
  name        = string { required = true, min_length = 1, max_length = 100 }
  description = string { required = false, max_length = 500 }
}
```

Update your flow to use validation:

```hcl
flow "create_item" {
  from {
    connector = "api"
    path      = "POST /items"
  }

  input_type = "item_input"  # Validate input

  to {
    connector = "db"
    table     = "items"
    operation = "insert"
  }

  transform {
    id          = "uuid()"
    name        = "input.name"
    description = "input.description ?? ''"
    created_at  = "now()"
  }
}
```

Now invalid requests are rejected:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{}'
```

Response:
```json
{
  "error": "validation failed",
  "details": {
    "name": "required field missing"
  }
}
```

## What's Next?

You've learned the basics. Here's where to go next:

### Connect to Real Databases

```hcl
# PostgreSQL
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = 5432
  name     = "myapp"
  user     = env("DB_USER", "postgres")
  password = env("DB_PASSWORD", "")
}
```

See: [Configuration Reference](CONFIGURATION.md#databases)

### Add Message Queues

```hcl
# RabbitMQ
connector "queue" {
  type     = "rabbitmq"
  host     = "localhost"
  port     = 5672
  user     = "guest"
  password = "guest"
}

flow "process_orders" {
  from {
    connector = "queue"
    queue     = "orders"
  }
  to {
    connector = "db"
    table     = "orders"
  }
}
```

See: [examples/mq](../examples/mq)

### Expose GraphQL API

```hcl
connector "graphql" {
  type = "graphql"
  port = 4000

  schema = <<-GRAPHQL
    type Query {
      items: [Item!]!
      item(id: ID!): Item
    }

    type Item {
      id: ID!
      name: String!
    }
  GRAPHQL
}
```

See: [examples/graphql](../examples/graphql)

### Add Caching

```hcl
connector "cache" {
  type   = "cache"
  driver = "redis"
  host   = "localhost"
  port   = 6379
}

flow "get_item" {
  cache {
    storage = "cache"
    ttl     = "5m"
    key     = "'item:' + input.id"
  }
  # ... rest of flow
}
```

See: [examples/cache](../examples/cache)

### Deploy to Production

```bash
# Docker
docker run -v ./config:/etc/mycel \
  -e MYCEL_ENV=production \
  -e MYCEL_LOG_FORMAT=json \
  ghcr.io/matutetandil/mycel

# Kubernetes
helm install my-api oci://ghcr.io/matutetandil/charts/mycel
```

See: [Helm Chart](../helm/mycel/README.md)

## Core Concepts Summary

| Concept | What it does | Example |
|---------|--------------|---------|
| **Connector** | Connects to external systems | Database, API, Queue, Cache |
| **Flow** | Defines how data moves | `from` → `transform` → `to` |
| **Transform** | Modifies data in-flight | Add fields, rename, filter |
| **Type** | Validates data structure | Required fields, formats |

## File Structure

A typical Mycel service:

```
my-service/
├── service.hcl      # Service name, port, global settings
├── connectors.hcl   # Database, API, queue connections
├── flows.hcl        # Data flow definitions
├── types.hcl        # Input/output validation
├── transforms.hcl   # Reusable transformations (optional)
└── environments/    # Environment-specific config (optional)
    ├── dev.hcl
    ├── staging.hcl
    └── prod.hcl
```

## Getting Help

- **Documentation**: [docs/CONFIGURATION.md](CONFIGURATION.md)
- **Examples**: [examples/](../examples)
- **Troubleshooting**: [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md)
- **GitHub Issues**: [github.com/matutetandil/mycel/issues](https://github.com/matutetandil/mycel/issues)
