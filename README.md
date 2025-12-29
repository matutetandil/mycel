# Mycel

**Declarative Microservice Framework**

Mycel is an open-source framework for creating microservices through HCL configuration, without writing code. It works as a single runtime (like nginx or Docker) that interprets configuration files and exposes services.

> **Philosophy:** Configuration, not code. You define WHAT you want, Mycel handles HOW.

## The Vision

Instead of programming each microservice in NestJS, Go, Python, etc., you:

1. Create HCL configuration files
2. Deploy Mycel with that configuration
3. Done - you have a microservice

```
Production Environment:

┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│     mycel       │  │     mycel       │  │     mycel       │
│  + customers/   │  │  + products/    │  │  + orders/      │
│    *.hcl        │  │    *.hcl        │  │    *.hcl        │
├─────────────────┤  ├─────────────────┤  ├─────────────────┤
│ customers-svc   │  │ products-svc    │  │ orders-svc      │
│ :3001           │  │ :3002           │  │ :3003           │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

Same binary, different configuration = different microservice.

## What Can You Connect?

Mycel connects **anything to anything**:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   SOURCE    │     │    MYCEL    │     │    TARGET   │
├─────────────┤     │             │     ├─────────────┤
│ REST API    │────▶│  validate   │────▶│ Database    │
│ Database    │     │  transform  │     │ REST API    │
│ Queue       │     │  route      │     │ Queue       │
│ TCP         │     │             │     │ TCP         │
│ GraphQL     │     │             │     │ GraphQL     │
│ Files       │     │             │     │ Files       │
│ gRPC        │     │             │     │ gRPC        │
└─────────────┘     └─────────────┘     └─────────────┘
```

**Example Use Cases:**
- `REST API → Database` - Classic CRUD microservice
- `Queue → Database` - Process messages and persist
- `REST → Queue` - Receive requests and enqueue for processing
- `Database → REST` - Sync data between systems
- `Queue → Queue` - Transform and route messages
- `File → Database` - Import legacy data
- `TCP → REST` - Protocol bridge

## Status

✅ **Phase 1 Complete** - Core runtime is functional!
✅ **Phase 2 Complete** - Extended connectors and features!

### Connector Support

| Connector | Input (Server/Consumer) | Output (Client/Producer) |
|-----------|------------------------|-------------------------|
| REST      | ✅ Phase 1             | ✅ Phase 2              |
| SQLite    | ✅ Phase 1             | ✅ Phase 1              |
| PostgreSQL| ✅ Phase 2             | ✅ Phase 2              |
| TCP       | 🔜 Phase 2.5           | 🔜 Phase 2.5            |
| GraphQL   | 🔜 Phase 3             | 🔜 Phase 3              |
| Queues    | 🔜 Phase 3             | 🔜 Phase 3              |
| gRPC      | 🔜 Phase 3             | 🔜 Phase 3              |
| Files     | 🔜 Phase 3             | 🔜 Phase 3              |

### Roadmap

**Phase 1 - Core Runtime** ✅
- [x] Project structure & CLI
- [x] HCL parser
- [x] REST connector (server)
- [x] SQLite connector
- [x] Flow executor
- [x] Validation system
- [x] Transform system
- [x] Runtime orchestration

**Phase 2 - Core Connectors** ✅
- [x] REST Client (call external APIs with OAuth2, API Key, Bearer)
- [x] PostgreSQL connector
- [x] Transforms (inline + reusable named transforms)
- [x] Type validation on flows (input/output validation)
- [x] Environment variables support (env(), file(), base64decode(), etc.)

**Phase 2.5 - TCP**
- [ ] TCP Server
- [ ] TCP Client
- [ ] Configurable protocols (JSON, protobuf, msgpack, raw)

**Phase 3 - Extended Protocols**
- [ ] GraphQL (server + client)
- [ ] gRPC (server + client)
- [ ] Message Queues (RabbitMQ, Kafka, SQS)
- [ ] File connector (read/write)

**Phase 4 - Production Ready**
- [ ] Hot reload
- [ ] Metrics & observability
- [ ] Rate limiting
- [ ] Circuit breaker
- [ ] Authentication & authorization (auth/)
- [ ] Aspects / AOP (logging, caching, retry policies)

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

  # CEL-powered transforms (Google Common Expression Language)
  # See docs/transformations.md for full documentation
  transform {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    created_at = "now()"
    is_active  = "true"
    status     = "input.age >= 18 ? 'active' : 'pending'"
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

## Directory Structure

```
my-service/
├── connectors/           # Database, API, queue connections
├── flows/                # Data flows (from → to)
├── types/                # Data schemas
├── transforms/           # Reusable transformations
├── validators/           # Custom validators
├── aspects/              # Cross-cutting concerns (logging, caching, etc.)
├── auth/                 # Authentication & authorization config
├── environments/         # Environment-specific variables
└── config.hcl            # Service configuration (name, version)
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

## Documentation

- [Transformations Guide](docs/transformations.md) - Complete CEL transformation reference

## Requirements

- Go 1.21+
- Make

## License

MIT

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a pull request.
