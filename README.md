# Mycel

**Declarative microservices through configuration, not code.**

Define HCL files. Run Mycel. Get a production-ready microservice.

## How It Works

Mycel is a single binary runtime (like nginx). Same binary, different configuration = different microservice.

Mycel has two core building blocks: **connectors** and **flows**. Everything else builds on top of them.

A **connector** is anything Mycel can talk to — a database, a REST API, a message queue, a gRPC service, a file system. Every connector is bidirectional: it can be a **source** (receives data that triggers a flow) or a **target** (destination where a flow writes data).

A **flow** wires two connectors together, moving data from one to the other:

```
Connector (source) ──→ Flow ──→ Connector (target)
```

On top of this, you can add [transforms](docs/CONCEPTS.md#transforms) (reshape data), [types](docs/CONCEPTS.md#types) (validate schemas), [steps](docs/CONCEPTS.md#steps) (multi-step orchestration), [auth](docs/CONCEPTS.md#auth), [aspects](docs/CONCEPTS.md#aspects), and [more](#features). But every feature ultimately serves the same pattern: data enters through a connector, optionally gets transformed, and exits through another connector.

Every Mycel service automatically includes health checks (`/health`, `/health/live`, `/health/ready`), Prometheus metrics (`/metrics`), and hot reload — no configuration needed. Change an HCL file and the service reloads with zero downtime.

That's the whole model. Everything else is configuration. Learn more in [Concepts](docs/CONCEPTS.md).

## Quick Start

**`config.hcl`** - Name and version your service:
```hcl
service {
  name    = "users-api"
  version = "1.0.0"
}
```

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
| [GraphQL Federation](examples/graphql-federation) | Federation v2, subscriptions, entity resolution |
| [GraphQL Subscription Client](examples/graphql-subscription-client) | Subscribe to external GraphQL events via WebSocket ([concept](docs/CONCEPTS.md#client-side-subscriptions)) |
| [gRPC Server & Client](examples/grpc) | Protocol Buffers based RPC |
| [gRPC Load Balancing](examples/grpc-loadbalancing) | Round-robin and weighted balancing |
| [RabbitMQ / Kafka](examples/mq) | Message queue producers and consumers |
| [WebSocket](examples/websocket) | Bidirectional real-time communication with rooms and per-user targeting ([concept](docs/CONCEPTS.md#websockets)) |
| [CDC (Change Data Capture)](examples/cdc) | Real-time database change streaming with wildcard matching ([concept](docs/CONCEPTS.md#cdc-change-data-capture)) |
| [SSE (Server-Sent Events)](examples/sse) | Unidirectional HTTP push with rooms and per-user targeting ([concept](docs/CONCEPTS.md#sse-server-sent-events)) |
| [TCP Server & Client](examples/tcp) | JSON, msgpack, and NestJS protocols |
| [Files / S3](examples/files) | Local filesystem and AWS S3 / MinIO |
| [Cache (Memory / Redis)](examples/cache) | In-memory and Redis caching |
| [Multi-step Flow Orchestration](examples/steps) | Sequential and conditional step execution ([concept](docs/CONCEPTS.md#steps)) |
| [Named Operations](examples/named-operations) | Reusable parameterized operations ([concept](docs/CONCEPTS.md#named-operations)) |
| [Data Enrichment](examples/enrich) | Combine data from multiple sources |
| [Auth (JWT, MFA, WebAuthn)](examples/auth) | Authentication with presets and MFA ([concept](docs/CONCEPTS.md#auth)) |
| [Rate Limiting / Circuit Breaker](examples/rate-limit) | Traffic control and fault tolerance |
| [Connector Profiles](examples/profiles) | Multiple backends with fallback |
| [Read Replicas](examples/read-replicas) | Route reads to replica databases |
| [Synchronization](examples/sync) | Distributed locks, semaphores, coordination ([concept](docs/CONCEPTS.md#synchronization)) |
| [Notifications](examples/notifications) | Email, Slack, Discord, SMS, Push, Webhook |
| [Aspects (AOP)](examples/aspects) | Cross-cutting concerns via pattern matching ([concept](docs/CONCEPTS.md#aspects)) |
| [Validators](examples/validators) | Regex, CEL, and custom validation rules ([concept](docs/CONCEPTS.md#validators)) |
| [WASM](examples/wasm-functions) | Custom functions and validators via WebAssembly ([concept](docs/CONCEPTS.md#wasm)) |
| [Mocks](examples/mocks) | Mock data for development and testing ([concept](docs/CONCEPTS.md#mocks)) |
| [Plugins](examples/plugin) | Extend Mycel with WASM plugins ([concept](docs/CONCEPTS.md#plugins)) |
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

- **[Concepts](docs/CONCEPTS.md)** - Understanding connectors, flows, transforms, and the full Mycel model
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
