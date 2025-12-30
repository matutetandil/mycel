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
- `REST + TCP enrichment → Database` - Aggregate data from multiple services

## Status

✅ **Phase 1 Complete** - Core runtime is functional!
✅ **Phase 2 Complete** - Extended connectors and features!
✅ **Phase 2.5 Complete** - TCP Server + Client!
✅ **Phase 3.1 Complete** - Message Queues (RabbitMQ + Kafka)!
✅ **Phase 3.2 Complete** - Exec Connector (local + SSH)!

### Connector Support

| Connector | Input (Server/Consumer) | Output (Client/Producer) |
|-----------|------------------------|-------------------------|
| REST      | ✅ Phase 1             | ✅ Phase 2              |
| SQLite    | ✅ Phase 1             | ✅ Phase 1              |
| PostgreSQL| ✅ Phase 2             | ✅ Phase 2              |
| TCP       | ✅ Phase 2.5           | ✅ Phase 2.5            |
| RabbitMQ  | ✅ Phase 3.1           | ✅ Phase 3.1            |
| Kafka     | ✅ Phase 3.1           | ✅ Phase 3.1            |
| Exec      | ✅ Phase 3.2           | ✅ Phase 3.2            |
| GraphQL   | 🔜 Phase 3             | 🔜 Phase 3              |
| gRPC      | 🔜 Phase 3             | 🔜 Phase 3              |
| Files     | 🔜 Phase 3             | 🔜 Phase 3              |
| Slack     | -                      | 🔜 Phase 6              |
| Discord   | -                      | 🔜 Phase 6              |
| Email     | -                      | 🔜 Phase 6              |
| Webhooks  | 🔜 Phase 6             | 🔜 Phase 6              |

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
- [x] **Data Enrichment** - Fetch data from other microservices during transformation (TCP, HTTP, Database)

**Phase 2.5 - TCP** ✅
- [x] TCP Server with length-prefixed framing
- [x] TCP Client with connection pooling
- [x] Configurable protocols (JSON, msgpack, raw, nestjs)
- [x] Request-Response and Fire-and-forget patterns
- [x] Message routing by type field
- [x] **NestJS TCP protocol compatibility** - Connect to existing NestJS microservices!

**Phase 3.1 - Message Queues** ✅
- [x] RabbitMQ connector (consumer + publisher)
- [x] AMQP topic pattern matching (`*` and `#` wildcards)
- [x] Queue/Exchange declaration and binding
- [x] Manual acknowledgment support
- [x] Concurrent consumers with prefetch
- [x] **Kafka connector** (consumer + producer)
- [x] Consumer groups with auto-commit
- [x] SASL authentication (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)
- [x] Compression support (gzip, snappy, lz4, zstd)

**Phase 3.2 - Exec Connector** ✅
- [x] Local shell command execution
- [x] SSH remote command execution
- [x] Multiple output formats (text, json, lines)
- [x] Multiple input formats (args, stdin, json)
- [x] Shell wrapper support (bash -c, etc.)
- [x] Environment variables and working directory
- [x] Timeout handling

**Phase 3 - Extended Protocols** (In Progress)
- [ ] GraphQL (server + client)
- [ ] gRPC (server + client)
- [ ] File connector (read/write)

**Phase 4 - Production Ready**
- [ ] Hot reload
- [ ] Metrics & observability
- [ ] Rate limiting
- [ ] Circuit breaker
- [ ] Authentication & authorization (auth/)
- [ ] Aspects / AOP (logging, caching, retry policies)

**Phase 5 - Advanced Features**
- [ ] Custom validators (WASM)
- [ ] Plugins system
- [ ] Mocks/Testing framework
- [ ] Documentation generation (OpenAPI, GraphQL schema)

**Phase 6 - Notifications & Integrations**
- [ ] Slack connector (send messages, post to channels)
- [ ] Discord connector (send messages, embeds)
- [ ] Email connector (SMTP, templates)
- [ ] Webhooks connector (incoming + outgoing)
- [ ] SMS connector (Twilio, etc.)
- [ ] Push notifications (Firebase, APNs)

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

# Enrich data from external services
flow "get_product_with_price" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  # Fetch price from external pricing microservice
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  # Combine input with enriched data
  transform {
    id       = "input.id"
    name     = "input.name"
    price    = "enriched.pricing.price"
    currency = "enriched.pricing.currency"
  }

  to {
    connector = "postgres"
    target    = "products"
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

- [Transformations Guide](docs/transformations.md) - Complete CEL transformation reference (includes data enrichment)
- [Data Enrichment Example](examples/enrich/) - Fetch data from external services
- [TCP Example](examples/tcp/README.md) - TCP connector usage guide
- [Message Queue Example](examples/mq/README.md) - RabbitMQ/Kafka integration guide
- [Exec Example](examples/exec/README.md) - External command execution (local + SSH)

## Requirements

- Go 1.21+
- Make

## License

MIT

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a pull request.
