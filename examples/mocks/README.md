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
