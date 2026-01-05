# Basic Example

A simple REST API with SQLite database demonstrating core Mycel concepts.

## What This Example Does

- Exposes a REST API on port 3000
- Stores data in a local SQLite database
- Implements CRUD operations for users
- Shows input validation
- Demonstrates CORS configuration

## Quick Start

```bash
# From the repository root
mycel start --config ./examples/basic

# Or with Docker
docker run -v $(pwd)/examples/basic:/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

## Verify It Works

### 1. Check service is running

```bash
curl http://localhost:3000/health
```

Expected response:
```json
{"status":"healthy"}
```

### 2. Create a user

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email": "john@example.com", "name": "John Doe"}'
```

Expected response:
```json
{"id":1,"email":"john@example.com","name":"John Doe"}
```

### 3. List all users

```bash
curl http://localhost:3000/users
```

Expected response:
```json
[{"id":1,"email":"john@example.com","name":"John Doe"}]
```

### 4. Get a single user

```bash
curl http://localhost:3000/users/1
```

Expected response:
```json
{"id":1,"email":"john@example.com","name":"John Doe"}
```

### 5. Test validation (should fail)

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Missing Email"}'
```

Expected response (validation error):
```json
{"error":"validation failed","details":{"email":"required field"}}
```

## File Structure

```
basic/
├── config.hcl              # Service name and version
├── connectors/
│   ├── api.hcl             # REST API configuration
│   └── database.hcl        # SQLite database connection
├── flows/
│   └── users.hcl           # User CRUD operations
├── types/
│   └── user.hcl            # User input validation schema
├── data/
│   └── app.db              # SQLite database file (created automatically)
└── setup.sql               # Initial database schema
```

## Configuration Explained

### Service (`config.hcl`)

```hcl
service {
  name    = "users-service"
  version = "1.0.0"
}
```

### REST API (`connectors/api.hcl`)

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

### Database (`connectors/database.hcl`)

```hcl
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"  # File path for SQLite
}
```

### Flows (`flows/users.hcl`)

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

Another service is using port 3000. Either stop it or change the port in `connectors/api.hcl`:

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
