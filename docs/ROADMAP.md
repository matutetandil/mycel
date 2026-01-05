# Mycel Roadmap

This document tracks the implementation status and future plans for Mycel.

## Current Status: Phase 6 Complete (Notifications)

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

### Phase 4.2 - Synchronization (Spec Ready)
> Full specification: [docs/PHASE-4.2-SYNC.md](./PHASE-4.2-SYNC.md)

- **MQ Headers Access**: `input.body`, `input.headers`, `input.properties` for RabbitMQ/Kafka
- **Lock (Mutex)**: Distributed locks by key with Redis/Memory backends
- **Semaphore**: Limit concurrent executions (e.g., max 10 parallel API calls)
- **Coordinate**: Signal/Wait pattern for dependency coordination
  - Wait for parent entity before processing child
  - Preflight checks against database
  - Configurable timeout behavior (fail/retry/skip/pass)
- **Flow Triggers**: `when` attribute for cron/interval scheduling
  - `when = "0 3 * * *"` (cron)
  - `when = "@every 5m"` (interval)
  - `when = "@daily"` (shortcuts)

### Phase 4.3 - Connector Profiles (Complete)
> Full specification: [docs/PHASE-4.3-PROFILES.md](./PHASE-4.3-PROFILES.md)

- **Multiple backend implementations** for the same logical connector
- **Profile selection** via CEL expression (e.g., `env('PRICE_SOURCE')`)
- **Per-profile transforms** to normalize data from different backends
- **Fallback chains** for automatic failover between profiles
- **ProfiledConnector** wrapper implementing Connector interface
- **Prometheus metrics** for profile usage and fallback tracking
- **Use cases**:
  - Same API, different data sources (Magento vs ERP vs Legacy)
  - Multi-region deployments
  - Read replicas vs primary database
  - Gradual migration between systems

### Phase 5 - Extensibility & Documentation (In Progress)
- ✅ **Aspects (AOP)** for cross-cutting concerns
  - Pattern matching on flows (e.g., `flows/**/create_*.hcl`)
  - When: before, after, around
  - Use cases: audit logging, caching, rate limiting, enrichment
- ✅ **Mock system** for testing
  - JSON-based mock files with conditional responses (CEL)
  - CLI flags: `--mock=connector`, `--no-mock=connector`
  - `mocks {}` block in service configuration
- ✅ **Documentation generation**
  - `mycel export openapi` - OpenAPI 3.0.3 for REST endpoints
  - `mycel export asyncapi` - AsyncAPI 2.6.0 for message queues
  - Flags: `-o`, `-f` (yaml/json), `--base-url`
  - Note: GraphQL has native introspection, no export needed
- ✅ **Custom validators** (Regex/CEL)
  - Regex validators for pattern matching
  - CEL validators for expression-based validation
  - Validator registry and factory
  - Integration with type validation system
- ✅ **Custom validators** with WASM
  - User-defined validation logic in compiled WASM
  - wazero runtime (pure Go, no CGO)
  - Memory management with alloc/free
  - JSON-based input/output
- ✅ **Custom functions** (CEL extensions)
  - WASM functions available in transform expressions
  - Parser for `functions` blocks
  - Dynamic function registration in CEL
  - Support for 0-5 arguments per function
- ✅ **Plugin system**
  - Custom connectors via WASM plugins
  - Plugin manifest (`plugin.hcl`) for metadata and configuration
  - Plugin loader for local directories
  - Plugin registry and factory for runtime integration
  - Git/registry sources planned for future

### Phase 5.1 - Authentication System (Complete)

#### Phase 5.1a - Core Auth System (Complete)
- ✅ JWT token generation and validation with HMAC/RSA
- ✅ Token rotation with refresh tokens
- ✅ Password hashing with Argon2id
- ✅ Password validation with configurable policies
- ✅ Session management (create, validate, revoke)
- ✅ Storage interfaces (User, Session, Token, BruteForce stores)
- ✅ Memory implementations for all stores
- ✅ Configuration presets (strict, standard, relaxed, development)

#### Phase 5.1b - Security Features (Complete)
- ✅ Redis storage (Session, Token, BruteForce stores)
- ✅ PostgreSQL storage (User, Password History, Audit)
- ✅ MySQL storage (User, Password History, Audit, Session, Token)
- ✅ Brute force protection with progressive delays
- ✅ Session cleanup service with idle timeout
- ✅ Per-endpoint rate limiting
- ✅ Audit logging

#### Phase 5.1c - Multi-Factor Authentication (Complete)
- ✅ TOTP (RFC 6238) with QR code generation
- ✅ Recovery codes with secure hashing
- ✅ WebAuthn/Passkeys support
- ✅ Manager integration for MFA flows
- ✅ Full test coverage

#### Phase 5.1d - SSO & Social Login (Complete)
- ✅ OAuth2 service with authorization code flow
- ✅ OpenID Connect with discovery documents
- ✅ Social providers: Google, GitHub, Apple
- ✅ Enterprise OIDC for Okta, Azure AD, Auth0
- ✅ Account linking service with configurable strategies
- ✅ State management with expiration
- ✅ Token refresh support
- ✅ Full test coverage

### Phase 6 - Notifications (Complete)
- ✅ **Webhooks** - Inbound/outbound with signature verification
- ✅ **Email** - SMTP, SendGrid, AWS SES
- ✅ **Slack** - Webhook and Bot API
- ✅ **Discord** - Webhook and Bot API
- ✅ **SMS** - Twilio, AWS SNS
- ✅ **Push** - Firebase Cloud Messaging, Apple Push Notification service

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
