# CLAUDE.local.md

## Workflow Preferences

- Me puedes hablar en español, pero todos los cambios de código y comentarios que hagas deben estar siempre en inglés
- Siempre que hagamos cambios en el código o agreguemos funcionalidades, debemos mantener el README y los archivos relacionados actualizados
- El README debe ser útil tanto para las personas, como para vos así sabés de qué se trata y qué estado tiene todo lo que estamos creando
- Cada vez que te pida hacer un commit, evita cualquier referencia a Claude o Claude Code. Incluso omití referencia a co-creators y demás. Hacelo siempre en inglés
- Mantené una sección en este archivo (editable solo por vos), como una bitácora persistente, anotando todo el progreso que vayamos haciendo así en el caso de que la sesión se pierda, se caiga la conexión, se apague la computadora o cualquier otra cosa, sepas exactamente en qué punto estábamos.
- Creá y mantené un archivo CHANGELOG.md con todas las modificaciones vayamos haciendo, versión a versión.

---

## ¿Qué es Mycel? (Visión del Proyecto)

**Mycel es un framework declarativo para crear microservicios mediante configuración HCL, sin escribir código.**

En vez de programar cada microservicio en NestJS/Go/Python, simplemente:
1. Creás archivos HCL de configuración
2. Deployás Mycel con esa configuración
3. Tenés un microservicio funcionando

**Es como nginx o Apache:** mismo binario, distinta configuración = distinto servicio.

**Conecta cualquier cosa con cualquier cosa:** REST↔DB, Queue↔DB, REST↔Queue, TCP↔REST, GraphQL↔REST, etc.

**Es como MuleSoft pero:** Libre, open source, a nivel de microservicios (no centralizado).

---

## Especificación Completa del Proyecto

### Estructura de Directorios

```
mycel-service/
├── connectors/     # DBs, APIs, queues (bidireccionales)
├── flows/          # Flujos de datos (from → to)
├── transforms/     # Transformaciones CEL reutilizables
├── types/          # Schemas de datos
├── validators/     # Validadores custom (regex, CEL, WASM)
├── aspects/        # Cross-cutting concerns (AOP)
├── auth/           # Configuración de autenticación
├── mocks/          # Mocks para testing
├── environments/   # Variables por ambiente
├── plugins/        # Plugins custom (WASM)
└── config.mycel      # Configuración global
```

### Connectors (Bidireccionales)

| Tipo | Read | Write/Expose |
|------|------|--------------|
| database | SELECT/Query | INSERT/UPDATE/DELETE |
| rest | GET de otra API | Exponer endpoints |
| graphql | Query | Exponer schema |
| queue | Consumir msgs | Publicar msgs |
| grpc | Llamar servicio | Exponer servicio |
| file/s3 | Leer archivo | Escribir archivo |

### Principales Bloques HCL

- **flow**: `from { connector.x = "..." } to { connector.y = "..." }`
- **transform**: CEL expressions, composición con `use = [...]`
- **type**: Validación con `string { format = "email" }`
- **aspect**: AOP con `when`, `on` (pattern matching), `action`
- **auth**: Sistema enterprise-grade con presets
- **saga**: Transacciones distribuidas con compensación automática
- **state_machine**: Estados y transiciones con guards y actions

### Features Implementadas

- Observabilidad: OpenTelemetry, Prometheus `/metrics`, Health checks
- Error Handling: Retry exponential backoff, DLQ
- Hot Reload: Cambios en HCL sin reiniciar
- Mocks: Por recurso, habilitables con `--mock=x` / `--no-mock=y`
- Extensibilidad: WASM (validators, functions, plugins)

---

## Roadmap Completo

| Fase | Estado | Contenido |
|------|--------|-----------|
| 1 | ✅ | REST Server, SQLite, Runtime, CLI |
| 2 | ✅ | REST Client, PostgreSQL, CEL Transforms, Types, Env |
| 2.5 | ✅ | TCP Server/Client (json/msgpack/nestjs) |
| 3 | ✅ | RabbitMQ, Kafka, Exec, GraphQL (Federation v2) |
| 3.2 | ✅ | MySQL, MongoDB |
| 3.3 | ✅ | Cache (Memory, Redis) |
| 4 | ✅ | gRPC, Files, S3, Health, Metrics, Rate limit, Circuit breaker, Hot reload, Docker |
| 4.1 | ✅ | Runtime env vars (MYCEL_ENV, MYCEL_LOG_*) |
| 4.2 | ✅ | Sincronización (Locks, Semaphores, Coordinate) |
| 4.3 | ✅ | Connector Profiles (múltiples backends) |
| 5 | ✅ | Auth, Mocks, OpenAPI/AsyncAPI, Validators, WASM, Functions, Plugins, Aspects |
| 6 | ✅ | Notifications (Slack, Discord, Email, SMS, Push, Webhook) |
| 7 | ✅ | Flow Orchestration - step blocks, filter, conditional, array transforms, on_error, merge, multi-to, dedupe |
| 8 | ✅ | GraphQL Query Optimization - Field selection, query rewriting, step skipping, DataLoader |
| 9 | ✅ | GraphQL Federation Complete - Subscriptions, publish, per-user filtering, entity resolution |
| 10 | ✅ | Real-time & Event-Driven - WebSockets, CDC, SSE |
| 11 | ✅ | Specialized Connectors - Elasticsearch, External OAuth, Batch Processing |
| 12 | ✅ | Enterprise Workflows - Sagas, State Machines (Long-running Processes deferred) |

### Pendientes (Baja Prioridad)

- ~~Phase 11: PDF Generation~~ ✅ (v1.13.0)
- Phase 12.3: Workflow Visualization, Workflow Versioning

### Posibles Connectors Futuros (via WASM plugins)

Estos connectors son para casos de uso específicos. No son esenciales para core — se resolverían mejor como WASM plugins:

| Connector | Caso de uso | Prioridad |
|-----------|------------|-----------|
| AWS SQS | Cola de mensajes en AWS | Baja (Kafka/RabbitMQ cubren el patrón) |
| AWS DynamoDB | NoSQL en AWS | Baja (MongoDB cubre el patrón) |
| NATS / NATS JetStream | Messaging moderno | Media (creciendo rápido) |
| LDAP / Active Directory | Directorio empresarial | Baja (nicho enterprise) |
| Cassandra | Wide-column DB | Baja (nicho de escala) |
| Neo4j | Graph DB | Baja (nicho) |
| InfluxDB / TimescaleDB | Time-series DB | Baja (nicho IoT/monitoring) |
| Azure Service Bus | Cola de mensajes en Azure | Baja (similar a SQS) |
| Google Pub/Sub | Cola de mensajes en GCP | Baja (similar a Kafka) |
| AWS Lambda / Cloud Functions | Invocación serverless | Baja (REST client cubre la mayoría) |

### ✅ Benchmark / Load Test (Completado)

Resultados completos en `benchmark/RESULTS.md`. Resumen: 3.2M requests en 12 min (parallel), Standard 8,437 RPS, Realistic 204 RPS (2ms median), Stress UNBREAKABLE (0.01% errors). Arquitectura: 5 VPS (3 targets $5 + 1 DB $5 + 1 attacker 4vCPU $24). Calibración adaptiva auto-descubre límites del hardware. Herramienta: k6. Scripts: `benchmark/loadtest.js`, `benchmark/realistictest.js`, `benchmark/stresstest.js`, `benchmark/calibrate.js`, `benchmark/run.sh`.

### Pendiente: Estrategia de Lanzamiento (interno)

Plan de comunicación para cuando esté listo para launch público:

**Canales principales:**
1. **Show HN** (Hacker News) — Mayor upside. Formato: "Show HN: Mycel — Create microservices without writing code". Audiencia: engineers curiosos, early adopters. No requiere karma.
2. **dev.to** — Artículo técnico detallado con los 3 archivos de ejemplo, demo real, números del benchmark
3. **Hashnode** — Similar a dev.to, mejor SEO a veces
4. **Golang Weekly** newsletter — Si alguien lo menciona, llega a miles de Go devs

**Canales secundarios:**
- Reddit: r/golang, r/microservices (construir karma antes participando)
- Conferencias NZ/AU: KiwiPyCon, Codemania Auckland, Linux.conf.au
- Blog oficial de Go (a veces destaca proyectos de la comunidad)

**Tagline**: *"Create microservices without writing code. You describe what you want, Mycel handles the how."*

**La demo ideal**: Mostrar los 3 archivos HCL, levantar el container, hacer el request, ver el resultado. Sin editar, sin cortes, en tiempo real. 2 minutos y el punto está hecho.

### Pendiente: Mycel LSP + IDE Extensions

Un paquete de tooling para editores que se haría junto, probablemente en un mismo repo (`mycel-ide` o similar):

**1. Language Server (LSP)** — Go, reutiliza el parser existente:
- **Autocompletado**: Tipos de connector, drivers, campos por tipo, references a connectors/types/transforms
- **Validación en tiempo real**: Campos requeridos, tipos válidos, drivers existentes, references rotas
- **Hover docs**: Documentación inline al pasar el mouse sobre bloques y atributos
- **Go-to-definition**: Click en `connector.postgres` → ir al archivo donde está definido
- **Diagnósticos semánticos**: "Flow references connector 'pg' which doesn't exist"

**2. VS Code Extension** — TypeScript, marketplace:
- Registra el LSP para archivos `.mycel` de Mycel
- Registra el DAP adapter → habilita **gutter breakpoints** en archivos HCL
- Mapea líneas del HCL (bloques `from {}`, `transform {}`, `to {}`, etc.) a pipeline stages del DAP
- Syntax highlighting mejorado (sobre el plugin genérico de HashiCorp)

**3. IntelliJ/WebStorm Plugin** — Kotlin, JetBrains Marketplace:
- Integra el LSP vía LSP API de IntelliJ (2023.2+)
- Registra debug adapter para HCL → habilita **gutter breakpoints**
- Misma lógica de mapeo HCL línea → pipeline stage

> **Nota:** El LSP sirve para todos los editores. Las extensiones de VS Code e IntelliJ son wrappers que registran el LSP + DAP adapter. Neovim usa nvim-lspconfig + nvim-dap directamente sin extensión dedicada. El plugin de HCL de HashiCorp cubre syntax highlighting básico mientras tanto.

> **Detalle de cada fase:** Ver `docs/architecture.md`, `examples/*/README.md`, y specs en `docs/archive/PHASE-*.md`

---

## Bitácora de Desarrollo (Claude)

> **Nota:** Ver CHANGELOG.md para detalles completos. Esta bitácora resume el estado actual.

### Historial Comprimido (Fases 1-6)

| Fecha | Fase | Resumen |
|-------|------|---------|
| 2025-12-29 | 1-2.5 ✅ | Runtime, REST, SQLite, PostgreSQL, TCP, Transforms (`internal/runtime/`, `internal/connector/`) |
| 2025-12-29 | 3.1 ✅ | RabbitMQ, Kafka (`internal/connector/mq/`) |
| 2025-12-30 | 3-3.3 ✅ | GraphQL, MySQL, MongoDB, Cache, Exec |
| 2025-12-31 | 4 ✅ | gRPC, Files, S3, Rate Limit, Circuit Breaker, Hot Reload, Docker |
| 2026-01-02 | 4.1-4.3 ✅ | Logging, env vars, Connector Profiles, Sync (Locks/Semaphores/Coordinate) |
| 2026-01-02 | 5 ✅ | Mocks, OpenAPI/AsyncAPI, Validators, WASM, Functions, Plugins |
| 2026-01-05 | 5.1 ✅ | Auth Core (JWT, Argon2id), Auth Security (storage, brute force), MFA (TOTP, WebAuthn) |
| 2026-01-06 | 6 ✅ | Notifications (Email, Slack, Discord, SMS, Push, Webhook) |

### Historial Comprimido (Fases 7-9)

| Fecha | Fase | Resumen |
|-------|------|---------|
| 2026-01-07 | Parser ✅ | GraphiQL IDE, +80 parser attrs, examples restored, Named Operations |
| 2026-01-07 | Analysis | MuleSoft Consumer (~70%), Mercury 11 NestJS (~40%) → identified gaps |
| 2026-01-08 | 7 ✅ | Step blocks, filter in from, conditional steps, array transforms (first/last/unique/pluck/sort_by/sum/avg), on_error with retry+DLQ, merge/omit/pick, multi-to, dedupe |
| 2026-01-16 | 8 ✅ | Field Analyzer, Result Pruner, CEL functions (has_field/field_requested), Database Optimizer, Step Optimizer, DataLoader. Bugfixes: circular refs (FieldsThunk), step param eval, CEL init |
| 2026-02-26 | Docs ✅ | CONCEPTS.md rewrite (1700→450 lines), README alignment, structured startup log, service block docs |
| 2026-02-26 | 9 ✅ | Subscription types, flow-triggered publish, per-user filtering, auto entity resolution |
| 2026-02-27 | 9+ ✅ | GraphQL subscription client (graphql-ws protocol), auto-enable Federation v2, CONCEPTS federation rewrite |

### Historial Comprimido (Fases 10-12 + Post)

| Fecha | Fase | Resumen |
|-------|------|---------|
| 2026-02-27 | 10 ✅ | WebSocket (rooms, broadcast), CDC (PG WAL pgoutput), SSE (rooms, heartbeat, CORS). 45 tests |
| 2026-02-27 | 11 ✅ | Elasticsearch (search/index/bulk), OAuth (google/github/apple/oidc), Batch Processing. 58 tests |
| 2026-02-27 | 12.1-12.2 ✅ | Sagas (forward+compensate), State Machines (guards+actions). 23 tests |
| 2026-03-04 | Infra ✅ | Error handling guide, on_error aspects, custom error_response, admin server (:9090), .env support, deployment guide, version unification |
| 2026-03-05 | v1.5.0 ✅ | MQ filter rejection (on_reject), notification api_url configurable, SOAP connector (client+server, WSDL), codec system (JSON/XML), format declarations, file watch mode, integration test suite (24 suites), runtime fixes (event-driven sources, input-as-filters) |
| 2026-03-06 | v1.6.0 ✅ | gRPC fixes (reflection, proto adaptation), PG INSERT RETURNING, GraphQL typed DTOs (`returns` attr), federation fixes, parallel tests (3-phase), CI grouped steps |
| 2026-03-09 | v1.7.0-1.9.0 ✅ | WASM docs (6 languages), security system (sanitize pipeline, XXE/injection fixes, WASM sanitizers), plugin git sources (semver, cache, lock file, CLI), validator wiring, request logging centralized, pretty logs (tint), CSV/TSV enhanced I/O, long-running workflows (delay/await/signal/cancel), plugin manifest detection |
| 2026-03-10 | v1.10.0-1.11.0 ✅ | Env-aware defaults, flow trace system (`mycel trace`), verbose flow logging, interactive breakpoints, DAP server (IDE integration), dev-only debug restriction, MQTT connector (QoS 0/1/2), FTP/SFTP connector, Redis Pub/Sub |
| 2026-03-11 | v1.12.0 ✅ | **Response block** (CEL transforms AFTER destination, `input.*`/`output.*`), **echo flows** (no `to` block), **status code overrides** (`http_status_code`/`grpc_status_code`), `TransformResponse` in CEL, `ExtractStatusCode` shared helper, nil-check fixes |
| 2026-03-11 | Benchmark ✅ | **Benchmark suite** (`benchmark/`): OpenTofu+Linode+k6, 3 test modes (standard/stress/full), auto deploy→test→destroy. **Results on $5 Nanode**: 5,768 RPS, p99 229ms, 0% HTTP errors. Stress test: 800 VUs, 100KB payloads, 12 CEL/req → survived (0.02% errors). See `benchmark/RESULTS.md` |
| 2026-03-12 | Benchmark ✅ | **Parallel benchmark architecture**: 5 VPS (3 targets + 1 DB + 1 attacker), adaptive calibration (`calibrate.js`), 3 tests simultaneous. Fixed: UDF variable injection, StackScript em-dash, PG DDL timing, k6 exit codes. **Final**: Standard 8,437 RPS, Realistic 204 RPS (2ms median), Stress 402 RPS (UNBREAKABLE). 3.2M requests, 0 crashes, $5/server |
| 2026-03-12 | v1.12.1 ✅ | **Aspects refactor**: target by flow name (not file path), `filepath.Match` glob. **Unique name validation**: per-type duplicate detection with file locations in errors. Removed `FlowPath` from FlowHandler |
| 2026-03-13 | v1.12.2 ✅ | **Structured error object** in on_error aspects (`error.code`/`.message`/`.type`), **common use cases guide** (10 examples) |
| 2026-03-13 | v1.12.3 ✅ | **Flow invocation from aspects** (`action { flow = "name" }`), FlowInvoker interface, internal flows (no `from`), parser validation (connector XOR flow), 3 new tests, docs updated |
| 2026-03-13 | v1.13.0 ✅ | **PDF connector** (HTML templates → PDF, pure Go, `go-pdf/fpdf`), **binary HTTP responses** in REST connector, **response enrichment** (after aspects with headers+fields), **idempotency keys** (cache-backed), **async execution** (HTTP 202 + polling), **database migrations** (`mycel migrate`), **file upload** (multipart/form-data), **HTML email templates**, **multi-tenancy** (request headers as `input.headers`), **distributed rate limiting** (Redis backend), 26 use case examples |
| 2026-03-15 | v1.14.0 ✅ | **Studio Debug Protocol** (`internal/debug/`): WebSocket JSON-RPC 2.0 on `:9090/debug`, session management, runtime inspection, event streaming, stage+rule breakpoints, per-CEL-rule stepping, watch/evaluate, TransformHook interface. 29 tests |
| 2026-03-16 | v1.14.1 ✅ | **Fan-out from source**: Multiple flows share same `from` connector+operation, concurrent execution. `ChainRequestResponse` (REST/gRPC/TCP first returns, rest fire-and-forget) + `ChainEventDriven` (MQ/CDC all parallel, wait for all). 14 connectors updated, 13 tests |
| 2026-03-16 | v1.14.2 ✅ | **Bugfixes**: Aspect metadata pollution (stripped `_flow`/`_operation`/`_target`/`_timestamp` before flow core), cache hit `[]interface{}` type mismatch, MongoDB ID preservation. Integration tests: cache key CEL quotes, SQLite locking retry, plugin health retry. PDF connector docs. 124/124 integration tests passing |
| 2026-03-16 | v1.14.3 ✅ | **Template in connector config**: PDF and email connectors now accept `template` at config level (black-box principle). Flow-level override still supported for dynamic cases. Email field renamed `template_file` → `template` for consistency. Docs updated |
| 2026-03-18 | v1.14.4 ✅ | **Automatic debug throttling**: When Studio debugger connects, all 7 event-driven connectors switch to single-message processing. `DebugThrottler` interface + `DebugGate` semaphore. RabbitMQ also sets AMQP prefetch=1. Zero overhead when no debugger. `OnClientChange` callback in debug server. **Start Suspended mode** (`--debug-suspend` / `MYCEL_DEBUG_SUSPEND=true`): event-driven connectors defer `Start()` until debugger connects. Source properties reference doc (13 connectors). 4 tests |
| 2026-03-18 | v1.15.0 ✅ | **Connector Owns Config refactor**: Parser no longer hardcodes connector-specific attributes. `SourceValidator`/`TargetValidator` interfaces — each connector validates its own params. `ConnectorParams` map + getter methods replace typed fields (`Operation`, `Target`, `Query`, `Filter`, `Format`, `Params`, `Body`, `QueryFilter`, `Update` removed from `FromConfig`/`ToConfig`/`StepConfig`/`EnrichConfig`). 14 connectors implement validation. New/plugin connectors need zero parser changes |
| 2026-03-19 | v1.15.1 ✅ | **Hot reload + debug suspend fix**: Event-driven connectors weren't started after hot reload when debugger already connected. **Notification connectors implement `connector.Writer`**: Slack, Discord, Email, SMS, Push, Webhook now usable from aspects. **Connector log improvements**: 7 connectors now log useful context on connection (queue names, topics, hosts, etc.) |
| 2026-03-19 | v1.15.2-1.15.4 ✅ | **Manual consume for event-driven debugging** (`debug.consume`): IDE-controlled one-at-a-time message pull. `DebugConsumer` interface. Breakpoint fixes (ShouldBreak, always-inject controller/hook). Protocol docs rewrite. Fixes: deadlock in handleConsume (v1.15.3), RabbitMQ Basic.Get→Basic.Consume for CloudAMQP (v1.15.4) |
| 2026-03-20 | v1.15.5-1.15.12 ✅ | **Studio-controlled debug gate**: Replaced manual consume with gate-based approach — `DebugGate` starts blocked, `debug.consume` → `AllowOne()` → one message passes via normal consumer loop. Removed `DebugConsumer` interface. All 7 event-driven connectors implement `AllowOne()`+`SourceInfo()` via expanded `DebugThrottler`. Fixes: re-apply SetDebugMode after Start() (v1.15.6), idempotent gate (v1.15.7), **reconnection** — `Session.ResumeAll()` on disconnect + cancel channel in DebugGate unblocks stuck workers (v1.15.12). Diagnostic logging (temporary, to remove) |
| 2026-03-21 | v1.16.0 ✅ | **Accept block**: New `accept` block in flows — business-level gate after `filter`, before `transform`. `when` (CEL) + `on_reject` (ack/reject/requeue). Enables multi-consumer patterns where flows requeue messages not for them. `StageAccept` trace stage, debug protocol support, `AcceptInfo` in `inspect.flow`. 5 tests (3 parser + 2 runtime) |
| 2026-03-21 | v1.17.0 ✅ | **IDE intelligence engine** (`pkg/ide/`): Importable Go package for Studio. Permissive HCL parser, project-wide index, context-aware completions, 3-layer diagnostics, go-to-definition, hover docs. Thread-safe, no `internal/` dependency. 14 tests |
| 2026-03-23 | SchemaProvider ✅ | **SchemaProvider refactor** (5 phases): `pkg/schema/` as single source of truth. 25+ connectors self-describe via `ConnectorSchemaProvider`. Registry wired into runtime+parser. `pkg/ide/` delegates to `pkg/schema/`. Schema-driven validation with defaults. Unknown attr detection. All breakpoints pause BEFORE execution. 130 integration tests passing |

### Estado Actual

- **Versión:** v1.18.0
- **Build:** ✅ `go build ./...`
- **Tests:** ✅ `go test ./...` — Todos pasando
- **CI:** ✅ `ci.yml` (vet + build + test -race + helm lint), `release.yml` (multi-platform Docker + Helm OCI + GitHub Release)
- **Docker:** Multi-platform `linux/amd64` + `linux/arm64` (QEMU + Buildx)
- **Fase 12:** ✅ Completa (Sagas + State Machines + Long-Running Workflows)
- **SOAP Connector:** ✅ Client + Server, SOAP 1.1/1.2, WSDL auto-gen
- **Codec System:** ✅ JSON + XML codecs, format declarations, auto-detection
- **Excel:** ✅ Native .xlsx read/write in file connector (`excelize/v2`)
- **Error Handling:** ✅ Guía completa, on_error aspects, custom error responses
- **Admin Server:** ✅ Health/metrics siempre disponibles (con o sin REST connector)
- **.env Support:** ✅ Auto-load `.env` file, `docs/DEPLOYMENT.md`
- **Helm Chart:** v0.2.0 — existingConfigMap + --set-file docs
- **MQ Filter Rejection:** ✅ `on_reject` policy (ack/reject/requeue) with dedup tracking
- **Notification api_url:** ✅ Configurable API URLs for all notification connectors
- **File Watch Mode:** ✅ Polling-based watcher, glob patterns, Starter/RouteRegistrar, 7 tests
- **MQTT Connector:** ✅ `internal/connector/mqtt/` — IoT messaging, QoS 0/1/2, topic wildcards (`+`, `#`), TLS, auto-reconnect. 13 tests
- **FTP/SFTP Connector:** ✅ `internal/connector/ftp/` — LIST/GET/PUT/MKDIR/DELETE, remoteClient interface, standard connector.Reader/Writer. 22 tests
- **Redis Pub/Sub:** ✅ `internal/connector/mq/redis/` — `driver = "redis"`, Subscribe/PSubscribe, handler resolution. 13 tests
- **Integration Tests:** ✅ `tests/integration/` — Docker Compose, 12 services + Cosmo Router, 30 test suites, parallel execution (3 phases), CI with grouped steps
- **GraphQL Typed DTOs:** ✅ `returns` attribute on flows, auto-generated `UserInput` types, introspection tests, federation SDL includes HCL types
- **`required = false`:** ✅ Type field attribute for optional fields (e.g., auto-generated IDs)
- **Kafka Logging:** ✅ Removed verbose debug logger, ErrorLogger downgraded to Warn
- **gRPC Fixes:** ✅ RegisterRoute interface, reflection (`grpcurl` support), proto response adaptation, input-as-filters, read-back on create
- **PostgreSQL INSERT RETURNING:** ✅ Full created row returned on INSERT (includes `id`, `created_at`, etc.)
- **GraphQL Federation:** ✅ Variable values filtering in `MapArgsToInput` prevents nested input injection from Cosmo Router
- **Runtime Fixes:** ✅ Event-driven source detection (MQ→POST), SOAP/TCP/gRPC input-as-filters, operation override in handleCreate/handleRead
- **WASM Documentation:** ✅ `docs/WASM.md` — 6 languages (Rust, Go/TinyGo, C, C++, AssemblyScript, Zig), interface spec, examples, size comparison. Fixed broken link, cross-references added
- **Security System:** ✅ Core sanitization pipeline (`internal/sanitize/`), always active. Vulnerability fixes (XXE, SSH injection, path traversal). Connector-specific rules. Security HCL block. WASM sanitizers. `docs/SECURITY.md`. Integration security tests (29 assertions against live endpoints)
- **Plugin Git System:** ✅ Git sources (SSH→HTTPS fallback), semver constraints (^/~/~>/>=/<), local cache (`mycel_plugins/`), lock file (`plugins.lock`), CLI (install/list/remove/update), auto-install on start
- **Plugin Validators & Sanitizers:** ✅ Plugins can provide validators and sanitizers in addition to connectors and functions. Registered automatically in runtime
- **Validator Registry:** ✅ Config validators now registered at startup (previously parsed but unused)
- **Validator Wiring:** ✅ `ValidatorRef` → `CustomValidatorConstraint` in FlowHandler. Plugin WASM validators usable from type fields
- **Request Logging:** ✅ Centralized in FlowHandler for ALL connectors (REST, GraphQL, gRPC, MQ, etc.)
- **Pretty Logs:** ✅ `lmittmann/tint` for colored pino-pretty-style terminal output
- **Plugin Manifest Detection:** ✅ `isPluginManifest()` detects by structure, only `mycel_plugins/` excluded by name
- **CSV/TSV Enhanced I/O:** ✅ Configurable delimiter, comment, skip_rows, no_header, custom columns, trim_space, BOM detection, sorted headers, connector defaults. 10 tests
- **Long-Running Workflows:** ✅ `internal/workflow/` — Engine, SQLStore (3 dialects), delay/await/signal/cancel/timeout, REST API, DBAccessor, NeedsPersistence. 13 tests. `docs/WORKFLOWS.md`
- **Environment-Aware Behavior:** ✅ `internal/envdefaults/` — Central defaults table, propagated to logger, hot reload, GraphQL playground, health manager, rate limiter, CORS, error responses, metrics, startup warnings. Tests passing
- **Flow Trace System:** ✅ `internal/trace/` — `mycel trace` CLI command, pipeline instrumentation, dry-run (all write ops), MemoryCollector/LogCollector, Renderer. 9 trace tests
- **Verbose Flow Logging:** ✅ `--verbose-flow` on `mycel start` — LogCollector per request, structured debug logs for all pipeline stages
- **Interactive Breakpoints:** ✅ `--breakpoints` (all stages) and `--break-at=stages` — interactive CLI debugging with next/continue/print/quit. 13 breakpoint tests
- **DAP Server:** ✅ `internal/dap/` — Debug Adapter Protocol over TCP. `--dap=4711` starts DAP server for IDE integration. BreakpointController interface. 11 tests
- **Dev-Only Debug:** ✅ `--verbose-flow`, `--breakpoints`, `--break-at`, `--dap` restricted to development mode. Warning in other modes
- **Connector Doc Cross-References:** ✅ 16 connector docs link to full configuration reference
- **Response Block:** ✅ `response` block in flows — transforms output AFTER destination. Echo flows (no `to`) fully supported. `http_status_code` (REST/SOAP), `grpc_status_code` (gRPC). `TransformResponse` in CEL. `ExtractStatusCode` shared helper
- **Benchmark Suite:** ✅ `benchmark/` — OpenTofu + Linode + k6, 5 modes, auto deploy→test→destroy, resource monitoring
- **Benchmark Parallel:** ✅ 5 VPS (3 targets + 1 DB + 1 attacker 4vCPU), adaptive calibration, 3 tests simultaneous. Standard 8,437 RPS, Realistic 204 RPS, Stress UNBREAKABLE. 3.2M requests, 0 crashes
- **Aspects Refactor:** ✅ `on` patterns match flow names (not file paths), `filepath.Match` glob, removed FlowPath from FlowHandler
- **Unique Name Validation:** ✅ Per-type duplicate detection in parser, error messages include file locations
- **Structured Error Object:** ✅ `error.code`/`.message`/`.type` in on_error aspects, `buildErrorInfo()` with type-switch + heuristics
- **Flow Invocation from Aspects:** ✅ `action { flow = "name" }` — FlowInvoker interface, parser validation (connector XOR flow), soft failure on error, 3 tests
- **Internal Flows:** ✅ Flows without `from` block invocable from aspects only
- **PDF Connector:** ✅ `internal/connector/pdf/` — HTML templates with Go template syntax, pure Go rendering via `go-pdf/fpdf`. Operations: `generate` (bytes for HTTP) and `save` (file). Supports h1-h6, p, table, strong/em, ul/ol, hr, img, basic CSS. 11 tests
- **Binary HTTP Responses:** ✅ REST connector detects `_binary`+`_content_type` fields and serves raw binary (PDF, images). `Content-Disposition` with filename
- **Response Enrichment in After Aspects:** ✅ `ResponseConfig` with `Fields` (CEL body) + `Headers` (HTTP headers). Propagated via `result.Metadata["_response_headers"]` → `_data`/`_response_headers` wrapper → REST connector `applyResponseHeaders`
- **Idempotency Keys:** ✅ Flow-level `idempotency` block (storage, key, ttl). Cache-backed, prevents duplicate execution, returns cached result
- **Async Execution:** ✅ Flow-level `async` block. Returns 202 with `job_id`, background goroutine, auto-registered `GET /jobs/{job_id}` polling endpoint
- **Database Migrations:** ✅ `mycel migrate` CLI command. SQL files from `migrations/`, `_mycel_migrations` tracking table, SQLite+PostgreSQL, `status` subcommand
- **File Upload:** ✅ multipart/form-data in REST connector (32MB max), files as base64 with metadata (`filename`, `content_type`, `size`, `data`)
- **HTML Email Templates:** ✅ `template_file` on Email struct, Go `text/template` rendering in SMTP/SendGrid/SES
- **Multi-Tenancy Headers:** ✅ Request headers as `input.headers` in flow transforms/CEL. Stripped from payload before DB writes
- **Distributed Rate Limiting:** ✅ `storage` attribute on `rate_limit` block. Redis-backed fixed-window counter via `RedisStore`. Automatic fallback to in-memory on Redis errors
- **Studio Debug Protocol:** ✅ `internal/debug/` — WebSocket JSON-RPC 2.0 on `:9090/debug`. Session management, runtime inspection (`inspect.flows/connectors/types/transforms`), event streaming (`event.flowStart/End/stageEnter/Exit/ruleEval`), stage-level breakpoints, per-CEL-rule breakpoints (`debug.stepInto`), variable inspection, CEL evaluation (`debug.evaluate`), conditional breakpoints. TransformHook interface in `internal/transform/hook.go`. Zero-cost when no debugger (~10ns nil-check). DAP coexistence preserved. Admin server always starts. 29 tests
- **Studio Debug Gate:** ✅ `DebugThrottler` interface with `AllowOne()` + `SourceInfo()`. `DebugGate` starts blocked when debugger connects — IDE sends `debug.consume` to allow one message at a time. Cancel channel unblocks workers on disconnect. `Session.ResumeAll()` resumes paused breakpoint threads. Reconnection works without restart. All 7 event-driven connectors support it. RabbitMQ also sets AMQP prefetch=1
- **Start Suspended Mode:** ✅ `--debug-suspend` / `MYCEL_DEBUG_SUSPEND=true`. Event-driven connectors defer `Start()` until debugger connects via `debug.ready`. REST/gRPC/GraphQL/SOAP/TCP/SSE start normally. Dev-only restriction
- **Connector Owns Config:** ✅ Parser no longer hardcodes connector-specific attributes. `SourceValidator`/`TargetValidator` interfaces let each connector validate its own params. `ConnectorParams` map + getter methods replace typed fields. 14 connectors implement validation. New/plugin connectors need zero parser changes
- **Fan-Out from Source:** ✅ `internal/connector/fanout.go` — Multiple flows share same `from` connector+operation, all execute concurrently. `ChainRequestResponse` for REST/gRPC/TCP/WebSocket/SOAP/SSE/GraphQL (first returns response, rest fire-and-forget). `ChainEventDriven` for RabbitMQ/Kafka/Redis/MQTT/CDC/File (all parallel, wait for all, ACK after). Input isolation via shallow copy. 14 connectors updated. 13 tests
- **Documentación:** ✅ CONCEPTS.md, CONFIGURATION.md, DEPLOYMENT.md, CHANGELOG.md, WASM.md, SECURITY.md, WORKFLOWS.md, soap.md, FORMAT.md, filesystem.md, environments.md, debugging.md, flows.md, transforms.md, use-cases.md, integration README actualizados
- **Accept Block:** ✅ `accept` block in flows — business-level gate after filter, before transform. `when` (CEL) + `on_reject` (ack/reject/requeue). Multi-consumer pattern support. `StageAccept` trace stage, debug protocol `AcceptInfo`
- **IDE Engine:** ✅ `pkg/ide/` — Importable Go package for Studio. Permissive HCL parser, project index, completions, diagnostics (4 layers), go-to-definition, hover docs, CEL completions (39 functions + context variables), connector-type-aware validation/completions, operation validation/completions, rename, code actions, workspace symbols, transform rule ordering, flow stage discovery, breakpoint locations on logic lines. 35 tests
- **Schema Architecture:** ✅ `pkg/schema/` — Single source of truth. 25+ connectors self-describe via `ConnectorSchemaProvider`. Registry wired into runtime+parser. Schema-driven validation with defaults (`ValidateParams`). 8 tests
- **Próximo paso:** Studio integration

### Dependencias Externas

```
github.com/golang-jwt/jwt/v5          # JWT (Phase 5)
github.com/redis/go-redis/v9          # Redis client (Phase 5)
github.com/boombuler/barcode          # QR codes (Phase 5)
github.com/go-webauthn/webauthn       # WebAuthn (Phase 5)
github.com/tetratelabs/wazero         # WASM runtime (Phase 5)
github.com/graph-gophers/dataloader/v7 # DataLoader N+1 (Phase 8)
github.com/jackc/pglogrepl            # PostgreSQL logical replication (Phase 10)
github.com/jackc/pgx/v5               # PostgreSQL driver (Phase 10)
github.com/xuri/excelize/v2           # Excel (.xlsx) read/write (File connector)
github.com/joho/godotenv              # .env file loading (CLI)
github.com/lmittmann/tint             # Pretty colored log output (Logging)
github.com/eclipse/paho.mqtt.golang   # MQTT client (MQTT connector)
github.com/jlaffaye/ftp              # FTP client (FTP connector)
github.com/pkg/sftp                   # SFTP client (FTP connector)
github.com/go-pdf/fpdf               # Pure Go PDF generation (PDF connector)
```

---

## Notas para Futuras Fases

### Fase 5 - Refactor Cache → Aspects
La cache es un cross-cutting concern. Plan: soportar tanto sintaxis inline en flow como aspects para aplicar cache a múltiples flows via pattern matching.

---

## Notas Técnicas

- **Pure Go:** Sin CGO. Todos los connectors deben ser pure Go.
- **HCL:** Toda la configuración es HCL. El binario es siempre el mismo.
- **Escaneo recursivo:** Todos los directorios se escanean recursivamente.
- **Hot reload:** ✅ Cambios en HCL se aplican sin reiniciar.
- **Protocolos estándar:** Siempre exponer/consumir protocolos estándar.
- **Código custom:** Validators y transforms complejos via WASM.
