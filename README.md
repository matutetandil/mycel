# Mycel

**Declarative Microservice Framework**

Mycel is an open-source framework for creating declarative microservices through HCL configuration, without writing code. It works as a single runtime (similar to nginx or Apache) that interprets configuration files and exposes services.

> **Philosophy:** Configuration, not code. You define WHAT you want, Mycel handles HOW.

## Status

✅ **Phase 1 Complete** - Core runtime is functional!

### Phase 1 Progress

- [x] Project structure
- [x] CLI scaffolding (start, validate, check)
- [x] Error handling
- [x] Core interfaces (connector, flow, validate, transform)
- [x] HCL parser
- [x] SQLite connector
- [x] REST connector & HTTP server
- [x] Validation system
- [x] Transform system
- [x] Flow executor
- [x] Runtime orchestration

### Coming Next (Phase 2)

- [ ] PostgreSQL connector
- [ ] Transforms (inline + reusable)
- [ ] Type validation on flows
- [ ] Environments support
- [ ] Hot reload
- [ ] Metrics & health checks

## Quick Start

```bash
# Clone the repository
git clone https://github.com/mycel-labs/mycel.git
cd mycel

# Build
make build

# Setup example database
mkdir -p data
sqlite3 data/app.db < examples/basic/setup.sql

# Run the example service
./bin/mycel start --config ./examples/basic

# Test the API (in another terminal)
curl http://localhost:3000/users
curl -X POST -H "Content-Type: application/json" \
     -d '{"email":"test@example.com","name":"Test User"}' \
     http://localhost:3000/users
```

## How It Works

Define your microservice in HCL files:

```hcl
# connectors/database.hcl
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  port     = 5432
  database = "myapp"
}

connector "api" {
  type = "rest"
  port = 3000
}
```

```hcl
# flows/users.hcl
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "postgres"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  validate {
    input = "type.user"
  }

  transform {
    output.id         = uuid()
    output.email      = lower(input.email)
    output.created_at = now()
  }

  to {
    connector = "postgres"
    target    = "users"
  }
}
```

```hcl
# types/user.hcl
type "user" {
  id    = number
  email = string { format = "email" }
  name  = string { min_length = 1, max_length = 100 }
}
```

Then run:

```bash
mycel start --config ./my-service
```

Mycel generates a standard microservice that speaks standard protocols (REST, GraphQL, gRPC). A microservice built with Mycel is indistinguishable from one built in NestJS, Go, or any other language.

## Directory Structure

```
my-service/
├── connectors/           # Database, API, queue connections
├── flows/                # Data flows (from → to)
├── types/                # Data schemas
├── transforms/           # Reusable transformations
├── validators/           # Custom validators
├── aspects/              # Cross-cutting concerns (AOP)
├── auth/                 # Authentication config
├── environments/         # Environment-specific variables
└── config.hcl            # Global configuration
```

## CLI

```bash
# Start the runtime
mycel start [--config=<path>] [--env=<environment>]

# Validate configuration
mycel validate [--config=<path>]

# Check connector connectivity
mycel check [--config=<path>]

# Show version
mycel version
```

## Architecture

Mycel follows SOLID principles:

- **S**ingle Responsibility: Each component has one job
- **O**pen/Closed: Extensible via plugins without modifying core
- **L**iskov Substitution: All connectors implement the same interface
- **I**nterface Segregation: Small, focused interfaces (Reader, Writer)
- **D**ependency Inversion: Core depends on abstractions, not implementations

```
┌─────────────────────────────────────────┐
│           mycel (runtime)               │
│  ┌─────────────────────────────────┐    │
│  │  Configuration Loader           │    │
│  │  • connectors/*.hcl             │    │
│  │  • flows/*.hcl                  │    │
│  │  • types/*.hcl                  │    │
│  └─────────────────────────────────┘    │
│                 ↓                       │
│  ┌─────────────────────────────────┐    │
│  │  Flow Executor (Pipeline)       │    │
│  │  validate → transform → execute │    │
│  └─────────────────────────────────┘    │
│                 ↓                       │
│         Microservice Running            │
└─────────────────────────────────────────┘
```

## Development

```bash
# Install dependencies
make deps

# Build
make build

# Run tests
make test

# Format code
make fmt

# Run linter
make lint
```

## Requirements

- Go 1.21+
- Make

## License

MIT

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a pull request.
