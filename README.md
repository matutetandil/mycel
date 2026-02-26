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
| GraphQL Query Optimization | ✅ | [examples/graphql-optimization](examples/graphql-optimization) |
| gRPC Server & Client | ✅ | [examples/grpc](examples/grpc) |
| gRPC Load Balancing | ✅ | [examples/grpc-loadbalancing](examples/grpc-loadbalancing) |
| RabbitMQ / Kafka | ✅ | [examples/mq](examples/mq) |
| TCP Server & Client | ✅ | [examples/tcp](examples/tcp) |
| Files (local) / S3 | ✅ | [examples/files](examples/files) |
| Cache (Memory / Redis) | ✅ | [examples/cache](examples/cache) |
| Multi-step Flow Orchestration | ✅ | [examples/steps](examples/steps) |
| Named Operations | ✅ | [examples/named-operations](examples/named-operations) |
| Data Enrichment | ✅ | [examples/enrich](examples/enrich) |
| Auth (JWT, MFA, WebAuthn) | ✅ | [examples/auth](examples/auth) |
| Rate Limiting / Circuit Breaker | ✅ | [examples/rate-limit](examples/rate-limit) |
| Connector Profiles | ✅ | [examples/profiles](examples/profiles) |
| Read Replicas | ✅ | [examples/read-replicas](examples/read-replicas) |
| Synchronization (Locks, Semaphores) | ✅ | [examples/sync](examples/sync) |
| Notifications (Email, Slack, SMS) | ✅ | [examples/notifications](examples/notifications) |
| Aspects (AOP) | ✅ | [examples/aspects](examples/aspects) |
| Validators (Regex, CEL) | ✅ | [examples/validators](examples/validators) |
| WASM (Functions, Validators) | ✅ | [examples/wasm-functions](examples/wasm-functions) |
| Mocks | ✅ | [examples/mocks](examples/mocks) |
| Plugins | ✅ | [examples/plugin](examples/plugin) |
| Exec (Shell Commands) | ✅ | [examples/exec](examples/exec) |
| Hot Reload | ✅ | - |
| Health Checks / Prometheus | ✅ | `/health`, `/metrics` |

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
