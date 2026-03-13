# Mycel Documentation

**Declarative microservices through configuration, not code.**

Mycel is a single binary runtime that reads HCL configuration files and exposes production-ready microservices. Same binary, different configuration = different service. No code required.

```
Connector (source) ──> Flow ──> Connector (target)
```

Every Mycel service automatically includes health checks, Prometheus metrics, hot reload, and rate limiting — zero configuration needed.

---

## Getting Started

New to Mycel? Start here.

| Document | Description |
|----------|-------------|
| [Introduction](getting-started/introduction.md) | What Mycel is, the core model, and how it differs from traditional microservice development |
| [Installation](getting-started/installation.md) | Docker, Go binary, Helm, and Docker Compose setup |
| [Quick Start](getting-started/quick-start.md) | Build and run your first Mycel service in 5 minutes |

---

## Core Concepts

The foundational building blocks of every Mycel service.

| Document | Description |
|----------|-------------|
| [Connectors](core-concepts/connectors.md) | Bidirectional adapters: REST, database, queues, gRPC, WebSocket, file, and more |
| [Flows](core-concepts/flows.md) | The unit of work — wiring connectors together with transforms, validation, caching, and error handling |
| [Transforms](core-concepts/transforms.md) | CEL-based data transformations and the complete built-in function reference |
| [Types](core-concepts/types.md) | Schema validation with field constraints, custom validators, and federation directives |
| [Environments](core-concepts/environments.md) | Environment variables, `.env` files, and per-environment configuration overlays |

---

## Guides

Step-by-step guides for specific features and patterns.

| Document | Description |
|----------|-------------|
| [Common Use Cases](guides/use-cases.md) | Complete examples: CRUD + Slack notification, welcome emails, audit logs, caching, event publishing, error alerting, and more |
| [Multi-Step Flows](guides/multi-step-flows.md) | Orchestrate data from multiple sources in a single flow using steps, enrich, and after blocks |
| [Caching](guides/caching.md) | In-memory and Redis caching: inline cache, named cache, invalidation, and deduplication |
| [Synchronization](guides/synchronization.md) | Distributed locks, semaphores, and coordinate (signal/wait) for concurrent flow safety |
| [Sagas & State Machines](guides/sagas.md) | Distributed transactions with automatic compensation, entity lifecycle management, and long-running workflows |
| [Real-Time](guides/real-time.md) | WebSocket, Server-Sent Events, Change Data Capture, and GraphQL subscriptions |
| [Batch Processing](guides/batch-processing.md) | Process large datasets in chunks: migrations, ETL, reindexing |
| [Notifications](guides/notifications.md) | Email, Slack, Discord, SMS, push notifications, and webhooks |
| [Auth](guides/auth.md) | JWT authentication, presets, MFA (TOTP, WebAuthn), and SSO/social login |
| [Security](guides/security.md) | Secure-by-default sanitization, XXE protection, SQL injection prevention, and WASM sanitizers |
| [Error Handling](guides/error-handling.md) | Retry with backoff, dead letter queues, circuit breakers, custom error responses, and on_error aspects |
| [Format System](guides/format-system.md) | Multi-format support (JSON, XML) at connector, flow, and step level |
| [Extending Mycel](guides/extending.md) | Custom validators, WASM functions, mocks for testing, and aspect patterns |
| [Debugging](guides/debugging.md) | Trace flows step-by-step, interactive breakpoints, dry-run, DAP server for IDE debugging (VS Code, IntelliJ, Neovim) |
| [Observability](guides/observability.md) | Prometheus metrics, Grafana dashboards, alerting rules, and monitoring setup |
| [Troubleshooting](guides/troubleshooting.md) | Common errors, diagnosis steps, and solutions for startup, database, flow, and deployment issues |

---

## Reference

Complete syntax reference for configuration and CLI.

| Document | Description |
|----------|-------------|
| [Configuration Reference](reference/configuration.md) | Complete HCL syntax for every block type: service, connector, flow, type, transform, aspect, auth, saga, state_machine |
| [CEL Functions](reference/cel-functions.md) | All built-in transform functions with signatures and examples |
| [CLI Reference](reference/cli.md) | All commands and flags: start, validate, check, export, plugin |
| [API Endpoints](reference/api-endpoints.md) | Built-in HTTP endpoints: health, metrics, workflow, auth |

---

## Deployment

Running Mycel in production.

| Document | Description |
|----------|-------------|
| [Docker](deployment/docker.md) | Docker run, Docker Compose, custom images, environment variables |
| [Kubernetes](deployment/kubernetes.md) | Helm chart, manual deployment, health probes, HPA, Prometheus scraping |
| [Production Guide](deployment/production.md) | Security checklist, database pooling, logging, monitoring, common issues |

---

## Advanced

Advanced features for complex requirements.

| Document | Description |
|----------|-------------|
| [GraphQL Federation](advanced/federation.md) | Building federated subgraphs: entities, cross-subgraph references, subscriptions, gateway setup |
| [WASM](advanced/wasm.md) | Building WASM modules in Rust, Go/TinyGo, C, C++, AssemblyScript, and Zig |
| [Plugins](advanced/plugins.md) | Extending Mycel with WASM plugins: connectors, validators, sanitizers |
| [Integration Patterns](advanced/integration-patterns.md) | Common architectural patterns: protocol bridge, event sourcing, CDC pipeline, saga orchestration |

---

## Connector Catalog

Documentation for each connector type.

| Connector | Type | Description |
|-----------|------|-------------|
| [REST](connectors/rest.md) | Server + Client | HTTP endpoints and API calls |
| [Database](connectors/database.md) | Read + Write | PostgreSQL, MySQL, SQLite, MongoDB |
| [GraphQL](connectors/graphql.md) | Server + Client | Schema-based API with Federation v2 |
| [Queue](connectors/message-queues.md) | Consumer + Producer | RabbitMQ, Kafka, and Redis Pub/Sub |
| [MQTT](connectors/mqtt.md) | Subscribe + Publish | IoT messaging (QoS 0/1/2) |
| [FTP](connectors/ftp.md) | Read + Write | FTP, FTPS, and SFTP |
| [gRPC](connectors/grpc.md) | Server + Client | Protocol Buffers RPC |
| [TCP](connectors/tcp.md) | Server + Client | JSON, msgpack, NestJS protocols |
| [Cache](connectors/cache.md) | Read + Write | Memory and Redis |
| [File](connectors/filesystem.md) | Read + Write | Local filesystem with watch mode |
| [S3](connectors/s3.md) | Read + Write | AWS S3 and MinIO |
| [WebSocket](connectors/websocket.md) | Bidirectional | Real-time with rooms and per-user targeting |
| [SSE](connectors/sse.md) | Server Push | Server-Sent Events with rooms |
| [CDC](connectors/cdc.md) | Stream | PostgreSQL WAL change capture |
| [Elasticsearch](connectors/elasticsearch.md) | Write | Search and analytics |
| [OAuth](connectors/oauth.md) | Server | Social login and OIDC |
| [SOAP](connectors/soap.md) | Server + Client | SOAP 1.1/1.2 web services |
| [Email](connectors/email.md) | Send | SMTP, SendGrid, AWS SES |
| [Slack](connectors/slack.md) | Send | Slack Bot API |
| [Discord](connectors/discord.md) | Send | Discord Bot API |
| [SMS](connectors/sms.md) | Send | Twilio, AWS SNS |
| [Push](connectors/push.md) | Send | FCM, APNs |
| [Webhook](connectors/webhook.md) | Send | HTTP callbacks |

---

## Project

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Design decisions: why HCL, why CEL, why WASM, why Go, trade-offs |
| [Roadmap](ROADMAP.md) | Implementation status, phase history, and pending work |
| [Changelog](../CHANGELOG.md) | Version history with detailed change descriptions |
