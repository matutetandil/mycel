# Mycel Configuration Reference

Complete HCL configuration reference for Mycel.

## Table of Contents

- [Service Configuration](#service-configuration)
- [Connectors](#connectors)
  - [REST](#rest-connector)
  - [Database](#database-connectors)
  - [GraphQL](#graphql-connector)
  - [gRPC](#grpc-connector)
  - [Message Queues](#message-queue-connectors)
  - [TCP](#tcp-connector)
  - [Cache](#cache-connector)
  - [Files](#files-connector)
  - [S3](#s3-connector)
  - [Exec](#exec-connector)
  - [Profiles](#connector-profiles)
- [Flows](#flows)
- [Synchronization](#synchronization)
  - [Flow Triggers](#flow-triggers-when)
  - [Lock (Mutex)](#lock-mutex)
  - [Semaphore](#semaphore)
  - [Coordinate](#coordinate)
- [Types](#types)
- [Transforms](#transforms)
- [Named Caches](#named-caches)

---

## Service Configuration

Every Mycel project should have a `service` block in `config.hcl`. The `name` and `version` are exposed in health checks, Prometheus metrics, and startup logs.

```hcl
service {
  name    = "my-service"
  version = "1.0.0"

  # Optional: Rate limiting (disabled by default)
  rate_limit {
    requests_per_second = 100
    burst               = 200
    key_extractor       = "ip"           # "ip", "header:X-API-Key", "query:api_key"
    exclude_paths       = ["/health", "/metrics"]
    enable_headers      = true           # X-RateLimit-* headers
  }
}
```

### Rate Limit Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable/disable rate limiting |
| `requests_per_second` | float | `100` | Token refill rate |
| `burst` | int | `200` | Maximum burst size |
| `key_extractor` | string | `"ip"` | How to identify clients |
| `exclude_paths` | list | `["/health", "/metrics"]` | Paths to exclude |
| `enable_headers` | bool | `true` | Add rate limit headers |

---

## Connectors

### REST Connector

Expose HTTP endpoints:

```hcl
connector "api" {
  type = "rest"
  port = 3000

  # Optional: CORS configuration
  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    headers = ["Content-Type", "Authorization"]
  }
}
```

HTTP Client:

```hcl
connector "external_api" {
  type     = "http"
  base_url = "https://api.example.com"
  timeout  = "30s"

  # Authentication options
  auth {
    type  = "bearer"    # "bearer", "api_key", "basic", "oauth2"
    token = env("API_TOKEN")
  }

  # For API Key auth
  auth {
    type   = "api_key"
    header = "X-API-Key"
    key    = env("API_KEY")
  }

  # For Basic auth
  auth {
    type     = "basic"
    username = env("API_USER")
    password = env("API_PASS")
  }

  # For OAuth2
  auth {
    type          = "oauth2"
    client_id     = env("OAUTH_CLIENT_ID")
    client_secret = env("OAUTH_CLIENT_SECRET")
    token_url     = "https://auth.example.com/token"
    scopes        = ["read", "write"]
  }

  # Retry configuration
  retry {
    count    = 3
    interval = "1s"
    backoff  = 2.0
  }
}
```

### Database Connectors

#### SQLite

```hcl
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"
}
```

#### PostgreSQL

```hcl
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("PG_HOST")
  port     = 5432
  database = env("PG_DATABASE")
  user     = env("PG_USER")
  password = env("PG_PASSWORD")
  ssl_mode = "require"    # "disable", "require", "verify-full"

  pool {
    max          = 100
    min          = 10
    max_lifetime = 300    # seconds
  }
}
```

#### MySQL

```hcl
connector "mysql" {
  type     = "database"
  driver   = "mysql"
  host     = env("MYSQL_HOST")
  port     = 3306
  database = env("MYSQL_DATABASE")
  user     = env("MYSQL_USER")
  password = env("MYSQL_PASSWORD")
  charset  = "utf8mb4"

  pool {
    max          = 100
    min          = 10
    max_lifetime = 300
  }
}
```

#### MongoDB

```hcl
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  uri      = env("MONGO_URI")
  database = "myapp"

  pool {
    max             = 200
    min             = 10
    connect_timeout = 30
  }
}
```

### GraphQL Connector

Server:

```hcl
connector "graphql_api" {
  type       = "graphql"
  driver     = "server"
  port       = 4000
  endpoint   = "/graphql"
  playground = true

  # Optional: Load schema from file
  schema {
    path = "./schema.graphql"
  }

  # Optional: Federation support
  federation {
    enabled = true
    version = 2
  }

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
  }
}
```

Client:

```hcl
connector "external_graphql" {
  type        = "graphql"
  driver      = "client"
  endpoint    = "https://api.example.com/graphql"
  timeout     = "30s"
  retry_count = 3

  auth {
    type  = "bearer"
    token = env("GRAPHQL_TOKEN")
  }
}
```

### gRPC Connector

Server:

```hcl
connector "grpc_api" {
  type   = "grpc"
  driver = "server"
  port   = 50051

  proto {
    path    = "./proto/service.proto"
    service = "MyService"
  }

  # Optional: TLS
  tls {
    cert_file = "/path/to/cert.pem"
    key_file  = "/path/to/key.pem"
  }
}
```

Client:

```hcl
connector "grpc_service" {
  type    = "grpc"
  driver  = "client"
  address = "localhost:50051"

  proto {
    path    = "./proto/service.proto"
    service = "MyService"
  }
}
```

### Message Queue Connectors

#### RabbitMQ

```hcl
connector "rabbitmq" {
  type   = "mq"
  driver = "rabbitmq"
  url    = env("RABBITMQ_URL")

  # Consumer settings
  consumer {
    queue       = "my-queue"
    prefetch    = 10
    auto_ack    = false
    workers     = 5
    retry_count = 3
  }

  # Publisher settings
  publisher {
    exchange    = "my-exchange"
    routing_key = "my.routing.key"
    mandatory   = false
    immediate   = false
  }
}
```

#### Kafka

```hcl
connector "kafka" {
  type    = "mq"
  driver  = "kafka"
  brokers = ["kafka1:9092", "kafka2:9092"]

  # Consumer settings
  consumer {
    group_id = "my-consumer-group"
    topics   = ["my-topic"]
    offset   = "latest"    # "earliest", "latest"
  }

  # Producer settings
  producer {
    topic       = "my-topic"
    acks        = "all"    # "none", "leader", "all"
    compression = "gzip"   # "none", "gzip", "snappy", "lz4"
  }

  # SASL auth
  sasl {
    mechanism = "PLAIN"
    username  = env("KAFKA_USER")
    password  = env("KAFKA_PASS")
  }
}
```

### TCP Connector

Server:

```hcl
connector "tcp_server" {
  type   = "tcp"
  driver = "server"
  host   = "0.0.0.0"
  port   = 9000
  codec  = "json"    # "json", "msgpack", "raw", "nestjs"
}
```

Client:

```hcl
connector "tcp_client" {
  type    = "tcp"
  driver  = "client"
  address = "localhost:9000"
  codec   = "json"
  timeout = "10s"
}
```

### Cache Connector

Memory:

```hcl
connector "cache" {
  type        = "cache"
  driver      = "memory"
  max_items   = 10000
  eviction    = "lru"
  default_ttl = "5m"
}
```

Redis:

```hcl
connector "redis_cache" {
  type       = "cache"
  driver     = "redis"
  address    = "localhost:6379"
  password   = env("REDIS_PASSWORD")
  db         = 0
  key_prefix = "myapp:"

  pool {
    max_connections = 100
    min_connections = 10
  }
}
```

### Files Connector

```hcl
connector "files" {
  type      = "file"
  driver    = "local"
  base_path = "/data/files"

  # Optional: File permissions
  permissions {
    file_mode = "0644"
    dir_mode  = "0755"
  }
}
```

### S3 Connector

```hcl
connector "s3" {
  type   = "file"
  driver = "s3"

  bucket     = env("S3_BUCKET")
  region     = env("AWS_REGION")
  access_key = env("AWS_ACCESS_KEY_ID")
  secret_key = env("AWS_SECRET_ACCESS_KEY")

  # For MinIO or other S3-compatible storage
  endpoint         = "http://localhost:9000"
  force_path_style = true
}
```

### Exec Connector

Local:

```hcl
connector "script" {
  type   = "exec"
  driver = "local"

  command       = "/usr/bin/python3"
  args          = ["script.py"]
  timeout       = "30s"
  working_dir   = "/app/scripts"
  input_format  = "json"     # "args", "stdin", "json"
  output_format = "json"     # "text", "json", "lines"

  env {
    CUSTOM_VAR = "value"
  }
}
```

SSH:

```hcl
connector "remote" {
  type   = "exec"
  driver = "ssh"

  command = "uptime"

  ssh {
    host     = "server.example.com"
    port     = 22
    user     = "admin"
    key_file = "/path/to/key"
  }
}
```

### Connector Profiles

Profiles allow a single logical connector to have multiple backend implementations with automatic selection and failover.

**Use case:** A pricing microservice that fetches data from different sources depending on environment.

```hcl
connector "pricing" {
  type = "profiled"

  # Profile selection via environment variable
  select  = "env('PRICE_SOURCE')"    # CEL expression
  default = "magento"                 # Fallback if select is empty

  # Fallback chain if primary fails
  fallback = ["erp", "legacy"]

  # Magento API profile
  profile "magento" {
    type     = "http"
    driver   = "client"
    base_url = "http://magento/api"

    auth {
      type  = "bearer"
      token = env("MAGENTO_TOKEN")
    }

    # Normalize response to common format
    transform {
      product_id = "input.entity_id"
      sku        = "input.sku"
      price      = "double(input.price)"
      currency   = "'USD'"
      source     = "'magento'"
    }
  }

  # ERP Database profile
  profile "erp" {
    type     = "database"
    driver   = "postgres"
    host     = env("ERP_HOST")
    database = "erp"
    user     = env("ERP_USER")
    password = env("ERP_PASSWORD")

    transform {
      product_id = "string(input.id)"
      sku        = "input.codigo"
      price      = "input.precio"
      currency   = "input.moneda"
      source     = "'erp'"
    }
  }

  # Legacy API profile
  profile "legacy" {
    type     = "http"
    driver   = "client"
    base_url = "http://legacy:9090/api"

    transform {
      product_id = "input.prod_id"
      sku        = "input.product_code"
      price      = "double(input.unit_price)"
      currency   = "input.currency_code"
      source     = "'legacy'"
    }
  }
}
```

#### Profile Options

| Option | Type | Description |
|--------|------|-------------|
| `select` | string | CEL expression to determine active profile |
| `default` | string | Default profile when select is empty/null |
| `fallback` | list | Ordered list of fallback profiles |

#### Profile Block

Each profile inherits standard connector options for its type:

| Option | Type | Description |
|--------|------|-------------|
| `type` | string | Connector type (http, database, graphql, etc.) |
| `driver` | string | Driver for the connector type |
| `transform` | block | CEL expressions to normalize output |

#### Fallback Behavior

When the active profile fails with a retriable error (connection timeout, 5xx):
1. Mycel logs the failure and tries the next profile in `fallback`
2. If all profiles fail, returns the last error
3. Non-retriable errors (4xx, validation) do not trigger fallback

#### Metrics

Profile usage is tracked in Prometheus:
- `mycel_connector_profile_active` - Currently active profile
- `mycel_connector_profile_requests_total` - Requests per profile
- `mycel_connector_profile_errors_total` - Errors per profile
- `mycel_connector_profile_fallback_total` - Fallback events

---

## Flows

Basic flow structure:

```hcl
flow "flow_name" {
  from {
    connector = "source_connector"
    operation = "GET /path/:id"
  }

  # Optional: Data enrichment
  enrich "enrichment_name" {
    connector = "other_connector"
    operation = "getData"
    params {
      id = "input.foreign_id"
    }
  }

  # Optional: Transform data
  transform {
    id         = "uuid()"
    email      = "lower(input.email)"
    created_at = "now()"
    extra_data = "enriched.enrichment_name.field"
  }

  # Optional: Use named transform
  transform {
    use = "transform_name"
  }

  to {
    connector = "target_connector"
    target    = "table_name"
  }

  # Optional: Caching
  cache {
    storage = "cache_connector"
    ttl     = "5m"
    key     = "resource:${input.id}"
  }

  # Optional: Post-action (e.g., cache invalidation)
  after {
    invalidate {
      storage  = "cache_connector"
      keys     = ["resource:${input.id}"]
      patterns = ["list:resources:*"]
    }
  }
}
```

### From Block Options

```hcl
from {
  connector = "api"
  operation = "POST /users"    # METHOD /path for REST
  operation = "Query.users"    # Type.field for GraphQL
  operation = "GetUser"        # Method name for gRPC
}
```

### To Block Options

```hcl
to {
  connector = "db"
  target    = "users"          # Table/collection name

  # Raw SQL (for complex queries)
  query = <<-SQL
    SELECT u.*, o.total
    FROM users u
    JOIN orders o ON o.user_id = u.id
    WHERE u.id = :id
  SQL

  # MongoDB filter
  query_filter = {
    status = "active"
    age    = { "$gte" = 18 }
  }

  # MongoDB update
  update = {
    "$set" = {
      status     = "input.status"
      updated_at = "now()"
    }
  }
}
```

---

## Synchronization

Mycel provides synchronization primitives for coordinating distributed flow execution.

### Flow Triggers (`when`)

Control when a flow executes:

```hcl
# Default: triggered by from block (request/message)
flow "get_users" {
  # when = "always"  # implicit default
  from { connector = "api", operation = "GET /users" }
  to   { connector = "db", target = "users" }
}

# Cron schedule (standard 5-field format)
flow "daily_cleanup" {
  when = "0 3 * * *"  # minute hour day month weekday
  to {
    connector = "db"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

# Interval schedule
flow "health_ping" {
  when = "@every 5m"
  to { connector = "monitoring", target = "POST /ping" }
}
```

#### When Values

| Value | Description |
|-------|-------------|
| `"always"` | Default. Triggered by `from` block |
| `"0 3 * * *"` | Cron expression (min hour day month weekday) |
| `"@every 5m"` | Interval (supports: s, m, h) |
| `"@hourly"` | Shortcut for `0 * * * *` |
| `"@daily"` | Shortcut for `0 0 * * *` |
| `"@weekly"` | Shortcut for `0 0 * * 0` |
| `"@monthly"` | Shortcut for `0 0 1 * *` |

### Lock (Mutex)

Distributed mutex for exclusive access by key:

```hcl
flow "process_payment" {
  from { connector = "rabbitmq", operation = "queue:payments" }

  lock {
    storage = "redis"                           # Cache connector to use
    key     = "'user:' + input.body.user_id"    # CEL expression
    timeout = "30s"                              # Max time to hold lock
    wait    = true                               # Wait for lock or fail immediately
    retry   = "100ms"                            # Retry interval when waiting
  }

  to { connector = "db", target = "payments" }
}
```

#### Lock Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage` | string | required | Cache connector name (Redis or Memory) |
| `key` | string | required | CEL expression for lock key |
| `timeout` | duration | `"30s"` | Maximum time to hold the lock |
| `wait` | bool | `true` | Wait for lock (`true`) or fail immediately (`false`) |
| `retry` | duration | `"100ms"` | Retry interval when `wait=true` |

### Semaphore

Limit concurrent access to a resource:

```hcl
flow "call_external_api" {
  from { connector = "rabbitmq", operation = "queue:requests" }

  semaphore {
    storage     = "redis"           # Cache connector
    key         = "'external_api'"  # CEL expression
    max_permits = 10                # Max concurrent executions
    timeout     = "30s"             # Max wait time for permit
    lease       = "60s"             # Auto-release after this time (crash protection)
  }

  to { connector = "external_api", target = "POST /process" }
}
```

#### Semaphore Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage` | string | required | Cache connector name |
| `key` | string | required | CEL expression for semaphore key |
| `max_permits` | int | required | Maximum concurrent permits |
| `timeout` | duration | `"30s"` | Max wait time to acquire permit |
| `lease` | duration | `"60s"` | Auto-release time (crash protection) |

### Coordinate

Signal/Wait pattern for dependency coordination:

```hcl
flow "process_entity" {
  from { connector = "rabbitmq", operation = "queue:entities" }

  coordinate {
    storage              = "redis"
    timeout              = "60s"
    on_timeout           = "retry"    # fail | retry | skip | pass
    max_retries          = 3          # Only for on_timeout = "retry"
    max_concurrent_waits = 10         # Limit simultaneous waiters

    # Child entities wait for their parent
    wait {
      when = "input.headers.type == 'child'"
      for  = "'entity:' + input.headers.parent_id + ':ready'"
    }

    # Parent entities emit signal when processed
    signal {
      when = "input.headers.type == 'parent'"
      emit = "'entity:' + input.body.id + ':ready'"
      ttl  = "5m"
    }

    # Skip waiting if parent already exists in database
    preflight {
      connector = "postgres"
      query     = "SELECT 1 FROM entities WHERE id = :parent_id"
      params    = { parent_id = "input.headers.parent_id" }
      if_exists = "pass"    # pass | fail
    }
  }

  to { connector = "db", target = "entities" }
}
```

#### Coordinate Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage` | string | required | Cache connector name |
| `timeout` | duration | `"60s"` | Max wait time |
| `on_timeout` | string | `"fail"` | Action on timeout: `fail`, `retry`, `skip`, `pass` |
| `max_retries` | int | `3` | Max retries when `on_timeout = "retry"` |
| `max_concurrent_waits` | int | `0` | Limit waiters (0 = unlimited) |

#### Wait Block

| Option | Type | Description |
|--------|------|-------------|
| `when` | string | CEL expression: when to wait |
| `for` | string | CEL expression: signal name to wait for |

#### Signal Block

| Option | Type | Description |
|--------|------|-------------|
| `when` | string | CEL expression: when to emit signal |
| `emit` | string | CEL expression: signal name to emit |
| `ttl` | duration | Signal time-to-live |

#### Preflight Block

| Option | Type | Description |
|--------|------|-------------|
| `connector` | string | Database connector to query |
| `query` | string | SQL query with `:param` placeholders |
| `params` | map | Parameter mappings (CEL expressions) |
| `if_exists` | string | Action if query returns results: `pass` or `fail` |

#### On Timeout Actions

| Action | Behavior |
|--------|----------|
| `fail` | Error, message goes to DLQ |
| `retry` | Requeue message (up to `max_retries`), then DLQ |
| `skip` | Silently acknowledge (no error, no processing) |
| `pass` | Continue processing without the signal |

### MQ Input Structure

When consuming from message queues, the input has a structured format:

**RabbitMQ:**
```
input.body        # Parsed message payload (JSON)
input.headers     # AMQP headers
input.properties  # AMQP properties (message_id, timestamp, etc)
input.routing_key # Routing key
```

**Kafka:**
```
input.body       # Parsed message payload (JSON)
input.headers    # Kafka headers
input.key        # Message key
input.topic      # Topic name
input.partition  # Partition number
input.offset     # Offset
```

---

## Types

Define data schemas for validation:

```hcl
type "user" {
  id = string {
    format = "uuid"
  }

  email = string {
    required = true
    format   = "email"
  }

  name = string {
    required  = true
    min_length = 2
    max_length = 100
  }

  age = number {
    min = 0
    max = 150
  }

  role = string {
    enum = ["admin", "user", "guest"]
    default = "user"
  }

  metadata = object {
    required = false
  }

  tags = array {
    items = string {}
  }
}
```

### Field Types

| Type | Options |
|------|---------|
| `string` | `format`, `pattern`, `min_length`, `max_length`, `enum` |
| `number` | `min`, `max`, `integer` |
| `bool` | - |
| `object` | nested fields |
| `array` | `items`, `min_items`, `max_items` |

### String Formats

- `email` - Email address
- `uuid` - UUID v4
- `url` - Valid URL
- `date` - ISO date
- `datetime` - ISO datetime
- `phone` - Phone number

---

## Transforms

Named transforms for reuse:

```hcl
transform "normalize_user" {
  email      = "lower(input.email)"
  name       = "trim(input.name)"
  updated_at = "now()"
}

transform "with_audit" {
  created_by = "input.user_id"
  created_at = "now()"
}
```

Usage in flows:

```hcl
flow "create_user" {
  from { ... }

  transform {
    use = "normalize_user"
    # Additional/override fields
    id = "uuid()"
  }

  to { ... }
}
```

### Available CEL Functions

See [transformations.md](transformations.md) for the complete list.

**String functions:**
- `lower(s)`, `upper(s)`, `trim(s)`, `replace(s, old, new)`
- `split(s, sep)`, `join(list, sep)`, `contains(s, substr)`

**Date/Time:**
- `now()`, `timestamp(s)`, `formatDate(t, layout)`

**Encoding:**
- `base64encode(s)`, `base64decode(s)`
- `jsonEncode(v)`, `jsonDecode(s)`
- `hash_sha256(s)`, `hash_md5(s)`

**Utilities:**
- `uuid()`, `env(name)`, `default(v, fallback)`
- `coalesce(v1, v2, ...)`, `len(v)`

---

## Named Caches

Define reusable cache configurations:

```hcl
cache "products" {
  storage = "cache"      # Reference to cache connector
  ttl     = "10m"
  prefix  = "products"
}

cache "sessions" {
  storage = "redis_cache"
  ttl     = "24h"
  prefix  = "sessions"
}
```

Usage in flows:

```hcl
flow "get_product" {
  from { ... }
  to   { ... }

  cache {
    use = "products"
    key = "${input.id}"
  }
}
```

---

## Environment Variables

Access environment variables:

```hcl
connector "db" {
  password = env("DB_PASSWORD")
  host     = env("DB_HOST")
}
```

Load from files:

```hcl
connector "api" {
  token = file("/run/secrets/api_token")
}
```

---

## CLI Commands

```bash
# Start the runtime
mycel start --config ./config --env prod --hot-reload

# Validate configuration
mycel validate --config ./config

# Check connector connectivity
mycel check --config ./config

# Show version
mycel version
```

### Start Options

| Flag | Default | Description |
|------|---------|-------------|
| `--config`, `-c` | `.` | Configuration directory |
| `--env`, `-e` | `dev` | Environment |
| `--verbose`, `-v` | `false` | Enable debug logging |
| `--hot-reload` | `true` | Auto-reload on config changes |
