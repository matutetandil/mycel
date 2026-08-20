# Quick Start

Build and run a REST API backed by a database in 10 minutes.

## Prerequisites

- Docker (recommended) or Go 1.21+
- A terminal

## Step 1: Create Your Service

```bash
mycel init my-first-service
cd my-first-service
```

That scaffolds a service that already runs, in the layout that stays
maintainable as it grows — one declaration per file, grouped by kind. See
[Project Structure](project-structure.md).

To follow along by hand instead, create the directory and these files
yourself:

```bash
mkdir my-first-service
cd my-first-service
```

!!! info "Why three files, and why `.mycel`"

    Mycel reads **every `.mycel` file** under the config directory, recursively,
    and merges them into one service. The extension is the only thing that
    matters — file and directory names are for your benefit, not the runtime's.
    These three could equally be one file.

    [Project Structure](project-structure.md) covers how to lay this out as a
    service grows.

### `config.mycel` — service identity

```hcl
service {
  name    = "my-first-api"
  version = "1.0.0"
}
```

### `connectors.mycel` — data sources

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

### `flows.mycel` — data flows

```hcl
flow "list_items" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    target    = "items"
  }
}

flow "create_item" {
  from {
    connector = "api"
    operation = "POST /items"
  }
  to {
    connector = "db"
    target    = "items"
  }
}

flow "get_item" {
  from {
    connector = "api"
    operation = "GET /items/:id"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
```

All three name the same `target` — the table. What each does with it comes from
the request: `GET` reads, `POST` writes, and the `:id` in a path becomes the
value the read filters on. You write the SQL yourself only when you want
something the shape of the request does not say; `query` is the attribute for
that.

### `migrations/001_create_items.sql` — the table

```sql
CREATE TABLE IF NOT EXISTS items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TEXT
);
```

Mycel does not invent tables: a flow writing to `items` needs `items` to exist.
Every `.sql` under `migrations/` is applied, in name order, by `mycel migrate`.

## Step 2: Run Your Service

### With Docker

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

### From source (requires Go 1.21+)

```bash
go install github.com/matutetandil/mycel/v2/cmd/mycel@latest

mycel migrate
mycel start
```

Run both from the service directory: a relative `database` path is relative to
where the process starts, so migrating from one directory and starting from
another creates the table beside the wrong file.

You should see:

```
    ███╗   ███╗██╗   ██╗ ██████╗███████╗██╗
    ████╗ ████║╚██╗ ██╔╝██╔════╝██╔════╝██║
    ██╔████╔██║ ╚████╔╝ ██║     █████╗  ██║
    ██║╚██╔╝██║  ╚██╔╝  ██║     ██╔══╝  ██║
    ██║ ╚═╝ ██║   ██║   ╚██████╗███████╗███████╗
    ╚═╝     ╚═╝   ╚═╝    ╚═════╝╚══════╝╚══════╝
    Declarative Microservice Runtime v2.18.0

    Service: my-first-api v1.0.0
    Environment: development
    Port: 3000

    Connectors:
    ✓ api (rest) listening on :3000
    ✓ db (database) → ./data.db

    Flows:
    ✓ list_items: GET /items → items
    ✓ create_item: POST /items → items
    ✓ get_item: GET /items/:id → items

    ✓ Ready! Press Ctrl+C to stop.
```

## Step 3: Test Your API

Open a new terminal:

```bash
# Create an item
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"name": "My first item", "description": "Created with Mycel!"}'
```

Response — what a write answers with is what it did, not the row back:

```json
{"affected":1,"id":1}
```

```bash
# List all items
curl http://localhost:3000/items
```

Response:
```json
[{"id":1,"name":"My first item","description":"Created with Mycel!","created_at":null}]
```

```bash
# Get a single item
curl http://localhost:3000/items/1
```

A read answers with rows, so this one comes back as a list of one:

```json
[{"id":1,"name":"My first item","description":"Created with Mycel!","created_at":null}]
```

You just created a REST API with a database backend without writing any code.

## Step 4: Add Data Transformation

Stamp every item with the time it arrived. Update `flows.mycel`:

```hcl
flow "create_item" {
  from {
    connector = "api"
    operation = "POST /items"
  }

  transform {
    name        = "input.name"
    description = "input.description"
    created_at  = "now()"
  }

  to {
    connector = "db"
    target    = "items"
  }
}
```

A transform decides what is written: only the fields it names reach the table,
so a request may carry anything and the row is what you said it is.

To assign the key yourself rather than letting the database count, add
`id = "uuid()"`. The column then has to be `TEXT PRIMARY KEY` instead of an
autoincrementing integer, which is one more migration — and the write answers
with whichever id ended up on the row.

Two variables appear here that nothing declared. `input` is the data that arrived — for a REST source, the request body, path and query parameters, all flat. The field name on the left of each line is what gets written out. See [Input and Output](../core-concepts/input-and-output.md) for the full picture.

Test it:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"name": "Stamped", "description": "Has a created_at"}'
```

Response:
```json
{"affected":1,"id":3}
```

And the row now carries the timestamp:

```json
[{"id":3,"name":"Stamped","description":"Has a created_at","created_at":"2026-08-20T22:06:15Z"}]
```

## Step 5: Add Input Validation

Create `types.mycel`:

```hcl
type "item_input" {
  name = string({
    required   = true
    min_length = 1
    max_length = 100
  })
  description = string({
    required   = false
    max_length = 500
  })
}
```

Reference it in the flow:

```hcl
flow "create_item" {
  from {
    connector = "api"
    operation = "POST /items"
  }

  validate {
    input = "item_input"
  }

  transform {
    name        = "input.name"
    description = "default(input.description, '')"
    created_at  = "now()"
  }

  to {
    connector = "db"
    target    = "items"
  }
}
```

Invalid requests are now rejected:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{}'
```

Answered `400 Bad Request`:

```json
{"error":"validation error on 'name': field is required"}
```

`description` is optional, so a request without one is accepted —
`default(input.description, '')` is what supplies the value the column gets.

## What's Next

### Use a real database

```hcl
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = env("DB_PORT", "5432")
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
- [Examples](https://github.com/matutetandil/mycel/tree/main/examples)
