# Configuration Reference

Complete HCL syntax reference for all Mycel block types. Every block is documented with all supported attributes.

## Table of Contents

- [service](#service)
- [connector](#connector)
- [flow](#flow)
    - [from](#from-block) · [to](#to-block) · [accept](#accept-block) · [step](#step-block) · [enrich](#enrich-block)
    - [transform](#transform-block) · [response](#response-block) · [validate](#validate-block) · [require](#require-block)
    - [cache](#cache-block) · [after](#after-block) · [dedupe](#dedupe-block) · [idempotency](#idempotency-block) · [async](#async-block)
    - [error_handling](#error_handling-block) · [lock](#lock-block) · [semaphore](#semaphore-block) · [coordinate](#coordinate-block) · [sequence_guard](#sequence_guard-block)
    - [batch](#batch-block) · [state_transition](#state_transition-block)
- [constants](#constants)
- [type](#type)
- [transform](#transform)
- [cache (named)](#cache-named)
- [validator](#validator)
- [functions](#functions)
- [plugin](#plugin)
- [aspect](#aspect)
- [security](#security)
- [auth](#auth)
- [saga](#saga)
- [state_machine](#state_machine)

---

## Naming Rules

All named blocks (connector, flow, type, transform, aspect, validator) must have **unique names within their type**. The parser validates this at startup and reports the file locations of any duplicates:

```
Error: duplicate flow name "create_user": defined in flows/api.mycel and flows/users.mycel
```

Names can overlap across different types (e.g., a connector and a flow can both be named `"users"`), but two connectors cannot share the same name.

---

## service

Global service configuration. Place in `config.mycel`.

```hcl
service {
  name       = "orders-api"     # Service name (in health, metrics, logs)
  version    = "2.1.0"          # Service version
  admin_port = 9090             # Health/metrics port when no REST connector (default: 9090)

  rate_limit {
    enabled             = true
    requests_per_second = 100
    burst               = 200
    key_extractor       = "ip"              # "ip", "header:X-API-Key", "query:api_key"
    exclude_paths       = ["/health", "/metrics"]
    enable_headers      = true              # X-RateLimit-* headers
    storage             = "redis_cache"     # Optional: Redis connector for distributed rate limiting
  }

  workflow {
    storage     = "db"              # Database connector name
    table       = "mycel_workflows" # Table name (default: mycel_workflows)
    auto_create = true              # Create table on startup

    api {                           # optional; no api block means no endpoints
      port = 9091                   # default 9091; may not be the admin port

      auth {                        # required when api is written
        type = "api_key"            # jwt, api_key or basic — a connector's auth block
        keys = [env("WORKFLOW_API_KEY")]
      }
    }
  }
}
```

### service attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | `"mycel-service"` | Service name |
| `version` | string | `"0.0.0"` | Service version |
| `admin_port` | int | `9090` | Standalone admin server port |

### rate_limit attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | bool | `true` | Enable/disable rate limiting |
| `requests_per_second` | float | `100` | Token refill rate |
| `burst` | int | `200` | Max burst size |
| `key_extractor` | string | `"ip"` | Client identifier |
| `exclude_paths` | list | `["/health", "/metrics"]` | Paths excluded from limiting |
| `enable_headers` | bool | `true` | Add X-RateLimit-* headers |
| `storage` | string | `""` | Cache connector name for distributed rate limiting (e.g., `"redis_cache"`) |

### workflow attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `storage` | string | required | Database connector name |
| `table` | string | `"mycel_workflows"` | Table name |
| `auto_create` | bool | `true` | Auto-create table |

#### workflow.api

An HTTP interface to running workflows, on a port of its own — never the admin
server's.

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `port` | int | `9091` | Port to listen on; may not be the admin port |
| `host` | string | every interface | Address to bind to |

#### workflow.api.auth

Required: these endpoints wake and cancel workflows.

| Attribute | Type | Description |
|-----------|------|-------------|
| `type` | string | **required** — `jwt`, `api_key` or `basic` |
| `header` | string | Header carrying the key (`api_key`) |
| `keys` | list | Accepted API keys (`api_key`) |
| `secret` | string | Secret tokens are signed with (`jwt`) |
| `jwks_url` | string | Where the signing keys are published (`jwt`) |

---

## connector

```hcl
connector "NAME" {
  type = "TYPE"
  # ... type-specific options
}
```

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
connector "external" {
  type     = "http"
  base_url = "https://api.example.com"
  timeout  = "30s"

  auth {
    type  = "bearer"              # "bearer", "api_key", "basic", "oauth2"
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
connector "db" {
  type     = "database"
  driver   = "postgres"     # "postgres", "mysql", "sqlite", "mongodb"
  host     = env("PG_HOST")
  port     = 5432
  database = env("PG_DATABASE")
  user     = env("PG_USER")
  password = env("PG_PASSWORD")
  ssl_mode = "require"      # "disable", "require", "verify-full"

  pool {
    max          = 100
    min          = 10
    max_lifetime = 300    # seconds
  }

  operation "find_by_email" {
    query = "SELECT * FROM users WHERE email = :email"

    param "email" {
      type     = "string"
      required = true
    }
  }
}
```

#### Named operations

A connector can name what it does, so a flow says `operation = "find_by_email"`
rather than repeating the query. Which attributes an operation carries depends
on the connector it belongs to.

| Attribute | Connector | Description |
|-----------|-----------|-------------|
| `description` | any | What the operation does |
| `input` / `output` | any | Type validating what goes in and comes back |
| `timeout` | any | Operation timeout |
| `method` | rest, http | HTTP method |
| `path` | rest, http | HTTP path, `:name` for path parameters |
| `query` | database | Raw SQL, `:name` for parameters |
| `table` | database | Table the operation reads or writes |
| `operation_type` | graphql | `Query`, `Mutation` or `Subscription` |
| `field` | graphql | Schema field |
| `service` / `rpc` | grpc | Service and RPC name |
| `exchange` / `routing_key` / `queue` | mq | Where the message goes |
| `protocol` / `action` | tcp | Wire protocol and action identifier |
| `path_pattern` | file, s3 | Path or key pattern |
| `key_pattern` / `ttl` | cache | Key pattern and how long it lives |
| `command` / `args` | exec | Command to run and its arguments |

#### `param` inside an operation

Each `param "name" {}` declares one parameter of the operation: what it is,
where it comes from, and what a valid value looks like. Defaults are filled in
and constraints are checked before the flow runs.

| Attribute | Type | Description |
|-----------|------|-------------|
| `type` | string | Declared type; a value that can be converted to it is |
| `required` | bool | Reject the request when it is absent and there is no default |
| `default` | any | Value used when the parameter is not supplied |
| `description` | string | What the parameter means |
| `in` | string | Where it comes from: `path`, `query`, `header` or `body` |
| `min` / `max` | number | Smallest and largest allowed value |
| `min_length` / `max_length` | number | Shortest and longest allowed value |
| `pattern` | string | Regular expression the value must match |
| `enum` | list | The complete set of allowed values |

### GraphQL

```hcl
# Server
connector "gql" {
  type           = "graphql"
  driver         = "server"
  port           = 4000
  endpoint       = "/graphql"
  playground     = true
  playground_path = "/graphql/playground"
  introspection  = true

  # One or the other: a file, or generated from the type blocks. Naming both
  # is refused, and naming neither leaves the server with no schema at all.
  schema {
    path = "./schema.graphql"
  }

  federation {
    enabled = true
    version = 2
  }

  subscriptions {
    enabled             = true
    path                = "/graphql/ws"
    keep_alive_interval = "30s"
  }

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
  }
}

# Client
connector "external_gql" {
  type        = "graphql"
  driver      = "client"
  endpoint    = "https://api.example.com/graphql"
  timeout     = "30s"

  auth {
    type  = "bearer"
    token = env("GRAPHQL_TOKEN")
  }

  subscriptions {
    enabled = true
    path    = "/subscriptions"
  }
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
  max_recv_mb = 4
  max_send_mb = 4

  tls {
    cert = "/certs/server.crt"
    key  = "/certs/server.key"
  }
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

### Message Queue

```hcl
# RabbitMQ
connector "rabbit" {
  type           = "mq"
  driver         = "rabbitmq"
  url            = env("RABBITMQ_URL")
  vhost          = "/"
  connection_name = "my-service"
  max_reconnects = 10

  consumer {
    queue       = "orders"
    prefetch    = 10
    auto_ack    = false
    workers     = 5
    exclusive   = false
    no_local    = false
  }

  publisher {
    exchange    = "orders"
    routing_key = "order.created"
    mandatory   = false
    immediate   = false
  }
}

# Kafka
connector "kafka" {
  type      = "mq"
  driver    = "kafka"
  brokers   = ["kafka1:9092", "kafka2:9092"]
  client_id = "my-service"

  consumer {
    group_id = "my-service-group"
    topics   = ["orders", "payments"]
    offset   = "latest"            # "earliest", "latest"
  }

  producer {
    topic       = "orders"
    acks        = "all"            # "none", "leader", "all"
    compression = "gzip"           # "none", "gzip", "snappy", "lz4"
  }

  sasl {
    mechanism = "PLAIN"
    username  = env("KAFKA_USER")
    password  = env("KAFKA_PASS")
  }
}

# Redis Pub/Sub
connector "redis_events" {
  type     = "mq"
  driver   = "redis"
  url      = env("REDIS_URL", "redis://localhost:6379")
  password = env("REDIS_PASSWORD")
  db       = 0
  channels = ["orders", "payments"]  # Subscribe to channels
  patterns = ["events.*"]            # PSUBSCRIBE glob patterns
}
```

### MQTT

```hcl
connector "sensors" {
  type      = "mqtt"
  broker    = "tcp://localhost:1883"   # tcp://, ssl://, ws://
  client_id = "mycel-iot-gateway"
  username  = env("MQTT_USER")
  password  = env("MQTT_PASS")
  qos       = 1                        # 0, 1, 2
  topic     = "default/topic"          # Default publish topic

  clean_session          = true
  keep_alive             = "30s"
  connect_timeout        = "10s"
  auto_reconnect         = true
  max_reconnect_interval = "5m"

  tls {
    cert    = "/certs/client.crt"
    key     = "/certs/client.key"
    ca_cert = "/certs/ca.crt"
  }
}
```

### FTP / SFTP

```hcl
# SFTP
connector "partner_sftp" {
  type      = "ftp"
  protocol  = "sftp"         # "ftp" or "sftp"
  host      = "sftp.partner.com"
  port      = 22             # 21 for FTP, 22 for SFTP
  username  = env("SFTP_USER")
  password  = env("SFTP_PASS")
  base_path = "/incoming"
  key_file  = "/keys/id_rsa" # SSH private key (SFTP only)
  passive   = true           # FTP passive mode
  timeout   = "30s"
  tls       = false          # Explicit TLS (FTPS)
}
```

### TCP

```hcl
# Server
connector "tcp_server" {
  type            = "tcp"
  driver          = "server"
  host            = "0.0.0.0"
  port            = 9000
  protocol        = "json"         # "json", "msgpack", "raw", "nestjs"
  max_connections = 1000
  read_timeout    = "30s"
  write_timeout   = "30s"
}

# Client
connector "tcp_client" {
  type            = "tcp"
  driver          = "client"
  host            = "localhost"
  port            = 9000
  protocol        = "json"
  connect_timeout = "10s"
  read_timeout    = "30s"
}
```

### Cache

```hcl
# Redis
connector "redis_cache" {
  type        = "cache"
  driver      = "redis"
  url         = env("REDIS_URL", "redis://localhost:6379")
  password    = env("REDIS_PASSWORD")
  db          = 0
  prefix      = "myapp:"
  default_ttl = "1h"
  mode        = "standalone"          # "standalone", "cluster", "sentinel"
}

# Memory
connector "local_cache" {
  type        = "cache"
  driver      = "memory"
  max_items   = 10000
  eviction    = "lru"
  default_ttl = "5m"
}
```

### File System

```hcl
connector "files" {
  type           = "file"
  base_path      = "./data"
  format         = "json"          # "json", "csv", "text", "binary", "excel"
  create_dirs    = true
  permissions    = "0644"
  watch          = true            # Enable file watching
  watch_interval = "5s"            # Polling interval
}
```

### S3

```hcl
connector "s3" {
  type              = "s3"
  bucket            = env("S3_BUCKET")
  region            = env("AWS_REGION")
  access_key        = env("AWS_ACCESS_KEY_ID")
  secret_key        = env("AWS_SECRET_ACCESS_KEY")
  endpoint          = env("S3_ENDPOINT")        # For MinIO/custom
  use_path_style    = true                       # Required for MinIO
}
```

### Exec

```hcl
connector "script" {
  type          = "exec"
  command       = "/usr/bin/python3"
  args          = ["./scripts/process.py"]
  shell         = false
  env           = { PYTHONPATH = "/app" }
  working_dir   = "/app"
  input_format  = "json"      # "args", "stdin", "json"
  output_format = "json"      # "text", "json", "lines"
  timeout       = "30s"
}
```

### WebSocket

```hcl
connector "ws" {
  type = "websocket"
  port = 8080
  path = "/ws"
}
```

### SSE

```hcl
connector "sse" {
  type = "sse"
  port = 8080
  path = "/events"
}
```

### CDC

```hcl
connector "cdc" {
  type        = "cdc"
  driver      = "postgres"
  host        = env("PG_HOST", "localhost")
  port        = 5432
  database    = env("PG_DATABASE")
  user        = env("PG_REPLICATION_USER")
  password    = env("PG_REPLICATION_PASSWORD")
  slot_name   = "mycel_slot"
  publication = "mycel_pub"
}
```

### Elasticsearch

```hcl
connector "es" {
  type     = "elasticsearch"
  url      = "http://localhost:9200"
  username = env("ES_USER")
  password = env("ES_PASSWORD")
}
```

### SOAP

```hcl
# Client
connector "soap_service" {
  type         = "soap"
  driver       = "client"
  endpoint     = "http://legacy.example.com/service"
  namespace    = "http://example.com/ns"
  soap_version = "1.1"            # "1.1" or "1.2"

  auth {
    type     = "basic"
    username = env("SOAP_USER")
    password = env("SOAP_PASS")
  }
}

# Server. A connector is a client when it names an endpoint and a server when
# it names a port; naming both is refused.
connector "soap_server" {
  type         = "soap"
  driver       = "server"
  port         = 8080
  namespace    = "http://example.com/ns"
  soap_version = "1.1"
}
```

### PDF

```hcl
connector "invoice_pdf" {
  type         = "pdf"
  template     = "./templates/invoice.html"   # Required: HTML template path
  page_size    = "A4"                         # A4, Letter, Legal
  font         = "Helvetica"
  margin_left  = 15                           # Millimetres
  margin_top   = 15
  margin_right = 15
  output_dir   = "./pdfs"                     # Used by the "save" operation
}
```

Flow `operation` values: `generate` (default — returns the PDF as binary) or
`save` (writes it to `output_dir`). See [PDF](../connectors/pdf.md).

### Connector Profiles

```hcl
connector "db" {
  select  = "input.tenant_id"   # CEL expression to pick profile
  default = "primary"
  fallback = ["primary", "replica"]

  # A profiled connector has no type at the root: each profile declares its
  # own, so the alternatives need not be the same kind of backend.
  profile "primary" {
    type     = "database"
    driver   = "postgres"
    host     = env("PRIMARY_HOST")
    database = "app"
    user     = env("DB_USER")
    password = env("DB_PASSWORD")
  }

  profile "replica" {
    type     = "database"
    driver   = "postgres"
    host     = env("REPLICA_HOST")
    database = "app"
    user     = env("DB_USER")
    password = env("DB_PASSWORD")
  }
}
```

---

## flow

```hcl
flow "NAME" {
  returns = "[User]"         # GraphQL return type
  when    = "0 3 * * *"      # Cron schedule or @every interval
  entity  = "Product"        # GraphQL Federation entity name

  from { ... }
  to { ... }
  accept { ... }
  step "NAME" { ... }
  enrich "NAME" { ... }
  transform { ... }
  response { ... }
  validate { ... }
  require { ... }
  cache { ... }
  after { ... }
  dedupe { ... }
  error_handling { ... }
  lock { ... }
  semaphore { ... }
  coordinate { ... }
  sequence_guard { ... }
  batch { ... }
  state_transition { ... }
  idempotency { ... }
  async { ... }
}
```

That is the complete set: 21 blocks and 4 attributes. Every one of them is
optional except `from`. For what each block is *for*, rather than its bare
syntax, see [Flow Anatomy](../core-concepts/flows.md#flow-anatomy).

### from block

```hcl
from {
  connector = "api"           # Required
  operation = "GET /users"    # Required for rest/graphql/grpc/soap/tcp/sse;
                              # optional (defaults to "*") for mq/mqtt/cdc/websocket/file
  format    = "json"          # "json", "xml", "csv", "tsv"

  # Simple filter (string)
  filter = "input.status == 'active'"

  # Full filter block (for MQ rejection policies)
  filter {
    condition   = "input.amount > 0"
    on_reject   = "requeue"  # "ack", "reject", "requeue"
    id_field    = "input.body.payment_id"
    max_requeue = 3
  }
}
```

### to block

```hcl
to {
  connector    = "db"
  target       = "users"
  operation    = "INSERT"       # Override operation type
  format       = "json"         # "json", "xml", "csv", "tsv"
  filter       = "input.user_id == context.params.userId"  # For subscriptions/WS/SSE
  query        = "SELECT * FROM users WHERE id = :id"      # Custom SQL
  query_filter = { status = "active" }                     # MongoDB filter
  update       = { "$set" = { status = "active" } }        # MongoDB update
  params       = { key = "value" }                         # Extra params (e.g., S3 COPY)
  when         = "output.amount > 0"                       # Conditional write
  parallel     = true                                      # Parallel multi-to (default: true)
  envelope     = "product"                                 # Wrap the payload under one root key

  transform { ... }    # Per-destination transform
}
```

`envelope` wraps what is sent under a single root key — `{"product": {...}}`
rather than `{...}` — which is what Magento's webapi, Spring's `@RequestBody`
and SOAP-derived REST interfaces expect. A `step` takes it too, for the same
reason.

### transaction block

Inside a `to`, a list of statements run over one pinned connection: they all
commit or none does. A destination with a transaction names no `target` or
`query` of its own — its statements say what they write.

```hcl
to {
  connector = "db"

  transaction {
    exec {
      query  = "DELETE FROM product_option WHERE product_id = :pid"
      params = { pid = "input.id" }
      when   = "input.id > 0"           # optional gate; false skips the statement
    }

    exec {
      query   = "INSERT INTO product (sku, name) VALUES (:sku, :name)"
      params  = { sku = "input.sku", name = "input.name" }
      capture = "product_id"            # available below as captured.product_id
    }

    each "option" in "input.options" {  # iterate a list from the payload
      exec {
        query   = "INSERT INTO product_option (product_id, code) VALUES (:pid, :code)"
        params  = {
          pid  = "captured.product_id"  # captured above
          code = "option.code"          # the current element
        }
        capture = "option_id"
      }
    }
  }
}
```

#### exec attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `query` | string | The statement, with `:name` placeholders |
| `params` | map | CEL expressions filling the placeholders |
| `when` | string | CEL gate; false skips this statement, which is not an error |
| `capture` | string | Store the result under `captured.<name>` — the last insert id for INSERT, UPDATE and DELETE, the first column of the first row for SELECT |

#### each

`each "<var>" in "<list expression>"` runs its statements once per element. The
element is bound to `<var>` and its position to `<var>_index`, and `each` nests.

Supported by MySQL and SQLite.

### accept block

Business-level gate. Runs after `filter` and before `transform`: `filter` decides
whether a message *matches* this flow, `accept` decides whether this flow should
*process* it. Useful when several flows consume the same queue.

```hcl
accept {
  when      = "input.payload.type == 'A1'"   # Required — CEL, must return true to proceed
  on_reject = "requeue"                      # "ack" (default), "reject", "requeue"
}
```

### step block

```hcl
step "NAME" {
  connector = "db"            # Required
  operation = "query"
  query     = "SELECT * FROM users WHERE id = ?"
  target    = "users"
  params    = [input.id]
  body      = { key = "value" }
  # A params entry that evaluates to a list is expanded inside IN (...) —
  # one placeholder per member. See "Binding a set" in destination-properties.
  format    = "json"
  # From a JSON body this is a boolean. From a query string it is the text
  # "true" — see Input and Output.
  when      = "input.include_details == true"
  timeout   = "5s"
  on_error  = "skip"
  default   = {}
}
```

### enrich block

```hcl
enrich "NAME" {
  connector = "pricing_service"    # Required
  operation = "getPrice"           # Required

  params {
    product_id = "input.id"        # CEL expressions as values
  }
}
```

### transform block

Transforms input data **before** sending to destination:

```hcl
transform {
  use        = "transform.normalize_user"   # Reference named transform
  field_name = "CEL expression"
}
```

### response block

Transforms output data **after** receiving from destination. For echo flows (no `to`), defines the response directly:

```hcl
response {
  full_name        = "output.first_name + ' ' + output.last_name"
  email            = "lower(output.email)"
  http_status_code = "200"      # Override HTTP status (REST/SOAP)
  grpc_status_code = "0"        # Override gRPC status code
}
```

Variables: `input.*` (request), `output.*` (destination result).

### validate block

```hcl
validate {
  input  = "user_input"    # Type name or type.name reference
  output = "user"
}
```

### require block

```hcl
require {
  roles       = ["admin", "manager"]
  permissions = ["orders:write"]
}
```

### cache block

```hcl
cache {
  storage       = "redis_cache"       # Required
  ttl           = "5m"
  key           = "'product:' + input.id"
  invalidate_on = ["product.updated"]
  use           = "cache.products"    # Reference named cache
  encoding      = ["json"]            # Optional: wire format, see below
}
```

**`encoding` — sharing a namespace with something that is not Mycel.** Entries are written with the codecs listed, applied left to right, and read with the same list reversed. `["json"]` is the default and is what a cache block that does not say gets.

Available codecs: `json` (the value ↔ bytes; must be first), then any number of byte transforms — `base64` and `gzip`.

```hcl
# Reads and writes what a service storing gzip(base64(JSON.stringify(v))) does
encoding = ["json", "base64", "gzip"]
```

This matters during a migration, which is exactly when a cache is most likely to be shared: the service being replaced is still up, still reading and writing the same keys. With the wrong format the two do not merely fail to help each other — they overwrite each other. Mycel cannot decode the other service's entry, treats that as a miss, does the work, and writes its own format over the key; the other service then fails on the next read. The only visible symptom is a cache that never seems to hit.

A found entry that cannot be decoded is counted as `mycel_cache_decode_errors_total` — not as a hit — and logged at warn with the key. That is the signal that the format is wrong.

### after block

```hcl
after {
  invalidate {
    storage       = "redis_cache"    # Required
    keys          = ["product:${input.id}"]
    patterns      = ["products:list:*"]
    keys_from     = "step.variants.map(v, 'product:' + v)"   # Optional, see below
    patterns_from = "step.stores.map(s, 'catalog:' + s + ':*')"
  }
}
```

**`keys` and `patterns`** are templates: `${input.id}` in one is replaced when the flow runs. One key out per template in — the *values* vary with the message, the *number* does not. A template aimed at a list cannot fan out; it renders Go syntax into the key (`url-rewrite-[a b c]`) and warns, pointing here.

**`keys_from` and `patterns_from`** are CEL expressions yielding a list of strings, for a set whose size is only known once the flow has run — every store view of a product, every rewrite path it has had, every variant of a parent. `input.*`, `output.*` and `step.*` are in scope, because the list almost always comes from a query the flow just ran. The result is unioned with the static list and deduplicated, so a fixed key and a computed set can be named together.

```hcl
step "affected_paths" {
  connector = "db"
  query     = "SELECT store_code, request_path AS path FROM url_rewrite WHERE entity_id = :id"
  params    = { id = "input.id" }
}

after {
  invalidate {
    storage   = "redis_cache"
    keys_from = "step.affected_paths.map(r, 'url-rewrite-' + r.store_code + '-' + r.path)"
  }
}
```

A wildcard is not a substitute when the members diverge — rewrite paths drift from the URL key through redirects and history — because one broad enough to catch them all also deletes unrelated entries, and one narrow enough to be safe misses exactly the ones that matter.

They are separate attributes rather than a second shape of `keys` because the two cannot be told apart. HCL refuses a bare `step.paths.map(r, ...)` outright — method calls are not its syntax — so the expression has to be quoted, and a quoted CEL expression and a quoted key template are the same thing to the parser.

**When it fails.** The flow's own work is committed by the time this runs, so the two failures are answered differently. A cache that could not be reached is logged at warn and counted as `mycel_cache_invalidate_errors_total{cache,attr}`, and the request still succeeds. A `keys_from` or `patterns_from` that cannot be evaluated, or that does not yield a list of strings, is logged, counted **and fails the request** — that one is a configuration mistake that will fail identically on every message, and `mycel validate` does not evaluate CEL, so it is not caught beforehand.

An `after` block also runs on a flow with no `to` at all, which is what an endpoint whose only job is invalidation looks like. See [Pattern 7](https://github.com/matutetandil/mycel/tree/main/examples/cache).

### dedupe block

Content-based, biphasic deduplication (since v2.1.0). Compares a canonical fingerprint of the projection against the last stored fingerprint for the same key and drops byte-for-byte matches before reaching `to`. Phase B stores the new fingerprint **only on `to` success**, so a failed-then-retried message does not self-discard. The primitive self-locks per key (in-process via memory-backed `SyncManager`) so concurrent workers cannot double-call the downstream with identical content.

```hcl
dedupe {
  cache        = "redis_cache"                                   # Required: name of a connector { type = "cache" }
  key          = "'sku_fp:' + input.body.payload.productItemId"  # Required: CEL expression for the per-resource key
  ttl          = "30d"                                           # Optional: supports "d" / "w" plus stdlib units; malformed values fail the parse
  on_duplicate = "ack"                                           # Optional: "ack" (default), "reject", "requeue"
  compare_when = "output.row_exists == 1"                        # Optional: gates the comparison only (see below)

  fingerprint {                                                   # Required: at least one named CEL expression
    name   = "output.name"                                        # Both input.* and output.* (transform result) are in scope
    prices = "output.prices"
    # ... one entry per persisted field — omitting one would silently drop real changes
  }
}
```

**`facet` — tracking parts of a message independently.**

A bare `fingerprint {}` answers one question — did anything change — and its only verb is to drop the message. That is the wrong shape when one message carries work of different weights: a product's data and its images arrive together, the images take minutes because the far side downloads them, and re-sending both because a name changed makes the cheap half wait for the expensive one.

Facets split the projection into independently-tracked parts. Each is fingerprinted, stored and committed on its own; a `to` naming a facet runs only when that facet changed, and the message is dropped only when **no** facet did.

```hcl
dedupe {
  cache        = "redis_cache"
  key          = "'sku_fp:' + input.body.payload.sku"
  ttl          = "30d"
  on_duplicate = "ack"                       # applies when no facet changed

  facet "data" {
    fingerprint {
      name   = "output.name"
      prices = "output.prices"
    }
  }

  facet "assets" {
    fingerprint {
      main_image = "output.main_image"
      gallery    = "output.gallery"
    }
  }
}

to {
  facet     = "data"                         # skipped when the data facet did not change
  connector = "magento"
  target    = "/rest/V1/products"
}

to {
  facet     = "assets"                       # skipped when the assets facet did not change
  connector = "rabbit"
  target    = "assets.q"
}
```

- A `to` **without** `facet` runs whenever the message is not dropped — which is what every flow without facets does.
- Each facet is stored under its own key (`dedupe:<flow>:<key>:<facet>`), so a facet that has never been seen reads as changed. Introducing a facet therefore re-runs its destinations once per key, which is a backfill, not a bug.
- **Commit is per facet.** A facet is committed only once every destination naming it has succeeded. When the data lands and the asset enqueue fails, the data facet is committed and the assets facet is not, so the retry re-sends only what did not land — committing them together would lose the assets for as long as the entry lives.
- `compare_when` stays a single flow-level gate. When it is false nothing is compared, so **every** facet runs — which is what a missing downstream record requires: an assets-only message against a record that no longer exists would otherwise never re-create it.
- A bare `fingerprint {}` and `facet` blocks in the same `dedupe` are refused. Honouring both would mean deciding which one drops the message.

!!! warning "Mycel does not check that facets are independent"
    Facets are a statement by the author that these parts of the message can be applied separately. Two facets whose destinations write the same thing will race, and nothing here will report it. Split by what the destinations actually touch — a domain, a table, a subsystem — not by what is convenient to name.

**Pipeline order:** `dedupe` runs **after** `transform` because the fingerprint expressions reference `output.*`. Earlier versions (≤ 2.0.0) ran a key-based dedupe before transform; see CHANGELOG v2.1.0 for migration.

**`compare_when` — when "already seen" and "already applied" diverge.** A stored fingerprint says this content was written once, not that it is still written. If the downstream record can disappear by a path the flow never observes — a manual delete, a restore, a data fix — nothing clears the fingerprint, and the re-send that was meant to repair the damage is dropped as a duplicate instead. `compare_when` is how a flow says the stored fingerprint is no longer trustworthy:

- **false** → the stored fingerprint is not consulted and the message **cannot** be dropped. It still writes, and Phase B still commits the new fingerprint, so the *next* message can be suppressed normally.
- **true** or absent → exactly the behavior above.
- Evaluated against `input.*` and `output.*`, the same scope as `fingerprint {}` — so a `step` result routed through `transform` is reachable from it.
- Fails **open**: a predicate that cannot be evaluated (or does not return a boolean) logs a warning and processes the message, matching the fail-open convention for cache errors. One extra downstream call is recoverable; a silently swallowed message is not.

!!! warning "Put the existence check in `compare_when`, never in `fingerprint {}`"
    A projection field is symmetric: it fires when the record disappears **and** when it appears. The fingerprint committed after a write is the one computed *before* it, so on a create the stored reading of "does this exist" is always `0` while every later message computes `1`. Both directions land backwards — real duplicates stop being suppressed, and the deletion the field was added to catch still matches and still gets dropped.

```hcl
step "check_present" {
  connector = "db"
  query     = "SELECT CAST(COALESCE((SELECT 1 FROM products WHERE sku = :sku LIMIT 1), 0) AS SIGNED) AS row_exists"
  params    = { sku = "input.body.sku" }
  on_error  = "fail"
}

transform {
  row_exists = "int(step.check_present.row_exists)"
  # ... the fields actually written downstream
}

dedupe {
  cache        = "redis_cache"
  key          = "'sku_fp:' + input.body.sku"
  ttl          = "30d"
  compare_when = "output.row_exists == 1"
  fingerprint {
    name = "output.name"
    # row_exists deliberately NOT here — it is a gate, not a hash input
  }
}
```

| situation | gate | outcome |
|---|---|---|
| record present, identical content | compare | dropped as a duplicate |
| record present, content changed | compare | fingerprint differs → processed |
| first write, record absent | skip | processed → Phase B stores the fingerprint |
| record deleted externally, identical content | skip | **processed and rewritten** |

**Canonical encoding rules:** map keys sorted alphabetically; array elements sorted by their encoded bytes (treated as order-insensitive sets); each value type-tagged and length-prefixed so `"a,b"` cannot collide with `["a","b"]`; whole-number floats normalize to ints.

### idempotency block

```hcl
idempotency {
  storage = "redis_cache"         # Required: cache connector name
  key     = "input.payment_id"   # Required: CEL expression for idempotency key
  ttl     = "24h"                 # How long to cache results
}
```

Returns cached results for duplicate requests with matching keys. The flow is not re-executed if a cached result exists.

### async block

```hcl
async {
  storage = "redis_cache"         # Required: cache connector for storing job results
  ttl     = "1h"                  # How long to keep job results
}
```

Returns HTTP 202 immediately with a `job_id`. The flow executes in the background. Auto-registers a `GET /jobs/{job_id}` endpoint for polling job status (pending, completed, or failed).

### error_handling block

```hcl
error_handling {
  retry {
    attempts  = 3
    delay     = "1s"
    max_delay = "30s"
    backoff   = "exponential"          # "linear" or "exponential"
  }

  fallback {
    connector     = "rabbit"
    target        = "orders.failed"
    include_error = true

    transform {
      original = "input"
      error    = "error.message"
    }
  }

  error_response {
    status = 422
    headers = { "X-Error-Code" = "VALIDATION_ERROR" }

    body {
      error = "'Validation failed'"
      code  = "'ORDER_ERROR'"
    }
  }
}
```

### lock block

```hcl
lock {
  storage {
    driver = "redis"
    url    = env("REDIS_URL", "redis://localhost:6379")
  }
  key     = "'account:' + input.account_id"  # Required
  timeout = "30s"
  wait    = true
  retry   = "100ms"
}
```

### semaphore block

```hcl
semaphore {
  storage {
    driver = "redis"
    url    = env("REDIS_URL", "redis://localhost:6379")
  }
  key     = "'api_quota'"        # Required
  limit   = 10                   # Required
  timeout = "5s"                 # Max time to wait for a permit
  lease   = "30s"                # Max time to hold one
}
```

`limit` and `max_permits` are the same setting under two names; either says how
many may hold a permit at once. `lease` bounds how long one is held, so a worker
that dies does not keep its permit forever.

### coordinate block

```hcl
coordinate {
  storage {
    driver = "redis"
    url    = env("REDIS_URL", "redis://localhost:6379")
  }
  timeout              = "60s"                # Default: 60s
  on_timeout           = "fail"               # "fail", "retry", "skip", "pass"
  max_retries          = 3                    # When on_timeout = "retry"
  max_concurrent_waits = 10                   # 0 = unlimited

  wait {
    when = "size(step.check_parent) == 0"     # CEL: wait only if true
    for  = "'parent_ready:' + input.parent_sku"  # Signal key to wait for
  }

  signal {
    when = "true"                             # CEL: signal only if true
    emit = "'parent_ready:' + input.sku"      # Signal key to emit
    ttl  = "24h"                              # Optional: signal expiry
  }

  preflight {                                 # Optional: check before waiting
    connector = "db"
    query     = "SELECT id FROM products WHERE sku = ?"
    params    = { sku = "input.parent_sku" }
    if_exists = "pass"                        # "pass" = skip wait, "fail" = error
  }
}
```

### sequence_guard block

Rejects messages that arrive out of order: the incoming `sequence` must be
strictly greater than the last one recorded for the same `key`. Composes inside
`lock` and `coordinate`, and runs before `transform`.

```hcl
sequence_guard {
  key      = "'sku_seq:' + input.body.sku"   # Required: CEL, the ordering scope
  sequence = "input.body.version"            # Required: CEL, monotonic number
  on_older = "ack"                           # "ack" (default), "reject", "requeue"
  ttl      = "30d"                           # How long a key's sequence is remembered

  storage {
    driver   = "redis"       # Required
    url      = env("REDIS_URL")
    host     = "localhost"   # Alternative to url
    port     = 6379
    password = env("REDIS_PASSWORD")
    db       = 0
  }
}
```

### batch block

```hcl
batch {
  source     = "postgres"    # Required: source connector
  query      = "SELECT * FROM users ORDER BY id"  # Required
  chunk_size = 100
  params     = { since = "input.since" }
  on_error   = "continue"    # "stop" or "continue"

  transform {
    email = "lower(input.email)"
  }

  to {
    connector = "new_db"
    target    = "users"
    operation = "INSERT"
  }
}
```

### state_transition block

```hcl
state_transition {
  machine   = "order_status"    # state_machine block name
  entity    = "orders"          # Database table
  id        = "input.id"
  event     = "input.event"
  data      = "input.data"
  connector = "orders_db"       # Where the entity lives (default: the flow's to)
}
```

`connector` names the connector holding the entity. When it is absent the
flow's own destination is used. With neither, the engine tries every connector
in turn and uses the first that accepts the read and the write — which in a
service with a message queue in it can publish the new state to a topic while
the row it was meant for goes untouched.

---

## type

```hcl
type "NAME" {
  # Federation directives (underscore-prefixed)
  _key         = "id"
  _shareable   = true
  _description = "A user entity"
  _implements  = ["Node"]

  # Field definitions
  field_name = base_type({ constraint = value, ... })
}
```

### Base types: `string`, `number`, `boolean`, `object`, `array`

### Field constraints

| Constraint | Applies to | Description |
|-----------|-----------|-------------|
| `required` | all | `true` (default) or `false` |
| `format` | string | `"email"`, `"url"`, `"uuid"`, `"date"`, `"datetime"`, `"phone"`, `"ip"` |
| `min_length` | string | Minimum string length |
| `max_length` | string | Maximum string length |
| `pattern` | string | Regex pattern |
| `enum` | string, number | Allowed values: `["a", "b"]` |
| `min` | number | Minimum value |
| `max` | number | Maximum value |
| `validate` | any | Custom validator reference |

### Field federation directives

| Directive | Description |
|-----------|-------------|
| `external = true` | `@external` — field from another subgraph |
| `provides = "field"` | `@provides(fields: "field")` |
| `requires = "field"` | `@requires(fields: "field")` |
| `shareable = true` | `@shareable` on field |
| `inaccessible = true` | `@inaccessible` |
| `override = "subgraph"` | `@override(from: "subgraph")` |

---

## constants

Values declared once and referred to by name from anywhere in the
configuration:

```hcl
constants {
  skus_to_skip = ["SKU-1", "SKU-2"]
  page_size    = 500
  region       = env("REGION", "us")
}
```

| Holds | Read as |
|-------|---------|
| Strings, numbers, booleans, lists, maps, `env()` calls — anything settled when the configuration is read | `constants.<name>` |

The same name works on both sides of the line: `${constants.page_size}` in an
attribute, which HCL folds in as the file is read, and `constants.page_size`
inside a CEL expression, which is evaluated per message.

A constant is not computed from a message — that is what a
[transform](../core-concepts/transforms.md) is for. Any `.mycel` file may
declare them, and they are read before anything else, so a flow may use one
that a later file declares. The same name twice is refused, naming both files.

See [Constants](../core-concepts/constants.md).

---

## transform

Named reusable transform:

```hcl
transform "NAME" {
  # Optional: fetch external data
  enrich "data_name" {
    connector = "service"
    operation = "getInfo"
    params {
      id = "input.id"
    }
  }

  field_name = "CEL expression"
  other_field = "enriched.data_name.value"
}
```

---

## cache (named)

Named cache configuration for reuse across flows:

```hcl
cache "NAME" {
  storage       = "redis_cache"    # Required
  ttl           = "10m"
  prefix        = "products"
  invalidate_on = ["product.updated", "product.deleted"]
  encoding      = ["json"]         # Inherited by any flow that references it
}
```

A flow referencing this with `use = "cache.NAME"` takes the encoding along with the namespace, and can override it by declaring its own.

---

## validator

```hcl
# Regex validator
validator "NAME" {
  type    = "regex"
  pattern = "^[A-Z]{3}[0-9]{4}$"
  message = "Error message"
}

# CEL validator
validator "NAME" {
  type    = "cel"
  expr    = "value.endsWith('@company.com')"
  message = "Must use company email"
}

# WASM validator
validator "NAME" {
  type       = "wasm"
  wasm       = "./validators.wasm"
  entrypoint = "validate_cuit"
  message    = "Invalid CUIT"
}
```

---

## functions

WASM custom functions for CEL transforms:

```hcl
functions "NAME" {
  wasm    = "./wasm/pricing.wasm"
  exports = ["calculate_price", "apply_discount"]
}
```

---

## plugin

```hcl
plugin "NAME" {
  source  = "github.com/acme/mycel-plugin"
  version = "^1.0"
}
```

---

## aspect

Cross-cutting concerns applied via flow name patterns:

```hcl
aspect "NAME" {
  when = "after"         # "before", "after", "around", "on_error"
  on   = ["create_*", "update_*"]  # Flow name patterns (glob syntax)

  if = "output.status == 'ok'"     # Optional CEL condition

  action {
    connector = "audit_db"          # Target connector (mutually exclusive with "flow")
    operation = "INSERT audit_logs"

    transform {
      flow      = "_flow"
      operation = "_operation"
      user_id   = "ctx.user_id"
      timestamp = "_timestamp"
    }
  }
}
```

### Flow invocation from aspects

Actions can invoke flows instead of writing to connectors. Use `flow` instead of `connector`:

```hcl
aspect "trigger_notification" {
  when = "after"
  on   = ["create_*"]

  action {
    flow = "send_notification"       # Invokes flow by name
    transform {
      message = "'New item created in ' + _flow"
    }
  }
}
```

`connector` and `flow` are mutually exclusive in an action block. The invoked flow receives the transform output as its input. Errors in the invoked flow are logged as warnings — they do not fail the main flow.

### Response enrichment

After aspects can include a `response` block to inject fields into the flow result. Each field is a CEL expression with access to `result.data`, `result.affected`, `input`, `_flow`, and `_operation`:

```hcl
aspect "v1_deprecation" {
  when = "after"
  on   = ["*_v1"]

  response {
    # HTTP headers (or protocol equivalent)
    headers = {
      Deprecation = "true"
      Sunset      = "Thu, 01 Jun 2026 00:00:00 GMT"
    }

    # Body fields (CEL expressions)
    _warning = "'This API version is deprecated. Migrate to v2.'"
  }
}

# Dynamic values using result data
aspect "add_count" {
  when = "after"
  on   = ["list_*"]

  response {
    _total = "size(result.data)"
  }
}
```

The `response` block is only valid for `after` aspects. Body fields (CEL expressions) are merged into every row of the response. Headers are set as HTTP headers by the REST connector (or mapped to protocol equivalents by other connectors, e.g., gRPC metadata). Useful for API versioning, deprecation notices, pagination metadata, CORS, or any cross-cutting response decoration.

### on_error variables

In `on_error` aspects, the `error` variable is a structured object:

| Field | Type | Description |
|-------|------|-------------|
| `error.message` | string | The error message |
| `error.code` | int | HTTP status code (e.g., 404, 500) or 0 if unknown |
| `error.type` | string | Error category (see below) |

Error types: `http` (from HTTP/GraphQL client), `flow` (from error_response block), `validation` (input validation failed), `not_found`, `timeout`, `connection`, `auth`, `unknown`.

```hcl
# Route errors by status code
aspect "alert_5xx" {
  when = "on_error"
  on   = ["*"]
  if   = "error.code >= 500"

  action {
    connector = "slack"
    transform {
      text = "':rotating_light: ' + _flow + ' failed (' + string(error.code) + '): ' + error.message"
    }
  }
}

# Route errors by type
aspect "handle_timeouts" {
  when = "on_error"
  on   = ["*"]
  if   = "error.type == 'timeout'"

  action {
    connector = "slack"
    transform {
      text = "':hourglass: Timeout in ' + _flow"
    }
  }
}
```

---

## security

Input sanitization configuration:

```hcl
security {
  max_input_length = 2097152   # 2 MB for a whole payload
  max_field_length = 131072    # per field
  max_field_depth  = 20        # how deeply a payload may nest

  sanitizer "NAME" {
    source     = "wasm"
    wasm       = "./wasm/sanitizer.wasm"
    entrypoint = "sanitize"
    apply_to   = ["flows/api/*"]
    fields     = ["email", "phone"]
  }
}
```

---

## auth

Full auth system configuration. See [Auth Guide](../guides/auth.md) for complete reference.

```hcl
auth {
  preset = "standard"    # "strict", "standard", "relaxed", "development"

  jwt {
    secret           = env("JWT_SECRET")
    algorithm        = "HS256"
    access_lifetime  = "15m"
    refresh_lifetime = "7d"
  }

  storage {
    driver    = "database"   # memory | redis | database
    connector = "db"
  }

  password {
    algorithm      = "argon2id"
    min_length     = 8
    require_upper  = true
    require_number = true
  }

  security {
    brute_force {
      enabled      = true
      max_attempts = 5
      window       = "15m"
      lockout_time = "1h"
      track_by     = "ip+user"   # ip | user | ip+user

      # Each failure after the first makes the next attempt wait longer.
      progressive_delay {
        enabled    = true
        initial    = "1s"
        max        = "30s"
        multiplier = 2
      }
    }
  }

  mfa {
    enabled  = true
    required = false
    methods  = ["totp", "webauthn"]
  }
}
```

---

## saga

Distributed transaction with compensation:

```hcl
saga "NAME" {
  timeout = "7d"

  from {
    connector = "api"
    operation = "POST /orders"
  }

  step "STEP_NAME" {
    on_error = "skip"

    action {
      connector = "db"
      operation = "INSERT"
      target    = "orders"
      data      = { status = "pending" }
    }

    compensate {
      connector = "db"
      operation = "DELETE"
      target    = "orders"
      where     = { id = "step.STEP_NAME.id" }
    }

    # For long-running workflows:
    delay = "24h"                # Pause for duration
    await = "event_name"         # Pause until signal
  }

  on_complete {
    connector = "db"
    operation = "UPDATE"
    target    = "orders"
    set       = { status = "confirmed" }
    where     = { id = "step.order.id" }
  }

  on_failure {
    connector = "notifications"
    operation = "POST /send"
  }
}
```

---

## state_machine

Entity lifecycle state management:

```hcl
state_machine "NAME" {
  initial = "pending"

  state "pending" {
    on "EVENT_NAME" {
      transition_to = "next_state"
      guard         = "input.amount > 0"  # CEL condition

      action {
        connector = "notifications"
        operation = "POST /send"
        data      = { message = "Transitioned" }
      }
    }
  }

  state "completed" {
    final = true    # Cannot transition further
  }
}
```
