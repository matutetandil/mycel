# Connectors

A connector is a bidirectional adapter between Mycel and an external system. Every connector can act as a **source** (receives data that triggers a flow) or a **target** (destination where a flow writes data). Some are naturally one-directional — email is output-only, cron is input-only — but most work both ways.

## Connector Table

| Type | Driver / Examples | As Source | As Target |
|------|-------------------|-----------|-----------|
| `rest` | HTTP server | Expose endpoints | — |
| `http` | HTTP client | — | Call APIs |
| `database` | `postgres`, `mysql`, `sqlite`, `mongodb` | Query data | Insert/Update/Delete |
| `graphql` | GraphQL server/client | Expose schema | Query/Mutate |
| `queue` | `rabbitmq`, `kafka`, `redis` | Consume messages | Publish messages |
| `grpc` | gRPC server/client | Expose services | Call services |
| `tcp` | TCP server/client | Receive connections | Send data |
| `cache` | `memory`, `redis` | — | Read/write cache |
| `file` | Local filesystem | Watch for files | Write files |
| `s3` | AWS S3, MinIO | Read objects | Write objects |
| `websocket` | WebSocket server | — | Push to clients |
| `sse` | Server-Sent Events | — | Push events |
| `cdc` | PostgreSQL WAL | Stream DB changes | — |
| `exec` | Shell commands | — | Execute commands |
| `email` | SMTP | — | Send emails |
| `slack` | Slack API | — | Send messages |
| `discord` | Discord API | — | Send messages |
| `sms` | Twilio | — | Send SMS |
| `push` | FCM, APNs | — | Push notifications |
| `webhook` | HTTP callbacks | — | Send webhooks |
| `soap` | SOAP 1.1/1.2 | Expose SOAP endpoints | Call SOAP services |
| `elasticsearch` | Elasticsearch | — | Index/Search |
| `oauth` | Google, GitHub, Apple, OIDC | OAuth callback | — |
| `mqtt` | MQTT 3.1.1/5.0 | Subscribe to topics | Publish messages |
| `ftp` | FTP, FTPS, SFTP | List/Download files | Upload/Delete files |

## Defining a Connector

```hcl
connector "NAME" {
  type = "CONNECTOR_TYPE"
  # ... type-specific options
}
```

The connector name is how flows reference it: `connector = "NAME"` in a flow's `from` or `to` block.

## Common Connectors

### REST Server

```hcl
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    headers = ["Content-Type", "Authorization"]
  }
}
```

### HTTP Client

```hcl
connector "external_api" {
  type     = "http"
  base_url = "https://api.example.com"
  timeout  = "30s"

  auth {
    type  = "bearer"
    token = env("API_TOKEN")
  }

  retry {
    count    = 3
    interval = "1s"
    backoff  = 2.0
  }
}
```

### Database

```hcl
# PostgreSQL
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("PG_HOST")
  port     = 5432
  database = env("PG_DATABASE")
  user     = env("PG_USER")
  password = env("PG_PASSWORD")
  ssl_mode = "require"

  pool {
    max          = 100
    min          = 10
    max_lifetime = 300
  }
}

# MySQL
connector "mysql" {
  type     = "database"
  driver   = "mysql"
  host     = env("MYSQL_HOST")
  port     = 3306
  database = env("MYSQL_DATABASE")
  user     = env("MYSQL_USER")
  password = env("MYSQL_PASSWORD")
}

# SQLite (no server needed)
connector "local_db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data.db"
}

# MongoDB
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  uri      = env("MONGO_URI")
  database = "myapp"
}
```

### Message Queue

```hcl
# RabbitMQ
connector "rabbit" {
  type     = "mq"
  driver   = "rabbitmq"
  host     = env("RABBITMQ_HOST")
  port     = 5672
  user     = "guest"
  password = env("RABBITMQ_PASS")
  vhost    = "/"
}

# Kafka
connector "kafka" {
  type    = "mq"
  driver  = "kafka"
  brokers = ["kafka:9092"]
}

# Redis Pub/Sub
connector "redis_events" {
  type     = "mq"
  driver   = "redis"
  url      = env("REDIS_URL", "redis://localhost:6379")
  channels = ["orders", "payments"]
}
```

### Cache

```hcl
# Redis
connector "cache" {
  type    = "cache"
  driver  = "redis"
  url     = env("REDIS_URL", "redis://localhost:6379")

  default_ttl = "1h"
  prefix      = "myapp:"
}

# In-memory (no external service)
connector "local_cache" {
  type      = "cache"
  driver    = "memory"
  max_items = 10000
  eviction  = "lru"
}
```

### GraphQL

```hcl
# Server
connector "gql" {
  type       = "graphql"
  driver     = "server"
  port       = 4000
  endpoint   = "/graphql"
  playground = true

  subscriptions {
    enabled   = true
    transport = "websocket"
    path      = "/graphql/ws"
  }
}

# Client
connector "external_gql" {
  type     = "graphql"
  driver   = "client"
  endpoint = "https://api.example.com/graphql"
  timeout  = "30s"
}
```

### gRPC

```hcl
# Server
connector "grpc_api" {
  type        = "grpc"
  driver      = "server"
  port        = 50051
  proto_path  = "./proto"
  proto_files = ["user.proto", "order.proto"]
  reflection  = true
}

# Client
connector "user_service" {
  type           = "grpc"
  driver         = "client"
  target         = "users-service:50051"
  proto_path     = "./proto"
  proto_files    = ["user.proto"]
  insecure       = false
  wait_for_ready = true
}
```

### File System

```hcl
connector "files" {
  type        = "file"
  base_path   = "./data"
  format      = "json"
  create_dirs = true

  # Enable file watching (triggers flows on new/modified files)
  watch          = true
  watch_interval = "5s"
}
```

### S3

```hcl
connector "storage" {
  type   = "s3"
  bucket = env("S3_BUCKET")
  region = env("AWS_REGION")

  # For MinIO or custom S3-compatible
  endpoint          = env("S3_ENDPOINT")
  access_key        = env("S3_ACCESS_KEY")
  secret_key        = env("S3_SECRET_KEY")
  force_path_style  = true
}
```

## Named Operations

Named operations define reusable parameterized queries on a connector. Instead of repeating SQL or API call patterns across flows, define them once and reference them by name.

```hcl
connector "db" {
  type   = "database"
  driver = "postgres"
  # ... connection details

  operation "find_active_users" {
    query  = "SELECT * FROM users WHERE status = 'active' AND org_id = $1"
    params = [{ name = "org_id", type = "string", required = true }]
  }

  operation "deactivate_user" {
    query  = "UPDATE users SET status = 'inactive' WHERE id = $1"
    params = [{ name = "id", type = "string", required = true }]
  }
}
```

Then in flows:

```hcl
flow "list_active_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    operation = "find_active_users"
  }
}
```

See the [named-operations example](../../examples/named-operations) for complete patterns.

## Connector Profiles

Profiles allow a single connector to have multiple backends selected at runtime. Useful for multi-tenant systems, A/B testing, or read/write splitting.

```hcl
connector "db" {
  type    = "database"
  driver  = "postgres"
  select  = "input.tenant_id"  # CEL expression to pick profile
  default = "primary"

  profile "primary" {
    host     = env("PRIMARY_HOST")
    database = "app"
    user     = env("DB_USER")
    password = env("DB_PASSWORD")
  }

  profile "analytics" {
    host     = env("ANALYTICS_HOST")
    database = "app_analytics"
    user     = env("DB_USER")
    password = env("DB_PASSWORD")
  }
}
```

The `select` expression is evaluated at flow execution time. Profiles can also use `fallback` for failover:

```hcl
connector "cache" {
  type     = "cache"
  driver   = "redis"
  fallback = ["primary", "secondary"]

  profile "primary" {
    url = env("REDIS_PRIMARY_URL")
  }

  profile "secondary" {
    url = env("REDIS_SECONDARY_URL")
  }
}
```

See the [profiles example](../../examples/profiles) for details.

## Per-Connector Reference

For complete configuration options and examples for each connector type, see the [Connector Catalog](../connectors/):

- [REST](../connectors/rest.md)
- [Database](../connectors/database.md)
- [GraphQL](../connectors/graphql.md)
- [gRPC](../connectors/grpc.md)
- [Message Queues](../connectors/message-queues.md)
- [TCP](../connectors/tcp.md)
- [Cache](../connectors/cache.md)
- [Filesystem](../connectors/filesystem.md)
- [S3](../connectors/s3.md)
- [WebSocket](../connectors/websocket.md)
- [SSE](../connectors/sse.md)
- [CDC](../connectors/cdc.md)
- [Elasticsearch](../connectors/elasticsearch.md)
- [SOAP](../connectors/soap.md)
- [OAuth](../connectors/oauth.md)
- [MQTT](../connectors/mqtt.md)
- [FTP / SFTP](../connectors/ftp.md)
- [Notifications](../connectors/notifications.md)
