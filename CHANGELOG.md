# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added - Cache Connector (Phase 3.3)
- **Cache Connector** (`internal/connector/cache/`)
  - In-memory and Redis caching for flow responses
  - Automatic cache lookup before flow execution (cache-aside pattern)
  - Cache storage after successful GET operations
  - Cache invalidation after write operations (POST/PUT/DELETE)
- **Memory Cache Driver** (`internal/connector/cache/memory/`)
  - LRU eviction policy with configurable max items
  - TTL-based expiration with background cleanup
  - Pattern-based key deletion with wildcard support (`*`)
  - Thread-safe operations with RWMutex
- **Redis Cache Driver** (`internal/connector/cache/redis/`)
  - Connection pooling with configurable settings
  - TTL support via Redis native expiration
  - Pattern deletion using SCAN (safe for large datasets)
  - Key prefix support for namespace isolation
- **Named Cache Definitions**
  - Reusable cache configurations (`cache "name" { ... }`)
  - Reference in flows with `cache { use = "name" }`
  - Shared TTL and prefix settings
- **Cache Invalidation**
  - `after { invalidate { ... } }` block for post-write invalidation
  - Specific key invalidation: `keys = ["products:${input.id}"]`
  - Pattern invalidation: `patterns = ["products:*", "lists:*"]`
  - Variable interpolation in keys and patterns
- **Cache Key Interpolation**
  - Path parameters: `${input.id}`
  - Query parameters: `${input.query.page}`
  - Request body: `${input.data.field}`
  - Result data: `${result.id}` (in invalidation)
- **Cache Example** (`examples/cache/`)
  - Memory cache with product and user caching
  - Inline and named cache configurations
  - Cache invalidation patterns
- **Dependencies**:
  - `github.com/hashicorp/golang-lru/v2` - LRU cache implementation
  - `github.com/redis/go-redis/v9` - Redis client
- **HCL Syntax**:
  ```hcl
  # Memory Cache Connector
  connector "cache" {
    type   = "cache"
    driver = "memory"
    max_items   = 10000
    eviction    = "lru"
    default_ttl = "5m"
  }

  # Redis Cache Connector
  connector "redis_cache" {
    type   = "cache"
    driver = "redis"
    url    = "redis://localhost:6379"
    prefix = "myapp"
    pool {
      max_connections = 10
      min_idle       = 2
    }
  }

  # Named Cache Definition
  cache "products" {
    storage = "cache"
    ttl     = "10m"
    prefix  = "products"
  }

  # Flow with Inline Cache
  flow "get_product" {
    from { connector = "api", operation = "GET /products/:id" }
    to   { connector = "db", target = "products" }
    cache {
      storage = "cache"
      ttl     = "5m"
      key     = "products:${input.id}"
    }
  }

  # Flow with Named Cache
  flow "get_user" {
    from { connector = "api", operation = "GET /users/:id" }
    to   { connector = "db", target = "users" }
    cache {
      use = "products"
      key = "user:${input.id}"
    }
  }

  # Flow with Cache Invalidation
  flow "update_product" {
    from { connector = "api", operation = "PUT /products/:id" }
    to   { connector = "db", target = "products" }
    after {
      invalidate {
        storage  = "cache"
        keys     = ["products:${input.id}"]
        patterns = ["lists:products:*"]
      }
    }
  }
  ```

### Added - MySQL and MongoDB Connectors (Phase 3.2)
- **MySQL Connector** (`internal/connector/database/mysql/`)
  - Full CRUD operations (SELECT, INSERT, UPDATE, DELETE)
  - Connection pooling configurable (max_open, max_idle, max_lifetime)
  - Named parameter support (`:param` syntax converted to `?` placeholders)
  - DSN auto-generation from HCL config
  - SSL/TLS support
  - **HCL Syntax**:
    ```hcl
    connector "mysql_db" {
      type     = "database"
      driver   = "mysql"
      host     = env("MYSQL_HOST")
      port     = 3306
      database = "myapp"
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
- **MongoDB Connector** (`internal/connector/database/mongodb/`)
  - Full NoSQL CRUD operations
  - Operations: INSERT_ONE/MANY, UPDATE_ONE/MANY, DELETE_ONE/MANY, REPLACE_ONE
  - Automatic ObjectID handling (string ↔ ObjectID conversion)
  - BSON to Map conversion with timestamp handling
  - MongoDB operators support (`$set`, `$gte`, `$lt`, `$in`, etc.)
  - Connection pooling configurable
  - **HCL Syntax**:
    ```hcl
    connector "mongo_db" {
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
- **NoSQL Query Support**
  - New `RawQuery` field in `connector.Query` for NoSQL filters
  - New `Update` field in `connector.Data` for MongoDB update operations
  - New `query_filter` and `update` attributes in HCL flows
  - Parser function `ctyValueToMap` for HCL → Go map conversion
  - **HCL Syntax for MongoDB queries**:
    ```hcl
    flow "get_active_users" {
      from { connector = "api", operation = "GET /users/active" }
      to {
        connector    = "mongo_db"
        target       = "users"
        query_filter = { status = "active", age = { "$gte" = 18 } }
      }
    }

    flow "update_user_status" {
      from { connector = "api", operation = "PUT /users/:id/status" }
      to {
        connector    = "mongo_db"
        target       = "users"
        query_filter = { "_id" = ":id" }
        update       = { "$set" = { status = "input.status" } }
      }
    }
    ```
- **Dependencies**:
  - `github.com/go-sql-driver/mysql` - MySQL driver
  - `go.mongodb.org/mongo-driver` - MongoDB driver

### Added - Integration Patterns Documentation
- **New guide:** `docs/integration-patterns.md` with complete, copy-paste ready examples for:
  - GraphQL API → Database (CRUD)
  - REST → GraphQL passthrough
  - GraphQL → REST passthrough
  - RabbitMQ → Database (message processing)
  - REST/GraphQL → RabbitMQ (async processing)
  - Raw SQL queries (JOINs, subqueries, aggregations)
- Quick reference for connector types and flow structure
- Common CEL functions reference

### Added - Raw SQL Query Support
- **Custom SQL queries** for complex database operations (JOINs, subqueries, multi-table operations)
  - Named parameter substitution with `:param` syntax
  - Automatic conversion to database-specific placeholders (`?` for SQLite, `$1, $2` for PostgreSQL)
  - Support for SELECT, INSERT, UPDATE, DELETE with raw SQL
  - Handles RETURNING clauses for INSERT/UPDATE operations
- **Updated connector interfaces** (`internal/connector/connector.go`)
  - Added `RawSQL` field to `Query` struct
  - Added `RawSQL` field to `Data` struct
- **SQLite connector** (`internal/connector/database/sqlite/connector.go`)
  - `parseNamedParams()` function for parameter substitution
  - String literal handling to avoid replacing `:param` inside strings
- **PostgreSQL connector** (`internal/connector/database/postgres/connector.go`)
  - Same features as SQLite but with PostgreSQL-style `$N` placeholders
- **REST connector improvements** (`internal/connector/rest/connector.go`)
  - Dynamic path parameter extraction for any route (not just `:id`)
  - New `extractParamNames()` function for parsing route definitions
- **HCL Syntax**:
  ```hcl
  # Using heredoc syntax for multi-line SQL
  flow "get_order_with_user" {
    from {
      connector = "api"
      operation = "GET /orders/:id"
    }
    to {
      connector = "sqlite"
      query = <<-SQL
        SELECT o.*, u.name as user_name, u.email as user_email
        FROM orders o
        JOIN users u ON u.id = o.user_id
        WHERE o.id = :id
      SQL
    }
  }

  # Using inline SQL with named parameters
  flow "get_orders_by_user" {
    from {
      connector = "api"
      operation = "GET /orders-by-user/:user_id"
    }
    to {
      connector = "sqlite"
      query = "SELECT * FROM orders WHERE user_id = :user_id AND status = :status"
    }
  }
  ```
- **Integration tests** (`internal/runtime/runtime_test.go`)
  - `TestIntegration_RawSQL` with 3 test cases:
    - JOIN query with path parameter
    - Multiple named parameters
    - Raw SQL INSERT

### Added - GraphQL Dual-Approach Schema Generation
- **Schema-first mode**: Define types in SDL file (`.graphql`), Mycel auto-connects flows
  - Full SDL parser with AST using `graphql-go/language/parser`
  - Automatic type conversion from SDL to graphql-go types
  - Smart resolver that auto-unwraps single-element arrays for non-list types
  - Support for custom scalars: DateTime, Date, Time, JSON
  - Input types, enums, and interfaces support
- **HCL-first mode**: Define types in HCL, Mycel generates GraphQL schema
  - TypeSchema to GraphQL converter (`hcl_to_graphql.go`)
  - New `returns` attribute in flows to specify return type
  - Automatic schema generation from HCL types
  - Type mapping: `id` → `ID`, `string` → `String`, `number` → `Int/Float`, `boolean` → `Boolean`
- **New files**:
  - `internal/connector/graphql/sdl_parser.go` - Complete SDL parser
  - `internal/connector/graphql/sdl_to_graphql.go` - SDL → graphql-go converter
  - `internal/connector/graphql/hcl_to_graphql.go` - HCL → GraphQL converter
  - `internal/connector/graphql/scalar_types.go` - Custom scalar types
- **Comprehensive integration tests** (`internal/runtime/runtime_test.go`)
  - Schema-first CRUD tests: 14 test cases
  - HCL-first CRUD tests: 13 test cases
  - Tests cover: Query, Mutation, UpdateUser, DeleteUser, Introspection, Playground
  - GraphQL Variables tests for both modes
  - Error handling tests (invalid queries, missing required fields, empty queries)
  - All tests use SQLite as backend
- **Column mapping (snake_case → camelCase)** for GraphQL responses
  - `snakeToCamel()` function in `resolver.go`
  - Automatic conversion: `external_id` → `externalId`, `created_at` → `createdAt`
  - Recursive conversion for nested objects
- **HCL Syntax for returns**:
  ```hcl
  flow "get_users" {
    from { connector = "gql", operation = "Query.users" }
    to   { connector = "db", target = "users" }
    returns = "User[]"  # Specifies GraphQL return type
  }

  flow "get_user" {
    from { connector = "gql", operation = "Query.user" }
    to   { connector = "db", target = "users" }
    returns = "User"  # Single object, auto-unwrap enabled
  }
  ```

### Added - GraphQL Connector (Phase 3)
- **GraphQL Server Connector** (`internal/connector/graphql/`)
  - Expose GraphQL API endpoints with playground UI
  - Dynamic schema building from registered handlers
  - SDL file loading support for schema-first approach
  - **Features**:
    - Query and Mutation support
    - GraphQL Playground UI at `/playground`
    - CORS configuration
    - JSON scalar type for flexible arguments
    - Health check endpoint at `/health`
  - **Operation format**: `Query.fieldName` or `Mutation.fieldName`
- **GraphQL Client Connector**
  - Call external GraphQL APIs
  - **Authentication types**:
    - Bearer token
    - API Key (custom header)
    - Basic auth
    - OAuth2 client credentials
  - Retry with exponential backoff
  - Timeout configuration
  - Custom headers support
  - Use as enrichment source via `Call()`
- **GraphQL Example** (`examples/graphql/`)
  - Server with CRUD operations
  - Schema file example
- **HCL Syntax**:
  ```hcl
  # GraphQL Server
  connector "graphql_api" {
    type   = "graphql"
    driver = "server"

    port       = 4000
    endpoint   = "/graphql"
    playground = true

    cors {
      origins = ["*"]
      methods = ["GET", "POST", "OPTIONS"]
    }
  }

  # GraphQL Client
  connector "external_api" {
    type     = "graphql"
    driver   = "client"
    endpoint = "https://api.example.com/graphql"

    auth {
      type  = "bearer"
      token = env("API_TOKEN")
    }

    timeout     = "30s"
    retry_count = 3
  }
  ```

### Added - Exec Connector (Phase 3.2)
- **Exec Connector** (`internal/connector/exec/`)
  - Execute external commands locally or on remote servers
  - **Local driver**: Shell command execution on the local machine
    - Direct command execution with arguments
    - Shell wrapper support (`bash -c`, etc.) for pipes and shell features
    - Environment variables injection
    - Working directory configuration
    - Timeout handling with context cancellation
  - **SSH driver**: Remote command execution via SSH
    - Key-based authentication (recommended)
    - Password authentication (supported but not recommended)
    - Custom SSH port configuration
    - Known hosts verification
  - **Input formats**:
    - `args`: Pass input as command-line arguments (`--key=value`)
    - `stdin` / `json`: Send JSON-encoded input via stdin
  - **Output formats**:
    - `text`: Raw output as single string `{"output": "..."}`
    - `json`: Parse output as JSON object/array
    - `lines`: Split output by newlines with line numbers
  - **Use cases**:
    - Execute local scripts and CLI tools
    - Remote server monitoring and management
    - Data enrichment via external APIs (curl, etc.)
    - Process data through external programs (jq, awk, etc.)
    - Integration with existing shell scripts
- **Exec Example** (`examples/exec/`)
  - Local command execution examples
  - Shell command with pipes
  - JSON output parsing
  - Data enrichment using exec connector
- **HCL Syntax**:
  ```hcl
  # Local execution
  connector "my_script" {
    type   = "exec"
    driver = "local"

    command       = "echo"
    args          = ["hello", "world"]
    timeout       = "10s"
    output_format = "text"
  }

  # SSH remote execution
  connector "remote_server" {
    type   = "exec"
    driver = "ssh"

    command = "uptime"
    ssh {
      host     = "server.example.com"
      user     = "admin"
      key_file = "/path/to/key"
    }
  }
  ```

### Added - Enrich System (Data Enrichment)
- **Enrich blocks** for fetching data from external services during transformation
  - Flow-level enrich: Specific to a single flow
  - Transform-level enrich: Reusable across multiple flows (inside named transforms)
  - Multiple enrichments per flow/transform
- **`enriched.*` namespace** available in CEL expressions
  - Access enriched data: `enriched.pricing.price`, `enriched.inventory.stock`
  - Combine with input: `input.quantity * enriched.pricing.unit_price`
- **CEL transformer enhancements** (`internal/transform/cel.go`)
  - `EvaluateExpression()`: Evaluate single expressions with input and enriched data
  - `TransformWithEnriched()`: Full transformation with enriched context
- **Connector support for enrichment**
  - Database connectors: Uses `Read()` for data lookup
  - TCP/HTTP connectors: Uses `Call()` interface for RPC-style calls
- **Enrich Example** (`examples/enrich/`)
  - Flow-level enrichment with pricing service
  - Multiple enrichments (pricing + inventory)
  - Reusable transforms with built-in enrichment
- **HCL Syntax**:
  ```hcl
  # Flow-level enrich
  flow "get_product" {
    enrich "pricing" {
      connector = "pricing_service"
      operation = "getPrice"
      params { product_id = "input.id" }
    }
    transform {
      price = "enriched.pricing.price"
    }
  }

  # Transform-level enrich (reusable)
  transform "with_pricing" {
    enrich "pricing" { ... }
    price = "enriched.pricing.price"
  }
  ```

### Added (Phase 3.1)
- **Message Queue Connector** (`internal/connector/mq/`)
  - **RabbitMQ Support**: Full producer and consumer implementation
    - Connection management with automatic reconnection
    - Queue and exchange declaration with binding support
    - Topic pattern matching (`*` matches one word, `#` matches zero or more)
    - Manual acknowledgment for reliable message processing
    - Concurrent consumers with configurable prefetch (QoS)
    - Publisher confirms for guaranteed delivery
  - **Kafka Support** (`internal/connector/mq/kafka/`): Full producer and consumer implementation
    - Consumer groups with auto-commit or manual offset management
    - Multiple topic subscription
    - SASL authentication (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)
    - TLS support
    - Compression (gzip, snappy, lz4, zstd)
    - Configurable acks (none, one, all) for delivery guarantees
    - Batch publishing with configurable batch size and linger time
    - Concurrent consumers
  - **Message types** (`internal/connector/mq/types/`)
    - Generic Message struct with headers, routing key, exchange
    - DeliveryMode (transient/persistent)
    - AckMode (auto/manual/none)
  - **Exchange types** (RabbitMQ): direct, fanout, topic, headers
  - **Consumer features**:
    - Routing key pattern matching for topic exchanges (RabbitMQ)
    - Consumer groups (Kafka)
    - Prefetch/QoS configuration
    - Concurrent worker goroutines
    - Graceful shutdown with message draining
  - **Publisher/Producer features**:
    - Exchange and routing key configuration (RabbitMQ)
    - Topic and partition key configuration (Kafka)
    - Persistent message delivery
    - Publisher confirms support (RabbitMQ)
    - Batch publishing
- **MQ Example** (`examples/mq/`)
  - RabbitMQ consumer and publisher configuration
  - Order processing with pub/sub pattern
  - Topic routing examples

### Added (Phase 2.5)
- **TCP Connector** (`internal/connector/tcp/`)
  - **TCP Server**: Listen for incoming TCP connections
    - Length-prefixed message framing (4-byte big-endian header)
    - Message routing by `type` field in JSON
    - Configurable max connections, read/write timeouts
    - TLS support (optional)
    - Graceful shutdown with connection draining
  - **TCP Client**: Connect to remote TCP servers
    - Connection pooling with configurable size
    - Automatic retry with configurable count and delay
    - Request-Response and Fire-and-forget patterns
    - TLS support with custom CA certificates
  - **Protocol codecs**: JSON, msgpack, raw, **nestjs**
  - **Wire protocols**:
    - Mycel: `[4-byte length][payload]`
    - NestJS: `{length}#{json}` (compatible with @nestjs/microservices TCP transport)
  - **NestJS Protocol Support** (`internal/connector/tcp/nestjs.go`)
    - Full compatibility with NestJS TCP microservices
    - Wire format: `{length}#{json}` where json is `{"pattern":"...", "data":{...}, "id":"..."}`
    - Handles NestJS patterns (string or `{cmd: "..."}` objects)
    - Automatic conversion between Mycel and NestJS message formats
    - Support for NestJS response format with `response`, `err`, and `isDisposed` fields
- **TCP Example** (`examples/tcp/`)
  - Complete example with TCP server + SQLite
  - Python and netcat testing scripts

### Added (Phase 2)
- **HTTP Client connector** (`internal/connector/http/`)
  - Call external REST APIs from flows
  - Authentication support: Bearer, OAuth2 (with refresh tokens), API Key, Basic
  - Configurable timeout and retry settings
  - Custom headers support
- **PostgreSQL connector** (`internal/connector/database/postgres/`)
  - Full CRUD operations with parameterized queries
  - Connection pooling configuration
  - SSL mode support
- **Transform system powered by CEL** (`internal/transform/`)
  - Google's Common Expression Language (CEL) for powerful, safe transformations
  - Full expression support: operators (`+`, `-`, `*`, `/`, `%`, `==`, `!=`, `<`, `>`, `&&`, `||`)
  - Ternary expressions: `age >= 18 ? "adult" : "minor"`
  - List operations: `filter()`, `map()`, `exists()`, `all()`, `size()`, `in`
  - Custom Mycel functions: `uuid()`, `now()`, `now_unix()`, `lower()`, `upper()`, `trim()`, `replace()`, `substring()`, `len()`, `default()`, `coalesce()`, `split()`, `join()`, `hash_sha256()`, `format_date()`
  - **CEL Standard Extensions enabled:**
    - `ext.Strings()`: charAt, indexOf, lastIndexOf, join, quote, replace, split, substring, trim, upperAscii, lowerAscii, reverse
    - `ext.Encoders()`: base64.encode, base64.decode
    - `ext.Math()`: math.abs, math.ceil, math.floor, math.round, math.sign, math.greatest, math.least, math.isNaN, math.isInf
    - `ext.Lists()`: lists.range, slice, flatten
    - `ext.Sets()`: sets.contains, sets.equivalent, sets.intersects
  - Expression validation at startup (early error detection)
  - Program caching for optimal runtime performance
  - Named/reusable transforms in separate HCL files
  - Inline transforms in flow definitions
- **Transformations documentation** (`docs/transformations.md`)
  - Complete CEL reference guide with examples
  - All available functions documented
  - Real-world transformation examples
- **Type validation on flows**
  - Input and output validation with type schemas
  - Built-in constraints: min, max, min_length, max_length, format, pattern, enum
  - Format validators: email, url, uuid, date, datetime
- **Environment support** - Enhanced HCL functions:
  - `env("VAR_NAME", "default")` - Environment variable with optional default
  - `file("./path/to/secret")` - Read file contents
  - `base64encode()` / `base64decode()` - Base64 encoding/decoding
  - `abspath()` - Convert relative paths to absolute
  - `coalesce()` - Return first non-empty value

### Added (Phase 1.5)
- **ASCII art banner** with colored terminal output
  - New `internal/banner/` package for styled console output
  - ANSI color support with automatic detection (respects NO_COLOR env var)
  - Color-coded HTTP methods (GET=green, POST=yellow, DELETE=magenta)
  - Clean startup display with service info, connectors, and flows

### Fixed
- **GET with path parameters** now correctly filters results
  - Operations like `GET /users/:id` automatically extract path params as query filters
  - `extractPathParams()` helper function added to flow registry

### Added (Phase 1)
- **`mycel start` command is now functional!**
  - Full runtime orchestration: parse config → init connectors → register flows → start HTTP server
  - Graceful shutdown with SIGINT/SIGTERM handling
- **REST connector** (`internal/connector/rest/`)
  - HTTP server with configurable port and CORS
  - Automatic route registration from flow configurations
  - JSON request/response handling
- **SQLite connector** (`internal/connector/database/sqlite/`)
  - Full CRUD operations (SELECT, INSERT, UPDATE, DELETE)
  - Pure Go driver (no CGO required) via `modernc.org/sqlite`
  - Connection pooling and health checks
- **Runtime engine** (`internal/runtime/`)
  - Configuration-driven service orchestration
  - Flow registry with automatic handler building
  - Connector lifecycle management
- Working example in `examples/basic/` with SQLite database
- `mycel validate` command to check configuration validity
- `mycel check` command to verify connector configuration

### Changed
- **BREAKING:** Updated flow block syntax for HCL compatibility
  - `from` block now uses `connector` and `operation` attributes
  - `to` block now uses `connector`, `target`, and optional `filter` attributes
  - Old syntax: `from { connector.api = "GET /users" }`
  - New syntax: `from { connector = "api", operation = "GET /users" }`

### Fixed
- Fixed `TestParseFlow` and `TestParseDirectory` parser tests
- Updated example files to use valid HCL syntax
- Fixed connector driver parsing in HCL parser

### Added (Initial)
- Initial project setup
- Project specification and design documents (CLAUDE.md)
- CLI scaffolding with cobra (start, validate, check commands)
- HCL parser for connectors, flows, types, and service blocks
- Connector interfaces (Reader, Writer, ReadWriter, Registry, Factory)
- Flow executor with pipeline pattern and stages
- Validation system with TypeValidator and built-in constraints
- Transform system with FunctionRegistry
- Custom HCL functions: `env()`, `coalesce()`

---

## Version History

_No releases yet. Development starting from Fase 1 - Core._
