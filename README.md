# Mycel

[![CI](https://github.com/matutetandil/mycel/actions/workflows/ci.yml/badge.svg)](https://github.com/matutetandil/mycel/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/matutetandil/mycel)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/matutetandil/mycel)](https://github.com/matutetandil/mycel/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fmatutetandil%2Fmycel-blue?logo=docker)](https://ghcr.io/matutetandil/mycel)

**Mycel is a declarative microservice runtime — you describe what connects to what, and it runs the service.**

Point Mycel at the things you want to connect — an API, a database, a queue, a gRPC service, a file store — and it runs the microservice that moves data between them. The plumbing every service repeats (HTTP server, connection pools, marshalling, retries, reconnection) is Mycel's job. The only logic you ever write is your service's own — a transform, a validation rule — and only when it actually needs it. You describe it in [HCL2](https://github.com/hashicorp/hcl) config files; Mycel runs it as a real, production-ready microservice. Pure Go, a single binary, standard protocols on the wire — from the outside, indistinguishable from one you'd hand-write.

## How It Works

Mycel is a single binary runtime. The same binary runs every service — only the configuration changes.

Mycel has two core building blocks: **connectors** and **flows**. Everything else builds on top of them.

A **connector** is anything Mycel can talk to — a database, a REST API, a message queue, a gRPC service, a file system. Every connector is bidirectional: it can be a **source** (receives data that triggers a flow) or a **target** (destination where a flow writes data).

A **flow** wires two connectors together, moving data from one to the other:

```
Connector (source) ──→ Flow ──→ Connector (target)
```

On top of this, you can add [transforms](docs/core-concepts/transforms.md) (reshape data), [types](docs/core-concepts/types.md) (validate schemas), [steps](docs/guides/multi-step-flows.md) (multi-step orchestration), [sagas](docs/guides/sagas.md) (distributed transactions), [auth](docs/guides/auth.md), [aspects](docs/core-concepts/aspects.md), [security](docs/guides/security.md), and [more](#features). But every feature ultimately serves the same pattern: data enters through a connector, optionally gets transformed, and exits through another connector.

Every Mycel service automatically includes health checks (`/health`, `/health/live`, `/health/ready`), Prometheus metrics (`/metrics`), and hot reload — no configuration needed. Change a `.mycel` file and the service reloads with zero downtime.

That's the whole model. Everything else is configuration. Learn more in [Core Concepts](docs/core-concepts/connectors.md).

## Quick Start

Create a directory with three `.mycel` files — that's your entire microservice:

```bash
mkdir orders-intake && cd orders-intake
```

**`config.mycel`** — Name and version your service:
```hcl
service {
  name    = "orders-intake"
  version = "1.0.0"
}
```

**`connectors.mycel`** — Define what your service talks to:
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

**`flows.mycel`** — Wire them together. An order arrives over HTTP, gets reshaped in flight, and lands in the database:
```hcl
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  transform {
    id         = "uuid()"
    customer   = "lower(trim(input.customer))"
    total      = "input.total"
    created_at = "now()"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}

flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}
```

Mycel serves the schema you give it, so create the table first:

```bash
mkdir -p data
sqlite3 data/app.db 'CREATE TABLE orders (
  id         TEXT PRIMARY KEY,
  customer   TEXT,
  total      REAL,
  created_at TEXT
);'
```

Now run it — Mycel reads the directory and starts the service:

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

Test it — send a messy order, get a normalized one back:

```bash
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{"customer":"  ADA@EXAMPLE.COM  ","total":42.5}'

curl http://localhost:3000/orders
# [{"created_at":"2026-06-01T...","customer":"ada@example.com","id":"870339c1-...","total":42.5}]
```

That's an HTTP intake service — validation-ready transforms and a database write — with no plumbing of your own to maintain. The flow is the stable part; the edges are pluggable. Swap the `from` to a RabbitMQ queue and the same flow becomes a durable event consumer. Swap the `to` to another REST API and it's a protocol bridge. You changed what connects to what; Mycel rebuilt the machinery underneath.

> See the [Quick Start Guide](docs/getting-started/quick-start.md) for a complete tutorial, or explore the [full documentation](#documentation).

## Purpose

- **What:** An open-source runtime that reads HCL configuration and runs it as a microservice. Same binary, different config = different service.
- **Why:** Most microservice code is plumbing — routing, database queries, data transformations, protocol translation, error handling, retries. It's the same patterns repeated across every service, in every team, in every company. Mycel extracts that into configuration so teams can focus on what's actually unique to their service.
- **Who:** Backend teams building microservices of any kind — APIs, integrations, event processors, protocol bridges — who'd rather declare the service than rewrite its plumbing.

## Features

The simple case is trivial — connect A to B, like above. The list below is the complexity that's *there when you need it*: a transform, a lock, a cache, a saga, a circuit breaker, a new protocol. Each one is a block of configuration you declare inside a flow, never machinery you have to build. You don't need any of it to start; you reach for it the day your service does.

### Connectors — the systems you wire together

The A's and B's of any flow. Use any as a source, a target, or both.

| Connector | Description |
|-----------|-------------|
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
| [MQTT](examples/mqtt) | IoT messaging protocol (QoS 0/1/2, TLS, auto-reconnect) ([docs](docs/connectors/mqtt.md)) |
| [WebSocket](examples/websocket) | Bidirectional real-time communication with rooms and per-user targeting ([docs](docs/connectors/websocket.md)) |
| [SSE (Server-Sent Events)](examples/sse) | Unidirectional HTTP push with rooms and per-user targeting ([docs](docs/connectors/sse.md)) |
| [CDC (Change Data Capture)](examples/cdc) | Real-time database change streaming with wildcard matching ([docs](docs/connectors/cdc.md)) |
| [Elasticsearch](examples/elasticsearch) | Full-text search and analytics over Elasticsearch REST API ([docs](docs/connectors/elasticsearch.md)) |
| [SOAP](examples/soap) | Call or expose SOAP/XML web services (SOAP 1.1/1.2) ([docs](docs/connectors/soap.md)) |
| [TCP Server & Client](examples/tcp) | JSON, msgpack, and NestJS protocols |
| [Files / S3](examples/files) | Local filesystem and AWS S3 / MinIO |
| [FTP / SFTP](examples/ftp) | Remote file transfer (FTP, FTPS, SFTP with key auth) ([docs](docs/connectors/ftp.md)) |
| [Notifications](examples/notifications) | Email, Slack, Discord, SMS, Push, Webhook ([docs](docs/guides/notifications.md)) |

### Shaping & validating data

What happens to the payload between `from` and `to`.

| Capability | Description |
|------------|-------------|
| [Format Declarations](examples/format) | Multi-format support (JSON, XML) at connector, flow, and step level ([docs](docs/guides/format-system.md)) |
| [Data Enrichment](examples/enrich) | Combine data from multiple sources |
| [Validators](examples/validators) | Regex, CEL, and custom validation rules ([docs](docs/guides/extending.md#validators)) |

### Orchestration & flow control

For when one `from → to` isn't enough: multiple steps, routing, reuse, long-running work.

| Capability | Description |
|------------|-------------|
| [Multi-step Flow Orchestration](examples/steps) | Sequential and conditional step execution ([docs](docs/guides/multi-step-flows.md)) |
| [Reusable Blocks](examples/reusable-blocks) | **Recommended:** declare dedupe/retry/lock/accept/response/etc. once with a name, reference from many flows with `use = "<kind>.<name>"` — named vs anonymous, like functions ([docs](docs/core-concepts/reusable-blocks.md)) |
| Accept Gate | Business-level message routing with `on_reject` policy (ack/reject/requeue) ([docs](docs/core-concepts/flows.md#the-accept-block)) |
| Source Fan-Out | Multiple flows from the same connector+operation, concurrent execution ([docs](docs/core-concepts/flows.md#source-fan-out-multiple-flows-from-same-source)) |
| [Named Operations](examples/named-operations) | Reusable parameterized operations |
| [Transactional Writes](examples/transactional-write) | Atomic, iterative, multi-statement DB writes: `to { transaction { } }` with `exec`/`each`, `LAST_INSERT_ID`/SELECT capture, all-or-nothing commit |
| [Sagas](examples/saga) | Distributed transactions with automatic compensation, delay/await steps, workflow persistence ([docs](docs/guides/sagas.md)) |
| [State Machines](examples/state-machine) | Entity lifecycle with guards, actions, final states ([docs](docs/guides/sagas.md#state-machines)) |
| [Long-Running Workflows](examples/workflows) | Persistent workflows with delay timers, await/signal events, timeout enforcement, REST API ([docs](docs/guides/sagas.md#long-running-workflows)) |
| [Batch Processing](examples/batch) | Chunked data processing for migrations, ETL, reindexing ([docs](docs/guides/batch-processing.md)) |
| [Scheduled Jobs](examples/scheduled) | Cron expressions and interval-based flow triggers |
| [Aspects (AOP)](examples/aspects) | Cross-cutting concerns (audit, metrics, alerting) applied across flows by name pattern ([docs](docs/core-concepts/aspects.md)) |

### Reliability & performance

What keeps the service standing when a downstream misbehaves or traffic spikes.

| Capability | Description |
|------------|-------------|
| [Error Handling](examples/error-handling) | Retry, DLQ, circuit breaker, custom error responses, on_error aspects ([docs](docs/guides/error-handling.md)) |
| [Resilience & Failure Recovery](docs/guides/resilience.md) | What survives a crash: availability vs durability, broker redelivery, sync vs async ingestion, idempotency, locks with TTL |
| [Rate Limiting / Circuit Breaker](examples/rate-limit) | Traffic control and fault tolerance |
| [Synchronization](examples/sync) | Distributed locks, semaphores, coordination ([docs](docs/guides/synchronization.md)) |
| [Connector Profiles](examples/profiles) | Multiple backends with fallback |
| [Read Replicas](examples/read-replicas) | Route reads to replica databases |
| [Cache (Memory / Redis)](examples/cache) | In-memory and Redis caching ([docs](docs/guides/caching.md)) |

### Security & auth

| Capability | Description |
|------------|-------------|
| [Auth (JWT, MFA, WebAuthn)](examples/auth) | Authentication with presets and MFA ([docs](docs/guides/auth.md)) |
| [OAuth (Social Login)](examples/oauth) | Declarative social login: Google, GitHub, Apple, OIDC, custom ([docs](docs/connectors/oauth.md)) |
| [Security](examples/security) | Secure-by-default input sanitization, XXE/injection protection, WASM sanitizers ([docs](docs/guides/security.md)) |

### Extending Mycel

When a connector or transform doesn't express what you need, drop down to your own code — and only that code.

| Capability | Description |
|------------|-------------|
| [WASM](examples/wasm-functions) | Custom functions and validators via WebAssembly ([docs](docs/advanced/wasm.md)) |
| [Plugins](examples/plugin) | Extend Mycel with WASM plugins ([docs](docs/advanced/plugins.md)) |
| [Exec](examples/exec) | Execute shell commands from flows |
| [Mocks](examples/mocks) | Mock data for development and testing ([docs](docs/guides/extending.md#mocks)) |

### Built in — no config needed

Every Mycel service gets these for free.

| Capability | Description |
|------------|-------------|
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

**Incoming payload logging.** To see the raw payload entering a flow — regardless of source connector, and in any environment including production — set `MYCEL_PAYLOAD_SHOW=true` together with `MYCEL_LOG_LEVEL=debug`. It logs the payload on entry (before sanitization/validation) at the single choke-point every request passes through, so it works for queues, HTTP, TCP, etc. Off by default (payloads may carry PII/secrets); `MYCEL_PAYLOAD_SIZE` caps the logged size (default `4k`, e.g. `512`/`4k`/`1m`).

```bash
MYCEL_LOG_LEVEL=debug MYCEL_PAYLOAD_SHOW=true mycel start
# DBG incoming payload flow=create_user source=api payload={"name":"Ada","email":"..."}
```

**Profiling (`pprof`).** For diagnosing a live process — goroutine leaks, heap growth, CPU hotspots — set `MYCEL_PPROF=true` to mount the Go `net/http/pprof` endpoints under `/debug/pprof/` on the admin server (`:9090`). It's off by default and safe to enable in any environment, including production: the admin port is internal (reach it with `kubectl port-forward`). Then:

```bash
# Full goroutine dump (what reveals a leak)
curl 'http://localhost:9090/debug/pprof/goroutine?debug=2'
# Or interactively
go tool pprof http://localhost:9090/debug/pprof/goroutine
```

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

Environment: `MYCEL_ENV` (default: development), `MYCEL_LOG_LEVEL` (default: info), `MYCEL_LOG_FORMAT` (default: text), `MYCEL_PPROF` (default: off — mounts `pprof` on the admin server when truthy), `MYCEL_PAYLOAD_SHOW` (default: off — logs incoming flow payloads at debug level) with `MYCEL_PAYLOAD_SIZE` (default: `4k`). Flags take precedence.

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
- [Connectors](docs/core-concepts/connectors.md) — All connector types
- [Flows](docs/core-concepts/flows.md) — Complete flow reference
- [Transforms](docs/core-concepts/transforms.md) — CEL functions and expressions
- [Types](docs/core-concepts/types.md) — Schema validation and field constraints
- [Aspects](docs/core-concepts/aspects.md) — Cross-cutting concerns (AOP) applied across flows by pattern
- [Environments](docs/core-concepts/environments.md) — Environment variables and overlays

**Guides**
- [Error Handling](docs/guides/error-handling.md) — Retry, DLQ, circuit breaker, fallback
- [Resilience & Failure Recovery](docs/guides/resilience.md) — Availability vs durability, broker redelivery, sync vs async ingestion, idempotency
- [Auth](docs/guides/auth.md) — JWT, MFA, SSO
- [Security](docs/guides/security.md) — Sanitization, XXE protection, WASM sanitizers
- [Real-Time](docs/guides/real-time.md) — WebSocket, SSE, CDC, GraphQL subscriptions
- [Sagas & State Machines](docs/guides/sagas.md) — Distributed transactions, entity lifecycle, long-running workflows
- [Notifications](docs/guides/notifications.md) — Email, Slack, Discord, SMS, push, webhook
- [Caching](docs/guides/caching.md) — In-memory and Redis caching
- [Synchronization](docs/guides/synchronization.md) — Distributed locks and semaphores
- [Batch Processing](docs/guides/batch-processing.md) — ETL and data migrations
- [Extending Mycel](docs/guides/extending.md) — Validators, WASM functions, mocks, plugins

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

<a href="https://github.com/sponsors/matutetandil" target="_blank"><img src="https://img.shields.io/badge/Sponsor-%E2%9D%A4-db61a2?logo=githubsponsors&logoColor=white&style=for-the-badge" alt="GitHub Sponsors" height="42"></a>
&nbsp;
<a href="https://buymeacoffee.com/matutetandil" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" width="200"></a>

## Contributing

Contributions are welcome! Please read the [contributing guidelines](CONTRIBUTING.md)
and our [Code of Conduct](CODE_OF_CONDUCT.md) before submitting a pull request.
For security issues, see the [security policy](SECURITY.md).

## License

Released under the [MIT License](LICENSE).
