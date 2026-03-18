# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.15.0] - 2026-03-18

### Changed
- **Connector Owns Config refactor**: Parser no longer hardcodes connector-specific attributes. Each connector now validates its own parameters via `SourceValidator` / `TargetValidator` interfaces. All connector-specific data flows through a `ConnectorParams` map and is accessed via getter methods (`GetOperation`, `GetTarget`, `GetQuery`, etc.)
- **Parser simplification**: Parser only declares flow-level attributes (`connector`, `when`, `parallel`, `filter`, `timeout`, `on_error`, `default`). Connector-specific attributes (`operation`, `target`, `query`, `format`, `filter`, `params`, `body`, `query_filter`, `update`) are captured dynamically into `ConnectorParams` instead of typed struct fields
- **Connector validation interfaces**: 14 connectors implement `SourceValidator` and/or `TargetValidator` interfaces to validate their own parameters at parse time. New or plugin connectors can accept any parameters without parser changes
- **Removed typed fields from config structs**: `Operation`, `Target`, `Query`, `Filter`, `Format`, `Params`, `Body`, `QueryFilter`, `Update` removed from `FromConfig`, `ToConfig`, `StepConfig`, `EnrichConfig`. All access goes through `ConnectorParams` getters

## [1.14.4] - 2026-03-18

### Added
- **Automatic debug throttling**: When a Studio debugger connects, all event-driven connectors automatically switch to single-message processing. RabbitMQ sets AMQP prefetch to 1, Kafka/Redis/MQTT/CDC/File/WebSocket use a shared semaphore gate. Original concurrency is restored when the debugger disconnects. Zero overhead when no debugger is connected
- **`DebugThrottler` interface** (`internal/connector/connector.go`): Optional interface for event-driven connectors — `SetDebugMode(enabled bool)`. Implemented by 7 connectors: RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch, WebSocket
- **`DebugGate`** (`internal/connector/debuggate.go`): Reusable token-based semaphore. `Acquire()` blocks when enabled, passes through when disabled. 4 unit tests
- **Debug server `OnClientChange` callback** (`internal/debug/server.go`): Called when clients go from 0→1 (enable) or 1→0 (disable). Runtime wires this to toggle `SetDebugMode` on all connectors
- **Start Suspended mode** (`--debug-suspend` / `MYCEL_DEBUG_SUSPEND=true`): Event-driven connectors defer `Start()` until a debugger connects via `debug.attach`. Prevents message consumption before breakpoints are set. REST/gRPC/GraphQL/SOAP/TCP/SSE start normally (needed for health checks). Dev-only, automatically disabled outside development mode
- **Source properties reference** (`docs/reference/source-properties.md`): Complete reference of `from` block properties per connector type — operation format, `input.*` variables, and examples for all 13 source connector types

## [1.14.3] - 2026-03-16

### Changed
- **PDF connector: template in config**: Template path moved from flow transform payload to connector configuration (`template` attribute). Connector-level template serves as default; flows can still override via `template` payload field for dynamic template selection. Follows the black-box principle — infrastructure details belong in connector config, not in business flows
- **Email connector: template in config**: Template path moved from flow payload (`template_file`) to connector configuration (`template` attribute). Same resolution: connector config as default, payload override for dynamic cases. Field renamed from `template_file` to `template` for consistency with PDF connector
- **Consistent template naming**: Both PDF and email connectors now use `template` as the field name (email previously used `template_file`)

## [1.14.2] - 2026-03-16

### Fixed
- **Aspect metadata pollution**: `enrichInput()` injected `_flow`, `_operation`, `_target`, `_timestamp` into input maps which reached DB connectors as column names, causing SQL errors on all flows when aspects were loaded. Metadata is now stripped before passing to the flow core
- **Cache hit type mismatch**: Cached results (from Redis or memory) deserialized as `[]interface{}` instead of `[]map[string]interface{}`, causing `resultToConnectorResult` to return empty results. Added `[]interface{}` handling with map conversion
- **MongoDB ID preservation**: `resultToConnectorResult` only handled `int64` and `int` for LastID, losing MongoDB's string hex ObjectIDs. Now preserves original ID type
- **Integration test: cache key format**: Cache flow HCL used CEL string literals (`'cached_users'`) for cache keys, but `buildCacheKey` doesn't evaluate CEL — resulting in keys with embedded quotes. Changed to plain strings
- **Integration test: aspects SQLite contention**: Aspects test `POST /aspects/init` failed with `SQLITE_BUSY` when running in parallel with SQLite test. Added retry with backoff
- **Integration test: plugin health flakiness**: Plugin test health check occasionally returned 503 during startup. Added retry loop
- **PDF connector documentation**: Added `docs/connectors/pdf.md` with full reference (configuration, operations, HTML template syntax, complete invoice example)

## [1.14.1] - 2026-03-16

### Added
- **Fan-out from source**: Multiple flows can now share the same `from` connector and operation. When a request or message arrives, all registered flows execute concurrently. For request-response connectors (REST, gRPC, TCP, WebSocket, SOAP, SSE, GraphQL), the first registered flow returns the response while additional flows run as fire-and-forget. For event-driven connectors (RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch), all flows execute in parallel and the message is acknowledged only after all complete. 13 tests covering chain helpers, input isolation, error handling, and nested chaining
- **Fan-out chain helpers** (`internal/connector/fanout.go`): `ChainRequestResponse` and `ChainEventDriven` helper functions that compose multiple handlers into a single handler. `CopyInput` ensures input isolation between concurrent handlers. Used by all 14 connector types
- **Common `HandlerFunc` type** (`internal/connector/fanout.go`): Universal handler type in the connector package, enabling type-safe chaining across all connector implementations

### Changed
- **All connector `RegisterRoute` methods**: Now detect duplicate registrations and chain handlers using fan-out instead of silently overwriting. Logs `fan-out: multiple flows registered` at Info level when chaining occurs. Affects: REST, gRPC, TCP, WebSocket, SOAP, SSE, GraphQL (server+client), RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch (14 connectors)

## [1.14.0] - 2026-03-15

### Added
- **Mycel Studio Debug Protocol** (`internal/debug/`): WebSocket JSON-RPC 2.0 debug server for IDE integration. Mounted on admin server at `:9090/debug`. Provides full runtime introspection and live debugging of running Mycel services. 29 tests covering all 6 protocol phases
- **Session management**: `debug.attach` / `debug.detach` RPC methods. Each connected client gets an isolated session with its own breakpoints, threads, and resume channels. Multiple IDE clients can connect simultaneously
- **Runtime introspection**: `RuntimeInspector` interface exposes read-only views of a live service. RPC methods: `inspect.flows`, `inspect.flow`, `inspect.connectors`, `inspect.types`, `inspect.transforms`. Enables IDEs to build autocompletion and object trees from a running service
- **Event streaming**: `EventStream` fan-out broadcasts pipeline trace events to all connected debug clients in real time. `StudioCollector` implements `trace.Collector`. Events: `event.flowStart`, `event.flowEnd`, `event.stageEnter`, `event.stageExit`, `event.ruleEval`
- **Stage-level breakpoints**: `StudioBreakpointController` implements `trace.BreakpointController`. `debug.setBreakpoints` configures stage breakpoints per flow. `debug.continue` / `debug.next` resume execution. `debug.threads` lists in-flight requests. `debug.variables` inspects data at paused stage. `event.stopped` / `event.continued` notifications. One `DebugThread` per concurrent request
- **Per-CEL-rule breakpoints**: `TransformHook` interface (`BeforeRule` / `AfterRule`) injected via `context.Context` into CEL rule evaluation loops. `StudioTransformHook` streams `event.ruleEval` and pauses at individual rules. `debug.stepInto` steps into the next rule within a transform block
- **Watch expressions and evaluate**: `debug.evaluate` executes a CEL expression against the paused thread's activation record. Enables ad-hoc queries like `output.email` or `size(input.items)` while paused at a breakpoint
- **Conditional breakpoints**: `debug.setBreakpoints` accepts an optional `condition` field (CEL expression). Breakpoint only pauses when condition evaluates to `true`
- **TransformHook context helpers** (`internal/transform/hook.go`): `WithTransformHook(ctx, hook)` / `HookFromContext(ctx)`. Zero-cost when no hook (~10ns nil-check)

### Changed
- **Admin server always starts**: `:9090` now starts unconditionally on every `mycel start`, regardless of REST connector presence. Ensures debug protocol, health checks, and metrics are always reachable
- **`internal/transform/cel.go`**: Hook injection in `Transform`, `TransformResponse`, `TransformWithContext` rule loops. Zero overhead when no hook: single nil-check per transform invocation
- **`internal/runtime/runtime.go`**: Debug server initialization, `RuntimeInspector` methods on Runtime, admin mux registration
- **`internal/runtime/flow_registry.go`**: Debug context injection in `HandleRequest` — trace, breakpoints, and transform hooks attached when debug session is active

## [1.13.0] - 2026-03-13

### Added
- **PDF connector** (`internal/connector/pdf/`): Generate PDF documents from HTML templates using pure Go (no CGO, no external binaries). Uses `go-pdf/fpdf` for rendering and Go's `text/template` for data binding. Supports: headings (h1-h6), paragraphs, tables with headers, bold/italic, lists (ul/ol), horizontal rules, images, and basic CSS styles (text-align, font-size, color). Two operations: `generate` (returns PDF bytes for HTTP response) and `save` (writes to file)
- **Binary HTTP responses**: REST connector now detects `_binary` + `_content_type` fields in results and serves raw binary responses (PDF, images, etc.) with proper Content-Type and Content-Disposition headers
- **Response enrichment in after aspects**: After aspects can now include a `response` block with CEL expression body fields and HTTP headers. Body fields are merged into every row of the result. Headers are set as actual HTTP headers by the REST connector (or protocol equivalent for other connectors). Only valid for `after` aspects. Useful for API versioning (RFC 8594 deprecation headers), pagination metadata, CORS, and cross-cutting response decoration
- **Idempotency keys**: Flow-level `idempotency` block with `storage` (cache connector), `key` (CEL expression), and `ttl`. Prevents duplicate processing by caching results and returning them for matching keys
- **Async execution (HTTP 202 + polling)**: Flow-level `async` block with `storage` (cache connector) and `ttl`. Returns HTTP 202 with a `job_id` immediately, processes in background, auto-registers `GET /jobs/{job_id}` polling endpoint
- **Database migrations**: `mycel migrate` CLI command runs SQL migration files from `migrations/` directory in alphabetical order. `mycel migrate status` shows migration status. Tracking via `_mycel_migrations` table (SQLite + PostgreSQL compatible)
- **File upload (multipart/form-data)**: REST connector auto-detects multipart uploads, parses files (32MB max), encodes as base64 with metadata (`filename`, `content_type`, `size`, `data`). Available in transforms as `input.files`
- **HTML email templates**: Email connectors (SMTP, SendGrid, SES) support `template_file` for Go `text/template` rendering. Template receives the full payload as data context
- **Multi-tenancy via request headers**: Request headers now available as `input.headers` in flow transforms/CEL expressions. Enables tenant isolation by reading `X-Tenant-ID` or similar headers
- **Distributed rate limiting**: Rate limiter now supports Redis backend via `storage` attribute in `rate_limit` block. Uses fixed-window counter algorithm with automatic fallback to in-memory on Redis errors
- **Use case examples #15-22**: Queue consumer to database, scheduled/cron jobs, API aggregation (BFF pattern), CDC pipeline, GraphQL API over database, circuit breaker on external APIs, PDF generation from HTML template, API versioning with deprecation warnings. Total: 22 complete examples
- New dependency: `github.com/go-pdf/fpdf` v0.9.0 (pure Go, BSD license)

## [1.12.3] - 2026-03-13

### Added
- **Flow invocation from aspects**: Aspect actions can now invoke flows directly using `action { flow = "flow_name" }` instead of only writing to connectors. The `connector` and `flow` attributes are mutually exclusive. The invoked flow receives the transform output as its input. Errors in invoked flows are soft failures (warning log, main flow unaffected)
- **Internal flows**: Flows without a `from` block can now serve as reusable building blocks, invocable only from aspects. Enables flow orchestration and composition through the AOP system
- **`FlowInvoker` interface** (`internal/aspect/executor.go`): Decoupled interface for flow invocation from aspect executor. `FlowRegistry` implements it via `InvokeFlow` method
- Use case examples #11-14: flow orchestration (welcome email), error recovery flow, notification hub (route by event type), data sync to external system

## [1.12.2] - 2026-03-13

### Added
- **Structured error object in on_error aspects**: The `error` variable in `on_error` aspects is now a structured object with `error.code` (int, HTTP status code), `error.message` (string), and `error.type` (string: `http`, `flow`, `validation`, `not_found`, `timeout`, `connection`, `auth`, `unknown`). Enables routing errors to different actions based on status code or error type (e.g., `if = "error.code == 404"` or `if = "error.type == 'timeout'"`)
- **Common use cases guide** (`docs/guides/use-cases.md`): 10 complete, copy-paste ready examples covering REST+DB+Slack notifications, welcome emails, audit logging, caching with invalidation, event publishing, error alerting with routing, input validation, response enrichment, webhook relay, and rate limiting

## [1.12.1] - 2026-03-12

### Changed
- **Aspects target flow names instead of file paths**: `on` patterns in aspects now match against flow names using `filepath.Match` glob syntax (e.g., `create_*`, `*_user`). File path matching removed entirely — aspects are now decoupled from filesystem layout
- **Unique name validation per type**: Parser now enforces unique names within each configuration type (connector, flow, type, transform, aspect, validator). Duplicate names produce clear errors with file locations: `duplicate flow name "create_user": defined in flows/api.hcl and flows/users.hcl`

### Removed
- **File path matching in aspects**: All `doublestar` and path-based matching code removed from `internal/aspect/registry.go`. No backward compatibility — patterns must reference flow names
- **`FlowPath` field**: Removed from `FlowHandler` struct in `internal/runtime/flow_registry.go`

## [1.12.0] - 2026-03-11

### Added
- **Response block**: New `response` block in flows transforms data **after** receiving it from the destination connector. For echo flows (no `to` block), the response block defines the output directly. Variables: `input.*` (original request), `output.*` (destination result)
- **Echo flows**: Flows without a `to` block are now fully supported. They return the transformed input (or response block output) directly, enabling pure transformation endpoints, health checks, and stub responses
- **HTTP status code override**: `http_status_code` field in response block sets custom HTTP status codes (REST, SOAP connectors). Example: `http_status_code = "501"` returns HTTP 501
- **gRPC status code override**: `grpc_status_code` field in response block sets custom gRPC status codes with optional `error` message field
- **`ExtractStatusCode` helper** (`internal/connector/connector.go`): Shared utility for extracting protocol-specific status codes from flow results, used by REST, SOAP, and gRPC connectors
- **`TransformResponse` method** (`internal/transform/cel.go`): CEL transformer method for response blocks with `input` and `output` context variables

### Fixed
- **Nil pointer dereference in echo flows**: Fixed multiple nil dereferences when `Config.To` is nil — in `registerFlows` (runtime.go), `executeFlowCoreInternal` (flow_registry.go lines 818, 874), and flow banner printing

## [1.11.0] - 2026-03-10

### Added
- **Environment-aware defaults** (`internal/envdefaults/`): `MYCEL_ENV` now changes runtime behavior, not just the banner. Central `ForEnvironment()` function returns defaults for development, staging, and production environments
- **Environment-aware logging**: Log level and format default to the environment (debug/text in dev, info/json in staging, warn/json in production). Priority: CLI flag > env var > environment default
- **Environment-aware hot reload**: Enabled by default in development/staging, disabled in production. Explicit `--hot-reload` flag overrides
- **Environment-aware GraphQL Playground**: Enabled in development/staging, disabled in production. Explicit `playground` property overrides
- **Environment-aware health checks**: Detailed mode (latencies + error messages) in development/staging, minimal (status only) in production via `SetDetailedMode()` on health manager
- **Environment-aware rate limiting**: Disabled by default in development, enabled with sensible defaults (100 req/s, burst 200) in staging/production when no explicit config
- **Environment-aware CORS**: Permissive (all origins) in development when no CORS config, strict (no CORS headers) in production
- **Environment-aware error responses**: Verbose errors in development/staging, minimal errors (no internal details) in production for 500-level responses
- **Startup warnings**: Production/staging log warnings for SQLite usage and missing auth configuration
- **Environment label in metrics**: `mycel_service_info` gauge now includes `environment` label
- **Environment propagation**: `connector.Config.Environment` field carries the runtime environment to all connector factories
- **Flow trace system** (`internal/trace/`): `mycel trace <flow-name>` CLI command executes a single flow and shows step-by-step data pipeline trace. Stages: input → sanitize → filter → dedupe → validate → enrich → transform → steps → read/write. Zero overhead in production (nil-check). `--dry-run` simulates writes without executing. `--list` shows available flows. `MemoryCollector` for CLI, `LogCollector` for runtime verbose logging. 9 tests
- **Debugging guide** (`docs/guides/debugging.md`): Complete reference for `mycel trace`, dry-run mode, breakpoints, verbose flow logging, Docker debugging
- **Connector doc cross-references**: All 16 connector docs now link to full configuration reference in `docs/reference/configuration.md`
- **Verbose flow logging** (`--verbose-flow`): Per-request pipeline tracing via structured logs at debug level. All pipeline stages logged for every request when enabled on `mycel start`
- **Interactive breakpoints** (`--breakpoints`, `--break-at`): Step-by-step interactive debugging in `mycel trace`. Pause at every stage or specific stages (input, sanitize, validate, transform, step, read, write). Commands: next, continue, print, quit, help
- **Dry-run for all write operations**: `--dry-run` now works for UPDATE, DELETE, and multi-destination writes (previously only INSERT)
- **DAP server** (`internal/dap/`): Debug Adapter Protocol server for IDE integration (VS Code, IntelliJ, Neovim). `mycel trace --dap=4711` starts a TCP DAP server. Supports: initialize, launch, setBreakpoints, configurationDone, threads, stackTrace, scopes, variables, continue, next, disconnect. Pipeline stages mapped to virtual line numbers. 11 tests
- **Dev-only debug features**: `--verbose-flow`, `--breakpoints`, `--break-at`, and `--dap` are restricted to development mode (`MYCEL_ENV=development`). In other environments, a warning is logged and the feature is silently disabled
- **BreakpointController interface** (`internal/trace/`): Breakpoint control abstracted to interface, enabling both CLI (`Breakpoint`) and IDE (`DAPBreakpoint`) implementations
- **MQTT connector** (`internal/connector/mqtt/`): Standalone IoT messaging connector. Publish/subscribe with QoS 0/1/2, topic wildcards (`+`, `#`), TLS support, automatic reconnection with re-subscription. `paho.mqtt.golang` client. 13 unit tests
- **FTP/SFTP connector** (`internal/connector/ftp/`): Remote file transfer over FTP, FTPS, and SFTP. Directory listing (LIST), file download (GET) with auto-format detection (JSON/CSV/text), file upload (PUT), directory creation (MKDIR), file deletion (DELETE). `remoteClient` interface abstracts both protocols. Standard `connector.Reader`/`connector.Writer` interfaces. 22 unit tests
- **Redis Pub/Sub** (`internal/connector/mq/redis/`): New MQ driver (`driver = "redis"`) for fire-and-forget pub/sub. Subscribe/PSubscribe with channel and glob-pattern matching. Handler resolution: exact channel → pattern → wildcard. Uses existing `go-redis/v9` dependency. 13 unit tests
- **Integration tests for MQTT, FTP/SFTP, Redis Pub/Sub**: Docker Compose services (Mosquitto MQTT broker, atmoz/sftp server), HCL configs (connectors + flows), test scripts following existing patterns. 3 new test suites added to parallel execution
- **Connector documentation**: `docs/connectors/mqtt.md` (MQTT), `docs/connectors/ftp.md` (FTP/SFTP), Redis Pub/Sub section added to `docs/connectors/message-queues.md`
- **Example configurations**: `examples/mqtt/` (IoT gateway), `examples/ftp/` (SFTP file processor), `examples/redis-pubsub/` (event processor)

### Changed
- **FTP connector interface compliance**: `Read` and `Write` methods now return standard `*connector.Result` instead of raw maps, enabling FTP/SFTP to work through the flow_registry like all other connectors
- `metrics.NewRegistry` now accepts an `environment` parameter for the service info metric
- REST connector CORS middleware is now environment-aware (permissive in dev, strict in prod)
- REST connector `writeError` now strips internal error details in production
- GraphQL factory uses `envdefaults.ForEnvironment()` for playground default instead of hardcoded `true`
- Logger creation uses environment defaults as baseline instead of hardcoded `info`/`text`
- Rate limiter initialization checks environment defaults when no explicit config

## [1.10.0] - 2026-03-09

### Added
- **CSV/TSV enhanced I/O** (`internal/connector/file/`): Configurable CSV options — delimiter (comma/tab/semicolon/pipe), comment character, skip_rows, no_header mode, custom column names, trim_space. TSV auto-detected from `.tsv`/`.tab` extensions. UTF-8 BOM detection and stripping. Sorted header output with optional column ordering. Connector-level CSV defaults via `csv_*` properties. 10 new tests
- **Long-running workflow engine** (`internal/workflow/`): Persistent workflow execution for sagas with delay/await steps. `Engine` manages background ticker (5s) for processing delayed and expired instances. `SQLStore` with SQLite/Postgres/MySQL dialect support (UPSERT, indexes, nullable timestamps). Workflow states: running, paused, completed, failed, timeout, cancelled
- **Delay steps** in sagas: `delay = "5m"` pauses workflow execution, persists `resume_at` timestamp, background ticker automatically resumes when delay expires
- **Await/Signal steps** in sagas: `await = "payment_confirmed"` pauses workflow until external signal. Signal API resumes execution with optional data payload. Step-level timeout for await steps
- **Workflow REST API**: `GET /workflows/{id}` (status), `POST /workflows/{id}/signal/{event}` (resume), `POST /workflows/{id}/cancel` (cancel). Auto-registered when workflow engine is active
- **Workflow service config**: `workflow {}` block in `service {}` — `storage` (connector name), `table` (custom table name), `auto_create` (auto-create schema). Parser support with `WorkflowConfig` type
- **Saga timeout**: `timeout = "24h"` on saga config enforces maximum workflow duration. Background ticker marks expired instances and runs compensation
- **DBAccessor interface** (`internal/connector/connector.go`): Database connectors expose `DB() *sql.DB` for workflow engine to reuse existing connections. Implemented on PostgreSQL and MySQL connectors
- **NeedsPersistence helper**: Detects if a saga has delay/await steps requiring async execution. Simple sagas (no delay/await) continue synchronous execution unchanged — full backward compatibility

### Changed
- **Saga parser** (`internal/parser/saga.go`): Added `delay` and `await` attributes to step schema. Delay/await steps don't require an action block
- **Saga executor** (`internal/saga/executor.go`): Added `ExecuteStep` and `ExecuteAction` exported methods for workflow engine access
- **FlowHandler** (`internal/runtime/flow_registry.go`): Async sagas dispatched via workflow engine return HTTP 202 with `workflow_id`. Sync sagas unchanged
- **Runtime** (`internal/runtime/runtime.go`): Workflow engine initialization, endpoint registration, graceful shutdown integration

## [1.9.0] - 2026-03-09

### Added
- **Plugin git sources** (`internal/plugin/git.go`): Plugins can now be sourced from GitHub, GitLab, Bitbucket, or any git-cloneable URL. SSH first with automatic HTTPS fallback. Version resolution via `git ls-remote --tags`
- **Semver constraint engine** (`internal/plugin/semver.go`): Full semver parsing and constraint matching — supports `^1.0` (caret), `~1.5` (tilde), `~> 2.0` (HashiCorp), `>= 1.0, < 3.0` (range), exact versions, and `latest`
- **Plugin cache** (`internal/plugin/cache.go`): Local cache in `mycel_plugins/` directory (like `node_modules`). Plugins downloaded once, reused across restarts. `copy = true` option for local plugins (useful for Docker)
- **Plugin lock file** (`internal/plugin/lockfile.go`): `plugins.lock` JSON file for reproducible builds. Atomic writes via temp+rename. Records source, version, resolved URL, and timestamp
- **Plugin CLI** (`cmd/mycel/plugin.go`): `mycel plugin install`, `mycel plugin list`, `mycel plugin remove`, `mycel plugin update`. Auto-install on `mycel start` when plugins are declared
- **Plugin validators and sanitizers** (`internal/plugin/types.go`, `internal/plugin/loader.go`): Plugins can now provide validators and sanitizers in addition to connectors and functions. Registered automatically in the runtime
- **Validator wiring** (`internal/runtime/flow_registry.go`, `internal/parser/types.go`): Custom validators (regex/CEL/WASM) can now be referenced from type field definitions via `validator = "name"` attribute. The `ValidatorRef` on type fields is resolved at validation time against the `validator.Registry`, connecting config/plugin validators to the type validation system
- **Plugin manifest detection** (`internal/parser/parser.go`): Main parser now auto-detects plugin manifest files (`plugin {}` without label + `provides {}` block) and skips them during recursive scanning. Only `mycel_plugins/` cache directory is excluded by name — user plugin directories can be placed anywhere in the config tree
- **Plugin integration test** (`tests/integration/`): End-to-end test with local WASM plugin providing an `always_valid` validator. Type `plugin_validated` references it via `validator = "always_valid"`. Flow validates input through the plugin's WASM binary before writing to SQLite. 4 assertions (startup, log verification, validated POST)
- **Request logging** (`internal/runtime/flow_registry.go`): Every flow execution is now logged with flow name, source connector, operation, and duration. Errors are logged at WARN level with the error message. Centralized in `FlowHandler.HandleRequest` — works for all connectors (REST, GraphQL, gRPC, SOAP, TCP, MQ, WebSocket, CDC, SSE, file watcher)
- **Pretty logs with tint** (`internal/logging/logging.go`): Text format now uses `lmittmann/tint` for colored, human-readable output similar to pino-pretty. Short timestamp (`4:49PM`), colored level (`INF`/`WRN`/`ERR`), dimmed attributes. JSON format unchanged for production

### Fixed
- **Integration test runner empty arrays** (`tests/integration/run.sh`): Fixed `unbound variable` errors when running a subset of tests by using `${ARRAY[@]+"${ARRAY[@]}"}` syntax for potentially empty arrays under `set -u`

## [1.8.0] - 2026-03-09

### Added
- **Security system — secure by default** (`internal/sanitize/`, `internal/security/`): Core input sanitization pipeline that runs before every flow execution. Cannot be disabled. Protects against null bytes, invalid UTF-8, control character injection, Unicode bidi attacks, oversized inputs, and deep nesting. Configurable thresholds via `security {}` HCL block (adjust limits, not disable)
- **WASM sanitizers**: Custom sanitization rules via WebAssembly modules. Define `sanitizer` blocks in the `security {}` config with field targeting and flow pattern matching. Same WASM interface as validators/functions
- **Connector-specific security rules** (`internal/sanitize/rules/`): XML entity blocking (XXE), file path containment, shell metacharacter detection, SQL identifier validation. Applied automatically based on connector type
- **Security HCL block** (`internal/parser/security.go`): New top-level `security {}` block for threshold overrides, WASM sanitizers, and per-flow security config. Parser, types, and runtime integration
- **Security documentation** (`docs/SECURITY.md`): Complete reference covering core pipeline, connector protections, HCL configuration, WASM sanitizer interface, and vulnerability mitigations
- **Security integration tests** (`tests/integration/scripts/test-security.sh`): 29 end-to-end assertions sending malicious payloads to real endpoints (REST, GraphQL, SOAP, File). Tests null byte injection, control character injection, bidi override attacks, SQL injection safety, oversized payloads, deep nesting (JSON bomb), XXE entity expansion, and path traversal — all against live services

### Fixed
- **XXE vulnerability** (`internal/codec/xml.go`, `internal/connector/soap/envelope.go`): Blocked XML entity expansion in both the XML codec and SOAP envelope parser by setting `decoder.Entity = map[string]string{}`
- **SSH command injection** (`internal/connector/exec/connector.go`): User-provided arguments to SSH remote commands and shell-wrapped local commands are now individually quoted with `shellQuote()` to prevent shell metacharacter injection
- **File path traversal** (`internal/connector/file/connector.go`): `resolvePath()` now strips absolute paths, normalizes `../` sequences, and validates that resolved paths stay within `BasePath`

### Changed
- **WASM documentation** (`docs/WASM.md`): Complete reference for building WASM modules in 6 languages — Rust, Go (TinyGo), C, C++, AssemblyScript, and Zig. Covers the WASM interface specification (alloc/free/validate/function exports), memory flow, HCL configuration for validators/functions/plugins, module size comparison, and best practices. Fixed broken link from `examples/plugin/README.md` and added cross-references between all WASM-related examples

## [1.7.0] - 2026-03-06

### Added
- **Integration Test Suite** (`tests/integration/`): Complete end-to-end testing infrastructure with Docker Compose. 10 infrastructure services (PostgreSQL, MySQL, MongoDB, Redis, RabbitMQ, Kafka, Elasticsearch, MinIO, Mock HTTP server, Cosmo Router), 25 test suites, 86 assertions. Tests every connector type and protocol (REST, GraphQL, gRPC, SOAP, AMQP, Kafka, S3, HTTP client, notifications). Includes mock server for capturing outbound API calls, shared bash test library, master runner script, and CI workflow (manual trigger). Kafka init container ensures topic exists before Mycel starts
- **Parallel test execution** (`tests/integration/run.sh`): Tests run in 3 phases: preflight (health/metrics), parallel (22 suites concurrently), solo (rate-limit). `--sequential` flag available for one-by-one execution. Mock-dependent tests (http-client, notifications) grouped to avoid `mock_clear` conflicts. `run-group.sh` helper for CI grouped parallel execution
- **CI grouped steps** (`.github/workflows/integration.yml`): Each test group is a separate collapsible step in GitHub Actions UI (Health & Metrics, Databases, Protocols, Messaging, Storage & Cache, Integration, Rate Limit). Tests within each group run in parallel via `run-group.sh`
- **GraphQL typed DTOs**: `returns` attribute on flows generates typed GraphQL output types instead of generic JSON. Mutations auto-infer typed `<TypeName>Input` argument from the return type. GraphQL introspection tests verify `User` and `UserInput` types exist with correct fields
- **`required = false` type field attribute** (`internal/parser/types.go`): Fields in HCL type definitions can now be marked as optional with `required = false` in their constraint block (e.g., `id = number({ min = 0, required = false })`). Fields remain required by default
- **Federation SDL type generation** (`internal/connector/graphql/schema.go`): `generateSDL()` now includes HCL-generated object types and input types in the federation SDL, enabling Cosmo Router composition

### Fixed
- **gRPC connector — RegisterRoute interface mismatch** (`internal/connector/grpc/server.go`): Changed handler signature from named `HandlerFunc` type to the concrete `func(ctx context.Context, input map[string]interface{}) (interface{}, error)` required by the `RouteRegistrar` interface. gRPC flows were silently never registered before this fix
- **gRPC connector — reflection registration** (`internal/connector/grpc/server.go`): Added `registerFileDescriptor()` that registers jhump `FileDescriptor` objects with `protoregistry.GlobalFiles` and sets `Metadata` on the `grpc.ServiceDesc`. Makes `grpcurl list` show registered services
- **gRPC connector — proto response adaptation** (`internal/connector/grpc/server.go`): Added `adaptResultForProto()` that auto-wraps arrays in repeated fields for list operations (e.g., `ListUsers`) and unwraps single-element arrays for scalar operations (e.g., `GetUser`)
- **gRPC input-as-filters in flow registry** (`internal/runtime/flow_registry.go`): `SourceType == "grpc"` is now included in the SOAP/TCP branch of `handleRead`, so gRPC input parameters are used as database query filters
- **gRPC read-back on create** (`internal/runtime/flow_registry.go`): The "read back created record" logic that was previously applied only for GraphQL sources now also applies for gRPC sources, returning the full created row after an INSERT
- **to.Operation method override** (`internal/runtime/flow_registry.go`): When `to.Operation` is explicitly set (e.g., `INSERT`), the HTTP method is now derived from the operation value. Fixes gRPC and other non-REST sources where `parseOperation()` would default to GET
- **PostgreSQL INSERT RETURNING** (`internal/connector/database/postgres/connector.go`): `RETURNING *` is now appended to all INSERT queries. `isSelectQuery()` updated to recognize the `RETURNING` keyword. Returns the full created row (including auto-generated `id`, `created_at`) instead of the empty `{id: 0, affected: 1}` result
- **GraphQL Federation variable values filtering** (`internal/connector/graphql/resolver.go`): In `MapArgsToInput()`, complex types (maps, slices) from `VariableValues` that are already resolved via `Args` are now skipped. Prevents Cosmo Router from re-injecting nested `input` objects that break SQL serialization
- **Event-driven source flow routing**: MQ consumers, CDC, and file watcher flows now correctly write to the destination instead of reading. Added `SourceType` field to FlowHandler and `isEventDrivenSource()` detection for `mq`, `cdc`, and `file` connector types
- **SOAP/TCP input-as-filters**: Non-REST read operations (SOAP GetItem, TCP message handlers) now pass all input parameters as query filters, enabling proper database lookups
- **Operation override in handleCreate/handleRead**: Both `handleCreate` and `handleRead` now respect the `to.operation` attribute, allowing connectors like Elasticsearch and HTTP client to use their native operations (e.g., `index`, `get`, `POST`) instead of hardcoded `INSERT`/`SELECT`
- **Auto-generated GraphQL input types are optional** (`internal/connector/graphql/hcl_to_graphql.go`): Input type fields generated from HCL types no longer apply NonNull, following GraphQL best practice where output types guarantee non-null but input types allow partial updates

### Changed
- **Kafka consumer logging** (`internal/connector/mq/kafka/consumer.go`): Removed verbose debug-level `Logger` from kafka-go Reader (was flooding logs with internal fetch/heartbeat/rebalance messages). Changed `ErrorLogger` from `Error` to `Warn` level since most kafka-go internal errors are transient and expected
- **Kafka integration test reliability** (`tests/integration/scripts/test-kafka.sh`): Retry timeout increased from 12x2s to 20x3s for more reliable consumer group initialization
- **PostgreSQL integration test assertion** (`tests/integration/scripts/test-postgres.sh`): POST response assertion updated from `affected` field to actual row data (`Alice`) to reflect the new `RETURNING *` behavior
- **Integration test rate limit config** (`tests/integration/config/config.hcl`): Increased from 10 req/s burst=20 to 50 req/s burst=200 to support parallel test execution. Rate limit test updated to use concurrent requests

## [1.6.0] - 2026-03-05

### Added
- **SOAP Connector** (`internal/connector/soap/`): Bidirectional SOAP web service support. Client mode (call external SOAP services) and Server mode (expose SOAP endpoints) auto-detected from config. SOAP 1.1 and 1.2 supported. Envelope build/parse, SOAP fault handling, WSDL auto-generation at `/wsdl`, basic/bearer auth. 22 tests
- **Codec System** (`internal/codec/`): Multi-format encoding/decoding with `Codec` interface and global registry. JSON and XML codecs included. XML↔map conversion with attributes (`@attr`), text content (`#text`), repeated elements (slices). `DetectFromContentType()` for auto-format detection. 18 tests
- **Format Declarations**: `format` attribute on connectors, flows (`from`/`to`), and steps. Connectors set a default format for all operations. Flow-level format overrides connector default. Context-based format propagation. REST server auto-detects incoming format from Content-Type header. HTTP client auto-detects response format
- **Format Documentation** (`docs/FORMAT.md`): Complete reference for the format system, XML mapping rules, auto-detection behavior, and extensibility
- **File Watch Mode**: Polling-based directory watcher for the file connector. When `watch = true`, the connector scans `base_path` for new and modified files and triggers flow handlers automatically. Glob pattern matching in `from.operation` (e.g., `*.csv`, `reports/*.xlsx`). Handler input includes file metadata (`_path`, `_name`, `_size`, `_mod_time`, `_event`) merged with parsed file content. Works on all filesystems including NFS, Docker volumes, and network mounts. 7 tests

## [1.5.1] - 2026-03-05

### Added
- **Configurable API URLs for notification connectors**: All notification connectors (Slack, Discord, Twilio SMS, FCM, APNs) now support an `api_url` property to override the default API base URL. Useful for proxies, enterprise endpoints, or testing

## [1.5.0] - 2026-03-05

### Added
- **MQ Filter Rejection Policy (`on_reject`)**: Configurable behavior for messages that don't match a `from.filter` in MQ flows. Three policies: `ack` (default, discard), `reject` (send to DLQ), `requeue` (return to queue with dedup tracking). Supports both string and block filter syntax
  - `FilterConfig` struct with `condition`, `on_reject`, `id_field`, `max_requeue`
  - `RequeueTracker` for in-memory dedup with TTL cleanup (prevents infinite requeue loops)
  - RabbitMQ: `Nack(false, false)` for reject, `Nack(false, true)` for requeue
  - Kafka: republish to `<topic>.dlq` for reject, republish to same topic for requeue
  - Lazy writer initialization for consumer-only Kafka connectors
  - Full backwards compatibility with string filter syntax

## [1.4.3] - 2026-03-04

### Added
- **RabbitMQ URL Support**: The `url` field now works as documented — if set, it takes precedence over `host`/`port`/`username`/`password`/`vhost`. Previously, the factory never read the `url` property
- **RabbitMQ Consumer Shorthands**: `consumer.queue` creates a queue declaration if no explicit `queue {}` block is set. `consumer.workers` is an alias for `concurrency`. `consumer.retry_count` creates a DLQ config with `max_retries`
- **RabbitMQ DLQ Documentation**: Full Dead Letter Queue configuration options now documented (exchange, queue, routing_key, max_retries, retry_delay, retry_header)

### Fixed
- **MQ Documentation Consistency**: Rewrote `docs/connectors/message-queues.md` to match implementation. Fixed Kafka `offset` → `auto_offset_reset` (correct field name), fixed default from `latest` to `earliest`, added all missing Kafka fields (auto_commit, min_bytes, max_bytes, max_wait_time, concurrency, retries, batch_size, linger_ms, client_id), added Kafka Schema Registry documentation, added RabbitMQ TLS/Queue/Exchange/DLQ sections, added Required column to all option tables

## [1.4.0] - 2026-03-04

### Fixed
- **Version Display**: Runtime version was hardcoded to `0.1.0` — now propagated from the CLI binary. Banner, health endpoints, and metrics all report the correct Mycel version
- **Runtime Metrics**: `mycel_uptime_seconds` and `mycel_goroutines` Prometheus metrics are now updated every 15 seconds via a background goroutine (previously the methods existed but were never called)

### Added
- **Service Version in Health**: All health endpoints (`/health`, `/health/live`, `/health/ready`) now include `service_version` from `config.hcl` in their JSON response
- **Mycel Version in Metrics**: `mycel_service_info` metric now includes a `mycel_version` label alongside `service` and `version`, making it easy to identify which Mycel release is running

## [1.3.0] - 2026-03-04

### Added
- **.env File Support**: Mycel now automatically loads a `.env` file on startup (`start`, `validate`, `check` commands). Looks for `<config-dir>/.env` first, falls back to `./.env`. Existing environment variables are never overridden. Silent when no `.env` file is found (normal for production/Docker)
- **Deployment Guide** (`docs/DEPLOYMENT.md`): New documentation covering Docker, Docker Compose, Kubernetes deployment, environment variable reference, and `.env` file usage

## [1.2.0] - 2026-03-04

### Added
- **Standalone Admin Server**: Health checks (`/health`, `/health/live`, `/health/ready`) and metrics (`/metrics`) are now always available, even without a REST connector. When no REST connector is configured, Mycel automatically starts a lightweight admin server on port 9090 (configurable via `admin_port` in the `service` block). This ensures Kubernetes probes and monitoring work for queue workers, CDC pipelines, and any service without HTTP endpoints.

## [1.1.0] - 2026-03-04

### Added
- **Error Handling Guide** (`docs/ERROR_HANDLING.md`): Comprehensive guide covering all error handling layers — retry, fallback/DLQ, circuit breaker, rate limiting, on-error aspects, connector profiles, health checks
- **On-Error Aspects** (`when = "on_error"`): New aspect timing that executes only when a flow fails. Provides `error.message` in transform expressions for logging errors, sending alerts, or notifying external systems
- **Custom Error Responses** (`error_response` block): Define custom HTTP status codes, headers, and response bodies for flow errors using CEL expressions

## [1.0.0] - 2026-03-03

### Added - Excel Support
- **Native Excel (.xlsx) read/write** in the file connector via `excelize/v2`
  - Auto-detect format from `.xlsx`/`.xls` extensions
  - First row treated as column headers (same convention as CSV)
  - Sheet selection via `params = { sheet = "SheetName" }` (defaults to first sheet)
  - Empty rows automatically skipped on read
  - Sorted column headers for deterministic write output

### Fixed
- **SSE data race**: Synchronized initial header flush with `client.mu` to prevent race with concurrent `sendEvent` writes
- **README features table**: Separated GraphQL Subscriptions into its own row (was incorrectly listed inside the Federation row)
- **CONCEPTS.md**: Updated subscriptions cross-reference to link to connector docs and example

### Changed - Documentation
- **README Quick Start**: Added context for creating a project directory and clearer step descriptions
- **README Installation**: Added Docker Hub as alternative registry (`mdenda/mycel`)
- **Filesystem connector docs**: Complete rewrite with all 8 operations, format output examples, and real-world usage patterns

### Changed - Helm Chart v0.2.0: Directory-Based Configuration
- **`config/` directory**: Chart auto-discovers all `.hcl` files under `helm/mycel/config/` using `.Files.Glob` — copy your project files in and deploy, no flags needed
- **`existingConfigMap`**: New `mycel.config.existingConfigMap` value to reference a pre-existing ConfigMap instead of creating one
- **Inline fallback**: Inline values in `values.yaml` (service, connectors, flows, types) still work when `config/` is empty
- **ConfigMap guard**: Chart skips ConfigMap creation when `existingConfigMap` is set
- **Deployment template**: Volume uses `existingConfigMap` when provided, falls back to generated ConfigMap
- **Chart version**: Bumped to 0.2.0

### Added - Phase 12.1: Saga Pattern
- **Saga pattern** for declarative distributed transactions with automatic compensation
  - New top-level `saga` HCL block with `step`, `action`, `compensate`, `on_complete`, `on_failure`
  - Saga executor: runs steps in order, compensates in reverse on failure
  - Step results available as `step.<name>.*` in subsequent actions and compensations
  - CEL expression resolution in `data`, `body`, `where`, `set` fields
  - `on_error = "skip"` for non-critical steps
  - Saga handler registers sagas as flow handlers in the runtime
- **New package**: `internal/saga/` (config types + executor)
- **New parser**: `internal/parser/saga.go` — `parseSagaBlock`, `parseSagaStepBlock`, `parseSagaActionBlock`
- **Tests**: 10 saga executor tests (all steps succeed, compensation, compensation failure, skip on error, multiple reverse compensations, etc.)
- **Parser tests**: `TestParseSaga` — validates full HCL parsing
- **New example**: `examples/saga/` — order creation with 3-step saga

### Added - Phase 12.2: State Machine
- **State machine** for entity lifecycle management with declarative states and transitions
  - New top-level `state_machine` HCL block with `state`, `on` (transitions), `guard`, `action`, `final`
  - State machine engine: validates transitions, evaluates CEL guards, executes actions, persists state
  - State stored in entity's `status` column — no separate state table needed
  - Guards (CEL expressions) prevent invalid transitions
  - Transition actions execute connector operations during state changes
  - Final states block further transitions
- **New `state_transition` block in flows** — triggers state machine transitions from REST/queue/etc.
  - CEL expressions for `id`, `event`, `data` fields
- **New package**: `internal/statemachine/` (config types + engine)
- **New parser**: `internal/parser/statemachine.go` — `parseStateMachineBlock`, `parseStateBlock`, `parseTransitionBlock`
- **Tests**: 10 state machine engine tests (valid transitions, initial state, invalid events, final states, guards, actions, multi-step, action failure)
- **Parser tests**: `TestParseStateMachine`, `TestParseFlowWithStateTransition`
- **New example**: `examples/state-machine/` — order status lifecycle
- **Docs**: CONCEPTS.md updated with Sagas and State Machines sections

### Added - Connector Documentation Catalog
- **New `docs/connectors/` directory** with individual documentation for every connector type
  - Catalog README with categorized tables linking to all 16 connector docs
  - 16 connector docs: REST, Database, GraphQL, gRPC, Message Queues, TCP, WebSocket, SSE, CDC, Elasticsearch, Cache, Filesystem, S3, Exec, Notifications, OAuth, Profile
  - Each doc follows a consistent template: description, configuration, operations table, example flow
- **Refactored CONCEPTS.md**: Removed 5 inline connector sections (WebSocket, CDC, SSE, Elasticsearch, OAuth) — now linked from the catalog
- **Updated README.md**: Connector concept links point to `docs/connectors/`, added Connector Catalog to Documentation section

### Added - Phase 11.1: Elasticsearch Connector
- **Elasticsearch connector** for full-text search and analytics via REST API
  - Read operations: `search` (query DSL), `get` (by ID), `count`, `aggregate`
  - Write operations: `index` (create/replace), `update` (partial), `delete`, `bulk`
  - Multi-node cluster support with round-robin load balancing
  - Basic auth support (`username`/`password`)
  - Filter→bool/must term conversion, pagination (`size`/`from`), sorting (`sort`)
  - Field selection via `_source` includes
  - Implements `Connector`, `Reader`, `Writer` interfaces
- **New connector type**: `elasticsearch` with `nodes`, `username`, `password`, `index`, `timeout` configuration
- **Tests**: 25 tests covering factory, search, get, count, aggregate, index, update, delete, bulk, auth, round-robin, health, errors
- **New example**: `examples/elasticsearch/` — search, CRUD, count

### Added - Phase 11.2: External OAuth Connector
- **OAuth connector** for declarative social login flows
  - Operations: `authorize` (generate state + auth URL), `callback` (exchange code for user info), `userinfo` (fetch profile), `refresh` (refresh token)
  - Built-in CSRF protection with state management and 10-minute expiry
  - Drivers: `google`, `github`, `apple`, `oidc` (with discovery), `custom` (manual URLs)
  - Reuses existing `internal/auth` OAuth2Service and provider implementations
  - Implements `Connector`, `Reader` interfaces
- **New connector type**: `oauth` with `driver`, `client_id`, `client_secret`, `redirect_uri`, `scopes` configuration
- **Tests**: 21 tests covering factory, authorize, callback, state validation, expired state, exchange errors, userinfo, refresh, custom driver
- **New example**: `examples/oauth/` — Google and GitHub social login

### Added - Phase 11.3: Batch Processing
- **Batch processing** for chunked data operations (migrations, ETL, reindexing)
  - New `batch` block in flows: `source`, `query`, `params`, `chunk_size`, `on_error`, `transform`, `to`
  - Reads from source connector in pages using LIMIT/OFFSET pagination
  - Optional per-item transform with CEL expressions
  - Error handling: `"stop"` (halt on first error) or `"continue"` (skip and report)
  - Returns `BatchResult` with `processed`, `failed`, `chunks`, `errors` stats
- **Parser**: `parseBatchBlock()` for HCL batch block parsing
- **Runtime**: `executeBatch()` method on FlowHandler
- **Tests**: 12 tests covering basic batch, chunking, transforms, on_error modes, empty source, params, stats, error cases
- **New example**: `examples/batch/` — user migration, product reindexing, order export

### Added - Phase 10.1: WebSocket Connector
- **Standalone WebSocket connector** for bidirectional real-time communication
  - Source operations: `message`, `connect`, `disconnect` — receive client events as flow triggers
  - Target operations: `broadcast` (all clients), `send_to_room` (room members), `send_to_user` (specific user)
  - Room management via JSON protocol (`join_room`, `leave_room`)
  - Configurable keepalive: `ping_interval` and `pong_timeout`
  - Thread-safe client tracking and room membership
  - Implements `Connector`, `Writer`, `Starter`, and `RouteRegistrar` interfaces
- **New connector type**: `websocket` with `port`, `host`, `path`, `ping_interval`, `pong_timeout` configuration
- **Tests**: 12 tests covering connect, message handling, broadcast, rooms, disconnect cleanup, factory, error cases
- **New example**: `examples/websocket/` — chat, broadcast, room notifications

### Added - Phase 10.2: CDC (Change Data Capture)
- **PostgreSQL CDC connector** for real-time database change streaming via logical replication
  - Source operations: `INSERT:table`, `UPDATE:table`, `DELETE:table` with wildcard support (`*`)
  - Target operations: none (source-only connector)
  - Uses `pgoutput` plugin (built into PostgreSQL 10+) via `jackc/pglogrepl`
  - Automatic publication and replication slot creation
  - Column type decoding: int, float, bool, timestamp, text from pgoutput format
  - Implements `Connector`, `Starter`, and `RouteRegistrar` interfaces
- **New connector type**: `cdc` with `driver`, `host`, `port`, `database`, `user`, `password`, `slot_name`, `publication` configuration
- **Tests**: 15 tests covering factory, dispatch, wildcards, event format, health, operation parsing
- **New example**: `examples/cdc/` — user creation, order status changes, session cleanup, product monitoring
- **New dependencies**: `jackc/pglogrepl`, `jackc/pgx/v5` (pure Go, no CGO)

### Added - Phase 10.3: SSE (Server-Sent Events)
- **SSE connector** for unidirectional server-to-client push over standard HTTP
  - Source operations: none (target-only connector)
  - Target operations: `broadcast` (all clients), `send_to_room` (room members), `send_to_user` (specific user)
  - Room and user targeting via query params (`?room=`, `?rooms=`, `?user_id=`)
  - Configurable heartbeat: `heartbeat_interval` sends periodic keepalive comments
  - CORS support: `cors { allowed_origins = [...] }` for cross-origin clients
  - Implements `Connector`, `Writer`, `Starter`, and `RouteRegistrar` interfaces
- **New connector type**: `sse` with `port`, `host`, `path`, `heartbeat_interval`, `cors` configuration
- **Tests**: 18 tests covering factory, connect, broadcast, rooms, disconnect cleanup, heartbeat, event format, health, CORS, user targeting
- **New example**: `examples/sse/` — live feed, room updates, per-user notifications

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
  - Pattern-based matching with flow name glob patterns (`create_*`, `update_*`, `*`)
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
