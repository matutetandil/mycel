# Mycel

[![CI](https://github.com/matutetandil/mycel/actions/workflows/ci.yml/badge.svg)](https://github.com/matutetandil/mycel/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/matutetandil/mycel)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/matutetandil/mycel)](https://github.com/matutetandil/mycel/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/matutetandil/mycel/v3.svg)](https://pkg.go.dev/github.com/matutetandil/mycel/v3)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fmatutetandil%2Fmycel-blue?logo=docker)](https://ghcr.io/matutetandil/mycel)

**Mycel is a declarative microservice runtime — you describe what connects to what, and it runs the service.**

Point Mycel at the things you want to connect — an API, a database, a queue, a gRPC service, a file store — and it runs the microservice that moves data between them. The plumbing every service repeats (HTTP server, connection pools, marshalling, retries, reconnection) is Mycel's job. The only logic you ever write is your service's own — a transform, a validation rule — and only when it actually needs it. You describe it in [HCL2](https://github.com/hashicorp/hcl) config files; Mycel runs it as a real, production-ready microservice. Pure Go, a single binary, standard protocols on the wire — from the outside, indistinguishable from one you'd hand-write.

📖 **[Full documentation](https://matutetandil.github.io/mycel/)** · 🚀 **[Quick Start](docs/getting-started/quick-start.md)** · 🧩 **[Examples](examples/)**

## How It Works

Mycel is a single binary. The same binary runs every service — only the configuration changes.

There are two core building blocks. A **connector** is anything Mycel can talk to — a database, a REST API, a message queue, a gRPC service, a file system — and every connector is bidirectional: it can be a **source** (receives data that triggers a flow) or a **target** (where a flow writes). A **flow** wires two connectors together:

```
Connector (source) ──→ Flow ──→ Connector (target)
```

Everything else builds on top: [transforms](docs/core-concepts/transforms.md) reshape data, [types](docs/core-concepts/types.md) validate it, [steps](docs/guides/multi-step-flows.md) orchestrate, [sagas](docs/guides/sagas.md) compensate, [aspects](docs/core-concepts/aspects.md) cut across. But every feature serves the same pattern: data enters through a connector, optionally gets transformed, and exits through another.

Every Mycel service gets health checks (`/health`, `/health/live`, `/health/ready`), Prometheus metrics (`/metrics`), OpenTelemetry tracing and hot reload with no configuration. Change a `.mycel` file and the service reloads with zero downtime.

That's the whole model. Everything else is configuration.

> **Writing your first flow?** [Flow Anatomy](docs/core-concepts/flows.md#flow-anatomy) lists every block a flow can contain, in the order they run.

## Quick Start

Create a directory with three `.mycel` files — that's your entire microservice:

```bash
mkdir orders-intake && cd orders-intake
```

**`config.mycel`** — name and version your service:
```hcl
service {
  name    = "orders-intake"
  version = "1.0.0"
}
```

**`connectors.mycel`** — define what your service talks to:
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

**`flows.mycel`** — wire them together. An order arrives over HTTP, gets reshaped in flight, and lands in the database:
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

Prefer to start from a generated skeleton? `mycel init my-service` writes the same three files for you.

> See the [Quick Start Guide](docs/getting-started/quick-start.md) for the complete tutorial.

## Why

Most microservice code is plumbing — routing, database queries, data transformations, protocol translation, error handling, retries. The same patterns repeated across every service, in every team, in every company. Mycel extracts that into configuration so teams can focus on what's actually unique to their service.

It's for backend teams building microservices of any kind — APIs, integrations, event processors, protocol bridges — who'd rather declare the service than rewrite its plumbing.

## When Mycel Fits

Mycel is at its best when the service's job is moving and reshaping data between systems:

- **Ingestion and integration** — a queue, a webhook or a file drop lands somewhere else, reshaped and validated on the way.
- **APIs over existing data** — expose a database, a SOAP service or an internal API as REST or GraphQL, with validation, auth and rate limiting.
- **Event-driven pipelines** — CDC, MQTT or Kafka events routed, filtered and fanned out to the systems that care.
- **Orchestration across services** — multi-step flows, sagas with compensation, transactions, retries and circuit breakers.

It's a worse fit when the service's value *is* the code:

- **The logic doesn't fit an expression.** Transforms are [CEL](docs/reference/cel-functions.md) — good at reshaping a payload, not at implementing an algorithm. Custom logic goes into a [WASM plugin](docs/advanced/wasm.md), which today runs pure functions only (validators, transforms, CEL functions — no I/O of its own).
- **You need a system Mycel doesn't speak.** Connectors ship with the runtime, so a protocol that isn't in the list is a change to Mycel, not a change to your config.
- **The domain model is the point.** If most of the service is business rules rather than data movement, you'd be writing that logic somewhere anyway — write it in Go and let Mycel handle the edges, or don't use it at all.

## Performance

The [benchmark suite](benchmark/) runs three tests in parallel — each against its own Mycel instance — on the cheapest hardware available: $5 VPS with 1 vCPU and 1 GB of RAM, with PostgreSQL on a *separate* machine over the public network. It calibrates itself to the hardware first, then loads it.

| Test | What it measures | Result |
|------|------------------|--------|
| **Standard** | Mycel itself — HTTP, CEL transforms, JSON, array processing, no external I/O | **8,437 RPS**, p99 151 ms, 0.000% errors |
| **Realistic** | Full CRUD against PostgreSQL over the network | **204 RPS**, median **2.0 ms**, p95 4.9 ms, 0.010% errors |
| **Stress** | 3× the calibrated safe limit, 100 KB payloads, chaos mix | **402 RPS**, 0.011% errors, **0 crashes, 0 restarts, 0 OOM kills** |

3.2 million requests across the three simultaneous tests, 12 minutes wall clock, under 0.01% errors overall. In the realistic test the bottleneck is the PostgreSQL connection over the public network, not Mycel — the median of 2 ms is the runtime; the p99 is the database.

Full methodology, per-phase numbers and resource usage in [`benchmark/RESULTS.md`](benchmark/RESULTS.md). Measured on v1.12.0; the suite is reproducible (Terraform + k6) if you want to run it against your own hardware.

## Features

The simple case is trivial — connect A to B, like above. What follows is the complexity that's *there when you need it*: a transform, a lock, a cache, a saga, a circuit breaker, a new protocol. Each one is a block of configuration you declare inside a flow, never machinery you have to build. You don't need any of it to start; you reach for it the day your service does.

<details>
<summary><b>Connectors</b> — the systems you wire together</summary>

The A's and B's of any flow. Use any as a source, a target, or both.

| Connector | Description |
|-----------|-------------|
| [REST API](examples/basic) | Expose and consume REST endpoints |
| [HTTP QUERY method](examples/query-method) | RFC 10008 safe method with a body — search criteria in the request body with GET's read/cache semantics |
| [SQLite / PostgreSQL / MySQL](examples/basic) | Relational database connectors |
| [MongoDB](examples/mongodb) | NoSQL document database |
| [GraphQL Server & Client](examples/graphql) | Schema-based GraphQL API |
| [GraphQL Query Optimization](examples/graphql-optimization) | Field selection, step skipping, DataLoader |
| [GraphQL Federation](examples/graphql-federation) | Federation v2, entity resolution, gateway-compatible subgraphs ([docs](docs/advanced/federation.md)) |
| [GraphQL Subscriptions](examples/graphql-federation) | Real-time push via WebSocket, per-user filtering ([docs](docs/guides/real-time.md#graphql-subscriptions)) |
| [GraphQL Subscription Client](examples/graphql-subscription-client) | Subscribe to external GraphQL events via WebSocket ([docs](docs/guides/real-time.md)) |
| [gRPC Server & Client](examples/grpc) | Protocol Buffers based RPC |
| [gRPC Load Balancing](examples/grpc-loadbalancing) | Round-robin and weighted balancing |
| [RabbitMQ / Redis Pub/Sub](examples/mq) | Message queue producers and consumers |
| [Kafka](examples/kafka) | Topics, consumer groups, offsets and acks ([docs](docs/connectors/message-queues.md)) |
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
| [PDF](examples/pdf) | Render a flow's result as a PDF document ([docs](docs/connectors/pdf.md)) |

</details>

<details>
<summary><b>Shaping & validating data</b> — what happens to the payload between <code>from</code> and <code>to</code></summary>

| Capability | Description |
|------------|-------------|
| [Transforms](docs/core-concepts/transforms.md) | Reshape the payload with CEL expressions — inline in a flow or declared once and reused |
| [Types](docs/core-concepts/types.md) | Schema validation with field constraints, applied to a flow's input or output |
| [Constants](docs/core-concepts/constants.md) | Declare a value once — a list, a map, a number — and use it from every flow, on both the HCL and the CEL side ([example](examples/constants)) |
| [Format Declarations](examples/format) | Multi-format support (JSON, XML) at connector, flow, and step level ([docs](docs/guides/format-system.md)) |
| [Data Enrichment](examples/enrich) | Combine data from multiple sources |
| [Validators](examples/validators) | Regex, CEL, and custom validation rules ([docs](docs/guides/extending.md#validators)) |

</details>

<details>
<summary><b>Orchestration & flow control</b> — for when one <code>from → to</code> isn't enough</summary>

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
| [Long-Running Workflows](examples/workflows) | Persistent workflows with delay timers, await/signal events, timeout enforcement, an authenticated HTTP interface on its own port ([docs](docs/guides/sagas.md#long-running-workflows)) |
| [Batch Processing](examples/batch) | Chunked data processing for migrations, ETL, reindexing ([docs](docs/guides/batch-processing.md)) |
| [Scheduled Jobs](examples/scheduled) | Cron expressions and interval-based flow triggers |
| [Transforms](examples/transforms) | Reshaping a messy record: fallbacks, splitting, list handling, dates, fingerprints ([docs](docs/core-concepts/transforms.md)) |
| [Async Jobs & Idempotency](examples/async-jobs) | A slow request answered with `202` and a job id, and a retry that does not write twice |
| [Aspects (AOP)](examples/aspects) | Cross-cutting concerns (audit, metrics, alerting) applied across flows by name pattern ([docs](docs/core-concepts/aspects.md)) |

</details>

<details>
<summary><b>Reliability & performance</b> — what keeps the service standing when a downstream misbehaves</summary>

| Capability | Description |
|------------|-------------|
| [Error Handling](examples/error-handling) | Retry, DLQ, circuit breaker, custom error responses, on_error aspects ([docs](docs/guides/error-handling.md)) |
| [Resilience & Failure Recovery](docs/guides/resilience.md) | What survives a crash: availability vs durability, broker redelivery, sync vs async ingestion, idempotency, locks with TTL |
| [Rate Limiting / Circuit Breaker](examples/rate-limit) | Traffic control and fault tolerance |
| [Synchronization](examples/sync) | Distributed locks, semaphores, coordination ([docs](docs/guides/synchronization.md)) |
| [Connector Profiles](examples/profiles) | Multiple backends with fallback |
| [Read Replicas](examples/read-replicas) | Route reads to replica databases |
| [Cache (Memory / Redis)](examples/cache) | In-memory and Redis caching ([docs](docs/guides/caching.md)) |

</details>

<details>
<summary><b>Security & auth</b></summary>

| Capability | Description |
|------------|-------------|
| [Auth (JWT, MFA, WebAuthn)](examples/auth) | Authentication with presets and MFA ([docs](docs/guides/auth.md)) |
| [OAuth (Social Login)](examples/oauth) | Declarative social login: Google, GitHub, Apple, OIDC, custom ([docs](docs/connectors/oauth.md)) |
| [Security](examples/security) | Secure-by-default input sanitization, XXE/injection protection, WASM sanitizers ([docs](docs/guides/security.md)) |

</details>

<details>
<summary><b>Extending Mycel</b> — when a connector or transform doesn't express what you need</summary>

Drop down to your own code — and only that code.

| Capability | Description |
|------------|-------------|
| [WASM](examples/wasm-functions) | Custom functions and validators via WebAssembly ([docs](docs/advanced/wasm.md)) |
| [Plugins](examples/plugin) | Connectors, validators and sanitizers distributed as WASM modules — declare `plugin "name" { source = "github.com/…" }` and use them like built-ins; auto-installed on `mycel start` ([docs](docs/advanced/plugins.md)) |
| [Exec](examples/exec) | Execute shell commands from flows |
| [Mocks](examples/mocks) | Mock data for development and testing ([docs](docs/guides/extending.md#mocks)) |

</details>

<details>
<summary><b>Built in</b> — every service gets these with no config</summary>

| Capability | Description |
|------------|-------------|
| Hot Reload | Apply `.mycel` changes without restart |
| Health Checks / Prometheus | `/health`, `/health/live`, `/health/ready`, `/metrics` |
| [OpenTelemetry Tracing](docs/guides/observability.md) | Opt-in OTLP traces: a span per flow, context propagated in and out over HTTP and MQ headers, `trace_id` in the logs |
| [Debugging](docs/guides/debugging.md) | Trace flows, interactive breakpoints, dry-run, IDE integration (VS Code, IntelliJ, Neovim) |

</details>

## CLI

```bash
mycel init [name]                  # scaffold a new service
mycel start [--env] [--hot-reload] [--verbose-flow] [--log-level] [--config]
mycel validate [--config]          # check the configuration without running it
mycel check [--config]             # connect to every configured system and report
mycel migrate [--connector]        # apply SQL migrations   (also: migrate status)
mycel version

mycel add connector|flow|aspect|type|constants|transform|validator|saga|state-machine

mycel trace <flow> [--input=<json>] [--dry-run] [--breakpoints] [--break-at=<stages>] [--dap=<port>]

mycel export openapi|asyncapi|graphql-schema
mycel plugin install|list|remove|update
```

Every command is documented in the [CLI Reference](docs/reference/cli.md). Environment: `MYCEL_ENV`, `MYCEL_LOG_LEVEL`, `MYCEL_LOG_FORMAT`, `MYCEL_PPROF`, `MYCEL_PAYLOAD_SHOW`/`MYCEL_PAYLOAD_SIZE`, `MYCEL_TRACING`. Flags take precedence.

## Debugging

Mycel traces data through your flows without log statements — step by step, as a dry run, or paused at a breakpoint:

```bash
mycel trace create_user --input '{"email":"test@x.com"}' --dry-run
mycel trace create_user --input '{"email":"test@x.com"}' --break-at=transform,write
mycel start --verbose-flow
```

These are development-only and disabled outside it. Two switches work anywhere, production included: `MYCEL_PAYLOAD_SHOW=true` (with `MYCEL_LOG_LEVEL=debug`) logs the raw payload entering any flow, whatever the source connector; `MYCEL_PPROF=true` mounts Go's `pprof` on the internal admin port for a live goroutine or heap dump. Both are off by default.

See the [Debugging Guide](docs/guides/debugging.md) for IDE setup and the full toolkit.

## Installation

```bash
# Docker
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel

# Homebrew
brew install matutetandil/tap/mycel

# Install script (Linux/macOS, amd64/arm64)
curl -fsSL https://raw.githubusercontent.com/matutetandil/mycel/main/install.sh | sh

# Kubernetes
helm install my-api oci://ghcr.io/matutetandil/charts/mycel

# Go
go install github.com/matutetandil/mycel/v3/cmd/mycel@latest
```

Every release also publishes `.deb`, `.rpm` and `.apk` packages (with a systemd unit) and plain tarballs — see [Installation](docs/getting-started/installation.md), or [helm/mycel/README.md](helm/mycel/README.md) for the chart's values, autoscaling and ingress.

**Requirements:** Docker, or Go 1.25+ to build from source.

## Documentation

Everything is at **[matutetandil.github.io/mycel](https://matutetandil.github.io/mycel/)** — the same pages live under [`docs/`](docs/index.md). The ones worth bookmarking:

- [Quick Start](docs/getting-started/quick-start.md) — first service in 5 minutes
- [Flows](docs/core-concepts/flows.md) — every block a flow can contain, in the order they run
- [Input & Output](docs/core-concepts/input-and-output.md) — what `input.*` holds per connector, and how `output` is built
- [HCL Syntax Reference](docs/reference/configuration.md) — every block type and attribute
- [CEL Functions](docs/reference/cel-functions.md) — all built-in transform functions
- [Error Handling](docs/guides/error-handling.md) · [Resilience](docs/guides/resilience.md) — retry, DLQ, circuit breaker, what survives a crash
- [Auth](docs/guides/auth.md) · [Security](docs/guides/security.md) — JWT, MFA, SSO, sanitization
- [Architecture](docs/architecture.md) — why HCL, why CEL, why WASM, why Go
- [Roadmap](docs/ROADMAP.md) — implementation status and what's next

Every connector has its own page under [`docs/connectors/`](docs/connectors/), and [`examples/`](examples/) has a runnable project per feature.

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
