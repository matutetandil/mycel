# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added - GraphQL Subscription Client
- **Client-side GraphQL subscriptions** — Mycel can subscribe to external GraphQL servers
  - `ClientConnector` implements `Starter` and `RouteRegistrar` interfaces
  - `from { connector = "ext_gql", operation = "Subscription.fieldName" }` syntax
  - WebSocket client using graphql-ws protocol (connection_init → subscribe → next)
  - Automatic reconnection with exponential backoff on disconnect
  - Auth headers forwarded to WebSocket handshake
  - Custom subscription path via `subscriptions { path = "/ws" }` config
  - HTTP→WS / HTTPS→WSS URL scheme conversion
- **New `ClientConfig` field**: `Subscriptions *SubscriptionsConfig`
- **Tests**: 6 new tests covering registration, WebSocket lifecycle, reconnect, URL building, factory

### Changed - Federation Auto-enabled
- **Federation v2 is now always enabled** on every GraphQL server connector
  - `_service { sdl }` always exposed — gateways discover and compose automatically
  - `_entities` available when types have `_key` attributes
  - `federation` block is now optional (only needed to override version)
  - Zero-config federation: just add `_key` to your types and entity resolver flows

### Added - Phase 9: GraphQL Federation Complete
- **Subscription type support** in GraphQL schema
  - `SchemaBuilder` supports `Subscription` fields alongside Query/Mutation
  - Channel-based resolvers backed by PubSub (publish/subscribe)
  - SDL generation includes `type Subscription { ... }` block
  - WebSocket delivery via graphql-ws protocol
- **Flow-triggered subscriptions** — publish from any flow to a subscription topic
  - `to { operation = "Subscription.fieldName" }` syntax
  - Runtime detects `Subscription.*` prefix and publishes instead of writing
  - Steps and transforms are applied before publishing
  - Works with any source connector (REST, Queue, TCP, etc.)
- **Per-user subscription filtering**
  - `PubSub.SubscribeWithFilter()` for per-subscriber message filtering
  - WebSocket `connection_init` params available in subscription context
  - `to { filter = "input.user_id == context.auth.user_id" }` syntax
- **Automatic entity resolution** from HCL types
  - Types with `_key` auto-register Federation entity resolvers
  - Explicit entity resolver flows with `entity = "TypeName"` attribute
  - Runtime matches types to query flows by return type
  - Compatible with Apollo Router and Cosmo Router
- **New flow attribute**: `entity` — marks a flow as a Federation entity resolver
- **New to block attribute**: `filter` — CEL expression for subscription filtering
- **New interfaces**: `SubscriptionPublisher`, `SubscriptionRegistrar`, `EntityRegistrar`
- **New example**: `examples/graphql-federation/` — complete federation subgraph
- **Spec**: `docs/PHASE-9-GRAPHQL-FEDERATION.md`

### Added - Named Operations for Connectors
- **Named operations** for better encapsulation and reusability
  - Connectors define their operations with metadata
  - Flows reference operations by name
  - Improves maintainability and enables mycel-studio introspection
- **Operation block syntax** in connector definitions
  ```hcl
  connector "api" {
    type = "rest"
    port = 8080

    operation "list_users" {
      method      = "GET"
      path        = "/users"
      description = "List all users"

      param "limit" {
        type    = "number"
        default = 100
      }
    }
  }
  ```
- **Parameter definitions** with type, required, default, and validation
  - `type`: string, number, boolean, array, object
  - `required`: mark parameters as mandatory
  - `default`: provide default values
  - `description`: documentation for the parameter
  - Validation constraints: min, max, min_length, max_length, pattern, enum
- **OperationResolver** for operation resolution
  - Automatic resolution based on connector type
  - Parameter validation and default value application
- **Supported operation attributes per connector type**
  - REST: method, path
  - Database: query, table
  - GraphQL: operation_type, field
  - gRPC: service, rpc
  - MQ: exchange, routing_key, queue
  - TCP: protocol, action
  - File/S3: path_pattern
  - Cache: key_pattern, ttl
  - Exec: command, args
- **New example**: `examples/named-operations/`

### Added - Phase 8: GraphQL Query Optimization
- **Automatic query optimization** - Zero configuration required, same HCL produces optimized execution
- **Field Analyzer** (`internal/graphql/analyzer/`)
  - Extract requested fields from GraphQL AST
  - `FieldTree` hierarchical data structure for field tracking
  - `RequestedFields` with `Has(path)`, `Get(path)`, `List()`, `ListFlat()`, `SubFields(path)`, `IsEmpty()`
  - Supports nested fields and arguments extraction
- **Result Pruner** (`internal/graphql/pruner/`)
  - Remove unrequested fields from response data (safety net)
  - `Prune(data, requested)` - prune using RequestedFields
  - `PruneWithPaths(data, paths)` - prune using path list
  - Handles nested objects and arrays recursively
- **Request Context Integration**
  - `__requested_fields` available in input (flat list of all field paths)
  - `__requested_top_fields` available in input (top-level fields only)
  - Automatic injection via `CreateSmartResolver`
- **CEL Functions** for field-based conditional logic
  - `has_field(input, path)` - check if field was requested
  - `field_requested(input, path)` - alias for has_field
  - `requested_fields(input)` - get all requested field paths
  - `requested_top_fields(input)` - get top-level fields only
- **Database Optimizer** (`internal/graphql/optimizer/`)
  - `SQLOptimizer` rewrites `SELECT *` to only fetch requested columns
  - `OptimizeQueryWithFields(query, fields)` - optimize any query
  - `CamelToSnake(field)` - convert GraphQL camelCase to SQL snake_case
  - `FieldsFromInput(input)`, `TopFieldsFromInput(input)` - extract fields from input
  - Integrated into runtime `handleRead` for automatic optimization
- **Step Optimizer** (`internal/graphql/optimizer/step_optimizer.go`)
  - `StepOptimizer` analyzes dependencies between steps and fields
  - `AnalyzeDependencies()` - determine which steps are needed
  - `GetSkippableSteps()` - identify steps that can be skipped
  - `GenerateStepConditions()` - auto-generate when conditions
  - `OptimizeFlowSteps(steps, transformExprs)` - return optimized steps
  - Detects step-to-step dependencies for correct execution order
- **DataLoader** (`internal/dataloader/`) - N+1 query prevention
  - Generic `Loader[K, V]` wrapper around graph-gophers/dataloader v7
  - `LoaderConfig` with BatchSize, Wait, and Cache options
  - `LoaderCollection` for request-scoped loader management
  - `WithLoaders(ctx, collection)` / `GetLoaders(ctx)` context integration
  - `GetOrCreateFromContext[K, V]` for easy loader retrieval
  - `SQLBatchLoader` helper for creating batch functions from SQL queries
  - `SQLManyBatchLoader` helper for one-to-many relationships (e.g., user → orders)
  - `LoaderKey(table, operation)` for generating unique loader keys
  - Automatic batching with configurable wait time (default: 1ms)
  - Optional caching per request (default: enabled)
- **Runtime Integration**
  - Step Optimizer integrated into `executeSteps` - skips unused steps automatically
  - `analyzeNeededSteps()` checks `__requested_top_fields` and uses optimizer
  - DataLoader middleware added to GraphQL server - creates LoaderCollection per request
  - GraphQL requests now have DataLoader context available for batching
- **Dependencies added**:
  - `github.com/graph-gophers/dataloader/v7` - DataLoader implementation
- **Transparent optimization** - Example:
  ```hcl
  # User writes:
  flow "get_users" {
    from { connector = "api", operation = "Query.users" }
    to   { connector = "postgres", target = "users" }
  }

  # Client requests: query { users { id, name } }
  # Mycel automatically executes: SELECT id, name FROM users (not SELECT *)
  ```
- **New example**: `examples/graphql-optimization/`
  - Demonstrates Field Selection, Step Skipping, and DataLoader
  - Complete setup with SQLite database and sample data
  - README with test queries and optimization explanations

### Fixed - HCL Type Circular References
- **Bug**: HCL types that reference each other (e.g., `Order { user = User }`) caused schema build error:
  "User fields must be an object with field names as keys or a function which return such an object"
- **Cause**: First pass created empty placeholder types, second pass created new objects but other types still referenced the empty placeholders
- **Fix**: Use `graphql.FieldsThunk` (lazy loading) for all HCL type conversions, allowing circular references to resolve correctly
- **File**: `internal/connector/graphql/hcl_to_graphql.go`

### Improved - GraphQL Arguments Inference
- **Before**: All GraphQL queries used generic `input: JSON` argument (e.g., `product(input: {id: "1"})`)
- **After**: Arguments are automatically inferred from step params (e.g., `product(id: "1")`)
- **How it works**:
  - Looks for `input.*` references in step params (e.g., `params = { id = "input.id" }`)
  - Creates typed `String` arguments for each discovered field
  - Falls back to `input: JSON` for flows without steps
- **Schema example**:
  ```graphql
  # Before (all flows)
  product(input: JSON): Product

  # After (flows with steps)
  product(id: String): Product

  # After (flows without steps - unchanged)
  users(input: JSON): [User]
  ```
- **Files**:
  - `internal/connector/graphql/schema.go` - Added `ArgDef`, `RegisterHandlerWithArgs`, `buildArgs`, `mapArgType`
  - `internal/connector/graphql/server.go` - Added `RegisterRouteWithArgs`
  - `internal/runtime/runtime.go` - Added `RouteRegistrarWithArgs`, `inferArgsFromFlow`, `extractInputArgs`

### Fixed - Step Param Evaluation in Flows
- **Bug**: Step params like `{ id = "input.id" }` were not being evaluated - the literal string "input.id" was passed instead of the actual value
- **Cause**: `executeSteps` didn't initialize the CEL Transformer, and `EvaluateExpression` didn't support the `step` variable
- **Fix**:
  - Initialize CEL Transformer at start of `executeSteps` if nil
  - Added `EvaluateExpressionWithSteps(ctx, input, steps, expr)` function that includes `step` in CEL activation
  - Updated step param evaluation to use new function
- **Additional fix**: Skipped steps now always set `stepResults[step.Name] = nil` so CEL expressions like `step.X != null` work correctly
- **Files**:
  - `internal/transform/cel.go` - Added `EvaluateExpressionWithSteps`
  - `internal/runtime/flow_registry.go` - Initialize Transformer, use new function, set nil for skipped steps

### Added - Phase 8: GraphQL Federation Complete
- **GraphQL Subscriptions with WebSocket transport**
  - Full `graphql-transport-ws` protocol support
  - WebSocket handler with connection management
  - PubSub mechanism for subscription events
  - Keep-alive ping/pong handling
  - Configurable via HCL `subscriptions` block
  - Path customization (default: `/subscriptions`)
  - Integration with GraphQL server connector
- **Federation directives support in HCL types**
  - Type-level federation directives:
    - `_key = "id"` or `_key = ["id", "email name"]` for @key directive
    - `_shareable = true` for @shareable directive
    - `_inaccessible = true` for @inaccessible directive
    - `_implements = ["Node", "Entity"]` for interface implementations
    - `_description = "..."` for type documentation
  - Field-level federation directives:
    - `external = true` for @external directive
    - `provides = "field1 field2"` for @provides directive
    - `requires = "otherField"` for @requires directive
    - `shareable = true` for @shareable directive
    - `inaccessible = true` for @inaccessible directive
    - `override = "subgraph-name"` for @override directive
    - `description = "..."` for field documentation
  - SDL generation with federation directives
  - Parser support for underscore-prefixed type directives
- **New HCL syntax for federated types**
  ```hcl
  type "User" {
    _key         = "id"
    _shareable   = true
    _description = "A user entity"

    id    = string
    email = string { external = true }
    name  = string { requires = "email" }
  }
  ```

### Added - Phase 5 Complete: Aspects Runtime (AOP)
- **Aspect-Oriented Programming (AOP)** for cross-cutting concerns
  - Pattern-based matching with glob patterns (`**/create_*.hcl`, `flows/**/*.hcl`)
  - Before/After/Around execution points
  - Priority ordering for multiple matching aspects
  - Conditional execution with `if` CEL expressions
- **Cache aspects** for transparent caching
  - Automatic cache lookup before flow execution
  - Cache storage after successful flow completion
  - Template-based cache keys with `${input.id}` interpolation
- **Cache invalidation aspects**
  - Invalidate specific keys after mutations
  - Pattern-based invalidation with wildcards
  - Template interpolation in keys and patterns
- **Rate limiting aspects**
  - Integrated with ratelimit package
  - Per-key rate limiting with CEL key expressions
  - Configurable RPS and burst limits
- **Circuit breaker aspects**
  - Integrated with circuitbreaker package
  - Automatic circuit state management
  - Configurable failure/success thresholds and timeout
- **Action aspects** for side effects
  - Execute connector operations (audit logs, notifications)
  - Transform expressions for building action data
  - Access to flow result in after aspects
- **Parser improvements**
  - Fixed template expression parsing in cache keys (`${input.id}`)
  - Fixed array parsing with template expressions in invalidate keys/patterns

### Added - Phase 7 Flow Orchestration: Step Blocks
- **Multi-step flow execution** with intermediate connector calls
  - Steps execute in order before the transform
  - Results available as `step.<name>.*` in subsequent steps and transforms
  - Support for database queries, HTTP operations, and all connector types
- **Conditional step execution** with `when` clause
  - CEL expressions to conditionally execute steps
  - Access to `input.*` and previous `step.*` results in conditions
- **Error handling per step** with `on_error` attribute
  - `fail`: Fail the entire flow if step fails (default)
  - `skip`: Skip the step and continue with nil result
  - `default`: Use a default value if step fails
- **Step timeout configuration** with `timeout` attribute
- **Request filtering** with `filter` in `from` block
  - CEL expression evaluated before any processing
  - Returns `FilteredResult` when filter evaluates to false
  - Example: `filter = "input.total >= 1000"` for high-value orders only
- **New CEL transformer methods**
  - `EvaluateCondition`: Evaluate boolean CEL expressions
  - `TransformWithSteps`: Transform with step results available
  - `TransformWithContext`: Unified transform with enriched and step data
- **Array helper functions** for data manipulation in transforms
  - `first(list)`, `last(list)`: Get first/last element
  - `unique(list)`: Remove duplicates
  - `reverse(list)`: Reverse list order
  - `flatten(list)`: Flatten nested lists
  - `pluck(list, key)`: Extract field from list of maps
  - `sum(list)`, `avg(list)`: Aggregate numeric values
  - `min_val(list)`, `max_val(list)`: Find min/max values
  - `sort_by(list, key)`: Sort list of maps by a key
- **Map helper functions** for response composition
  - `merge(map1, map2, ...)`: Combine multiple maps (later values override earlier)
  - `omit(map, key1, ...)`: Remove specified keys from a map
  - `pick(map, key1, ...)`: Select only specified keys from a map
  - Supports 2-4 maps for merge, 1-4 keys for omit/pick
  - Ideal for API Gateway aggregation and data sanitization
- **Multi-destination fan-out** for writing to multiple destinations
  - Multiple `to` blocks in a single flow
  - Parallel execution by default (configurable per destination)
  - Conditional writes with `when` CEL expressions
  - Per-destination transforms with access to `output.*`
  - Use cases: event broadcasting, data replication, audit logging
- **Message deduplication** with `dedupe` block
  - Prevent duplicate message processing using cache-based deduplication
  - Configurable key expression (CEL) for unique message identification
  - TTL-based expiration for dedup keys
  - Behavior on duplicate: `skip` (silent) or `fail` (return error)
  - Fail-open design: continues processing if cache is unavailable
  - Use cases: idempotent APIs, message queue exactly-once processing, payment idempotency
- **Flow-level error handling** with `error_handling` block
  - `retry`: Automatic retries with configurable backoff (constant, linear, exponential)
  - `fallback`: Send failed messages to DLQ (Dead Letter Queue)
  - `include_error`: Include error details in fallback message
  - `max_delay`: Cap delay for exponential backoff
- **New example** (`examples/steps/`)
  - Basic multi-step flow (user/product lookup → order creation)
  - Conditional steps (optional pricing/inventory)
  - Chained steps (step results used in subsequent steps)
  - Error handling strategies
  - Request filtering examples
  - Array transforms with aggregation functions
  - Retry with exponential backoff and DLQ fallback
  - Response composition with merge/omit/pick functions
  - API Gateway aggregation pattern
  - Multi-destination fan-out examples
  - Message deduplication examples (order processing, idempotent payments)

### Added - Event-Driven Integration Examples
- **RabbitMQ → REST** (`examples/integration/rabbit-to-rest/`)
  - Consume messages and call external REST APIs
  - Includes DLQ, circuit breaker, and retry configuration
  - Order processing and CRM sync examples
- **RabbitMQ → GraphQL** (`examples/integration/rabbit-to-graphql/`)
  - Consume messages and call GraphQL APIs
  - Inventory updates and user sync examples
  - Bulk operations and query-before-mutation patterns
- **RabbitMQ → Exec** (`examples/integration/rabbit-to-exec/`)
  - Consume messages and execute processes/scripts
  - PDF generation, image processing, video transcoding
  - Semaphore-based concurrency control examples
- **REST → RabbitMQ** (`examples/integration/rest-to-rabbit/`)
  - API Gateway pattern: receive HTTP, queue for processing
  - Webhook receiver with dynamic routing
  - Bulk event ingestion and request-reply patterns
- **File → RabbitMQ** (`examples/integration/file-to-rabbit/`)
  - Scheduled file imports (cron-based)
  - Drop folder watching with polling
  - S3 file processing and log streaming
  - CSV, JSON, XML file processing examples
- **Integration Patterns Documentation** (`docs/INTEGRATION-PATTERNS.md`)
  - Event-driven architecture patterns section
  - Best practices: DLQ, semaphores, locks, circuit breakers
  - Complete order processing pipeline example

### Added - Phase 4.2 Runtime Integration
- **SyncManager**: Unified manager for sync primitives (Lock, Semaphore, Coordinator)
  - Memory and Redis backends for all primitives
  - Automatic resource cleanup on shutdown
  - Stats collection for monitoring
- **Flow execution with sync primitives**
  - Locks: Execute flows with distributed mutex protection
  - Semaphores: Limit concurrent flow executions
  - Coordinator: Signal/wait pattern for flow dependencies
  - CEL expression evaluation for dynamic sync keys
- **Scheduler integration**: Cron-based flow triggers fully integrated
  - Flows with `when` attribute automatically scheduled
  - Support for cron expressions, intervals, and shortcuts

### Added - Enterprise Connector Examples
- **Dynamic API Key Validation** (`examples/dynamic-api-key/`)
  - Validates API keys against database instead of static config
  - Supports user association, expiration, and metadata
  - Auth context available in flows via `auth.user_id` and `auth.claims`
- **gRPC Load Balancing** (`examples/grpc-loadbalancing/`)
  - Client-side load balancing with `round_robin` and `pick_first` policies
  - DNS-based service discovery support
  - Client-side health checking
- **Redis Cluster/Sentinel** (`examples/redis-cluster/`)
  - Redis Cluster mode for horizontal sharding
  - Redis Sentinel mode for automatic failover
  - Connection pooling and timeout configuration
- **Database Read Replicas** (`examples/read-replicas/`)
  - Automatic read/write routing for PostgreSQL and MySQL
  - Load balancing strategies: round_robin, random, least_conn
  - Replication lag handling with max_lag configuration
- **Validators README** (`examples/validators/README.md`)
  - Documentation for regex and CEL validators
  - Usage examples and best practices

### Added - Documentation Improvements
- **Getting Started Guide** (`docs/GETTING_STARTED.md`)
  - Step-by-step tutorial from zero to running service
  - Examples for service, connectors, flows, and types
  - Verification commands with expected outputs
  - Next steps with links to advanced features
- **Troubleshooting Guide** (`docs/TROUBLESHOOTING.md`)
  - Quick diagnosis commands
  - Startup issues (port in use, parse errors)
  - Database issues (connection, auth, missing db)
  - Flow issues (not triggered, transform errors)
  - Message queue issues
  - Performance issues
  - Docker/Kubernetes issues
- **Observability Guide** (`docs/OBSERVABILITY.md`)
  - Complete Prometheus metrics reference
  - Health check endpoints documentation
  - Kubernetes probe configuration
  - Grafana dashboard examples
  - Common PromQL queries
  - Alerting rules examples
- **Example Verification Sections**
  - Added "Verify It Works" sections to 13 examples
  - Each includes expected logs, curl commands, common issues
  - Examples: basic, enrich, tcp, graphql, cache, profiles,
    grpc, auth, notifications, mocks, files, s3, mongodb
- **CLI Help Messages**
  - Added Quick Start section to root command
  - Added comprehensive examples to all commands
  - Added environment variables documentation
  - Added common issues to check command

### Added - Helm Chart for Kubernetes
- **Helm Chart** (`helm/mycel/`)
  - Complete Kubernetes deployment configuration
  - Deployment with configurable replicas and rolling updates
  - Service (ClusterIP, NodePort, LoadBalancer)
  - Ingress with TLS support
  - ConfigMap for HCL configuration files
  - Secret for sensitive environment variables
  - ServiceAccount with configurable annotations
  - HorizontalPodAutoscaler for auto-scaling
  - PodDisruptionBudget for high availability
  - ServiceMonitor for Prometheus Operator
  - Health checks (liveness/readiness probes)
  - Security context (non-root, read-only filesystem)
  - Resource limits and requests
  - Comprehensive documentation and examples
- **GitHub Actions for Helm Release**
  - Automatic chart versioning from git tags
  - Push to GitHub Container Registry (GHCR)
  - Chart attached to GitHub releases
  - Install via: `helm install mycel oci://ghcr.io/matutetandil/charts/mycel`

### Added - Notification Connectors (Phase 6)
- **Webhooks** (`internal/connector/webhook/`)
  - Inbound webhook receiver with HTTP handler
  - Outbound webhook sender with retry and exponential backoff
  - Signature verification (HMAC-SHA256, HMAC-SHA1)
  - Support for Stripe and GitHub signature formats
  - Timestamp validation for replay protection
  - IP allowlist for inbound webhooks
- **Email** (`internal/connector/email/`)
  - SMTP connector with connection pooling and STARTTLS/TLS
  - SendGrid API connector with template support
  - AWS SES connector with v2 SDK
  - Support for attachments, CC/BCC, reply-to
  - HTML and plain text content
- **Slack** (`internal/connector/slack/`)
  - Webhook-based messaging
  - Bot API support with OAuth tokens
  - Rich message formatting with blocks
  - Attachments and interactive elements
- **Discord** (`internal/connector/discord/`)
  - Webhook-based messaging
  - Bot API support
  - Embeds with fields, images, thumbnails
  - Interactive components
- **SMS** (`internal/connector/sms/`)
  - Twilio connector with full API support
  - AWS SNS connector for SMS delivery
- **Push Notifications** (`internal/connector/push/`)
  - Firebase Cloud Messaging (FCM) with legacy API
  - Apple Push Notification service (APNs) with HTTP/2

### Added - SSO and Social Login (Phase 5.1d)
- **OAuth2/OIDC Base** (`internal/auth/sso_oauth.go`)
  - OAuth2Service for authorization code flow
  - OIDCService with discovery document support
  - State generation and token exchange
  - User info fetching and token refresh
  - ID token parsing for OIDC claims
- **Social Providers** (`internal/auth/sso_providers.go`)
  - `GoogleProvider` with offline access and refresh tokens
  - `GitHubProvider` with email fetching from emails API
  - `AppleProvider` with Sign in with Apple (ES256 client secret)
  - `OIDCProvider` for enterprise SSO (Okta, Azure AD, Auth0)
  - Configurable scopes and claim mappings per provider
- **Account Linking** (`internal/auth/sso_linking.go`)
  - `LinkedAccountStore` interface with memory implementation
  - `AccountLinkingService` for user/account association
  - Match strategies: email, none
  - On-match actions: link, prompt, reject
  - Prevention of unlinking only authentication method
  - Duplicate provider account prevention
- **SSO Orchestration** (`internal/auth/sso.go`)
  - `SSOService` coordinating all SSO flows
  - Provider initialization and OIDC discovery
  - State management with automatic expiration
  - Background cleanup for expired states
  - Unified callback handling with account linking
- **Tests** (`internal/auth/sso_test.go`)
  - OAuth2 flow tests with mock HTTP servers
  - OIDC discovery and ID token parsing
  - Account linking scenarios (new user, existing user, reuse)
  - Provider auth URL generation tests
  - State management and cleanup tests

### Added - Multi-Factor Authentication (Phase 5.1c)
- **MFA Service** (`internal/auth/mfa.go`)
  - Complete MFA orchestration service
  - Support for multiple MFA methods (TOTP, WebAuthn, Recovery)
  - `MFAStatus` for user MFA state
  - `MFASetup` for setup ceremony data
  - `MFAUserData` for persistent storage
  - `MFAStore` interface with memory implementation
- **TOTP Implementation** (`internal/auth/mfa_totp.go`)
  - RFC 6238 compliant TOTP generation
  - Support for SHA1, SHA256, SHA512 algorithms
  - Configurable digits (6/8) and period (30s default)
  - Clock skew tolerance
  - QR code generation for authenticator apps
  - Provisioning URI (otpauth://) generation
- **Recovery Codes**
  - Configurable count and length
  - Secure hashing with Argon2id
  - One-time use with automatic consumption
  - Regeneration support
- **WebAuthn/Passkeys** (`internal/auth/mfa_webauthn.go`)
  - Registration and login ceremonies
  - Support for platform and cross-platform authenticators
  - Attestation preferences (none, indirect, direct)
  - User verification options
  - Multiple credentials per user
- **Manager Integration** (`internal/auth/manager.go`)
  - `WithMFAStore` option for MFA store injection
  - MFA verification in Login flow (TOTP and recovery codes)
  - MFA management methods:
    - `GetMFAStatus`: Check user's MFA status
    - `BeginTOTPSetup`/`ConfirmTOTPSetup`: TOTP enrollment flow
    - `DisableMFA`: Disable all MFA methods (requires password)
    - `RegenerateRecoveryCodes`: Generate new recovery codes
  - WebAuthn methods:
    - `BeginWebAuthnRegistration`/`FinishWebAuthnRegistration`
    - `GetWebAuthnCredentials`/`RemoveWebAuthnCredential`
- **UserStore Interface Extensions**
  - `UpdateMFAEnabled` method for all implementations
  - Memory, PostgreSQL, and MySQL support
- **Dependencies**
  - `github.com/boombuler/barcode` - QR code generation
  - `github.com/go-webauthn/webauthn` - WebAuthn protocol
- **Tests** (`internal/auth/mfa_test.go`)
  - Full TOTP setup and validation flow
  - Recovery code lifecycle
  - MFA store operations

### Added - MySQL Storage Support
- **MySQL Storage** (`internal/auth/storage_mysql.go`)
  - `MySQLUserStore` for user CRUD operations
  - Configurable table and column names via HCL
  - `MySQLPasswordHistoryStore` for password history
  - `MySQLAuditStore` for audit logging
  - `MySQLSessionStore` for session management
  - `MySQLTokenStore` for token blacklist
  - MySQL-specific syntax (? placeholders, ON DUPLICATE KEY)
  - Tests in `storage_mysql_test.go`

### Added - Authentication System Security Features (Phase 5.1b)
- **Redis Storage** (`internal/auth/storage_redis.go`)
  - `RedisSessionStore` for session storage with TTL
  - `RedisTokenStore` for refresh tokens and blacklist
  - `RedisBruteForceStore` with progressive delay support
  - `RedisReplayProtectionStore` for one-time token usage
  - All stores implement the base interfaces
- **PostgreSQL Storage** (`internal/auth/storage_postgres.go`)
  - `PostgresUserStore` for user CRUD operations
  - Configurable table and column names via HCL
  - `PostgresPasswordHistoryStore` for password history
  - `PostgresAuditStore` for audit logging
  - Event filtering support
- **Brute Force Service** (`internal/auth/bruteforce.go`)
  - `BruteForceService` for coordinated protection
  - `CheckAccess()` returns lockout status and progressive delay
  - `RecordFailedAttempt()` with automatic lockout
  - `RecordSuccess()` clears attempts
  - `GetStats()` for monitoring
  - Progressive delay: exponential backoff with max cap
- **Session Cleanup Service** (`internal/auth/cleanup.go`)
  - `CleanupService` with configurable interval
  - Automatic cleanup of expired sessions
  - Idle session timeout support
  - Token blacklist cleanup
  - Graceful start/stop with context support
  - `MemorySessionStoreWithIdle` for idle timeout
- **Per-Endpoint Rate Limiting** (`internal/auth/ratelimit.go`)
  - `RateLimiter` for global rate limiting
  - `PerKeyRateLimiter` for per-IP/user rate limiting
  - `RateLimitConfig` with per-endpoint configuration
  - Default stricter limits for sensitive endpoints (login: 5/min, register: 10/min)
  - `RateLimitMiddleware` for HTTP handler integration
  - Key extraction by IP, user, or combined
- **Extended BruteForceStore Interface**
  - Added `GetAttempts()` alias for compatibility
  - Added `GetDelay()` for progressive delay retrieval
  - Added `SetDelay()` for progressive delay storage
- **Tests**
  - `bruteforce_test.go` - Brute force protection tests
  - `cleanup_test.go` - Cleanup service lifecycle tests
  - `ratelimit_test.go` - Rate limiting tests

### Added - Authentication System Core (Phase 5.1a)
- **Auth Package** (`internal/auth/`)
  - Complete enterprise-grade authentication system
  - Declarative configuration via HCL `auth {}` block
- **Types and Config** (`internal/auth/types.go`)
  - Full configuration structs for all auth features
  - User, Session, TokenPair, Claims types
  - LoginRequest, RegisterRequest, RefreshRequest
  - Common auth errors (ErrInvalidCredentials, ErrUserNotFound, etc.)
- **Presets** (`internal/auth/presets.go`)
  - `strict`: Maximum security (MFA required, 15m tokens, strong passwords)
  - `standard`: Balanced (MFA optional, 1h tokens, moderate passwords)
  - `relaxed`: Minimal (no MFA, 24h tokens, basic passwords)
  - `development`: For dev (no security, 7d tokens, no requirements)
  - `MergeWithPreset()` for combining user config with defaults
  - `ParseDuration()` supporting day suffix (e.g., "7d", "90d")
- **Password Hashing** (`internal/auth/password.go`)
  - Argon2id hashing (memory-hard, GPU-resistant)
  - PHC string format for hash storage
  - `PasswordHasher` with configurable parameters
  - `PasswordValidator` for policy enforcement
  - Complexity requirements (upper, lower, number, special)
  - Strength scoring (0-100)
  - `GenerateRandomPassword()` utility
- **JWT Tokens** (`internal/auth/jwt.go`)
  - Support for HS256, RS256, ES256 and variants
  - Access and refresh token generation
  - Token validation with issuer/audience verification
  - Custom claims support
  - `TokenManager` for all JWT operations
- **Storage Interfaces** (`internal/auth/storage.go`)
  - `UserStore` interface for user CRUD
  - `SessionStore` interface for session management
  - `TokenStore` interface for blacklist/replay protection
  - `BruteForceStore` interface for failed attempt tracking
  - In-memory implementations for all stores (development/testing)
- **Auth Manager** (`internal/auth/manager.go`)
  - Central coordination of all auth components
  - `Register()`, `Login()`, `Logout()`, `LogoutAll()`
  - `ValidateToken()`, `RefreshToken()`
  - `ChangePassword()`, `GetSessions()`, `RevokeSession()`
  - Brute force protection with configurable lockout
  - Session limits with oldest-first revocation
- **HTTP Handlers** (`internal/auth/handlers.go`)
  - REST endpoints for all auth operations
  - Automatic endpoint registration on HTTP mux
  - Configurable paths and methods
  - Proper error responses with codes
- **Middleware** (`internal/auth/middleware.go`)
  - `Middleware` for protecting routes
  - Path exclusion support
  - Role and permission-based authorization
  - `RequireAuth()`, `OptionalAuth()` helpers
  - `RequireRoles()`, `RequirePermissions()` helpers
  - Context extraction: `GetUser()`, `GetClaims()`
- **HCL Parser** (`internal/parser/auth.go`)
  - Full parsing of `auth {}` block
  - Support for all nested blocks (jwt, password, mfa, security, etc.)
  - WebAuthn configuration with biometrics/passkeys
  - Social login and SSO configuration
  - External provider configuration
- **Runtime Integration** (`internal/runtime/runtime.go`)
  - `authManager` and `authHandler` fields
  - Automatic initialization when auth config present
  - `AuthManager()` and `AuthHandler()` getters
- **Tests** (`internal/auth/auth_test.go`)
  - Password hashing and verification
  - Password validation and strength
  - Token generation and validation
  - Full auth flow (register, login, refresh, logout)
  - Memory store operations
  - Preset configuration
- **Example**: `examples/auth/`
  - Complete auth service configuration
  - Database schema for PostgreSQL
  - API documentation with curl examples

### Added - Plugin System (Phase 5e)
- **Plugin Types** (`internal/plugin/types.go`)
  - `PluginDeclaration` for plugin references in config
  - `PluginManifest` for plugin metadata
  - `ConnectorProvide` for connector definitions
  - `ConfigField` for connector configuration schema
  - `LoadedPlugin` for runtime plugin state
- **Plugin Loader** (`internal/plugin/loader.go`)
  - Load plugins from local directories
  - Parse `plugin.hcl` manifest files
  - Resolve plugin paths (local, git planned, registry planned)
- **WASM Connector** (`internal/plugin/connector.go`)
  - `WASMConnector` implementing `connector.Connector`, `Reader`, `Writer`
  - JSON-based communication with WASM modules
  - Support for `init()`, `read()`, `write()`, `call()`, `health()`, `close()`
- **Plugin Registry** (`internal/plugin/registry.go`)
  - Manage loaded plugins
  - Create connector instances from plugins
  - Track connector types provided by plugins
- **Plugin Factory** (`internal/plugin/factory.go`)
  - Factory for creating plugin connectors
  - Support for `type = "plugin"` or direct plugin type names
- **Parser support** for `plugin` blocks
  - `source` attribute for plugin location
  - `version` attribute for version constraints (git/registry)
- **Runtime integration**
  - Plugin registry initialization at startup
  - Plugin connector factory registration
  - Plugin functions integration with CEL
- **Example**: `examples/plugin/`
  - Example plugin structure and manifest
  - Documentation for building WASM connectors

### Added - WASM Functions (Phase 5d)
- **Custom Functions** (`internal/functions/`)
  - WASM functions that extend CEL transform expressions
  - `functions "name" { wasm = "...", exports = [...] }` blocks
  - Registry for managing function modules
  - Support for 0-5 function arguments
- **CEL Integration** (`internal/transform/wasm_functions.go`)
  - `CreateWASMFunctionOptions()` for CEL environment setup
  - `NewCELTransformerWithOptions()` for custom function support
  - Automatic JSON serialization for function calls
- **Parser support** for `functions` blocks
  - `wasm` attribute for .wasm file path
  - `exports` array for function names
- **Example**: `examples/wasm-functions/`
  - Pricing functions (calculate_price, apply_discount, tax_for_country)
  - Complete Rust example with checkout flow

### Added - WASM Runtime and Validators (Phase 5)
- **WASM Runtime** (`internal/wasm/`)
  - Pure Go runtime using wazero (no CGO)
  - Module loading from .wasm files
  - Memory management with alloc/free helpers
  - JSON-based function I/O
  - Hot reload support for WASM modules
- **WASM Validators** (`internal/validator/wasm.go`)
  - `WASMValidator` type for compiled validators
  - Shared runtime with module caching
  - CallValidate helper for validation functions
- **Example**: `examples/wasm-validator/`
  - Complete Rust example for building validators
  - Documentation for WASM interface specification

### Added - Custom Validators (Phase 5)
- **Custom Validators** (`internal/validator/`)
  - Regex validators for pattern matching (email, phone, UUID, etc.)
  - CEL validators for expression-based validation (age checks, enums, password strength)
  - WASM validators for complex custom logic
  - Validator registry for managing validators
  - Factory function for creating validators from config
- **Parser support** for `validator` blocks
  - `type = "regex"` with `pattern` attribute
  - `type = "cel"` with `expr` attribute
  - `type = "wasm"` with `wasm` and `entrypoint` attributes
  - Custom `message` for validation errors
- **Integration with type system**
  - `ValidatorRef` field in FieldSchema
  - `CustomValidatorConstraint` for using validators as constraints
- **Example**: `examples/validators/`
  - Regex validators: email, phone_ar, uuid, slug, username
  - CEL validators: adult_age, positive_number, valid_status, strong_password

### Fixed - Parser & Example Files
- **Parser support for MQ connectors** (`internal/parser/connector.go`)
  - Added `username` attribute (alias for `user`)
  - Added `vhost` attribute for RabbitMQ virtual host
  - Added `exchange` block for MQ exchange configuration
- **MQ example files** (`examples/mq/`)
  - Fixed CORS syntax: `allowed_origins` → `origins`, `allowed_methods` → `methods`
  - Fixed types.hcl syntax: changed from `field {}` blocks to simple `field = type` attributes
  - Removed invalid `operation` attribute from `to` blocks in flows
- **AsyncAPI CLI flags** (`cmd/mycel/main.go`)
  - Added `-o/--output` and `-f/--format` flags to AsyncAPI export command

### Added - Documentation Export (Phase 5)
- **Mock System** (`internal/mock/`)
  - JSON-based mock files for connector responses
  - Conditional responses with CEL expressions
  - CLI flags: `--mock=connector` and `--no-mock=connector`
  - `mocks {}` block in service configuration
  - Connector wrapping for seamless mock injection
  - Example: `examples/mocks/`
- **OpenAPI Export** (`internal/export/openapi/`)
  - Generate OpenAPI 3.0.3 specification from Mycel configuration
  - REST endpoints from flows with path parameters
  - Request/response schemas from types
  - Server information from connectors
  - CLI command: `mycel export openapi`
  - Flags: `-o/--output`, `-f/--format` (yaml/json), `--base-url`
- **AsyncAPI Export** (`internal/export/asyncapi/`)
  - Generate AsyncAPI 2.6.0 specification from Mycel configuration
  - Message channels from MQ flows (RabbitMQ, Kafka)
  - Subscribe/Publish operations
  - Message schemas from types
  - Server information with protocol bindings
  - CLI command: `mycel export asyncapi`
  - Flags: `-o/--output`, `-f/--format` (yaml/json)
- **HCL Syntax for Mocks**:
  ```hcl
  service {
    name = "my-service"

    mocks {
      enabled = true
      path    = "./mocks"
    }
  }
  ```
- **Mock File Format**:
  ```json
  {
    "responses": [
      {"when": "input.id == 1", "data": {"id": 1, "name": "John"}},
      {"default": true, "data": []}
    ]
  }
  ```

### Added - Synchronization Primitives (Phase 4.2)
- **Lock (Mutex)** - Distributed mutex for exclusive access by key
  - Memory and Redis implementations
  - `lock {}` block in flows with key, timeout, wait, retry options
  - Lua script for safe release (only owner can release)
- **Semaphore** - Limit concurrent access to resources
  - Memory and Redis implementations (sorted sets + Lua)
  - `semaphore {}` block with max_permits, lease, timeout
  - Automatic lease expiration for crash protection
- **Coordinate** - Signal/Wait pattern for dependency coordination
  - `wait {}` - Wait for a signal with conditional expression
  - `signal {}` - Emit signal when condition is met
  - `preflight {}` - Check database before waiting
  - `on_timeout` options: fail, retry, skip, pass
  - Redis Pub/Sub hub for efficient waiting
- **Flow Triggers** - Cron and interval scheduling
  - `when` attribute: "always", cron expressions, "@every X"
  - Shortcuts: @hourly, @daily, @weekly, @monthly
  - Uses robfig/cron/v3 library
- **MQ Headers Access**
  - `input.body`, `input.headers`, `input.properties` for RabbitMQ
  - `input.body`, `input.headers`, `input.key`, `input.topic` for Kafka
- **Prometheus Metrics** for sync primitives
  - Lock acquired/released/timeout counters
  - Semaphore acquired/released/available gauges
  - Coordinate signal/wait/timeout metrics
  - Scheduler execution counters
- **Parser Support** for lock, semaphore, coordinate, when blocks
- **Full specification**: [docs/PHASE-4.2-SYNC.md](docs/PHASE-4.2-SYNC.md)

### Added - Connector Profiles (Phase 4.3)
- **Connector Profiles** - Multiple backend implementations for the same logical connector
  - `type = "profiled"`: New connector type for profile-based routing
  - `select` attribute: CEL expression to determine active profile (e.g., `env('PRICE_SOURCE')`)
  - `default` attribute: Fallback profile when select evaluates to empty
  - `fallback` attribute: Ordered list of profiles to try on failure
- **ProfiledConnector** (`internal/connector/profile/`)
  - Wrapper implementing Connector interface
  - Routes operations to the active profile
  - Automatic fallback on retriable errors (connection timeout, 5xx)
  - Statistics tracking per profile (requests, errors, fallbacks)
- **Per-profile transforms** to normalize data from different backends
  - Each profile can have its own transform block
  - CEL expressions applied after reading from backend
  - Normalizes data before passing to flow (consistent interface)
- **Prometheus Metrics** for profile observability
  - `mycel_connector_profile_active` - Currently active profile (gauge)
  - `mycel_connector_profile_requests_total` - Requests per profile (counter)
  - `mycel_connector_profile_errors_total` - Errors per profile (counter)
  - `mycel_connector_profile_fallback_total` - Fallback events (counter)
  - `mycel_connector_profile_latency_seconds` - Latency per profile (histogram)
- **Parser Support** (`internal/parser/connector.go`)
  - `select`, `default`, `fallback` attributes
  - `profile "name" {}` blocks with label for name
  - `transform {}` blocks within profiles
- **Factory Integration** (`internal/connector/profile/factory.go`)
  - ProfileFactory creates ProfiledConnector instances
  - Uses Registry to create underlying connectors for each profile
- **Example** (`examples/profiles/`)
  - Pricing service with Magento, ERP, and Legacy backends
  - Profile selection via PRICE_SOURCE environment variable
- **Use cases**:
  - Same API, different data sources (Magento vs ERP vs Legacy)
  - Multi-region deployments
  - Read replicas vs primary database
  - Gradual migration between systems
- **HCL Syntax**:
  ```hcl
  connector "pricing" {
    type = "profiled"

    select   = "env('PRICE_SOURCE')"
    default  = "magento"
    fallback = ["erp", "legacy"]

    profile "magento" {
      type     = "http"
      driver   = "client"
      base_url = "http://magento/api"

      transform {
        product_id = "input.entity_id"
        price      = "double(input.price)"
        source     = "'magento'"
      }
    }

    profile "erp" {
      type     = "database"
      driver   = "postgres"
      host     = env("ERP_DB_HOST")

      transform {
        product_id = "string(input.id)"
        price      = "input.precio"
        source     = "'erp'"
      }
    }
  }
  ```
- **Full specification**: [docs/PHASE-4.3-PROFILES.md](docs/PHASE-4.3-PROFILES.md)

### Added - Runtime Configuration (Phase 4.1)
- **Environment Variables** for runtime configuration
  - `MYCEL_ENV`: Select environment (development, staging, production)
  - `MYCEL_LOG_LEVEL`: Set log level (debug, info, warn, error)
  - `MYCEL_LOG_FORMAT`: Set log format (text, json)
  - Flags override environment variables (priority: flag > env var > default)
- **Logging Package** (`internal/logging/`)
  - Centralized logging configuration
  - JSON logging support for production environments
  - Level filtering with standard slog integration
  - Comprehensive test coverage
- **CLI Improvements**
  - New `--log-level` flag: debug, info, warn, error
  - New `--log-format` flag: text, json
  - Deprecated `--verbose` flag (use `--log-level=debug` instead)
- **Docker Configuration Updates**
  - Standard config path: `/etc/mycel` (instead of `/config`)
  - Production defaults: `MYCEL_LOG_FORMAT=json`
  - Updated docker-compose.yml with documented env vars
- **Documentation Updates**
  - README updated with environment variables table
  - ROADMAP marked Phase 4.1 as complete

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
