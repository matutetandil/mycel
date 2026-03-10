# Connector Catalog

Mycel connectors are bidirectional adapters between your service and external systems. Each connector can act as a **source** (receives data that triggers a flow) or a **target** (destination where a flow writes data).

For the general connector concept and how they fit into the Mycel model, see [Concepts — Connectors](../CONCEPTS.md#connectors).

---

## Transport & Protocol

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [REST](rest.md) | `rest` / `http` | Expose HTTP endpoints or call external REST APIs |
| [GraphQL](graphql.md) | `graphql` | Expose a GraphQL schema or query external GraphQL APIs |
| [gRPC](grpc.md) | `grpc` | Expose gRPC services or call external gRPC endpoints |
| [TCP](tcp.md) | `tcp` | Raw TCP server/client with JSON, msgpack, or NestJS codec |
| [SOAP](soap.md) | `soap` | Call or expose SOAP/XML web services (SOAP 1.1/1.2) |
| [WebSocket](websocket.md) | `websocket` | Bidirectional real-time communication with rooms |
| [SSE](sse.md) | `sse` | Unidirectional server-to-client push over HTTP |

## Database

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [Database](database.md) | `database` | SQLite, PostgreSQL, MySQL, MongoDB (driver-based) |
| [Elasticsearch](elasticsearch.md) | `elasticsearch` | Full-text search and analytics over Elasticsearch REST API |

## Event & Streaming

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [Message Queues](message-queues.md) | `mq` | RabbitMQ, Kafka, and Redis Pub/Sub producers/consumers |
| [MQTT](mqtt.md) | `mqtt` | IoT messaging with QoS 0/1/2 and topic wildcards |
| [CDC](cdc.md) | `cdc` | Real-time database change streaming via logical replication |

## Storage

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [Filesystem](filesystem.md) | `file` (driver: `local`) | Read and write local files |
| [S3](s3.md) | `file` (driver: `s3`) | AWS S3 and S3-compatible object storage |
| [FTP/SFTP](ftp.md) | `ftp` | Remote file transfer over FTP, FTPS, and SFTP |
| [Cache](cache.md) | `cache` | In-memory (LRU) and Redis caching |

## Notifications

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [Notifications](notifications.md) | `email` / `slack` / `discord` / `sms` / `push` / `webhook` | Email, Slack, Discord, SMS, Push, Webhook |

## Auth

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [OAuth](oauth.md) | `oauth` | Social login with Google, GitHub, Apple, OIDC, custom |

## System

| Connector | HCL `type` | Description |
|-----------|------------|-------------|
| [Exec](exec.md) | `exec` | Execute shell commands (local or SSH) |
| [Profile](profile.md) | `profiled` | Multi-backend routing with fallback and per-profile transforms |
