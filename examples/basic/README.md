# Basic Example

A simple REST API with SQLite database demonstrating core Mycel concepts.

## What This Example Does

- Exposes a REST API on port 3000
- Stores data in a local SQLite database
- Implements CRUD operations for users
- Shows input validation
- Demonstrates CORS configuration

## Quick Start

The database file is created where the service is started from, so run these
from this directory and everything lands where the file structure below says it
does.

```bash
cd examples/basic

# Create the users table. Nothing creates it for you, and every command
# below fails without it.
mycel migrate --config .

mycel start --config .
```

With Docker, mount the example and give the database somewhere that outlives
the container:

```bash
docker run -v $(pwd):/etc/mycel -v $(pwd)/data:/data -p 3000:3000 \
  ghcr.io/matutetandil/mycel
```

## Verify It Works

### 1. Check service is running

```bash
curl http://localhost:3000/health
```

Expected response — abbreviated here; the service also reports its version,
uptime and the health of each connector:
```json
{"status":"healthy","components":[{"name":"api","status":"healthy"},{"name":"sqlite","status":"healthy"}]}
```

### 2. Create a user

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email": "john@example.com", "name": "John Doe"}'
```

Expected response — a write answers with what it did, not with the row:
```json
{"affected":1,"id":1}
```

### 3. List all users

```bash
curl http://localhost:3000/users
```

Expected response — `created_at` is filled in by the database:
```json
[{"created_at":"2026-08-19T18:30:11Z","email":"john@example.com","id":1,"name":"John Doe"}]
```

### 4. Get a single user

```bash
curl http://localhost:3000/users/1
```

Expected response — a read answers with the rows it found, so a single user
arrives as a list of one:
```json
[{"created_at":"2026-08-19T18:30:11Z","email":"john@example.com","id":1,"name":"John Doe"}]
```

### 5. Test validation (should fail)

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Missing Email"}'
```

Expected response (validation error):
```json
{"error":"validation error on 'email': field is required"}
```

`id` and `age` are declared optional in `types/user.mycel`, which is why the
request above does not have to supply them — every field of a type is required
unless it says otherwise.

### 6. Delete a user

```bash
curl -X DELETE http://localhost:3000/users/1
```

Expected response:
```json
{"affected":1}
```

## File Structure

```
basic/
├── config.mycel  # Service name and version
├── connectors/
│   ├── api.mycel     # REST API configuration
│   └── sqlite.mycel  # SQLite database connection
├── flows/
│   └── users.mycel  # User CRUD operations
├── types/
│   └── user.mycel  # User input validation schema
├── data/
│   └── app.db  # SQLite database file (created by the migration)
└── migrations/
    └── 001_create_users.sql  # The users table
```

## Configuration Explained

### Service (`config.mycel`)

```hcl
service {
  name    = "users-service"
  version = "1.0.0"
}
```

### REST API (`connectors/api.mycel`)

```hcl
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]           # Allow all origins
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}
```

### Database (`connectors/sqlite.mycel`)

```hcl
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"  # File path for SQLite
}
```

### Flows (`flows/users.mycel`)

```hcl
# GET /users - List all users
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# POST /users - Create user with validation
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  validate {
    input = "type.user"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}
```

## What You Should See in Logs

When the service starts:
```
INFO  Starting service: users-service
INFO  Loaded 2 connectors: api, sqlite
INFO  Registered 4 flows: get_users, get_user, create_user, delete_user
INFO  REST server listening on :3000
```

When you create a user:
```
INFO  POST /users → create_user → sqlite:users
```

## Common Issues

### "Database file not found"

The SQLite database is created automatically. If you see errors, ensure the `data/` directory exists:

```bash
mkdir -p examples/basic/data
```

### "Port 3000 already in use"

Another service is using port 3000. Either stop it or change the port in `connectors/api.mycel`:

```hcl
connector "api" {
  type = "rest"
  port = 3001  # Changed port
}
```

## Next Steps

- Add transforms to auto-generate timestamps: See [examples/enrich](../enrich)
- Add caching: See [examples/cache](../cache)
- Switch to PostgreSQL: See [docs/CONFIGURATION.md](../../docs/CONFIGURATION.md#postgresql)
- Add GraphQL API: See [examples/graphql](../graphql)
