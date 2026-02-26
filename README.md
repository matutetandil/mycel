# Mycel

**Declarative microservices through configuration, not code.**

Define HCL files. Run Mycel. Get a production-ready microservice.

## Quick Start

**`connectors.hcl`** - Define your data sources:
```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"
}
```

**`flows.hcl`** - Define how data moves:
```hcl
flow "list_users" {
  from { connector = "api", operation = "GET /users" }
  to   { connector = "db", target = "users" }
}

flow "create_user" {
  from { connector = "api", operation = "POST /users" }
  transform {
    id         = "uuid()"
    email      = "lower(input.email)"
    created_at = "now()"
  }
  to { connector = "db", target = "users" }
}
```

**Run it:**
```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

**Test it:**
```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","name":"Test"}'

curl http://localhost:3000/users
```

That's it. REST API + database, zero code.

> See [Getting Started Guide](docs/GETTING_STARTED.md) for a complete tutorial, or explore the [full documentation](#documentation).

## Purpose

- **What:** An open-source runtime that reads HCL configuration and exposes microservices. Same binary, different config = different service.
- **Why:** 70-80% of microservice code is plumbing (routing, DB queries, transformations, integrations). Mycel replaces that with configuration.
- **Who:** Backend teams building CRUD APIs, event processors, integrations, or protocol bridges.

## Features

| Feature | Description |
|---------|-------------|
| [REST API](examples/basic) | Expose and consume REST endpoints |
| [SQLite / PostgreSQL / MySQL](examples/basic) | Relational database connectors |
| [MongoDB](examples/mongodb) | NoSQL document database |
| [GraphQL Server & Client](examples/graphql) | Schema-based GraphQL API |
| [GraphQL Query Optimization](examples/graphql-optimization) | Field selection, step skipping, DataLoader |
| [gRPC Server & Client](examples/grpc) | Protocol Buffers based RPC |
| [gRPC Load Balancing](examples/grpc-loadbalancing) | Round-robin and weighted balancing |
| [RabbitMQ / Kafka](examples/mq) | Message queue producers and consumers |
| [TCP Server & Client](examples/tcp) | JSON, msgpack, and NestJS protocols |
| [Files / S3](examples/files) | Local filesystem and AWS S3 / MinIO |
| [Cache (Memory / Redis)](examples/cache) | In-memory and Redis caching |
| [Multi-step Flow Orchestration](examples/steps) | Sequential and conditional step execution |
| [Named Operations](examples/named-operations) | Reusable parameterized operations |
| [Data Enrichment](examples/enrich) | Combine data from multiple sources |
| [Auth (JWT, MFA, WebAuthn)](examples/auth) | Authentication with presets and MFA |
| [Rate Limiting / Circuit Breaker](examples/rate-limit) | Traffic control and fault tolerance |
| [Connector Profiles](examples/profiles) | Multiple backends with fallback |
| [Read Replicas](examples/read-replicas) | Route reads to replica databases |
| [Synchronization](examples/sync) | Distributed locks, semaphores, coordination |
| [Notifications](examples/notifications) | Email, Slack, Discord, SMS, Push, Webhook |
| [Aspects (AOP)](examples/aspects) | Cross-cutting concerns via pattern matching |
| [Validators](examples/validators) | Regex, CEL, and custom validation rules |
| [WASM](examples/wasm-functions) | Custom functions and validators via WebAssembly |
| [Mocks](examples/mocks) | Mock data for development and testing |
| [Plugins](examples/plugin) | Extend Mycel with WASM plugins |
| [Exec](examples/exec) | Execute shell commands from flows |
| Hot Reload | Apply HCL changes without restart |
| Health Checks / Prometheus | `/health`, `/metrics` endpoints |

## CLI

```bash
mycel start [--config=<path>] [--env=<env>] [--log-level=<level>] [--log-format=<format>] [--hot-reload]
mycel validate [--config=<path>]
mycel check [--config=<path>]
mycel version
```

Environment: `MYCEL_ENV` (default: development), `MYCEL_LOG_LEVEL` (default: info), `MYCEL_LOG_FORMAT` (default: text). Flags take precedence.

## Installation

**Docker (recommended):**
```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

**Go:**
```bash
go install github.com/matutetandil/mycel/cmd/mycel@latest
```

**Kubernetes (Helm):**
```bash
helm install my-api oci://ghcr.io/matutetandil/charts/mycel
```

See [helm/mycel/README.md](helm/mycel/README.md) for full Helm documentation including values, autoscaling, and ingress configuration.

**Requirements:** Docker (recommended) or Go 1.24+ (for building from source).

## Documentation

- **[Concepts](docs/CONCEPTS.md)** - What is a connector, flow, transform, and more
- **[Configuration Reference](docs/CONFIGURATION.md)** - Complete HCL syntax reference
- **[Integration Patterns](docs/integration-patterns.md)** - Common use cases
- **[Transformations](docs/transformations.md)** - CEL transformation guide
- **[Roadmap](docs/ROADMAP.md)** - Project status and future plans

## More Examples

Variants and integration patterns beyond the individual features above:

| Example | Description |
|---------|-------------|
| [s3](examples/s3) | AWS S3 / MinIO (variant of Files) |
| [redis-cluster](examples/redis-cluster) | Redis cluster setup (variant of Cache) |
| [dynamic-api-key](examples/dynamic-api-key) | Dynamic API key auth (variant of Auth) |
| [wasm-validator](examples/wasm-validator) | WASM validators (variant of WASM) |
| [integration](examples/integration) | Multi-connector integration patterns |

## Support

If you find this project useful, consider supporting its development:

<a href="https://buymeacoffee.com/matutetandil" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" width="200"></a>

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a pull request.

## License

MIT
