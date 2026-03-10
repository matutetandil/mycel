# Mycel

[![CI](https://github.com/matutetandil/mycel/actions/workflows/ci.yml/badge.svg)](https://github.com/matutetandil/mycel/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/matutetandil/mycel)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/matutetandil/mycel)](https://github.com/matutetandil/mycel/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fmatutetandil%2Fmycel-blue?logo=docker)](https://ghcr.io/matutetandil/mycel)

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

On top of this, you can add [transforms](docs/core-concepts/transforms.md) (reshape data), [types](docs/core-concepts/types.md) (validate schemas), [steps](docs/guides/multi-step-flows.md) (multi-step orchestration), [sagas](docs/guides/sagas.md) (distributed transactions), [auth](docs/guides/auth.md), [aspects](docs/guides/extending.md#aspects), [security](docs/guides/security.md), and [more](#features). But every feature ultimately serves the same pattern: data enters through a connector, optionally gets transformed, and exits through another connector.

Every Mycel service automatically includes health checks (`/health`, `/health/live`, `/health/ready`), Prometheus metrics (`/metrics`), and hot reload — no configuration needed. Change an HCL file and the service reloads with zero downtime.

That's the whole model. Everything else is configuration. Learn more in [Core Concepts](docs/core-concepts/connectors.md).

## Quick Start

Create a directory with three HCL files — that's your entire microservice:

```bash
mkdir my-api && cd my-api
```

**`config.hcl`** — Name and version your service:
```hcl
service {
  name    = "users-api"
  version = "1.0.0"
}
```

**`connectors.hcl`** — Define what your service talks to:
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

**`flows.hcl`** — Wire them together:
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

Now run it — Mycel reads the directory and starts the service:

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

Test it:

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","name":"Test"}'

curl http://localhost:3000/users
```

That's it. REST API + database, zero code.

> See the [Quick Start Guide](docs/getting-started/quick-start.md) for a complete tutorial, or explore the [full documentation](#documentation).

## Purpose

- **What:** An open-source runtime that reads HCL configuration and exposes microservices. Same binary, different config = different service.
- **Why:** Most microservice code is plumbing — routing, database queries, data transformations, protocol translation, error handling, retries. It's the same patterns repeated across every service, in every team, in every company. Mycel extracts that into configuration so teams can focus on what's actually unique to their service.
- **Who:** Backend teams building CRUD APIs, event processors, integrations, or protocol bridges.

## Features

| Feature | Description |
|---------|-------------|
| [REST API](examples/basic) | Expose and consume REST endpoints |
| [SQLite / PostgreSQL / MySQL](examples/basic) | Relational database connectors |
| [MongoDB](examples/mongodb) | NoSQL document database |
| [GraphQL Server & Client](examples/graphql) | Schema-based GraphQL API |
| [GraphQL Query Optimization](examples/graphql-optimization) | Field selection, step skipping, DataLoader |
| [GraphQL Federation](examples/graphql-federation) | Federation v2, entity resolution, gateway-compatible subgraphs ([docs](docs/advanced/federation.md)) |
| [GraphQL Subscriptions](examples/graphql-federation) | Real-time push via WebSocket, per-user filtering ([docs](docs/guides/real-time.md#graphql-subscriptions)) |
| [GraphQL Subscription Client](examples/graphql-subscription-client) | Subscribe to external GraphQL events via WebSocket ([docs](docs/guides/real-time.md)) |
| [gRPC Server & Client](examples/grpc) | Protocol Buffers based RPC |
| [gRPC Load Balancing](examples/grpc-loadbalancing) | Round-robin and weighted balancing |
| [RabbitMQ / Kafka / Redis Pub/Sub](examples/mq) | Message queue producers and consumers |
| [MQTT](examples/mqtt) | IoT messaging protocol (QoS 0/1/2, TLS, auto-reconnect) |
| [FTP / SFTP](examples/ftp) | Remote file transfer (FTP, FTPS, SFTP with key auth) |
| [WebSocket](examples/websocket) | Bidirectional real-time communication with rooms and per-user targeting ([docs](docs/connectors/websocket.md)) |
| [CDC (Change Data Capture)](examples/cdc) | Real-time database change streaming with wildcard matching ([docs](docs/connectors/cdc.md)) |
| [SSE (Server-Sent Events)](examples/sse) | Unidirectional HTTP push with rooms and per-user targeting ([docs](docs/connectors/sse.md)) |
| [Elasticsearch](examples/elasticsearch) | Full-text search and analytics over Elasticsearch REST API ([docs](docs/connectors/elasticsearch.md)) |
| [OAuth (Social Login)](examples/oauth) | Declarative social login: Google, GitHub, Apple, OIDC, custom ([docs](docs/connectors/oauth.md)) |
| [Batch Processing](examples/batch) | Chunked data processing for migrations, ETL, reindexing ([docs](docs/guides/batch-processing.md)) |
| [Sagas](examples/saga) | Distributed transactions with automatic compensation, delay/await steps, workflow persistence ([docs](docs/guides/sagas.md)) |
| [State Machines](examples/state-machine) | Entity lifecycle with guards, actions, final states ([docs](docs/guides/sagas.md#state-machines)) |
| [Long-Running Workflows](examples/workflows) | Persistent workflows with delay timers, await/signal events, timeout enforcement, REST API ([docs](docs/guides/sagas.md#long-running-workflows)) |
| [SOAP](examples/soap) | Call or expose SOAP/XML web services (SOAP 1.1/1.2) ([docs](docs/connectors/soap.md)) |
| [Format Declarations](examples/format) | Multi-format support (JSON, XML) at connector, flow, and step level ([docs](docs/guides/format-system.md)) |
| [TCP Server & Client](examples/tcp) | JSON, msgpack, and NestJS protocols |
| [Files / S3](examples/files) | Local filesystem and AWS S3 / MinIO |
| [Cache (Memory / Redis)](examples/cache) | In-memory and Redis caching ([docs](docs/guides/caching.md)) |
| [Multi-step Flow Orchestration](examples/steps) | Sequential and conditional step execution ([docs](docs/guides/multi-step-flows.md)) |
| [Named Operations](examples/named-operations) | Reusable parameterized operations |
| [Data Enrichment](examples/enrich) | Combine data from multiple sources |
| [Auth (JWT, MFA, WebAuthn)](examples/auth) | Authentication with presets and MFA ([docs](docs/guides/auth.md)) |
| [Rate Limiting / Circuit Breaker](examples/rate-limit) | Traffic control and fault tolerance |
| [Connector Profiles](examples/profiles) | Multiple backends with fallback |
| [Read Replicas](examples/read-replicas) | Route reads to replica databases |
| [Synchronization](examples/sync) | Distributed locks, semaphores, coordination ([docs](docs/guides/synchronization.md)) |
| [Notifications](examples/notifications) | Email, Slack, Discord, SMS, Push, Webhook ([docs](docs/guides/notifications.md)) |
| [Aspects (AOP)](examples/aspects) | Cross-cutting concerns via pattern matching ([docs](docs/guides/extending.md#aspects)) |
| [Validators](examples/validators) | Regex, CEL, and custom validation rules ([docs](docs/guides/extending.md#validators)) |
| [WASM](examples/wasm-functions) | Custom functions and validators via WebAssembly ([docs](docs/advanced/wasm.md)) |
| [Mocks](examples/mocks) | Mock data for development and testing ([docs](docs/guides/extending.md#mocks)) |
| [Plugins](examples/plugin) | Extend Mycel with WASM plugins ([docs](docs/advanced/plugins.md)) |
| [Exec](examples/exec) | Execute shell commands from flows |
| [Error Handling](examples/error-handling) | Retry, DLQ, circuit breaker, custom error responses, on_error aspects ([docs](docs/guides/error-handling.md)) |
| [Security](examples/security) | Secure-by-default input sanitization, XXE/injection protection, WASM sanitizers ([docs](docs/guides/security.md)) |
| [Scheduled Jobs](examples/scheduled) | Cron expressions and interval-based flow triggers |
| Hot Reload | Apply HCL changes without restart |
| Health Checks / Prometheus | `/health`, `/metrics` endpoints |
| [Debugging](docs/guides/debugging.md) | Trace flows, interactive breakpoints, dry-run, IDE integration (VS Code, IntelliJ, Neovim) |

## Debugging

Mycel has a built-in debugging toolkit for tracing data through your flows — no log statements needed.

```bash
# See what a flow does, step by step
mycel trace get_users

# Simulate a write without touching the database
mycel trace create_user --input '{"email":"test@x.com"}' --dry-run

# Interactive breakpoints — pause at each pipeline stage
mycel trace create_user --input '{"email":"test@x.com"}' --breakpoints

# Pause only at specific stages
mycel trace create_user --input '{"email":"test@x.com"}' --break-at=transform,write

# IDE debugging (VS Code, IntelliJ, Neovim) via DAP
mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711

# Per-request pipeline logging in a running service
mycel start --verbose-flow
```

All debug features are **development-only** — automatically disabled in staging/production with zero overhead.

See the [Debugging Guide](docs/guides/debugging.md) for full documentation including IDE setup.

## CLI

```bash
mycel start [--config=<path>] [--env=<env>] [--log-level=<level>] [--log-format=<format>] [--hot-reload] [--verbose-flow]
mycel validate [--config=<path>]
mycel check [--config=<path>]
mycel version

mycel trace <flow-name> [--input=<json>] [--params=<k=v>] [--dry-run] [--breakpoints] [--break-at=<stages>] [--dap=<port>] [--list]

mycel plugin install [name]
mycel plugin list
mycel plugin remove <name>
mycel plugin update [name]
```

Environment: `MYCEL_ENV` (default: development), `MYCEL_LOG_LEVEL` (default: info), `MYCEL_LOG_FORMAT` (default: text). Flags take precedence.

See the [Debugging Guide](docs/guides/debugging.md) for `mycel trace` usage and examples.

## Plugins

Plugins extend Mycel with additional connectors, validators, and sanitizers via WASM. Declare them in your config and they work like built-in features — no extra wiring needed.

**Declare the plugin:**
```hcl
plugin "salesforce" {
  source  = "github.com/acme/mycel-salesforce"
  version = "^1.0"
}
```

**Use it like any built-in connector:**
```hcl
connector "sf" {
  type         = "salesforce"
  instance_url = env("SF_URL")
  api_key      = env("SF_KEY")
}

flow "sync_accounts" {
  from { connector.api = "POST /accounts" }
  to   { connector.sf  = "accounts" }
}
```

Plugin validators and sanitizers are also available immediately after declaration — use them in type definitions or security rules just like native ones.

Plugins are auto-installed on `mycel start`. Sources: local paths (`./plugins/my-plugin`), GitHub, GitLab, Bitbucket, or any git URL. Versions use semver constraints (`^1.0`, `~2.0`, `>= 1.0, < 3.0`). Cache stored in `mycel_plugins/`.

See the [plugin example](examples/plugin) for a complete walkthrough.

## Installation

**Docker (recommended):**
```bash
# From GitHub Container Registry
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel

# Or from Docker Hub
docker run -v $(pwd):/etc/mycel -p 3000:3000 mdenda/mycel
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

Full documentation is at [docs/index.md](docs/index.md). Quick links:

**Getting Started**
- [Introduction](docs/getting-started/introduction.md) — What Mycel is and how it works
- [Installation](docs/getting-started/installation.md) — Docker, Go binary, Helm
- [Quick Start](docs/getting-started/quick-start.md) — First service in 5 minutes

**Core Concepts**
- [Connectors](docs/core-concepts/connectors.md) — All 24 connector types
- [Flows](docs/core-concepts/flows.md) — Complete flow reference
- [Transforms](docs/core-concepts/transforms.md) — CEL functions and expressions
- [Types](docs/core-concepts/types.md) — Schema validation and field constraints
- [Environments](docs/core-concepts/environments.md) — Environment variables and overlays

**Guides**
- [Error Handling](docs/guides/error-handling.md) — Retry, DLQ, circuit breaker, fallback
- [Auth](docs/guides/auth.md) — JWT, MFA, SSO
- [Security](docs/guides/security.md) — Sanitization, XXE protection, WASM sanitizers
- [Real-Time](docs/guides/real-time.md) — WebSocket, SSE, CDC, GraphQL subscriptions
- [Sagas & State Machines](docs/guides/sagas.md) — Distributed transactions, entity lifecycle, long-running workflows
- [Notifications](docs/guides/notifications.md) — Email, Slack, Discord, SMS, push, webhook
- [Caching](docs/guides/caching.md) — In-memory and Redis caching
- [Synchronization](docs/guides/synchronization.md) — Distributed locks and semaphores
- [Batch Processing](docs/guides/batch-processing.md) — ETL and data migrations
- [Extending Mycel](docs/guides/extending.md) — Validators, WASM functions, mocks, aspects

**Reference**
- [Configuration Reference](docs/reference/configuration.md) — Complete HCL syntax
- [CEL Functions](docs/reference/cel-functions.md) — All built-in transform functions
- [CLI Reference](docs/reference/cli.md) — All commands and flags
- [API Endpoints](docs/reference/api-endpoints.md) — Health, metrics, workflow, auth endpoints

**Deployment**
- [Docker](docs/deployment/docker.md) — Docker run and Docker Compose
- [Kubernetes](docs/deployment/kubernetes.md) — Helm chart and manual deployment
- [Production Guide](docs/deployment/production.md) — Security checklist and monitoring

**Advanced**
- [GraphQL Federation](docs/advanced/federation.md) — Federated subgraphs, entities, gateway setup
- [WASM](docs/advanced/wasm.md) — Building WASM modules in 6 languages
- [Plugins](docs/advanced/plugins.md) — Extending Mycel with WASM plugins
- [Integration Patterns](docs/advanced/integration-patterns.md) — Protocol bridges, CDC pipelines, saga orchestration

**Project**
- [Architecture](docs/architecture.md) — Design decisions: why HCL, why CEL, why WASM, why Go
- [Roadmap](docs/ROADMAP.md) — Implementation status and future plans
- [Connector Catalog](docs/connectors/) — Individual documentation for every connector type

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
