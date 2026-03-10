# Quick Start

Build and run a REST API backed by a database in 10 minutes.

## Prerequisites

- Docker (recommended) or Go 1.21+
- A terminal

## Step 1: Create Your Service

Create a directory and three files:

```bash
mkdir my-first-service
cd my-first-service
```

### `config.hcl` — service identity

```hcl
service {
  name    = "my-first-api"
  version = "1.0.0"
}
```

### `connectors.hcl` — data sources

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data.db"
}
```

### `flows.hcl` — data flows

```hcl
flow "list_items" {
  from { connector = "api", operation = "GET /items" }
  to   { connector = "db", target = "items" }
}

flow "create_item" {
  from { connector = "api", operation = "POST /items" }
  to   { connector = "db", target = "INSERT items" }
}

flow "get_item" {
  from { connector = "api", operation = "GET /items/:id" }
  to   { connector = "db", target = "items WHERE id = :id" }
}
```

## Step 2: Run Your Service

### With Docker

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

### From source (requires Go 1.21+)

```bash
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

Open a new terminal:

```bash
# Create an item
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"name": "My first item", "description": "Created with Mycel!"}'
```

Response:
```json
{"id":1,"name":"My first item","description":"Created with Mycel!"}
```

```bash
# List all items
curl http://localhost:3000/items
```

Response:
```json
[{"id":1,"name":"My first item","description":"Created with Mycel!"}]
```

```bash
# Get a single item
curl http://localhost:3000/items/1
```

You just created a REST API with a database backend without writing any code.

## Step 4: Add Data Transformation

Add automatic UUIDs and timestamps. Update `flows.hcl`:

```hcl
flow "create_item" {
  from { connector = "api", operation = "POST /items" }

  transform {
    id          = "uuid()"
    name        = "input.name"
    description = "input.description"
    created_at  = "now()"
  }

  to { connector = "db", target = "INSERT items" }
}
```

Test it:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"name": "With UUID", "description": "Auto-generated ID"}'
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "With UUID",
  "description": "Auto-generated ID",
  "created_at": "2024-01-15T10:30:00Z"
}
```

## Step 5: Add Input Validation

Create `types.hcl`:

```hcl
type "item_input" {
  name        = string { required = true, min_length = 1, max_length = 100 }
  description = string { required = false, max_length = 500 }
}
```

Reference it in the flow:

```hcl
flow "create_item" {
  from { connector = "api", operation = "POST /items" }

  validate {
    input = "item_input"
  }

  transform {
    id          = "uuid()"
    name        = "input.name"
    description = "default(input.description, '')"
    created_at  = "now()"
  }

  to { connector = "db", target = "INSERT items" }
}
```

Invalid requests are now rejected:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{}'
```

Response:
```json
{
  "error": "validation failed",
  "details": {"name": "required field missing"}
}
```

## What's Next

### Use a real database

```hcl
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = 5432
  database = "myapp"
  user     = env("DB_USER", "postgres")
  password = env("DB_PASSWORD", "")
}
```

### Add environment variables

Create a `.env` file (never commit it):

```bash
DB_HOST=localhost
DB_USER=postgres
DB_PASSWORD=secret
MYCEL_LOG_LEVEL=debug
```

Mycel loads it automatically on startup.

### Deploy with Docker

```bash
docker run \
  -v ./config:/etc/mycel \
  -e MYCEL_ENV=production \
  -e MYCEL_LOG_FORMAT=json \
  -e DB_HOST=db.example.com \
  -e DB_PASSWORD=secret \
  ghcr.io/matutetandil/mycel
```

## Core Concepts Summary

| Concept | What it does |
|---------|--------------|
| **connector** | Connects to an external system (database, API, queue, cache) |
| **flow** | Defines how data moves from a source to a target |
| **transform** | Reshapes data with CEL expressions |
| **type** | Validates data structure with schema constraints |

## Reference

- [Core Concepts: Connectors](../core-concepts/connectors.md)
- [Core Concepts: Flows](../core-concepts/flows.md)
- [Core Concepts: Transforms](../core-concepts/transforms.md)
- [Core Concepts: Types](../core-concepts/types.md)
- [Deployment Guide](../deployment/docker.md)
- [Examples](../../examples/)
