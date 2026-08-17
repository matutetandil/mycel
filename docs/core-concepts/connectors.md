# Connectors

A connector is a bidirectional adapter between Mycel and an external system. Every connector can act as a **source** (receives data that triggers a flow) or a **target** (destination where a flow writes data). Some are naturally one-directional — email is output-only, cron is input-only — but most work both ways.

## Connector Table

| Type | Driver / Examples | As Source | As Target |
|------|-------------------|-----------|-----------|
| `rest` | HTTP server | Expose endpoints | — |
| `http` | HTTP client | — | Call APIs |
| `database` | `postgres`, `mysql`, `sqlite`, `mongodb` | Query data | Insert/Update/Delete |
| `graphql` | GraphQL server/client | Expose schema | Query/Mutate |
| `mq` | `rabbitmq`, `kafka`, `redis` | Consume messages | Publish messages |
| `grpc` | gRPC server/client | Expose services | Call services |
| `tcp` | TCP server/client | Receive connections | Send data |
| `cache` | `memory`, `redis` | — | Read/write cache |
| `file` | Local filesystem | Watch for files | Write files |
| `s3` | AWS S3, MinIO | Read objects | Write objects |
| `websocket` | WebSocket server | Receive client events | Push to clients |
| `sse` | Server-Sent Events | Connect/disconnect events | Push events |
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
| `pdf` | PDF generation | — | Render documents |

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
    attempts  = 3          # total tries, including the first
    delay     = "1s"       # wait before the second try
    backoff   = "exponential"  # constant | linear | exponential
    max_delay = "30s"      # cap however far the wait grows
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
    enabled             = true
    path                = "/graphql/ws"  # default /subscriptions
    keep_alive_interval = "30s"          # ping period on an idle socket
    connection_timeout  = "60s"          # drop a connection that stops answering
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
  endpoint       = env("S3_ENDPOINT")
  access_key     = env("S3_ACCESS_KEY")
  secret_key     = env("S3_SECRET_KEY")
  use_path_style = true
}
```

`use_path_style` addresses objects as `endpoint/bucket/key` instead of
`bucket.endpoint/key`, which MinIO and most S3-compatible stores require. If you
are arriving from the AWS SDK v1 or an older Terraform provider, this is the
setting they call `force_path_style`.

## Named Operations

Named operations define reusable parameterized queries on a connector. Instead of repeating SQL or API call patterns across flows, define them once and reference them by name.

```hcl
connector "db" {
  type   = "database"
  driver = "postgres"
  # ... connection details

  operation "find_active_users" {
    query       = "SELECT * FROM users WHERE status = 'active' AND org_id = $1"
    description = "Active users for one organisation"

    param "org_id" {
      type        = "string"
      required    = true
      description = "Organisation to filter by"
    }
  }

  operation "list_recent" {
    query = "SELECT * FROM users ORDER BY created_at DESC LIMIT $1"

    param "limit" {
      type    = "number"
      default = 100
    }
  }
}
```

Each parameter is its own `param` block, named by its label. The block is a
contract, applied before the flow runs: defaults fill in what was not sent, and
what was sent is converted to the declared type and checked against the
constraints.

| Attribute | Applies to | Description |
|---|---|---|
| `type` | any | `string`, `number`, `boolean`, `array` or `object`. A value that can be converted is — see below. |
| `required` | any | Reject the request when the parameter is absent and no `default` covers it. |
| `default` | any | Value used when the parameter is not supplied. A parameter with a default is never missing. |
| `in` | any | Where the value comes from: `path`, `query`, `header` or `body`. |
| `min`, `max` | numbers | Smallest and largest allowed value. |
| `min_length`, `max_length` | strings | Shortest and longest allowed value. |
| `pattern` | strings | Regular expression the value must match. |
| `enum` | strings | The complete set of allowed values. |
| `description` | any | Documentation, carried into the exported OpenAPI spec. |

```hcl
connector "api" {
  type = "rest"
  port = 8080

  operation "search_users" {
    method = "GET"
    path   = "/users"

    param "limit" {
      type    = "number"
      default = 100
      min     = 1
      max     = 500
    }

    param "sort" {
      type    = "string"
      enum    = ["name", "email", "created_at"]
      default = "name"
    }

    param "tenant" {
      type       = "string"
      required   = true
      min_length = 3
    }
  }
}
```

`GET /users?limit=600` is answered with `400` and
`invalid parameters: limit: value must be at most 500`, before the flow runs.
Every problem in a request is reported at once, so a caller is not made to fix
one per round trip.

**The declared type converts.** Path and query parameters arrive as strings —
always — so `type = "number"` would reject every request that uses it if it
were enforced literally. `?limit=25` reaches the flow as the number `25`, and
`?limit=abc` is a `400` naming the parameter. The same applies to `boolean`,
which accepts `true` and `false` as written in a query string.

!!! note "Parameters are checked on the source"
    The contract belongs to the operation a flow reads from, since that is the
    request being made. An operation used as a destination formats the write;
    its parameters are supplied by the flow, not by a caller.

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

See the [named-operations example](https://github.com/matutetandil/mycel/tree/main/examples/named-operations) for complete patterns.

## Connector Profiles

A profiled connector is one name that resolves to a different backend at
runtime. Each profile declares **what it is** — its own `type` and `driver` —
so the alternatives do not have to be the same kind of thing: one flow can read
prices from an HTTP API for one tenant and from a database for another, without
knowing which it got.

Because the profile carries the type, a profiled connector has none at the root.

```hcl
connector "prices" {
  select  = "env('PRICE_SOURCE')"   # CEL expression evaluated per execution
  default = "magento"
  fallback = ["erp", "legacy"]      # tried in order if the selected one fails

  profile "magento" {
    type     = "http"
    driver   = "client"
    base_url = env("MAGENTO_URL")

    auth {
      type  = "bearer"
      token = env("MAGENTO_TOKEN")
    }
  }

  profile "erp" {
    type     = "database"
    driver   = "sqlite"
    database = "erp.db"
  }
}
```

The alternatives can equally be the same kind of backend — read replicas, a
tenant per database — in which case every profile repeats the same `type` and
`driver` and varies only the connection:

```hcl
connector "db" {
  select  = "input.tenant_id"
  default = "primary"

  profile "primary" {
    type     = "database"
    driver   = "postgres"
    host     = env("PRIMARY_HOST")
    database = "app"
  }

  profile "analytics" {
    type     = "database"
    driver   = "postgres"
    host     = env("ANALYTICS_HOST")
    database = "app_analytics"
  }
}
```

`select` is evaluated at flow execution time and its result names the profile;
`default` is used when it evaluates to nothing or names a profile that does not
exist. `fallback` lists profiles to try, in order, when the selected one fails.

See the [profiles example](https://github.com/matutetandil/mycel/tree/main/examples/profiles) for details.

## TLS

Connectors that speak TLS — `http`, `grpc`, `tcp`, `mq` and `mqtt` — configure it
with the same block and the same attribute names.

```hcl
connector "payments" {
  type     = "http"
  base_url = "https://payments.internal"

  tls {
    ca_cert = "/certs/internal-ca.pem"   # verify the other side
    cert    = "/certs/mycel.pem"         # prove who we are (mutual TLS)
    key     = "/certs/mycel.key"
  }
}
```

| Attribute | Type | Description |
|---|---|---|
| `enabled` | bool | Defaults to `true` when the block is present. Set it to `false` to switch TLS off without deleting the certificate paths, which is what makes it drivable from the environment. |
| `ca_cert` | string | CA certificate used to verify the other side. Needed for a private CA; the system trust store is used when it is absent. |
| `cert` | string | The certificate this connector presents — its own when it is a server, the client certificate for mutual TLS. |
| `key` | string | Private key for `cert`. |
| `server_name` | string | Expected server name, overriding the address used to connect (SNI). `grpc` only. |
| `insecure_skip_verify` | bool | Skip verification of the other side's certificate. Development only. |

Writing the block is the opt-in, so `enabled = true` is never required. This is
the same rule the [`mfa` block](../guides/auth.md) follows.

`cert` and `key` are one setting seen from two sides: on a server they are the
certificate it presents, on a client the pair it uses for mutual TLS. The
connector already knows which it is, so the names do not repeat it.

!!! warning "A connector that cannot load its certificates does not start"
    If TLS is enabled and the certificate or CA cannot be read, startup fails
    with the reason. It is never downgraded to an unencrypted connection.

### Older attribute names

Three connectors used to read different names for these settings. All of them
are still accepted and mean exactly what they meant before, so existing
configuration keeps working — but they are no longer offered as completions, and
new configuration should use the names above.

| Older name | Written by | Now |
|---|---|---|
| `client_cert` | `http` | `cert` |
| `client_key` | `http` | `key` |
| `cert_file` | `grpc` | `cert` |
| `key_file` | `grpc` | `key` |
| `ca_file` | `grpc` | `ca_cert` |
| `skip_verify` | `grpc` | `insecure_skip_verify` |

Writing both spellings of one setting in the same block is an error rather than
a silent choice between them.

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
