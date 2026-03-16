# Integration Tests

End-to-end integration tests for Mycel. Spins up a complete environment with all supported databases, message queues, and services via Docker Compose, then exercises every connector and protocol.

## Prerequisites

- Docker & Docker Compose
- `jq` (for JSON assertions)
- `grpcurl` (optional, for gRPC tests): `go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest`

## Quick Start

```bash
cd tests/integration
bash run.sh
```

This will:
1. Build and start all services (Mycel + 10 infrastructure services + Cosmo Router)
2. Wait for health checks
3. Run 26 test suites (~95 assertions) in parallel
4. Tear down all services

## Options

```bash
bash run.sh --keep          # Don't tear down after tests (for debugging)
bash run.sh --skip-build    # Skip Docker build (use existing images)
bash run.sh --sequential    # Run tests one by one (default: parallel)
bash run.sh postgres graphql # Run only specific test suites
```

## Parallel Execution

By default, tests run in three phases:

1. **Preflight** — `health` and `metrics` run first on a clean server
2. **Parallel** — All other suites run concurrently (`&` + `wait`). Mock-dependent tests (`http-client`, `notifications`) run sequentially within their own group to avoid `mock_clear` conflicts
3. **Solo** — `rate-limit` runs last and alone (it triggers 429 server-wide)

Use `--sequential` to disable parallelism and run one test at a time.

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Test       │────▶│   Mycel      │────▶│  PostgreSQL   │
│   Scripts    │     │  (all ports) │────▶│  MySQL        │
│  (curl/bash) │     │              │────▶│  MongoDB      │
└──────────────┘     │  REST :3000  │────▶│  Redis        │
                     │  GQL  :4000  │────▶│  RabbitMQ     │
┌──────────────┐     │  SOAP :8081  │────▶│  Kafka        │
│ Cosmo Router │────▶│  gRPC :50051 │────▶│  Elasticsearch│
│  (Federation)│     │  Admin:9090  │────▶│  MinIO (S3)   │
│  :5000       │     └──────────────┘────▶│  Mock :8888   │
└──────────────┘                          └──────────────┘
```

Ports are auto-remapped if busy (e.g., `:3000` → `:3001`).

## Test Coverage

| Script | Protocol | Connectors | Assertions |
|--------|----------|-----------|------------|
| test-health.sh | HTTP | REST, Admin | 4 — Health endpoints |
| test-metrics.sh | HTTP | Admin | 3 — Prometheus metrics |
| test-postgres.sh | HTTP | REST + PostgreSQL | 5 — CRUD |
| test-mysql.sh | HTTP | REST + MySQL | 5 — CRUD |
| test-mongodb.sh | HTTP | REST + MongoDB | 5 — CRUD |
| test-sqlite.sh | HTTP | REST + SQLite | 5 — CRUD |
| test-graphql.sh | GraphQL | GraphQL + PostgreSQL | 11 — Typed queries, mutations, introspection |
| test-grpc.sh | gRPC | gRPC + PostgreSQL | 4 — Reflection, RPCs |
| test-soap.sh | SOAP/XML | SOAP + PostgreSQL | 5 — WSDL, SOAP operations |
| test-cache.sh | HTTP | REST + Redis + Memory | 4 — SET/GET |
| test-rabbitmq.sh | AMQP | REST + RabbitMQ + PostgreSQL | 2 — Pub/Sub → DB |
| test-kafka.sh | Kafka | REST + Kafka + PostgreSQL | 2 — Pub/Sub → DB |
| test-elasticsearch.sh | HTTP | REST + Elasticsearch | 4 — Index/Search |
| test-s3.sh | HTTP | REST + MinIO | 2 — List files |
| test-files.sh | HTTP | REST + File | 2 — Read/Write |
| test-http-client.sh | HTTP | REST + HTTP Client + Mock | 3 — Outbound capture |
| test-transforms.sh | HTTP | REST | 6 — CEL functions |
| test-validation.sh | HTTP | REST | 3 — Type validation |
| test-steps.sh | HTTP | REST + PostgreSQL | 2 — Multi-step orchestration |
| test-error-handling.sh | HTTP | REST + PostgreSQL | 2 — Retry + DLQ |
| test-rate-limit.sh | HTTP | REST | 2 — Concurrent burst → 429 |
| test-notifications.sh | HTTP | REST + Mock | 5 — Slack, Discord, SMS, Push |
| test-exec.sh | HTTP | REST + Exec | 2 — Command execution |
| test-filter.sh | HTTP | REST | 3 — CEL filter pass/reject |
| test-federation.sh | GraphQL | Cosmo Router + GraphQL | 5 — Federated queries + mutations |
| test-security.sh | HTTP, GraphQL, SOAP | REST + GraphQL + SOAP + File | 29 — Null bytes, control chars, bidi, XXE, path traversal, oversized, deep nesting |
| test-fanout.sh | HTTP, AMQP | REST + RabbitMQ + PostgreSQL | 8 — Source fan-out: REST (2 flows, 1 endpoint) + MQ (2 consumers, 1 queue) |

**27 test suites, 103 assertions**

## Mock Server

A lightweight Go server (`mock-server/`) that captures all incoming HTTP requests:

- `POST/PUT/PATCH/DELETE *` → Captured in memory
- `GET /requests` → Return all captured requests
- `GET /requests?path=/prefix` → Filter by path prefix
- `DELETE /requests` → Clear all
- `GET /health` → Health check

Used by notification connectors and HTTP client tests to verify outbound calls.

## CI

The GitHub Actions workflow (`.github/workflows/integration.yml`) runs on manual trigger. Each test group is a separate collapsible step in the UI:

| Step | Tests (parallel within group) |
|------|-------------------------------|
| Start services | Docker Compose build + wait |
| Health & Metrics | health, metrics |
| Databases | postgres, mysql, mongodb, sqlite |
| Protocols | graphql, grpc, soap, federation |
| Messaging | rabbitmq, kafka |
| Storage & Cache | elasticsearch, s3, files, cache |
| Integration | http-client, transforms, validation, steps, error-handling, notifications, exec, filter |
| Rate Limit | rate-limit (solo) |

Groups use `scripts/run-group.sh` which launches tests in parallel, waits, and aggregates results.

## Directory Structure

```
tests/integration/
├── docker-compose.yml          # All services
├── run.sh                      # Master test runner (parallel by default)
├── config/                     # Mycel HCL configuration
│   ├── config.hcl              # Service config (rate_limit: 50 req/s, burst=200)
│   ├── connectors/             # All connector definitions
│   ├── flows/                  # All flow definitions
│   ├── types/                  # Type schemas
│   └── protos/                 # gRPC proto files
├── cosmo/                      # Cosmo Router federation config
├── mock-server/                # Mock HTTP server (Go)
├── seed/                       # Database init scripts
├── scripts/                    # Test scripts
│   ├── lib.sh                  # Shared assertions (assert_status, assert_contains, etc.)
│   ├── wait-for-services.sh    # Service readiness check
│   ├── run-group.sh            # Parallel group runner (used by CI)
│   └── test-*.sh               # Individual test suites
└── README.md
```
