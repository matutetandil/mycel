# Architecture & Design Decisions

This document captures the reasoning behind Mycel's core design decisions. Understanding these helps contributors work with the grain of the project rather than against it. Every decision here was made deliberately, with alternatives considered and trade-offs accepted.

---

## Why Mycel Exists

The observation behind Mycel is simple: most microservice code is not business logic. It's plumbing. HTTP routing, database queries, input validation, data transformation, error handling with retries, authentication, metrics, health checks — the same patterns, reimplemented from scratch in every service, in every team, in every company. A team building an order service and a team building a user service are solving the same structural problems with different field names.

This is not a new observation. The industry's answer has historically been frameworks — Spring Boot, NestJS, Express — which reduce boilerplate but still require writing code. You still wire routes, write handlers, define database access layers, and configure middleware. The framework handles the boring parts *faster*, but it doesn't eliminate them.

Mycel takes the idea one step further: if the patterns are the same, and only the names and shapes change, then the patterns can be described as configuration instead of code. The result is a runtime that reads HCL files and produces a service that speaks standard protocols — indistinguishable from one built in Go, Java, or TypeScript, but defined entirely through configuration.

The core connectors cover the protocols that virtually every backend needs: REST, GraphQL, gRPC, SQL databases, MongoDB, message queues (RabbitMQ, Kafka), WebSocket, SSE, S3, cache, files. For application-specific integrations (Salesforce, SAP, or any proprietary API), the WASM plugin system allows extending Mycel without modifying the core binary.

---

## The Core Model

Mycel is a **runtime**, not a framework. The distinction matters.

A framework gives you scaffolding and conventions but expects you to write code. A runtime interprets your configuration and runs the service itself. You never write code — you describe what you want, and the runtime handles the rest.

The mental model is nginx or Apache: the binary is the same everywhere, the configuration is what changes. One team runs Mycel as a REST-to-PostgreSQL gateway. Another runs it as a Kafka consumer that feeds into Elasticsearch. Same binary, different `.hcl` files.

This model shapes every other decision in the project. When evaluating a feature request, the first question is always: "Can this be expressed as configuration?" If yes, it belongs in Mycel. If it requires arbitrary code, that's what WASM plugins and external services are for.

### The Two Building Blocks

Everything in Mycel reduces to two primitives:

- **Connectors** — anything Mycel talks to. A PostgreSQL database, a REST API, a Kafka topic, an S3 bucket, a WebSocket server. Each connector knows how to read from and write to its target.
- **Flows** — wiring between connectors. A flow says "when this connector receives data, transform it and send it to that connector."

Everything else — transforms, types, auth, aspects, sagas, state machines — is layered on top of these two primitives. New contributors should internalize this: if a concept doesn't ultimately express itself as connectors or flows, it probably doesn't belong in core Mycel.

---

## Why HCL, Not YAML, JSON, or TOML

The configuration format is where users spend most of their time. It needs to be robust, readable, and backed by a capable parser.

**YAML** was the first candidate and the most obvious choice given its dominance in infrastructure tooling. The problem is indentation-sensitivity. A misplaced space produces either a silent error (wrong structure, no warning) or a cryptic parse error with a useless line number. For something as critical as service architecture definitions — files that get committed, reviewed, and deployed — that fragility is unacceptable. YAML also has a well-documented list of parsing surprises (the Norway problem, multi-line string inconsistencies, implicit type coercion) that cause real bugs in production.

**JSON** has no indentation issues, but it's verbose for human-authored files. Required quotes on all keys, no comments, trailing comma errors. JSON is excellent for machine-generated data; it's a poor authoring experience for configuration.

**TOML** is flat by design, which works well for simple key-value configuration but breaks down with nesting. Mycel needs deep nesting: connectors have sub-blocks, flows have steps, steps have transforms, transforms have conditions. TOML's table syntax becomes awkward and unreadable at this depth.

**HCL** (HashiCorp Configuration Language) hits the right balance:

- Brace-delimited blocks mean the parser always knows where it stands. No indentation sensitivity.
- Keys don't require quotes. Blocks read like natural language.
- Comments work (`#` and `//`).
- The parser (`hashicorp/hcl/v2`) provides precise error messages with context, not just line numbers.
- Terraform has normalized HCL across the infrastructure engineering community. Most engineers who run infrastructure have already read HCL. It's not a niche bet.

The tradeoff is that HCL is less universally known than YAML. The bet is that the structural robustness justifies the small learning curve — and that anyone who has fought YAML indentation bugs in a Kubernetes manifest will appreciate the difference immediately.

---

## Why CEL for Transformations

Almost every flow needs to reshape data: rename a field, compute a value, filter a list, format a string. Mycel needs an expression language for this. The requirements are strict: it must be safe, fast, validated at load time, and expressive enough to cover the realistic transformation surface.

**CEL** (Common Expression Language) was designed by Google for exactly this use case. It powers Kubernetes admission webhooks, Firebase security rules, and Google Cloud IAM conditions. Its design constraints align perfectly with Mycel's needs:

- **Safe by design.** CEL expressions run in a sandbox with no side effects. They cannot make network calls, write to disk, or access anything outside the data they are given. They also cannot loop infinitely — the language has no general looping constructs, only list operations with bounded complexity. A malicious or buggy expression cannot crash or hang the runtime.
- **Compiled once, evaluated many times.** CEL expressions are parsed and type-checked at startup. A syntax error or type mismatch fails loudly when loading config, not silently in production at 3 AM. At runtime, expression evaluation is microseconds against pre-compiled bytecode.
- **Expressive enough.** Conditionals, arithmetic, string operations, list comprehensions (`filter`, `map`, `exists`, `all`), `has()` for optional field checks. Covers the vast majority of transformation needs without becoming a general-purpose language.
- **Ecosystem alignment.** CEL is well-documented, has bindings for Go, and is actively maintained. Choosing it means Mycel benefits from ongoing development without owning the expression engine.

The alternative was a custom expression DSL. That path means building a parser, a type checker, an evaluator, documentation, and IDE support from scratch. The upside is full control. The downside is everything else. CEL solves the problem with a proven, battle-tested implementation.

---

## Why WASM for Plugins and Custom Logic

Every declarative system eventually hits the same wall: a user needs to do something the system doesn't support. The response to this wall defines the system's long-term viability.

The wrong answer is "you can't." That turns Mycel into a dead end for any sufficiently complex service.

The wrong answer is "drop down to Go and write native code." That breaks the distribution model (users would need to compile their own binary) and introduces security risk (native code has full host access).

The right answer is a sandboxed plugin system. **WASM** is the right technology for it:

- **Security by design.** A WASM module cannot access the host filesystem, network, or memory outside its allocated region unless explicitly granted those capabilities. A plugin from an untrusted source cannot exfiltrate data or compromise the host. Traditional plugin systems (shared libraries, embedded scripting engines) offer no such guarantee.
- **Language-agnostic.** Teams can write plugins in Rust, Go via TinyGo, C, C++, AssemblyScript, or Zig. Whatever language the team already knows, it compiles to WASM. Mycel doesn't impose a language choice.
- **Pure Go runtime.** Mycel uses wazero as the WASM runtime. wazero is implemented entirely in Go with no CGO dependencies, which is consistent with the project-wide constraint of staying pure Go for cross-compilation and container cleanliness.
- **Solves the right problem.** CEL handles the 90% case of transformations and validations. WASM handles the 10% that genuinely requires computation CEL can't express — complex validation logic, custom encoding, domain-specific algorithms. The two-tier model (CEL first, WASM for the rest) keeps the common case simple and the uncommon case possible.

WASM is the escape hatch that prevents Mycel from becoming a dead end, without compromising the safety model.

---

## Why Go

The implementation language shapes what the runtime can do and how it behaves at the edges.

**Single binary deployment.** A Go binary has no runtime dependencies. No JVM to install, no Node.js version to manage, no Python virtualenv to maintain. Copy the binary, run it. In containers this means minimal base images (`FROM scratch` or `FROM alpine`). At the edge, it means deployment is `scp`.

**Pure Go, no CGO.** Every dependency in Mycel must be pure Go. This is enforced as a project rule. The benefit is that cross-compilation works reliably: `GOOS=linux GOARCH=arm64 go build` produces a working binary. CGO breaks this — it requires the target platform's C toolchain to be present during compilation. Pure Go also eliminates an entire class of memory safety bugs that come from C interop.

**Concurrency model.** A realistic Mycel service might simultaneously run a REST server, a GraphQL endpoint, WebSocket connections, Server-Sent Events streams, RabbitMQ consumers, and Kafka consumers. Go's goroutine model handles this naturally — thousands of concurrent lightweight threads, scheduled cooperatively. This is what Mycel's connector architecture is built around: each connector runs its own goroutines, and the runtime orchestrates them.

**Performance.** Go is compiled and fast enough for a runtime that sits in the critical path of every request. Latency matters when Mycel is processing tens of thousands of requests per second. Garbage collection pauses are real but manageable with Go's tuner — and for a data-plane runtime that doesn't allocate aggressively on the hot path, they rarely matter in practice.

**Ecosystem fit.** Everything Mycel needs has a quality pure-Go implementation: `net/http` for REST, `google.golang.org/grpc` for gRPC, `database/sql` with multiple drivers for databases, `hashicorp/hcl/v2` for configuration parsing, `tetratelabs/wazero` for WASM, `graph-gophers/graphql-go` for GraphQL. There were no ecosystem gaps that required reaching for non-Go solutions.

The main alternative considered was Rust. Rust has a higher performance ceiling and stronger memory safety guarantees. The trade-off is development velocity: Rust is slower to write, the borrow checker has a steep learning curve, and the contributor pool is smaller. For a project where development speed and community accessibility matter, Go is the better fit.

---

## Why Bidirectional Connectors

Many integration platforms distinguish between "input adapters" (sources) and "output adapters" (targets). Mycel does not.

Every connector is bidirectional. A `rest` connector can expose HTTP endpoints (acting as a server) and make outbound HTTP calls (acting as a client). A `database` connector can run SELECT queries (reading) and INSERT/UPDATE/DELETE (writing). A `queue` connector can consume messages and publish them.

This collapses what would otherwise be two separate concepts into one. You define `connector "my_db"` once and reference it in both `from` and `to` blocks across any number of flows. The connector carries all the connection configuration — host, port, credentials — in one place. There is no duplication, no "input version" and "output version" of the same connection.

The mental simplicity this creates is significant. Users learn one concept (connector) instead of two. The configuration is smaller. The mental model maps directly to how engineers already think about integrations: "I have a database and a REST API, I want to connect them."

---

## Why Configuration Directory, Not a Single File

Mycel scans a directory recursively for `.hcl` files. Every `.hcl` file it finds is part of the service configuration. File names and subdirectory structure are up to the user.

This mirrors how real teams organize code. A team of four might have:

```
connectors/
  database.hcl
  external-api.hcl
flows/
  users/
    create-user.hcl
    get-user.hcl
  orders/
    create-order.hcl
auth/
  config.hcl
```

Or they might put everything in a single `config.hcl`. Both work. Mycel doesn't impose an opinion on organization because different teams have different conventions, and enforcing one structure would create friction without benefit.

The recursive scan also enables composition patterns: common connectors can live in a shared directory, and service-specific flows live alongside them. Hot reload watches the entire directory tree, so changes to any file in any subdirectory take effect without a restart.

---

## Why Hot Reload by Default

Mycel is a configuration runtime, not a compiled application. Changing configuration should not require rebuilding or redeploying.

The comparison again is nginx: `nginx -s reload` picks up config changes with zero downtime. Mycel aims for the same behavior automatically. Change a flow definition, save the file, and the service picks up the new behavior within seconds — no process restart, no dropped connections, no rolling deployment.

In development, this creates an immediate feedback loop. In production, it means configuration changes (new endpoints, updated transforms, modified auth rules) can be applied by updating a ConfigMap or a mounted config volume, without triggering a pod restart or a deployment rollout.

The implementation uses filesystem watching (with polling as a fallback for environments where inotify isn't available). When a change is detected, Mycel reloads the affected parts of the configuration, reregisters routes, and continues serving.

---

## Why Built-in Observability

Every Mycel service includes health checks, Prometheus metrics, and structured logging — with zero configuration required.

The alternative is opt-in observability: document how to enable it, let users add it when they need it. The problem is that most services ship without it, observability gets added reactively after an incident, and the "how to enable it" documentation becomes a required prerequisite rather than a bonus.

Making observability built-in means every Mycel service is production-observable on day one. Health check endpoints (`/health`, `/health/live`, `/health/ready`) work immediately. Prometheus metrics are at `/metrics` without any configuration. Structured JSON logs are the default in non-development environments.

The standalone admin server (port 9090 by default) is a related decision: health and metrics endpoints should be available even if the service has no REST connector at all. A service that only consumes from Kafka and writes to PostgreSQL should still be health-checkable. The admin server runs independently of whatever connectors the user configures.

---

## Why Sagas for Distributed Consistency

Microservices cannot use database transactions across service boundaries. The classic solution is eventual consistency with compensating transactions — the Saga pattern.

Mycel's saga implementation is declarative. You define the steps and their compensations in HCL; Mycel orchestrates execution. If a step fails, Mycel automatically executes the compensation steps for all previously succeeded steps, in reverse order.

```hcl
saga "create_order" {
  step "reserve_inventory" {
    # ... forward action
    compensate { # ... rollback action }
  }
  step "charge_payment" {
    compensate { # ... refund action }
  }
}
```

Long-running workflows (steps that pause for hours or days, waiting for an external event) are built on the same infrastructure. Workflow state is persisted to a database (SQLite, PostgreSQL, or MySQL), so the runtime can restart and resume in-progress workflows without data loss.

The alternative is telling users to implement Sagas themselves, in code. That's the right answer for complex orchestration platforms, but for the typical Mycel use case — a service that coordinates a handful of steps across a few systems — a declarative saga implementation handles the 80% case with minimal cognitive overhead.

---

## The Security Model

Security in Mycel is built on one principle: **it is not optional**.

The sanitization pipeline runs before any flow processing begins. UTF-8 normalization, null byte stripping, bidi override character removal, depth limits, size limits — these run on every incoming request regardless of configuration. There is no `security.enabled = false` flag.

This is a deliberate product decision. Opt-in security means security is skipped in development (understandably), then forgotten in staging, then deployed to production without it. Built-in, always-active sanitization removes this failure mode.

Connector-specific protections are similarly built into each connector rather than exposed as options:

- The XML/SOAP connector blocks external entity references (XXE) unconditionally.
- The file connector validates paths against the configured base directory (path traversal protection).
- The exec connector shell-quotes all arguments before execution.
- The database connector uses parameterized queries throughout.

WASM sanitizers allow teams to add domain-specific sanitization rules (custom field formats, business-rule validation) without modifying the core pipeline, and without the security risk that comes from running arbitrary code in the sanitization layer.

---

## Trade-offs and Limitations

These are the known constraints, accepted deliberately.

**Not a general-purpose language.** Mycel deliberately limits expressiveness. If a service needs complex business logic that goes beyond CEL expressions and WASM modules, that logic belongs in a purpose-built service written in the team's language of choice. Mycel handles the 70-80% of microservice code that is protocol translation, data mapping, and plumbing. Trying to stretch it to cover the remaining 20% produces awkward configuration that fights the tool.

**HCL learning curve.** Engineers need to learn HCL syntax before they can use Mycel. The syntax is simple — simpler than YAML in practice, because there are fewer rules to memorize — but it is unfamiliar to most developers. The first 30 minutes of using Mycel include a syntax learning curve. This cost is accepted because HCL's structural robustness pays dividends over time.

**All connectors in the binary.** Connector types (REST, gRPC, Kafka, PostgreSQL, etc.) are compiled into the Mycel binary. Adding support for a new protocol requires a new binary release. WASM plugins solve this for custom business logic, but not for new transport protocols. The trade-off is simplicity (one binary, no runtime dependency resolution) against extensibility (new connectors require rebuilding).

**No visual editor.** Configuration is text files. There is no drag-and-drop interface, no web UI for building flows visually. This is intentional: text files integrate with git, code review, CI/CD pipelines, and diff tools. A visual editor might complement Mycel in the future, but text-first is the baseline because it composes with the tools engineers already use.

**CEL is not Turing-complete.** This is listed as a trade-off, but it is also a feature. The sandboxing and startup validation that make CEL safe depend on its bounded computational complexity. Complex conditional logic that requires general-purpose computation belongs in a WASM module, not in a CEL expression. The boundary between the two is intentional.

---

## Summary

| Decision | Choice | Core Reason |
|---|---|---|
| Format | HCL | Structural robustness over YAML's fragility |
| Transform language | CEL | Safe, compiled, validated at startup |
| Plugin system | WASM | Sandboxed, language-agnostic, pure Go runtime |
| Implementation language | Go | Single binary, pure Go constraint, goroutine concurrency |
| Connector model | Bidirectional | One concept instead of two |
| Configuration scope | Directory (recursive) | Team organization, hot reload |
| Observability | Built-in, always on | Production-ready by default |
| Distributed consistency | Saga pattern | Declarative compensation for multi-step flows |
| Security | Non-optional pipeline | Opt-in security is security that gets skipped |
