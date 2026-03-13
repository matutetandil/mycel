# Mycel Roadmap

This document tracks the implementation status and future plans for Mycel.

## Current Status: v1.7.0 — Phase 12 Complete

All core features are implemented and production-ready. The roadmap below reflects the complete implementation history.

## Connector Support

| Connector | Input (Server) | Output (Client) | Phase |
|-----------|----------------|-----------------|-------|
| REST      | ✅ | ✅ | 1-2 |
| SQLite    | ✅ | ✅ | 1 |
| PostgreSQL| ✅ | ✅ | 2 |
| MySQL     | ✅ | ✅ | 3.2 |
| MongoDB   | ✅ | ✅ | 3.2 |
| TCP       | ✅ | ✅ | 2.5 |
| RabbitMQ  | ✅ | ✅ | 3 |
| Kafka     | ✅ | ✅ | 3 |
| Exec      | ✅ | ✅ | 3 |
| GraphQL   | ✅ | ✅ | 3 |
| Cache (Memory) | ✅ | ✅ | 3.3 |
| Cache (Redis)  | ✅ | ✅ | 3.3 |
| gRPC      | ✅ | ✅ | 4 |
| Files     | ✅ | ✅ | 4 |
| S3/MinIO  | ✅ | ✅ | 4 |
| Webhooks  | ✅ | ✅ | 6 |
| Email     | - | ✅ | 6 |
| Slack     | - | ✅ | 6 |
| Discord   | - | ✅ | 6 |
| SMS       | - | ✅ | 6 |
| Push      | - | ✅ | 6 |
| WebSocket | ✅ | ✅ | 10 |
| SSE       | ✅ | ✅ | 10 |
| CDC       | ✅ | - | 10 |
| Elasticsearch | - | ✅ | 11 |
| OAuth     | ✅ | - | 11 |
| SOAP      | ✅ | ✅ | 12+ |

## Feature Support

| Feature | Status | Phase |
|---------|--------|-------|
| CEL Transforms | ✅ | 2 |
| Type Validation | ✅ | 2 |
| Environment Variables | ✅ | 2 |
| Data Enrichment | ✅ | 3 |
| Raw SQL Queries | ✅ | 3.2 |
| GraphQL Federation v2 | ✅ | 3 |
| Named Caches | ✅ | 3.3 |
| Cache Invalidation | ✅ | 3.3 |
| Health Checks | ✅ | 4 |
| Prometheus Metrics | ✅ | 4 |
| Rate Limiting | ✅ | 4 |
| Circuit Breaker | ✅ | 4 |
| Hot Reload | ✅ | 4 |
| MQ Headers Access | ✅ | 4.2 |
| Distributed Locks | ✅ | 4.2 |
| Semaphores | ✅ | 4.2 |
| Coordinate (Signal/Wait) | ✅ | 4.2 |
| Flow Triggers (Cron) | ✅ | 4.2 |
| Connector Profiles | ✅ | 4.3 |
| Aspects (AOP) | ✅ | 5 |
| Mocks/Testing | ✅ | 5 |
| OpenAPI Export | ✅ | 5 |
| AsyncAPI Export | ✅ | 5 |
| Custom Validators (Regex/CEL) | ✅ | 5 |
| Custom Validators (WASM) | ✅ | 5 |
| Custom Functions (WASM) | ✅ | 5 |
| Plugins System | ✅ | 5 |
| Auth System Core | ✅ | 5.1a |
| Auth Security Features | ✅ | 5.1b |
| Auth MFA (TOTP/WebAuthn) | ✅ | 5.1c |
| Auth SSO/Social | ✅ | 5.1d |
| Notifications (6 channels) | ✅ | 6 |
| Step Orchestration | ✅ | 7 |
| Filter Rejection Policies | ✅ | 7 |
| Dedupe | ✅ | 7 |
| GraphQL Query Optimization | ✅ | 8 |
| GraphQL DataLoader | ✅ | 8 |
| GraphQL Subscriptions (server) | ✅ | 9 |
| GraphQL Subscription Client | ✅ | 9 |
| WebSocket Connector | ✅ | 10 |
| CDC (PostgreSQL WAL) | ✅ | 10 |
| SSE Connector | ✅ | 10 |
| Elasticsearch Connector | ✅ | 11 |
| OAuth Connector | ✅ | 11 |
| Batch Processing | ✅ | 11 |
| Sagas | ✅ | 12 |
| State Machines | ✅ | 12 |
| Long-Running Workflows | ✅ | 12 |
| SOAP Connector | ✅ | 12+ |
| Codec System (JSON/XML) | ✅ | 12+ |
| File Watch Mode | ✅ | 12+ |
| Error Handling Guide | ✅ | 12+ |
| Standalone Admin Server | ✅ | 12+ |
| .env File Support | ✅ | 12+ |
| WASM Documentation | ✅ | 12+ |
| Integration Test Suite | ✅ | 12+ |
| Typed GraphQL DTOs | ✅ | 12+ |

## Phase Details

### Phase 1 - Core Runtime (Complete)
- REST Server connector
- SQLite database connector
- Basic flow execution
- CLI commands (start, validate, check)
- ASCII banner

### Phase 2 - Extended Connectors (Complete)
- HTTP Client connector with OAuth2, Bearer, API Key, Basic auth
- PostgreSQL with connection pooling and SSL
- CEL-powered transformation engine
- Type validation with schemas
- Environment variable support

### Phase 2.5 - TCP Protocol (Complete)
- TCP Server and Client connectors
- Length-prefixed framing
- Codecs: JSON, msgpack, raw
- NestJS protocol compatibility

### Phase 3 - Message Queues & More (Complete)
- RabbitMQ with topic patterns and concurrent workers
- Kafka with consumer groups and SASL auth
- Exec connector (local and SSH)
- GraphQL Server and Client
- GraphQL Federation v2 support
- Data enrichment system

### Phase 3.2 - SQL/NoSQL Databases (Complete)
- MySQL with connection pooling
- MongoDB with full NoSQL operations
- Raw SQL query support for complex joins

### Phase 3.3 - Caching (Complete)
- Memory cache with LRU eviction
- Redis cache with connection pooling
- Named cache definitions
- Cache invalidation patterns

### Phase 4 - Operations & Resilience (Complete)
- Health check endpoints (`/health`, `/health/live`, `/health/ready`)
- Prometheus metrics (`/metrics`)
- gRPC Server and Client
- Files connector (local filesystem)
- S3/MinIO connector
- Rate limiting with token bucket algorithm
- Circuit breaker pattern
- Hot reload with file watching

### Phase 4.1 - Runtime Configuration (Complete)
- Environment variables for runtime configuration:
  - `MYCEL_ENV`: Environment selection (development, staging, production)
  - `MYCEL_LOG_LEVEL`: Log level (debug, info, warn, error)
  - `MYCEL_LOG_FORMAT`: Log format (text, json)
- CLI flags override environment variables
- Docker path standardized to `/etc/mycel`
- JSON logging for production environments

### Phase 4.2 - Synchronization (Complete)
- ✅ **MQ Headers Access**: `input.body`, `input.headers`, `input.properties` for RabbitMQ/Kafka
- ✅ **Lock (Mutex)**: Distributed locks by key with Redis/Memory backends
- ✅ **Semaphore**: Limit concurrent executions (e.g., max 10 parallel API calls)
- ✅ **Coordinate**: Signal/Wait pattern for dependency coordination
  - Wait for parent entity before processing child
  - Preflight checks against database
  - Configurable timeout behavior (fail/retry/skip/pass)
- ✅ **Flow Triggers**: `when` attribute for cron/interval scheduling
  - `when = "0 3 * * *"` (cron)
  - `when = "@every 5m"` (interval)
  - `when = "@daily"` (shortcuts)
- ✅ **SyncManager**: Unified manager for sync primitives integrated with flow execution
- ✅ **Scheduler**: Cron-based flow triggers integrated with runtime

### Phase 4.3 - Connector Profiles (Complete)
- Multiple backend implementations for the same logical connector
- Profile selection via CEL expression (e.g., `env('PRICE_SOURCE')`)
- Per-profile transforms to normalize data from different backends
- Fallback chains for automatic failover between profiles
- ProfiledConnector wrapper implementing Connector interface
- Prometheus metrics for profile usage and fallback tracking

### Phase 5 - Extensibility (Complete)
- ✅ **Aspects (AOP)** for cross-cutting concerns
  - Pattern matching on flow names (e.g., `create_*`, `update_*`)
  - When: before, after, around, on_error
  - Use cases: audit logging, caching, rate limiting, enrichment
- ✅ **Mock system** for testing
  - JSON-based mock files with conditional responses (CEL)
  - CLI flags: `--mock=connector`, `--no-mock=connector`
- ✅ **Documentation generation**
  - `mycel export openapi` - OpenAPI 3.0.3 for REST endpoints
  - `mycel export asyncapi` - AsyncAPI 2.6.0 for message queues
- ✅ **Custom validators** (Regex/CEL/WASM)
- ✅ **Custom functions** (WASM CEL extensions, 0-5 arguments)
- ✅ **Plugin system** (WASM plugins, auto-install on start)

### Phase 5.1 - Authentication System (Complete)

#### Phase 5.1a - Core Auth System
- ✅ JWT token generation and validation with HMAC/RSA
- ✅ Token rotation with refresh tokens
- ✅ Password hashing with Argon2id
- ✅ Session management (create, validate, revoke)
- ✅ Configuration presets (strict, standard, relaxed, development)

#### Phase 5.1b - Security Features
- ✅ Redis/PostgreSQL/MySQL storage backends
- ✅ Brute force protection with progressive delays
- ✅ Session cleanup service with idle timeout
- ✅ Per-endpoint rate limiting
- ✅ Audit logging

#### Phase 5.1c - Multi-Factor Authentication
- ✅ TOTP (RFC 6238) with QR code generation
- ✅ Recovery codes with secure hashing
- ✅ WebAuthn/Passkeys support

#### Phase 5.1d - SSO & Social Login
- ✅ OAuth2 service with authorization code flow
- ✅ OpenID Connect with discovery documents
- ✅ Social providers: Google, GitHub, Apple
- ✅ Enterprise OIDC for Okta, Azure AD, Auth0
- ✅ Account linking service with configurable strategies

### Phase 6 - Notifications (Complete)
- ✅ **Webhooks** - Inbound/outbound with signature verification
- ✅ **Email** - SMTP, SendGrid, AWS SES
- ✅ **Slack** - Webhook and Bot API
- ✅ **Discord** - Webhook and Bot API
- ✅ **SMS** - Twilio, AWS SNS
- ✅ **Push** - Firebase Cloud Messaging, Apple Push Notification service
- ✅ **Configurable api_url** for all notification connectors

### Phase 7 - Flow Orchestration (Complete)
- ✅ **Step blocks**: sequential multi-step execution with `when` conditions
- ✅ **Filter in from**: CEL condition + rejection policy (`ack`/`reject`/`requeue`) with dedup tracking
- ✅ **Conditional steps**: skip steps based on CEL expressions
- ✅ **Array transforms**: `first`, `last`, `unique`, `pluck`, `sort_by`, `sum`, `avg`
- ✅ **on_error with retry + DLQ**: exponential backoff, dead letter queues
- ✅ **merge / omit / pick**: object manipulation helpers in transforms
- ✅ **multi-to**: fan-out to multiple targets
- ✅ **dedupe**: deduplication with storage backend, TTL, and `on_duplicate` policy

### Phase 8 - GraphQL Query Optimization (Complete)
- ✅ **Field Analyzer**: parses incoming queries to determine requested fields
- ✅ **Result Pruner**: removes unrequested fields from responses
- ✅ **CEL functions**: `has_field()`, `field_requested()`, `requested_fields()`, `requested_top_fields()`
- ✅ **Database Optimizer**: generates targeted SELECT statements
- ✅ **Step Optimizer**: skips steps whose results are not requested
- ✅ **DataLoader**: batches N+1 queries via `graph-gophers/dataloader`

### Phase 9 - GraphQL Federation Complete (Complete)
- ✅ **Subscription types**: `subscription` flows with publish targets
- ✅ **Flow-triggered publish**: MQ/CDC events push to subscribed clients
- ✅ **Per-user filtering**: subscribers receive only their events
- ✅ **Auto entity resolution**: `_entities` routing via `entity =` on flow
- ✅ **GraphQL subscription client**: `graphql-ws` protocol, auto-reconnect
- ✅ **Auto-enable Federation v2**: gateway compatibility without explicit config

### Phase 10 - Real-Time & Event-Driven (Complete)
- ✅ **WebSocket Connector**: bidirectional, rooms, broadcast, per-user targeting
- ✅ **CDC (Change Data Capture)**: PostgreSQL WAL with `pgoutput`, wildcard table matching
- ✅ **SSE (Server-Sent Events)**: unidirectional push, rooms, heartbeat, CORS

### Phase 11 - Specialized Connectors (Complete)
- ✅ **Elasticsearch**: search, get, count, aggregate, index, update, delete, bulk
- ✅ **OAuth Connector**: Google, GitHub, Apple, OIDC, custom — authorization code flow
- ✅ **Batch Processing**: `batch` block in flows, chunk-based processing, params, `on_error`

### Phase 12 - Enterprise Workflows (Complete)
- ✅ **Saga Pattern**: distributed transactions with forward steps + reverse compensation
- ✅ **State Machines**: entity lifecycle with guards, actions, final states
- ✅ **Long-Running Workflows**: `delay` and `await` steps, workflow persistence, REST API

### Phase 12+ - Polish & Infrastructure (Complete)
- ✅ **SOAP Connector**: client + server, SOAP 1.1/1.2, WSDL auto-generation
- ✅ **Codec System**: JSON and XML codecs, `format` declarations, Content-Type auto-detection
- ✅ **File Watch Mode**: polling-based watcher, glob patterns
- ✅ **Standalone Admin Server**: health + metrics on `:9090` when no REST connector
- ✅ **.env File Support**: auto-load `.env` from config directory or cwd
- ✅ **Error Handling Guide**: all 9 error handling layers documented
- ✅ **On-error Aspects**: `when = "on_error"` in aspect blocks
- ✅ **Custom Error Responses**: `error_response` block in `error_handling`
- ✅ **PostgreSQL INSERT RETURNING**: full created row returned
- ✅ **Typed GraphQL DTOs**: `returns` attribute, auto-generated input types
- ✅ **Integration Test Suite**: 25 test suites, 86+ assertions, parallel execution
- ✅ **WASM Documentation**: 6 languages with examples (Rust, Go/TinyGo, C, C++, AssemblyScript, Zig)
- ✅ **Docs Reorganization**: hierarchical structure at `docs/{getting-started,core-concepts,guides,reference,deployment,advanced}`

## Pending (Low Priority)

| Item | Notes |
|------|-------|
| Environment-aware behavior | Log defaults, GraphiQL, CORS, stack traces by environment |
| Mycel LSP | Language Server Protocol for editor autocompletion and validation |
| PDF generation | File connector extension |
| CSV/Excel export | File connector extension |
| Long-running process visualization | Workflow state visualization UI |
| Workflow versioning | Schema migrations for long-running workflow state |

## Philosophy

> Configuration, not code. You define WHAT you want, Mycel handles HOW.

Mycel is designed to be:
- **Declarative**: All configuration is HCL
- **Pure Go**: No CGO dependencies
- **Standard protocols**: REST, GraphQL, gRPC, etc.
- **Hot reloadable**: Changes without restart
- **Production ready**: Observability, resilience, security

## Contributing

Contributions are welcome! See the main README for guidelines.
