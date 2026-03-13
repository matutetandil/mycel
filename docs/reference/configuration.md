# Configuration Reference

Complete HCL syntax reference for all Mycel block types. Every block is documented with all supported attributes.

## Table of Contents

- [service](#service)
- [connector](#connector)
- [flow](#flow)
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
Error: duplicate flow name "create_user": defined in flows/api.hcl and flows/users.hcl
```

Names can overlap across different types (e.g., a connector and a flow can both be named `"users"`), but two connectors cannot share the same name.

---

## service

Global service configuration. Place in `config.hcl`.

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
  }

  workflow {
    storage     = "db"              # Database connector name
    table       = "mycel_workflows" # Table name (default: mycel_workflows)
    auto_create = true              # Create table on startup
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

### workflow attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `storage` | string | required | Database connector name |
| `table` | string | `"mycel_workflows"` | Table name |
| `auto_create` | bool | `true` | Auto-create table |

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
    count    = 3
    interval = "1s"
    backoff  = 2.0
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
    query  = "SELECT * FROM users WHERE email = $1"
    params = [{ name = "email", type = "string", required = true }]
  }
}
```

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

  schema {
    path          = "./schema.graphql"
    auto_generate = true              # Auto-generate from type blocks
  }

  federation {
    enabled = true
    version = 2
  }

  subscriptions {
    enabled   = true
    transport = "websocket"
    path      = "/graphql/ws"
    keepalive = "30s"
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
  retry_count = 3

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
    cert_file = "/certs/server.crt"
    key_file  = "/certs/server.key"
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
  type           = "queue"
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
  type      = "queue"
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
  type     = "queue"
  driver   = "redis"
  address  = env("REDIS_ADDRESS")   # "host:port"
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
    enabled  = true
    cert     = "/certs/client.crt"
    key      = "/certs/client.key"
    ca       = "/certs/ca.crt"
    insecure = false
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
  type     = "tcp"
  driver   = "client"
  host     = "localhost"
  port     = 9000
  protocol = "json"
  timeout  = "10s"
}
```

### Cache

```hcl
# Redis
connector "redis_cache" {
  type        = "cache"
  driver      = "redis"
  address     = env("REDIS_ADDRESS")  # "host:port"
  url         = env("REDIS_URL")      # Alternative: Redis URL
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
  force_path_style  = true                       # Required for MinIO
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
  retry_count   = 3
  retry_delay   = "1s"
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
  type              = "cdc"
  driver            = "postgres"
  connection_string = env("PG_REPLICATION_URL")
  tables            = ["orders", "products"]
}
```

### Elasticsearch

```hcl
connector "es" {
  type      = "elasticsearch"
  addresses = ["http://localhost:9200"]
  username  = env("ES_USER")
  password  = env("ES_PASSWORD")
}
```

### SOAP

```hcl
# Client
connector "soap_service" {
  type        = "soap"
  driver      = "client"
  endpoint    = "http://legacy.example.com/service"
  soap_action = "urn:operation"
  namespace   = "http://example.com/ns"
  version     = "1.1"             # "1.1" or "1.2"

  auth {
    type     = "basic"
    username = env("SOAP_USER")
    password = env("SOAP_PASS")
  }
}

# Server
connector "soap_server" {
  type       = "soap"
  driver     = "server"
  port       = 8080
  path       = "/service"
  namespace  = "http://example.com/ns"
  wsdl_path  = "/service?wsdl"   # WSDL endpoint path
  version    = "1.1"
}
```

### Connector Profiles

```hcl
connector "db" {
  type    = "database"
  driver  = "postgres"
  select  = "input.tenant_id"   # CEL expression to pick profile
  default = "primary"
  fallback = ["primary", "replica"]

  profile "primary" {
    host     = env("PRIMARY_HOST")
    database = "app"
    user     = env("DB_USER")
    password = env("DB_PASSWORD")
  }

  profile "replica" {
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
  batch { ... }
  state_transition { ... }
}
```

### from block

```hcl
from {
  connector = "api"           # Required
  operation = "GET /users"    # Required
  format    = "json"          # "json", "xml", "csv"

  # Simple filter (string)
  filter = "input.status == 'active'"

  # Full filter block (for MQ rejection policies)
  filter {
    condition   = "input.amount > 0"
    on_reject   = "requeue"  # "ack", "reject", "requeue"
    id_field    = "input.payment_id"
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
  format       = "json"         # "json", "xml"
  filter       = "input.user_id == context.params.userId"  # For subscriptions/WS/SSE
  query        = "SELECT * FROM users WHERE id = :id"      # Custom SQL
  query_filter = { status = "active" }                     # MongoDB filter
  update       = { "$set" = { status = "active" } }        # MongoDB update
  params       = { key = "value" }                         # Extra params (e.g., S3 COPY)
  when         = "output.amount > 0"                       # Conditional write
  parallel     = true                                      # Parallel multi-to (default: true)

  transform { ... }    # Per-destination transform
}
```

### step block

```hcl
step "NAME" {
  connector = "db"            # Required
  operation = "query"
  query     = "SELECT * FROM users WHERE id = ?"
  target    = "users"
  params    = [input.params.id]
  body      = { key = "value" }
  format    = "json"
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
  key           = "'product:' + input.params.id"
  invalidate_on = ["product.updated"]
  use           = "cache.products"    # Reference named cache
}
```

### after block

```hcl
after {
  invalidate {
    storage  = "redis_cache"    # Required
    keys     = ["product:${input.params.id}"]
    patterns = ["products:list:*"]
  }
}
```

### dedupe block

```hcl
dedupe {
  storage      = "redis_cache"         # Required
  key          = "input.payment_id"    # Required: CEL expression
  ttl          = "24h"
  on_duplicate = "skip"                # "skip" or "error"
}
```

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
  storage = "connector.redis"    # Required
  key     = "'account:' + input.account_id"  # Required
  timeout = "30s"
  wait    = true
  retry   = "100ms"
}
```

### semaphore block

```hcl
semaphore {
  storage = "connector.redis"    # Required
  key     = "'api_quota'"        # Required
  limit   = 10                   # Required
  timeout = "5s"
}
```

### coordinate block

```hcl
coordinate {
  storage = "connector.redis"    # Required
  key     = "input.batch_id"     # Required
  signal  = "batch_ready"        # Emit signal (producer)
  wait    = "batch_ready"        # Wait for signal (consumer)
  timeout = "60s"
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
  machine = "order_status"      # state_machine block name
  entity  = "orders"            # Database table
  id      = "input.params.id"
  event   = "input.event"
  data    = "input.data"
}
```

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
  field_name = base_type { constraint = value, ... }
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
}
```

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

  condition = "result.status == 'ok'"  # Optional CEL condition

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
  max_input_size = 2097152   # 2 MB (default: 1 MB)
  max_depth      = 20        # Nesting depth (default: 10)
  max_string_len = 100000    # Per-string limit (default: 50000)

  sanitizer "NAME" {
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
    secret      = env("JWT_SECRET")
    algorithm   = "HS256"
    access_ttl  = "15m"
    refresh_ttl = "7d"
  }

  storage {
    users    = "connector.db"
    sessions = "connector.redis"
  }

  password {
    hashing = "argon2id"
    min_length = 8
    require_uppercase = true
    require_number    = true
  }

  brute_force {
    max_attempts = 5
    window       = "15m"
    lockout      = "1h"
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
