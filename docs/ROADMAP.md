# Mycel Roadmap

This document tracks the implementation status and future plans for Mycel.

## Current Status: Phase 4 Complete

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
| Webhooks  | 🔜 | 🔜 | 6 |
| Email     | - | 🔜 | 6 |
| Slack     | - | 🔜 | 6 |
| Discord   | - | 🔜 | 6 |
| SMS       | - | 🔜 | 6 |
| Push      | - | 🔜 | 6 |

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
| Auth System | 🔜 | 5 |
| Aspects (AOP) | 🔜 | 5 |
| Custom Validators (WASM) | 🔜 | 5 |
| Plugins System | 🔜 | 5 |
| Mocks/Testing | 🔜 | 5 |
| OpenAPI Export | 🔜 | 5 |
| GraphQL Schema Export | 🔜 | 5 |

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

### Phase 5 - Enterprise Features (Planned)
- Enterprise-grade authentication system
  - JWT with token rotation
  - MFA (TOTP, WebAuthn, Passkeys)
  - SSO (SAML, OIDC)
  - Social login with account linking
  - Session management
- Aspects (AOP) for cross-cutting concerns
- Custom validators with WASM
- Plugin system
- Mock system for testing
- Documentation generation (OpenAPI, GraphQL schema)

### Phase 6 - Notifications (Planned)
- Webhook support (inbound/outbound)
- Email notifications
- Slack integration
- Discord integration
- SMS notifications
- Push notifications

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
