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

| Feature | Status | Example |
|---------|--------|---------|
| REST API | ✅ | [examples/basic](examples/basic) |
| SQLite / PostgreSQL / MySQL | ✅ | [examples/basic](examples/basic) |
| MongoDB | ✅ | [examples/mongodb](examples/mongodb) |
| GraphQL Server & Client | ✅ | [examples/graphql](examples/graphql) |
| gRPC Server & Client | ✅ | [examples/grpc](examples/grpc) |
| RabbitMQ / Kafka | ✅ | [examples/mq](examples/mq) |
| TCP Server & Client | ✅ | [examples/tcp](examples/tcp) |
| Files (local) / S3 | ✅ | [examples/files](examples/files) |
| Cache (Memory / Redis) | ✅ | [examples/cache](examples/cache) |
| Multi-step Flow Orchestration | ✅ | [examples/steps](examples/steps) |
| Auth (JWT, MFA, WebAuthn) | ✅ | - |
| Rate Limiting / Circuit Breaker | ✅ | [examples/rate-limit](examples/rate-limit) |
| Hot Reload | ✅ | - |
| Health Checks / Prometheus | ✅ | `/health`, `/metrics` |
| Notifications (Email, Slack, SMS) | ✅ | - |
| GraphQL Query Optimization | ✅ | [examples/graphql-optimization](examples/graphql-optimization) |

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

## Examples

| Example | Description |
|---------|-------------|
| [basic](examples/basic) | REST API + SQLite |
| [graphql](examples/graphql) | GraphQL server with schema |
| [graphql-optimization](examples/graphql-optimization) | Field selection, step skipping, DataLoader |
| [grpc](examples/grpc) | gRPC server and client |
| [mq](examples/mq) | RabbitMQ and Kafka |
| [tcp](examples/tcp) | TCP server with protocols |
| [cache](examples/cache) | Memory and Redis caching |
| [files](examples/files) | Local file operations |
| [s3](examples/s3) | AWS S3 / MinIO |
| [rate-limit](examples/rate-limit) | Rate limiting configuration |
| [enrich](examples/enrich) | Data enrichment from services |
| [mongodb](examples/mongodb) | MongoDB NoSQL operations |
| [exec](examples/exec) | Execute shell commands |
| [steps](examples/steps) | Multi-step flow orchestration |
| [named-operations](examples/named-operations) | Reusable named operations |

## Documentation

- **[Concepts](docs/CONCEPTS.md)** - What is a connector, flow, transform, and more
- **[Configuration Reference](docs/CONFIGURATION.md)** - Complete HCL syntax reference
- **[Integration Patterns](docs/integration-patterns.md)** - Common use cases
- **[Transformations](docs/transformations.md)** - CEL transformation guide
- **[Roadmap](docs/ROADMAP.md)** - Project status and future plans

## Support

If you find this project useful, consider supporting its development:

<a href="https://buymeacoffee.com/matutetandil" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" width="200"></a>

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a pull request.

## License

MIT
