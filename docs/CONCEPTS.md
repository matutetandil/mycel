# Mycel Concepts

This document defines all Mycel concepts and their configuration.

---

## 1. Core Concepts

### Connector

**What it is:** Bidirectional adapter that connects Mycel with an external system (database, API, queue, file, etc).

**Operation modes:**
- **Input (Source):** Receives data or events that trigger a flow. Examples: exposed REST endpoint, message consumed from a queue, incoming gRPC request.
- **Output (Target):** Destination where the flow writes data. Examples: INSERT into database, HTTP call to another API, publish message to queue.

**Note:** Some connectors are input-only (e.g., cron), others are output-only (e.g., email/notifications), and most can be either depending on the context.

---

### Flow

**What it is:** Unit of work that defines the data path. Connects an input with an output, optionally transforming the data in between.

**Structure:**
```hcl
flow "name" {
  from { connector.input = "trigger" }   # What triggers the flow

  transform { ... }                       # Optional: transform data

  to { connector.output = "destination" } # Where the data goes
}
```

**When it executes:** When the `from` connector receives an event (HTTP request, queue message, etc) or according to the configured trigger (cron, interval).

---

### Transform

**What it is:** Data transformation using CEL (Common Expression Language) expressions. Maps input fields to output fields.

**Modes:**
- **Inline:** Defined within the flow, for single use.
- **Reusable:** Defined in a separate file, referenced with `use`.
- **Composition:** Combine multiple transforms with override.

```hcl
# Inline
transform {
  output.id = "uuid()"
  output.email = "lower(input.email)"
  output.created_at = "now()"
}

# Reusable with composition
transform {
  use = [transform.normalize_user, transform.add_timestamps]
  output.source = "'api'"  # Override
}
```

---

### Type

**What it is:** Schema that defines the expected data structure. Validates fields, types, and formats.

**Usage:** Validate flow input, or output before sending.

```hcl
type "user" {
  id       = string { required = true }
  email    = string { format = "email" }
  age      = number { min = 0, max = 150 }
  role     = string { enum = ["admin", "user", "guest"] }
  metadata = object { optional = true }
}
```

**Validation in flow:**
```hcl
flow "create_user" {
  from { ... }
  input_type = type.user    # Validate input
  output_type = type.user   # Validate output
  to { ... }
}
```

---

### Validator

**What it is:** Custom validation rule for fields that require special logic beyond built-in types.

**Types:**
- **regex:** Regular expression pattern
- **cel:** CEL expression that returns true/false
- **wasm:** Compiled WASM module for complex validations

```hcl
# Regex
validator "cuit_argentina" {
  type    = "regex"
  pattern = "^(20|23|24|27|30|33|34)\\d{8}\\d$"
  message = "Invalid CUIT"
}

# CEL
validator "adult" {
  type    = "cel"
  expr    = "value >= 18"
  message = "Must be of legal age"
}

# Usage in type
type "customer" {
  cuit = string { validate = validator.cuit_argentina }
  age  = number { validate = validator.adult }
}
```

---

## 2. Connectors by Type

### REST

**What it is:** HTTP protocol for web APIs. The most common connector.

#### As Input (Server) - Expose endpoints

Mycel acts as an HTTP server, exposing endpoints that trigger flows.

```hcl
connector "api" {
  type = "rest"
  mode = "server"  # Implicit when it has port

  port = 8080
  host = "0.0.0.0"  # Optional, default 0.0.0.0

  # TLS
  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }

  # CORS
  cors {
    origins = ["https://app.example.com"]
    methods = ["GET", "POST", "PUT", "DELETE"]
    headers = ["Authorization", "Content-Type"]
    credentials = true
    max_age = 3600
  }

  # Global rate limiting
  rate_limit {
    requests = 100
    window   = "1m"
    by       = "ip"  # ip, header, query
  }

  # Incoming authentication (validate requests)
  # GAP: Not yet implemented
  auth {
    type = "jwt"  # jwt, api_key, basic, oauth2

    # For JWT
    jwt {
      secret      = env("JWT_SECRET")      # Or jwks_url
      jwks_url    = "https://..."          # To validate with JWKS
      issuer      = "https://auth.example.com"
      audience    = ["my-api"]
      algorithms  = ["RS256", "HS256"]
    }

    # For API Key
    api_key {
      header = "X-API-Key"           # Or query param
      keys   = [env("API_KEY_1")]    # List of valid keys
      # Or validate against DB/external service
      validate = { connector.keys_db = "api_keys" }
    }

    # For Basic Auth
    basic {
      users = {
        admin = env("ADMIN_PASSWORD")
      }
      # Or validate against DB
      validate = { connector.users_db = "users" }
    }

    # Public routes (no auth)
    public = ["/health", "/metrics", "/docs/*"]
  }

  # Required headers
  # GAP: Not implemented
  required_headers = ["X-Request-ID", "X-Correlation-ID"]

  # Headers added to all responses
  # GAP: Not implemented
  response_headers {
    "X-Powered-By" = "Mycel"
    "X-Request-ID" = "${request.id}"
  }
}
```

#### As Output (Client) - Call external APIs

Mycel acts as an HTTP client, calling external APIs.

```hcl
connector "external_api" {
  type = "rest"
  mode = "client"  # Implicit when it has base_url

  base_url = env("EXTERNAL_API_URL")

  # Timeout and retry
  timeout = "30s"
  retry {
    attempts = 3
    backoff  = "exponential"  # exponential, linear, constant
    initial  = "1s"
    max      = "30s"
  }

  # Outgoing authentication (to authenticate with the API)
  # GAP: Only basic implemented, missing OAuth2, dynamic API Key
  auth {
    type = "bearer"  # bearer, basic, api_key, oauth2, custom

    # Static bearer token
    bearer {
      token = env("API_TOKEN")
    }

    # Basic auth
    basic {
      username = env("API_USER")
      password = env("API_PASS")
    }

    # API Key
    api_key {
      header = "X-API-Key"       # Or "query" for query param
      name   = "api_key"         # Param name if query
      value  = env("API_KEY")
    }

    # OAuth2 Client Credentials
    # GAP: Not implemented
    oauth2 {
      grant_type    = "client_credentials"
      token_url     = "https://auth.example.com/oauth/token"
      client_id     = env("CLIENT_ID")
      client_secret = env("CLIENT_SECRET")
      scopes        = ["read", "write"]
      # Automatic token caching
    }

    # OAuth2 with refresh token
    # GAP: Not implemented
    oauth2 {
      grant_type    = "refresh_token"
      token_url     = "https://auth.example.com/oauth/token"
      refresh_token = env("REFRESH_TOKEN")
      client_id     = env("CLIENT_ID")
    }

    # Custom header
    custom {
      headers = {
        "X-Custom-Auth" = env("CUSTOM_TOKEN")
        "X-Tenant-ID"   = "tenant-123"
      }
    }
  }

  # Static headers for all requests
  headers {
    "Accept"       = "application/json"
    "User-Agent"   = "Mycel/1.0"
    "X-Request-ID" = "${uuid()}"  # Dynamic per request
  }

  # Circuit breaker
  circuit_breaker {
    threshold         = 5      # Failures to open
    timeout           = "30s"  # Time in open before half-open
    success_threshold = 2      # Successes to close
  }

  # Custom TLS (for APIs with custom certs)
  # GAP: Not implemented
  tls {
    ca_cert             = "/path/to/ca.pem"
    client_cert         = "/path/to/client-cert.pem"
    client_key          = "/path/to/client-key.pem"
    insecure_skip_verify = false  # Only for development
  }
}
```

**Usage in flows:**
```hcl
# As input (server)
flow "get_users" {
  from { connector.api = "GET /users" }
  to   { connector.database = "users" }
}

# As output (client)
flow "sync_to_external" {
  from { connector.database = "SELECT * FROM users WHERE synced = false" }
  to   { connector.external_api = "POST /users" }
}
```

---

### Database (SQL)

**What it is:** Connection to relational databases (PostgreSQL, MySQL, SQLite).

**Drivers:** `postgres`, `mysql`, `sqlite`

#### Common configuration

```hcl
connector "db" {
  type   = "database"
  driver = "postgres"  # postgres, mysql, sqlite

  # Connection
  host     = env("DB_HOST")
  port     = 5432
  database = env("DB_NAME")
  username = env("DB_USER")
  password = env("DB_PASS")

  # Or connection string
  # dsn = env("DATABASE_URL")

  # Connection pool
  pool {
    max_open = 25       # Maximum open connections
    max_idle = 5        # Maximum idle connections
    max_lifetime = "1h" # Maximum connection lifetime
  }

  # SSL/TLS
  ssl {
    mode     = "require"  # disable, require, verify-ca, verify-full
    ca_cert  = "/path/to/ca.pem"
    cert     = "/path/to/client-cert.pem"
    key      = "/path/to/client-key.pem"
  }

  # Read replica (for read queries)
  # GAP: Not implemented
  replica {
    host     = env("DB_REPLICA_HOST")
    port     = 5432
    # Inherits credentials from primary
  }

  # Default schema
  schema = "public"  # PostgreSQL
}
```

#### As Input (read data)

```hcl
flow "get_users" {
  from { connector.api = "GET /users" }
  to   { connector.db = "users" }  # SELECT * FROM users
}

flow "get_user_by_id" {
  from { connector.api = "GET /users/:id" }
  to   { connector.db = "users WHERE id = :id" }
}

# Raw query
flow "complex_query" {
  from { connector.api = "GET /reports/sales" }
  to   {
    connector.db = <<SQL
      SELECT
        date_trunc('month', created_at) as month,
        SUM(amount) as total
      FROM orders
      WHERE status = 'completed'
      GROUP BY 1
      ORDER BY 1
    SQL
  }
}
```

#### As Output (write data)

```hcl
flow "create_user" {
  from { connector.api = "POST /users" }
  to   { connector.db = "INSERT users" }
}

flow "update_user" {
  from { connector.api = "PUT /users/:id" }
  to   { connector.db = "UPDATE users WHERE id = :id" }
}

flow "delete_user" {
  from { connector.api = "DELETE /users/:id" }
  to   { connector.db = "DELETE users WHERE id = :id" }
}
```

---

### Database (NoSQL - MongoDB)

**What it is:** Connection to MongoDB.

```hcl
connector "mongo" {
  type   = "database"
  driver = "mongodb"

  # Connection
  uri      = env("MONGO_URI")  # mongodb://user:pass@host:27017/db
  database = "myapp"

  # Or by parts
  host     = env("MONGO_HOST")
  port     = 27017
  username = env("MONGO_USER")
  password = env("MONGO_PASS")

  # Options
  auth_source  = "admin"
  replica_set  = "rs0"

  # Pool
  pool {
    min = 5
    max = 100
  }

  # TLS
  tls {
    enabled  = true
    ca_cert  = "/path/to/ca.pem"
  }
}
```

**Usage:**
```hcl
# Input: read documents
flow "get_products" {
  from { connector.api = "GET /products" }
  to   { connector.mongo = "products" }  # find in collection
}

# Output: write documents
flow "create_product" {
  from { connector.api = "POST /products" }
  to   { connector.mongo = "INSERT products" }
}

# Query with filter
flow "get_active_products" {
  from { connector.api = "GET /products/active" }
  to   {
    connector.mongo = {
      collection = "products"
      filter     = { status = "active" }
      sort       = { created_at = -1 }
      limit      = 100
    }
  }
}
```

---

### Message Queue (RabbitMQ)

**What it is:** Connection to RabbitMQ for asynchronous messaging.

#### Configuration

```hcl
connector "rabbit" {
  type   = "queue"
  driver = "rabbitmq"

  # Connection
  host     = env("RABBIT_HOST")
  port     = 5672
  username = env("RABBIT_USER")
  password = env("RABBIT_PASS")
  vhost    = "/"

  # Or connection string
  # url = "amqp://user:pass@host:5672/vhost"

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # Heartbeat and timeouts
  heartbeat        = "10s"
  connection_timeout = "30s"

  # Default exchange
  exchange {
    name    = "myapp"
    type    = "topic"      # direct, topic, fanout, headers
    durable = true
    auto_delete = false
  }

  # Prefetch (for consumers)
  prefetch = 10

  # Automatic reconnection
  reconnect {
    enabled  = true
    interval = "5s"
    max_attempts = 0  # 0 = infinite
  }
}
```

#### As Input (Consumer) - Read messages

Mycel consumes messages from a queue and executes the flow.

```hcl
flow "process_order" {
  from {
    connector.rabbit = {
      queue = "orders"

      # Queue configuration
      durable     = true
      auto_delete = false
      exclusive   = false

      # Binding (from which exchange/routing key)
      bind {
        exchange    = "myapp"
        routing_key = "order.created"
      }

      # Consumer
      consumer_tag = "mycel-orders"
      auto_ack     = false  # Manual ack after processing

      # DLQ for failed messages
      # GAP: Partially implemented
      dlq {
        enabled     = true
        queue       = "orders.dlq"
        exchange    = "myapp.dlq"
        routing_key = "order.failed"
        max_retries = 3
      }

      # Message parsing
      format = "json"  # json, msgpack, protobuf, raw
    }
  }

  to { connector.db = "INSERT orders" }
}
```

**Access to message headers:**
```hcl
transform {
  # The message is structured as:
  # input.body    = message content
  # input.headers = AMQP headers
  # input.properties = properties (correlation_id, message_id, etc)

  order_id       = "input.body.id"
  correlation_id = "input.properties.correlation_id"
  source         = "input.headers.x_source"
}
```

#### As Output (Producer) - Publish messages

```hcl
flow "notify_order_created" {
  from { connector.db = "SELECT * FROM orders WHERE notified = false" }

  to {
    connector.rabbit = {
      exchange    = "myapp"
      routing_key = "order.created"

      # Message properties
      persistent  = true       # Durable message
      mandatory   = true       # Error if no queue receives it

      # Custom headers
      headers {
        "x-source"   = "mycel"
        "x-priority" = "high"
      }

      # AMQP properties
      content_type   = "application/json"
      correlation_id = "${input.id}"
      message_id     = "${uuid()}"
      expiration     = "3600000"  # TTL in ms

      # Serialization format
      format = "json"
    }
  }
}
```

---

### Message Queue (Kafka)

**What it is:** Connection to Apache Kafka for event streaming.

#### Configuration

```hcl
connector "kafka" {
  type   = "queue"
  driver = "kafka"

  # Brokers
  brokers = [
    env("KAFKA_BROKER_1"),
    env("KAFKA_BROKER_2"),
    env("KAFKA_BROKER_3")
  ]

  # Authentication
  # GAP: Only SASL_PLAIN implemented
  auth {
    mechanism = "SASL_PLAIN"  # SASL_PLAIN, SASL_SCRAM_256, SASL_SCRAM_512
    username  = env("KAFKA_USER")
    password  = env("KAFKA_PASS")
  }

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # For Confluent Cloud or other managed services
  # GAP: Not implemented
  schema_registry {
    url      = "https://schema-registry.example.com"
    username = env("SR_USER")
    password = env("SR_PASS")
  }
}
```

#### As Input (Consumer)

```hcl
flow "process_events" {
  from {
    connector.kafka = {
      topic = "events"

      # Consumer group
      group_id = "mycel-events"

      # Initial offset
      offset = "earliest"  # earliest, latest, timestamp

      # Specific partitions (optional)
      partitions = [0, 1, 2]

      # Automatic or manual commit
      auto_commit = false

      # Batch processing
      # GAP: Not implemented
      batch {
        enabled  = true
        size     = 100
        timeout  = "5s"
      }

      # Format
      format = "json"  # json, avro, protobuf
    }
  }

  to { connector.db = "INSERT events" }
}
```

**Access to metadata:**
```hcl
transform {
  # input.body      = message content
  # input.key       = message key
  # input.headers   = Kafka headers
  # input.partition = partition number
  # input.offset    = message offset
  # input.timestamp = message timestamp

  event_id  = "input.body.id"
  event_key = "input.key"
  partition = "input.partition"
}
```

#### As Output (Producer)

```hcl
flow "emit_event" {
  from { connector.api = "POST /events" }

  to {
    connector.kafka = {
      topic = "events"

      # Key for partitioning
      key = "${input.user_id}"  # Messages from same user go to same partition

      # Headers
      headers {
        "event-type" = "${input.type}"
        "source"     = "mycel"
      }

      # Specific partition (overrides key-based)
      # partition = 0

      # Acks
      acks = "all"  # 0, 1, all

      # Format
      format = "json"

      # Compression
      compression = "snappy"  # none, gzip, snappy, lz4, zstd
    }
  }
}
```

---

### gRPC

**What it is:** High-performance RPC protocol based on Protocol Buffers.

#### As Input (Server) - Expose gRPC services

```hcl
connector "grpc_server" {
  type = "grpc"
  mode = "server"

  port = 50051

  # Proto files
  proto {
    path = "./protos"           # Directory with .proto files
    files = ["service.proto"]   # Or specific files
  }

  # TLS
  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }

  # Reflection (for tools like grpcurl)
  reflection = true

  # Standard gRPC health check
  health_check = true

  # Interceptors
  # GAP: Not implemented
  interceptors {
    auth {
      type = "jwt"
      # ... config similar to REST
    }
    logging = true
    metrics = true
  }

  # Max message size
  max_recv_message_size = "4MB"
  max_send_message_size = "4MB"
}
```

#### As Output (Client) - Call gRPC services

```hcl
connector "grpc_client" {
  type = "grpc"
  mode = "client"

  address = env("GRPC_SERVICE_ADDR")  # host:port

  # Proto
  proto {
    path = "./protos"
  }

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # No TLS (development)
  # insecure = true

  # Auth
  # GAP: Not implemented
  auth {
    type = "bearer"
    token = env("GRPC_TOKEN")
  }

  # Timeout and retry
  timeout = "30s"
  retry {
    attempts = 3
  }

  # Load balancing
  # GAP: Not implemented
  load_balancing = "round_robin"  # round_robin, pick_first
}
```

---

### TCP

**What it is:** Direct TCP connection for custom or legacy protocols.

#### As Input (Server)

```hcl
connector "tcp_server" {
  type = "tcp"
  mode = "server"

  port = 9000
  host = "0.0.0.0"

  # Message protocol
  protocol = "json"  # json, msgpack, line, length_prefixed, nestjs

  # For length_prefixed
  length_prefix {
    size   = 4       # Prefix bytes
    endian = "big"   # big, little
  }

  # For line protocol
  line {
    delimiter = "\n"
    max_length = 65536
  }

  # TLS
  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }

  # Connections
  max_connections = 1000
  read_timeout    = "30s"
  write_timeout   = "30s"
}
```

#### As Output (Client)

```hcl
connector "tcp_client" {
  type = "tcp"
  mode = "client"

  host = env("TCP_SERVER_HOST")
  port = 9000

  protocol = "json"

  # TLS
  tls {
    enabled = true
    ca_cert = "/path/to/ca.pem"
  }

  # Timeouts
  connect_timeout = "10s"
  read_timeout    = "30s"
  write_timeout   = "30s"

  # Reconnection
  reconnect {
    enabled  = true
    interval = "5s"
  }

  # Connection pool
  pool {
    size = 10
  }
}
```

---

### Files

**What it is:** Local file read/write.

```hcl
connector "files" {
  type = "file"

  # Base directory
  base_path = "/data"

  # Permissions for new files
  file_mode = "0644"
  dir_mode  = "0755"
}
```

**Usage:**
```hcl
# Read file
flow "import_data" {
  from { connector.files = "input/data.json" }
  to   { connector.db = "INSERT data" }
}

# Write file
flow "export_report" {
  from { connector.db = "SELECT * FROM reports" }
  to   {
    connector.files = {
      path   = "output/report.json"
      format = "json"     # json, csv, text, lines
      append = false
    }
  }
}

# With template in filename
flow "daily_export" {
  from { connector.db = "SELECT * FROM orders WHERE date = today()" }
  to   {
    connector.files = {
      path = "exports/orders_${date('YYYY-MM-DD')}.csv"
      format = "csv"
      csv {
        delimiter = ","
        header    = true
      }
    }
  }
}
```

---

### S3

**What it is:** S3-compatible object storage (AWS, MinIO, etc).

```hcl
connector "s3" {
  type = "s3"

  # AWS S3
  region     = "us-east-1"
  bucket     = env("S3_BUCKET")
  access_key = env("AWS_ACCESS_KEY")
  secret_key = env("AWS_SECRET_KEY")

  # Or S3-compatible (MinIO, etc)
  endpoint = "http://minio:9000"

  # Path style (for MinIO)
  force_path_style = true

  # Default prefix for all objects
  prefix = "mycel/"
}
```

**Usage:**
```hcl
# Read object
flow "import_from_s3" {
  from { connector.s3 = "data/input.json" }
  to   { connector.db = "INSERT data" }
}

# Write object
flow "backup_to_s3" {
  from { connector.db = "SELECT * FROM users" }
  to   {
    connector.s3 = {
      key     = "backups/users_${date('YYYY-MM-DD')}.json"
      format  = "json"

      # Metadata
      metadata {
        "x-backup-type" = "daily"
      }

      # Storage class
      storage_class = "STANDARD_IA"  # STANDARD, STANDARD_IA, GLACIER, etc
    }
  }
}

# Generate presigned URL
flow "get_download_url" {
  from { connector.api = "GET /files/:key/url" }
  to   {
    connector.s3 = {
      operation = "presign_get"
      key       = "${input.key}"
      expires   = "1h"
    }
  }
}
```

---

### Cache

**What it is:** Cache storage to speed up frequent access.

**Drivers:** `memory`, `redis`

```hcl
# Memory (local, for development or single-instance)
connector "cache" {
  type   = "cache"
  driver = "memory"

  # Limits
  max_size = "100MB"
  max_items = 10000

  # Default TTL
  ttl = "10m"

  # Eviction policy
  eviction = "lru"  # lru, lfu
}

# Redis (distributed)
connector "cache" {
  type   = "cache"
  driver = "redis"

  host     = env("REDIS_HOST")
  port     = 6379
  password = env("REDIS_PASS")
  db       = 0

  # Key prefix
  prefix = "mycel:"

  # Cluster
  # GAP: Not implemented
  cluster {
    enabled = true
    nodes   = ["redis1:6379", "redis2:6379", "redis3:6379"]
  }

  # Sentinel
  # GAP: Not implemented
  sentinel {
    master = "mymaster"
    nodes  = ["sentinel1:26379", "sentinel2:26379"]
  }
}
```

**Usage in flows:**
```hcl
flow "get_product" {
  cache {
    storage = "connector.cache"
    key     = "'product:' + input.id"
    ttl     = "5m"
  }

  from { connector.api = "GET /products/:id" }
  to   { connector.db = "products WHERE id = :id" }
}
```

---

### Exec

**What it is:** Execute system commands or scripts.

```hcl
connector "exec" {
  type = "exec"

  # Working directory
  working_dir = "/app/scripts"

  # Additional environment variables
  env {
    PATH = "/usr/local/bin:/usr/bin"
    MY_VAR = "value"
  }

  # Default timeout
  timeout = "60s"

  # Shell
  shell = "/bin/bash"

  # Remote SSH
  # GAP: Implemented but limited
  ssh {
    host     = env("SSH_HOST")
    port     = 22
    user     = env("SSH_USER")
    key_file = "/path/to/key"
    # Or password
    # password = env("SSH_PASS")
  }
}
```

**Usage:**
```hcl
flow "run_script" {
  from { connector.api = "POST /jobs/run" }
  to   {
    connector.exec = {
      command = "./process.sh"
      args    = ["${input.file}", "${input.mode}"]
      timeout = "5m"
    }
  }
}

# Pipe output
flow "get_stats" {
  from { connector.api = "GET /stats" }
  to   {
    connector.exec = {
      command = "df -h | grep /dev/sda"
      shell   = true  # Execute in shell
    }
  }
}
```

---

### GraphQL

**What it is:** Flexible query protocol for APIs.

#### As Input (Server) - Expose GraphQL API

```hcl
connector "graphql" {
  type = "graphql"
  mode = "server"

  port = 8080
  path = "/graphql"

  # Schema SDL
  schema = <<SDL
    type Query {
      users: [User!]!
      user(id: ID!): User
    }

    type Mutation {
      createUser(input: CreateUserInput!): User!
    }

    type User {
      id: ID!
      email: String!
      name: String
    }

    input CreateUserInput {
      email: String!
      name: String
    }
  SDL

  # Or from file
  # schema_file = "./schema.graphql"

  # Playground/GraphiQL
  playground = true

  # Auth (similar to REST)
  # GAP: Not integrated
  auth {
    type = "jwt"
    # ...
  }

  # Introspection
  introspection = true  # false in production

  # Limits
  max_depth       = 10
  max_complexity  = 1000
}
```

#### As Output (Client) - Call GraphQL APIs

```hcl
connector "graphql_client" {
  type = "graphql"
  mode = "client"

  endpoint = env("GRAPHQL_ENDPOINT")

  # Auth
  auth {
    type = "bearer"
    token = env("GRAPHQL_TOKEN")
  }

  # Headers
  headers {
    "X-Custom" = "value"
  }

  # Timeout
  timeout = "30s"
}
```

**Usage:**
```hcl
# Query
flow "get_external_users" {
  from { connector.api = "GET /external-users" }
  to   {
    connector.graphql_client = {
      query = <<GRAPHQL
        query GetUsers($limit: Int) {
          users(limit: $limit) {
            id
            name
            email
          }
        }
      GRAPHQL
      variables {
        limit = 100
      }
    }
  }
}

# Mutation
flow "create_external_user" {
  from { connector.api = "POST /external-users" }
  to   {
    connector.graphql_client = {
      query = <<GRAPHQL
        mutation CreateUser($input: CreateUserInput!) {
          createUser(input: $input) {
            id
            email
          }
        }
      GRAPHQL
      variables {
        input = "${input}"
      }
    }
  }
}
```

---

## 3. Synchronization

### Lock (Mutex)

**What it is:** Distributed mutual exclusion. Guarantees that only one flow processes a specific resource at a time.

**When to use:**
- Avoid duplicate processing of the same order
- Operations that cannot be concurrent (e.g., update balance)

```hcl
flow "process_order" {
  lock {
    key     = "'order:' + input.order_id"
    storage = "connector.redis"
    timeout = "30s"

    # What to do if lock cannot be acquired
    on_fail = "wait"  # wait, skip, fail
    wait_timeout = "10s"
  }

  from { connector.rabbit = "orders" }
  to   { connector.db = "UPDATE orders SET status = 'processing'" }
}
```

---

### Semaphore

**What it is:** Limit concurrency to N simultaneous executions.

**When to use:**
- Rate limiting towards external APIs that have limits
- Limit load on shared resources

```hcl
flow "call_external_api" {
  semaphore {
    key     = "external_api"
    permits = 5  # Maximum 5 concurrent requests
    storage = "connector.redis"
    timeout = "30s"

    on_fail = "wait"
    wait_timeout = "1m"
  }

  from { connector.rabbit = "api_calls" }
  to   { connector.external_api = "POST /endpoint" }
}
```

---

### Coordinate (Signal/Wait)

**What it is:** Coordinate execution between dependent flows. One flow waits until another signals.

**When to use:**
- Process child items only after parent exists
- Synchronize parallel flows

```hcl
# Flow that processes the parent and signals
flow "process_order" {
  from { connector.rabbit = "orders" }

  signal {
    key     = "'order:' + input.id"
    storage = "connector.redis"
  }

  to { connector.db = "INSERT orders" }
}

# Flow that waits for the parent
flow "process_order_item" {
  wait {
    key     = "'order:' + input.order_id"
    storage = "connector.redis"
    timeout = "5m"

    # Prior verification in DB
    check {
      connector.db = "SELECT 1 FROM orders WHERE id = :order_id"
    }

    # What to do on timeout
    on_timeout = "retry"  # fail, skip, retry, dlq
    max_retries = 3
  }

  from { connector.rabbit = "order_items" }
  to   { connector.db = "INSERT order_items" }
}
```

---

### Flow Triggers (when)

**What it is:** Define when a flow executes besides the normal `from` trigger.

```hcl
# Default: executes when something arrives at from
flow "on_request" {
  from { connector.api = "GET /data" }
  to   { connector.db = "data" }
}

# Cron: execute at specific schedules
flow "daily_report" {
  when = "0 3 * * *"  # 3am every day

  from { connector.db = "SELECT * FROM sales WHERE date = yesterday()" }
  to   { connector.email = "reports@example.com" }
}

# Interval: execute every X time
flow "health_check" {
  when = "@every 1m"

  from { connector.external_api = "GET /health" }
  to   { connector.metrics = "health_status" }
}

# Shortcuts
flow "weekly_cleanup" {
  when = "@weekly"  # @hourly, @daily, @weekly, @monthly

  from { connector.db = "DELETE FROM logs WHERE created_at < now() - interval '30 days'" }
  to   { connector.logs = "cleanup_complete" }
}
```

---

## 4. Extensibility

### Functions (WASM)

**What it is:** Custom functions compiled to WASM that can be used in CEL expressions.

**When to use:**
- Complex business logic that cannot be expressed in CEL
- Specific algorithms (pricing, scoring, etc)

```hcl
functions "pricing" {
  wasm    = "./wasm/pricing.wasm"
  exports = ["calculate_price", "apply_discount", "calculate_tax"]
}
```

**Usage in transforms:**
```hcl
transform {
  subtotal = "calculate_price(input.items)"
  discount = "apply_discount(subtotal, input.coupon)"
  tax      = "calculate_tax(subtotal - discount, input.country)"
  total    = "subtotal - discount + tax"
}
```

---

### Plugins

**What it is:** Extensions that add new connector types via WASM.

**When to use:**
- Integrate systems not natively supported (Salesforce, SAP, etc)
- Proprietary protocols

```hcl
# Declare plugin
plugin "salesforce" {
  source  = "./plugins/salesforce"  # Or "registry/salesforce"
  version = "1.0.0"
}

# Use connector from plugin
connector "sf" {
  type = "salesforce"

  instance_url = env("SF_URL")
  client_id    = env("SF_CLIENT_ID")
  client_secret = env("SF_CLIENT_SECRET")
}
```

---

### Aspects (AOP)

**What it is:** Cross-cutting concerns applied automatically to multiple flows by pattern matching.

**When to use:**
- Audit logging on all write operations
- Automatic cache on all reads
- Custom metrics on all flows

```hcl
aspect "audit_log" {
  # When to execute
  when = "after"  # before, after, around, on_error

  # Which flows to apply to (glob patterns)
  on = [
    "flows/**/create_*.hcl",
    "flows/**/update_*.hcl",
    "flows/**/delete_*.hcl"
  ]

  # Exclude
  except = ["flows/internal/*"]

  # Action to execute
  action {
    connector.audit_db = {
      operation = "INSERT audit_logs"
      data = {
        flow       = "${flow.name}"
        user       = "${context.user.id}"
        action     = "${flow.operation}"
        input      = "${json(input)}"
        output     = "${json(output)}"
        timestamp  = "${now()}"
      }
    }
  }
}

aspect "cache_reads" {
  when = "around"
  on   = ["flows/**/get_*.hcl", "flows/**/list_*.hcl"]

  cache {
    storage = "connector.cache"
    key     = "'flow:' + flow.name + ':' + hash(input)"
    ttl     = "5m"
  }
}
```

---

## 5. Authentication System

**What it is:** Enterprise-grade declarative authentication system.

### Basic configuration

```hcl
auth {
  # Base preset (strict, standard, relaxed, development)
  preset = "standard"

  # JWT
  jwt {
    secret         = env("JWT_SECRET")
    issuer         = "mycel"
    audience       = ["my-api"]
    access_ttl     = "15m"
    refresh_ttl    = "7d"
    algorithm      = "HS256"  # HS256, RS256, ES256
  }

  # Password
  password {
    min_length     = 8
    require_upper  = true
    require_lower  = true
    require_number = true
    require_special = true
    check_breach   = true  # Check HaveIBeenPwned
  }

  # Sessions
  session {
    max_per_user   = 5
    idle_timeout   = "30m"
    absolute_timeout = "24h"
  }

  # Brute force protection
  brute_force {
    max_attempts   = 5
    lockout_time   = "15m"
    progressive_delay = true
  }

  # MFA
  mfa {
    enabled  = true
    required = false  # true = mandatory for everyone

    totp {
      issuer = "MyApp"
      digits = 6
      period = 30
    }

    webauthn {
      rp_name = "MyApp"
      rp_id   = "myapp.com"
    }

    recovery_codes {
      count = 10
    }
  }

  # Storage
  storage {
    users    = "connector.db"  # users table
    sessions = "connector.redis"
    tokens   = "connector.redis"
  }

  # Endpoints (optional, reasonable defaults)
  endpoints {
    login           = "POST /auth/login"
    logout          = "POST /auth/logout"
    register        = "POST /auth/register"
    refresh         = "POST /auth/refresh"
    change_password = "POST /auth/password"
    mfa_setup       = "POST /auth/mfa/setup"
    mfa_verify      = "POST /auth/mfa/verify"
  }
}
```

---

## Summary of Identified GAPs

### REST Input (Server)
- [x] Incoming auth (JWT, API Key, Basic, OAuth2 validation) ✅
- [x] Required headers validation ✅
- [x] Custom response headers ✅

### REST Output (Client)
- [x] OAuth2 client credentials with token refresh ✅
- [x] OAuth2 refresh token flow ✅
- [x] TLS with client certificates ✅
- [x] Dynamic API key (from DB/service) ✅

### Database
- [x] Read replica routing ✅ (PostgreSQL and MySQL)

### Message Queues
- [x] Complete DLQ with retry count ✅ (RabbitMQ)
- [x] Kafka SASL_SCRAM authentication ✅
- [x] Kafka Schema Registry integration ✅
- [x] Kafka batch processing ✅

### gRPC
- [x] Server interceptors (auth, logging) ✅ (JWT, API Key, mTLS)
- [x] Client auth ✅ (Bearer, API Key, OAuth2)
- [x] Load balancing ✅ (round_robin, pick_first)

### Cache
- [x] Redis Cluster ✅
- [x] Redis Sentinel ✅

### General
- [x] Aspects (AOP) ✅ (before, after, around, on_error)
- [x] Sync primitives ✅ (Lock, Semaphore, Coordinate with Redis and Memory backends)
