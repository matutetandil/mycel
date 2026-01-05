# Mock System Example

This example demonstrates how to use Mycel's mock system for testing without external dependencies.

## Directory Structure

```
mocks/
├── config.hcl           # Service config with mocks enabled
├── connectors.hcl       # REST API + SQLite connectors
├── flows.hcl            # User CRUD flows
├── mocks/
│   └── connectors/
│       └── db/
│           └── users.json   # Mock data for "users" table
└── README.md
```

## Mock File Format

Mock files are JSON with two formats:

### Simple (static data)

```json
{
  "data": [
    {"id": 1, "name": "John"},
    {"id": 2, "name": "Jane"}
  ]
}
```

### Conditional (dynamic based on input)

```json
{
  "responses": [
    {
      "when": "input.id == 1",
      "data": {"id": 1, "name": "John"}
    },
    {
      "default": true,
      "error": "Not found",
      "status": 404
    }
  ]
}
```

## Running

```bash
# With mocks from config.hcl
mycel start --config ./examples/mocks

# Override via CLI - mock specific connectors
mycel start --config ./examples/mocks --mock=db

# Disable mocks for specific connectors
mycel start --config ./examples/mocks --no-mock=api
```

## Testing

```bash
# GET all users (returns mock data)
curl http://localhost:3000/users

# GET specific user (uses CEL condition)
curl http://localhost:3000/users/1

# POST new user (mock returns success)
curl -X POST -d '{"email":"new@test.com","name":"New User"}' \
  http://localhost:3000/users
```

## Features

- **Conditional responses**: Use CEL expressions to match input
- **Latency simulation**: Add realistic delays per-connector
- **Error simulation**: Return errors for specific conditions
- **CLI overrides**: `--mock` and `--no-mock` flags
- **Per-connector config**: Different settings per connector

## Verify It Works

### 1. Start with mocks enabled

```bash
mycel start --config ./examples/mocks
```

You should see:
```
INFO  Starting service: mocks-example
INFO  Loaded 2 connectors: api, db
INFO    db: MOCKED (from mocks/connectors/db/)
INFO  REST server listening on :3000
```

### 2. List users (from mock data)

```bash
curl http://localhost:3000/users
```

Expected response (mock data):
```json
[
  {"id": 1, "email": "john@example.com", "name": "John Doe"},
  {"id": 2, "email": "jane@example.com", "name": "Jane Doe"}
]
```

### 3. Get specific user (conditional mock)

```bash
curl http://localhost:3000/users/1
```

Expected response:
```json
{"id": 1, "email": "john@example.com", "name": "John Doe"}
```

```bash
curl http://localhost:3000/users/999
```

Expected response (mock error):
```json
{"error": "Not found", "status": 404}
```

### 4. Disable mock for db

```bash
mycel start --config ./examples/mocks --no-mock=db
```

Now log shows:
```
INFO    db: connected to SQLite (real database)
```

### What to check in logs

```
INFO  GET /users
INFO    Mock response for: db/users
INFO    Latency simulation: 50ms
INFO  Response sent in 52ms
```

### Common Issues

**"Mock file not found"**

Mock files must be in `mocks/connectors/<connector>/<target>.json`:
```
mocks/connectors/db/users.json
```

**"CEL condition not matching"**

Check your `when` expressions match the input structure:
```json
{"when": "input.id == 1"}  // input.id comes from path param
```
