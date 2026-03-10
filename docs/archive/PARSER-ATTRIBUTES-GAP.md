# Parser Attributes Gap Analysis

This document compares attributes supported by the HCL parser (`internal/parser/connector.go`) versus attributes expected by connector factories.

## Legend

- ✅ = Supported in Parser
- ❌ = NOT supported in Parser (but factory expects it)
- 🔸 = Supported in Parser but NOT used by factory

---

## Global Connector Attributes

These attributes are defined in `parseConnectorBlock()` schema:

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `type` | ✅ | ✅ | Required |
| `driver` | ✅ | ✅ | Required |
| `host` | ✅ | ✅ | |
| `port` | ✅ | ✅ | |
| `database` | ✅ | ✅ | |
| `user` | ✅ | ✅ | |
| `username` | ✅ | ✅ | Alias for user (MQ) |
| `password` | ✅ | ✅ | |
| `base_url` | ✅ | ✅ | HTTP client |
| `timeout` | ✅ | ✅ | |
| `retry_count` | ✅ | ✅ | |
| `endpoint` | ✅ | ✅ | GraphQL |
| `playground` | ✅ | ✅ | GraphQL |
| `playground_path` | ✅ | 🔸 | In parser but not used |
| `protocol` | ✅ | ✅ | TCP |
| `max_connections` | ✅ | ✅ | TCP |
| `read_timeout` | ✅ | ✅ | TCP |
| `write_timeout` | ✅ | ✅ | TCP |
| `brokers` | ✅ | ✅ | Kafka |
| `vhost` | ✅ | ✅ | RabbitMQ |
| `command` | ✅ | ✅ | Exec |
| `args` | ✅ | ✅ | Exec |
| `shell` | ✅ | ✅ | Exec |
| `env` | ✅ | ✅ | Exec |
| `working_dir` | ✅ | ✅ | Exec |
| `input_format` | ✅ | ✅ | Exec |
| `output_format` | ✅ | ✅ | Exec |
| `retry_delay` | ✅ | ✅ | Exec |
| `select` | ✅ | ✅ | Profiles |
| `default` | ✅ | ✅ | Profiles |
| `fallback` | ✅ | ✅ | Profiles |

---

## Cache Connector

**Factory:** `internal/connector/cache/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `mode` | ❌ | ✅ | standalone/cluster/sentinel |
| `url` | ❌ | ✅ | Redis connection URL |
| `prefix` | ❌ | ✅ | Key prefix for namespacing |
| `max_items` | ❌ | ✅ | Memory cache max items |
| `eviction` | ❌ | ✅ | Eviction policy (lru) |
| `default_ttl` | ❌ | ✅ | Default TTL for entries |

### Nested Blocks (Cache)

| Block | Parser | Notes |
|-------|--------|-------|
| `pool` | ✅ | Connection pool settings |
| `cluster` | ❌ | Redis Cluster configuration |
| `sentinel` | ❌ | Redis Sentinel configuration |

**Cluster block attributes:** `nodes`, `password`, `max_redirects`, `route_by_latency`, `route_randomly`, `read_only`

**Sentinel block attributes:** `master_name`, `nodes`, `password`, `master_password`, `db`, `route_by_latency`, `route_randomly`, `replica_only`

---

## gRPC Connector

**Factory:** `internal/connector/grpc/factory.go`

### Server Mode

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `proto_path` | ❌ | ✅ | Path to .proto files directory |
| `proto_files` | ❌ | ✅ | Specific .proto files to load |
| `reflection` | ❌ | ✅ | Enable gRPC reflection |
| `max_recv_mb` | ❌ | ✅ | Max receive message size (MB) |
| `max_send_mb` | ❌ | ✅ | Max send message size (MB) |

### Client Mode

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `target` | ❌ | ✅ | Server address (host:port) |
| `proto_path` | ❌ | ✅ | Path to .proto files |
| `proto_files` | ❌ | ✅ | Specific .proto files |
| `insecure` | ❌ | ✅ | Disable TLS |
| `wait_for_ready` | ❌ | ✅ | Wait for server ready |
| `max_recv_mb` | ❌ | ✅ | Max receive message size |
| `max_send_mb` | ❌ | ✅ | Max send message size |

### Nested Blocks (gRPC)

| Block | Parser | Notes |
|-------|--------|-------|
| `tls` | ✅ | TLS configuration |
| `auth` | ✅ | Authentication |
| `keep_alive` | ❌ | Keep-alive settings |
| `load_balancing` | ❌ | Load balancing config |

**keep_alive attributes:** `time`, `timeout`

**load_balancing attributes:** `policy`, `targets`, `health_check`

---

## File Connector

**Factory:** `internal/connector/file/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `base_path` | ❌ | ✅ | Base directory for operations |
| `format` | ❌ | ✅ | Default format (json/csv/text/binary) |
| `watch` | ❌ | ✅ | Enable file watching |
| `watch_interval` | ❌ | ✅ | Polling interval |
| `create_dirs` | ❌ | ✅ | Auto-create directories |
| `permissions` | ❌ | ✅ | Default file permissions |

---

## S3 Connector

**Factory:** `internal/connector/s3/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `bucket` | ❌ | ✅ | S3 bucket name |
| `region` | ❌ | ✅ | AWS region |
| `endpoint` | ❌ | ✅ | Custom endpoint (MinIO) |
| `access_key` | ❌ | ✅ | AWS access key ID |
| `secret_key` | ❌ | ✅ | AWS secret access key |
| `session_token` | ❌ | ✅ | AWS session token (STS) |
| `prefix` | ❌ | ✅ | Key prefix |
| `format` | ❌ | ✅ | Default file format |
| `use_path_style` | ❌ | ✅ | Use path-style URLs (MinIO) |

---

## MongoDB Connector

**Factory:** `internal/connector/database/mongodb/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `uri` | ❌ | ✅ | MongoDB connection URI |
| `host` | ✅ | ✅ | Alternative to URI |
| `port` | ✅ | ✅ | Alternative to URI |
| `user` | ✅ | ✅ | Alternative to URI |
| `password` | ✅ | ✅ | Alternative to URI |
| `database` | ✅ | ✅ | Database name |

---

## PostgreSQL Connector

**Factory:** `internal/connector/database/postgres/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `sslmode` | ❌ | ✅ | SSL mode |
| `ssl_mode` | ❌ | ✅ | Alias for sslmode |
| `replicas` | ❌ | ✅ | Read replicas configuration |
| `use_replicas` | ❌ | ✅ | Enable read replicas |

---

## MySQL Connector

**Factory:** `internal/connector/database/mysql/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `charset` | ❌ | ✅ | Character set (utf8mb4) |
| `replicas` | ❌ | ✅ | Read replicas configuration |
| `use_replicas` | ❌ | ✅ | Enable read replicas |

---

## Email Connector (Notifications)

**Factory:** `internal/connector/email/factory.go`

### Common

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `from` | ❌ | ✅ | From email address |
| `from_name` | ❌ | ✅ | From display name |
| `reply_to` | ❌ | ✅ | Reply-to address |

### SMTP Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `host` | ✅ | ✅ | SMTP host |
| `port` | ✅ | ✅ | SMTP port |
| `username` | ✅ | ✅ | SMTP username |
| `password` | ✅ | ✅ | SMTP password |
| `tls` | ❌ | ✅ | TLS mode (starttls/tls/none) |
| `pool_size` | ❌ | ✅ | Connection pool size |

### SendGrid Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `api_key` | ❌ | ✅ | SendGrid API key |
| `endpoint` | ✅ | ✅ | API endpoint |

### AWS SES Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `region` | ❌ | ✅ | AWS region |
| `access_key_id` | ❌ | ✅ | AWS access key |
| `secret_access_key` | ❌ | ✅ | AWS secret key |
| `configuration_set` | ❌ | ✅ | SES configuration set |

---

## Slack Connector (Notifications)

**Factory:** `internal/connector/slack/connector.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `webhook_url` | ❌ | ✅ | Slack webhook URL |
| `token` | ❌ | ✅ | Bot token |
| `channel` | ❌ | ✅ | Default channel |
| `username` | ✅ | ✅ | Bot username |
| `icon_emoji` | ❌ | ✅ | Icon emoji |
| `icon_url` | ❌ | ✅ | Icon URL |

---

## Discord Connector (Notifications)

**Factory:** `internal/connector/discord/connector.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `webhook_url` | ❌ | ✅ | Discord webhook URL |
| `bot_token` | ❌ | ✅ | Bot token |
| `channel_id` | ❌ | ✅ | Default channel ID |
| `username` | ✅ | ✅ | Override username |
| `avatar_url` | ❌ | ✅ | Override avatar |

---

## SMS Connector (Notifications)

**Factory:** `internal/connector/sms/connector.go`

### Twilio Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `account_sid` | ❌ | ✅ | Twilio account SID |
| `auth_token` | ❌ | ✅ | Twilio auth token |
| `from` | ❌ | ✅ | From phone number |

### AWS SNS Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `region` | ❌ | ✅ | AWS region |
| `access_key_id` | ❌ | ✅ | AWS access key |
| `secret_access_key` | ❌ | ✅ | AWS secret key |
| `sender_id` | ❌ | ✅ | Sender ID |
| `sms_type` | ❌ | ✅ | Transactional/Promotional |

---

## Push Connector (Notifications)

**Factory:** `internal/connector/push/connector.go`

### FCM Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `server_key` | ❌ | ✅ | FCM server key (legacy) |
| `project_id` | ❌ | ✅ | Firebase project ID |
| `service_account_json` | ❌ | ✅ | Service account JSON |

### APNS Driver

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `team_id` | ❌ | ✅ | Apple team ID |
| `key_id` | ❌ | ✅ | Apple key ID |
| `private_key` | ❌ | ✅ | Private key (PEM) |
| `bundle_id` | ❌ | ✅ | iOS bundle ID |
| `production` | ❌ | ✅ | Use production APNS |

---

## Webhook Connector (Notifications)

**Factory:** `internal/connector/webhook/factory.go`

### Common

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `mode` | ❌ | ✅ | inbound/outbound |
| `secret` | ❌ | ✅ | Signature secret |
| `signature_header` | ❌ | ✅ | Signature header name |
| `signature_algorithm` | ❌ | ✅ | hmac-sha256, etc |

### Inbound Mode

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `path` | ❌ | ✅ | Webhook endpoint path |
| `timestamp_header` | ❌ | ✅ | Timestamp header |
| `timestamp_tolerance` | ❌ | ✅ | Tolerance duration |

### Outbound Mode

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `url` | ❌ | ✅ | Target URL |
| `method` | ❌ | ✅ | HTTP method |

---

## MQ Connector (RabbitMQ)

**Factory:** `internal/connector/mq/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `connection_name` | ❌ | ✅ | Connection identifier |
| `max_reconnects` | ❌ | ✅ | Max reconnection attempts |

*Note: `queue`, `exchange`, `consumer`, `publisher` blocks are already supported.*

---

## MQ Connector (Kafka)

**Factory:** `internal/connector/mq/factory.go`

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `client_id` | ❌ | ✅ | Kafka client ID |

### Consumer Block Attributes

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `group_id` | ❌ | ✅ | Consumer group ID |
| `auto_offset_reset` | ❌ | ✅ | earliest/latest |

### Producer Block Attributes

| Attribute | Parser | Factory | Notes |
|-----------|--------|---------|-------|
| `topic` | ❌ | ✅ | Default topic |
| `acks` | ❌ | ✅ | Ack policy (all/1/0) |
| `compression` | ❌ | ✅ | Compression type |

### Nested Blocks (Kafka)

| Block | Parser | Notes |
|-------|--------|-------|
| `sasl` | ❌ | SASL authentication |
| `schema_registry` | ❌ | Schema Registry config |

---

## Nested Blocks Summary

| Block | Parser | Attributes Supported |
|-------|--------|---------------------|
| `pool` | ✅ | min, max |
| `cors` | ✅ | origins, methods, headers |
| `auth` | ✅ | type, token, header, grant_type, refresh_token, token_url, client_id, client_secret, scopes, api_key, api_key_header, api_key_query, username, password, secret, jwks_url, issuer, audience, algorithms, scheme, keys, query_param, users, realm, public, required_headers, response_headers |
| `retry` | ✅ | attempts, backoff |
| `mock` | ✅ | enabled, source |
| `headers` | ✅ | (dynamic attributes) |
| `tls` | ✅ | ca_cert, client_cert, client_key, insecure_skip_verify |
| `queue` | ✅ | (generic block) |
| `exchange` | ✅ | (generic block) |
| `consumer` | ✅ | (generic block) |
| `publisher` | ✅ | (generic block) |
| `producer` | ✅ | (generic block) |
| `federation` | ✅ | enabled, version |
| `profile` | ✅ | (requires label) |
| `cluster` | ❌ | Redis Cluster |
| `sentinel` | ❌ | Redis Sentinel |
| `sasl` | ❌ | Kafka SASL |
| `schema_registry` | ❌ | Kafka Schema Registry |
| `keep_alive` | ❌ | gRPC keep-alive |
| `load_balancing` | ❌ | gRPC load balancing |

---

## Flow Blocks

**Parser:** `internal/parser/flow.go`

| Block/Attribute | Parser | Notes |
|-----------------|--------|-------|
| `from.connector` | ✅ | Required |
| `from.operation` | ✅ | Required |
| `to.connector` | ✅ | Required |
| `to.target` | ✅ | |
| `to.filter` | ✅ | |
| `to.query` | ✅ | SQL query |
| `to.query_filter` | ✅ | MongoDB filter |
| `to.update` | ✅ | MongoDB update |
| **`to.operation`** | ❌ | **HEAVILY USED in examples** |
| `enrich.connector` | ✅ | |
| `enrich.operation` | ✅ | |
| `enrich.params` | ✅ | Block |
| `cache.storage` | ✅ | Required |
| `cache.ttl` | ✅ | |
| `cache.key` | ✅ | No variable interpolation |
| `cache.use` | ✅ | Reference named cache |
| `after.invalidate` | ✅ | |
| `lock` | ✅ | Sync primitive |
| `semaphore` | ✅ | Sync primitive |
| `coordinate` | ✅ | Sync primitive |

---

## Priority Recommendations

### HIGH Priority (heavily used in examples)

1. **`to.operation`** - Used in almost every write flow
2. **Cache attributes:** `url`, `prefix`, `default_ttl`, `mode`, `max_items`, `eviction`
3. **gRPC attributes:** `proto_path`, `reflection`, `target`, `insecure`
4. **File attributes:** `base_path`, `format`
5. **S3 attributes:** `bucket`, `region`, `access_key`, `secret_key`, `endpoint`, `use_path_style`
6. **MongoDB:** `uri`

### MEDIUM Priority (useful for production)

1. **Database:** `sslmode`, `ssl_mode`, `charset`, `replicas`, `use_replicas`
2. **MQ:** `connection_name`, `max_reconnects`, `client_id`
3. **gRPC blocks:** `keep_alive`, `load_balancing`
4. **Cache blocks:** `cluster`, `sentinel`
5. **Kafka:** `sasl`, `schema_registry` blocks

### LOW Priority (new notification connectors)

1. All Email connector attributes
2. All Slack connector attributes
3. All Discord connector attributes
4. All SMS connector attributes
5. All Push connector attributes
6. All Webhook connector attributes

---

## Statistics

- **Total attributes in factories but NOT in parser:** ~80+
- **Total nested blocks not supported:** 6
- **Critical missing:** `to.operation` in flows

---

## Related Files

- Parser: `internal/parser/connector.go`
- Flow Parser: `internal/parser/flow.go`
- Factories: `internal/connector/*/factory.go`
