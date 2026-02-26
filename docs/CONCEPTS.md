# Mycel Concepts

This guide explains what each Mycel concept is, why it exists, and when to use it. For full HCL syntax and options, see the [Configuration Reference](CONFIGURATION.md).

## Table of Contents

- [The Mycel Model](#the-mycel-model)
- [Service](#service)
- [Connectors](#connectors)
- [Flows](#flows)
- [Transforms](#transforms)
- [Types](#types)
- [Steps](#steps)
- [Named Operations](#named-operations)
- [Auth](#auth)
- [Aspects](#aspects)
- [Validators](#validators)
- [WASM](#wasm)
- [Plugins](#plugins)
- [Mocks](#mocks)
- [Synchronization](#synchronization)
- [Environments](#environments)
- [Scheduled Jobs](#scheduled-jobs)
- [Configuration Structure](#configuration-structure)

---

## The Mycel Model

Mycel has two core building blocks: **connectors** and **flows**. Everything else builds on top of them.

A **connector** is anything Mycel can talk to — a database, a REST API, a message queue, a file system. A **flow** wires two connectors together, moving data from one to the other. That's the entire model.

```
Connector (source) ──→ Flow ──→ Connector (target)
```

On top of this, you can add transforms (reshape data), types (validate schemas), steps (multi-step orchestration), auth, aspects, and more. But every feature ultimately serves the same pattern: data enters through a connector, optionally gets transformed, and exits through another connector.

---

## Service

Every Mycel project starts with a `service` block — typically in `config.hcl` at the root of your project. It identifies your microservice with a name and version. These appear in startup logs, the `/health` endpoint, and Prometheus metrics, so you always know exactly what's running in each environment.

```hcl
service {
  name    = "orders-api"
  version = "2.1.0"
}
```

Without a `service` block, Mycel falls back to defaults (`mycel-service` / `0.0.0`), but you should always define it explicitly. The `service` block also supports global [rate limiting](CONFIGURATION.md#service-configuration).

See [Configuration Reference — Service Configuration](CONFIGURATION.md#service-configuration) for full syntax.

---

## Connectors

A connector is a bidirectional adapter between Mycel and an external system. Every connector can act as a **source** (receives data that triggers a flow) or a **target** (destination where a flow writes data). Some connectors are naturally one-directional — email is output-only, cron is input-only — but most work both ways.

| Type | Examples | As Source | As Target |
|------|----------|-----------|-----------|
| `rest` | HTTP server/client | Expose endpoints | Call APIs |
| `database` | PostgreSQL, MySQL, SQLite, MongoDB | Query data | Insert/Update/Delete |
| `graphql` | GraphQL server/client | Expose schema | Query/Mutate |
| `queue` | RabbitMQ, Kafka | Consume messages | Publish messages |
| `grpc` | gRPC server/client | Expose services | Call services |
| `tcp` | TCP server/client | Receive connections | Send data |
| `cache` | Memory, Redis | — | Read/write cache |
| `file` | Local filesystem | Read files | Write files |
| `s3` | AWS S3, MinIO | Read objects | Write objects |
| `exec` | Shell commands | — | Execute commands |
| `email` | SMTP | — | Send emails |
| `slack` / `discord` | Messaging platforms | — | Send notifications |
| `sms` / `push` | Twilio, FCM/APNs | — | Send messages |
| `webhook` | HTTP callbacks | — | Send webhooks |

```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  database = env("DB_NAME")
}
```

See [Configuration Reference — Connectors](CONFIGURATION.md#connectors) for full syntax per connector type.

---

## Flows

A flow is the unit of work in Mycel. It defines where data comes **from**, what happens to it, and where it goes **to**. When the source connector receives an event (an HTTP request, a queue message, a cron tick), the flow executes.

A minimal flow needs just `from` and `to`. You can add transforms, steps, filters, error handling, caching, and synchronization as needed.

```hcl
flow "get_users" {
  from { connector = "api", operation = "GET /users" }
  to   { connector = "db", target = "users" }
}
```

Flows can also have multiple `to` blocks for fan-out to several targets, and a `filter` in `from` to skip events that don't match a condition.

See [Configuration Reference — Flows](CONFIGURATION.md#flows) for full syntax.

---

## Transforms

Transforms reshape data between source and target using [CEL (Common Expression Language)](https://github.com/google/cel-go) expressions. You can define them inline within a flow, or as standalone reusable blocks that multiple flows reference with `use`.

Mycel provides built-in functions: `uuid()`, `now()`, `lower()`, `upper()`, `hash()`, `merge()`, `pick()`, `omit()`, plus array helpers like `first()`, `last()`, `pluck()`, `sort_by()`, `sum()`, `avg()`, and more.

```hcl
transform {
  output.id         = "uuid()"
  output.email      = "lower(input.email)"
  output.created_at = "now()"
}
```

You can compose transforms by referencing named ones:

```hcl
transform {
  use = [transform.normalize_user, transform.add_timestamps]
  output.source = "'api'"  # Override or add fields
}
```

See [Configuration Reference — Transforms](CONFIGURATION.md#transforms) for full syntax, and the [Transformations Guide](transformations.md) for a deep dive into CEL expressions and built-in functions.

---

## Types

Types define data schemas for validation. Attach them to flows with `input_type` or `output_type` to validate data before processing or before sending it to the target.

Each field specifies a base type (`string`, `number`, `boolean`, `object`, `array`) with optional constraints like `format`, `min`, `max`, `enum`, and `required`.

```hcl
type "user" {
  email = string { format = "email", required = true }
  age   = number { min = 0, max = 150 }
  role  = string { enum = ["admin", "user", "guest"] }
}
```

Reference in a flow:

```hcl
flow "create_user" {
  from { connector = "api", operation = "POST /users" }
  input_type = type.user
  to { connector = "db", target = "users" }
}
```

See [Configuration Reference — Types](CONFIGURATION.md#types) for full syntax.

---

## Steps

Steps add multi-step orchestration to a flow. Each step calls a connector and makes its result available to subsequent steps and the final transform. Steps can be conditional (`when`), letting you skip expensive calls when their data isn't needed.

Use steps when a single flow needs data from multiple sources — for example, fetching a customer record, calling a pricing API, and querying product details before composing a response.

```hcl
flow "get_order" {
  from { connector = "api", operation = "GET /orders/:id" }

  step "order" {
    connector = "db"
    operation = "query"
    query     = "SELECT * FROM orders WHERE id = ?"
    params    = [input.params.id]
  }

  step "customer" {
    connector = "customers_api"
    operation = "GET /customers/${step.order.customer_id}"
  }

  transform {
    output = merge(step.order, { "customer": step.customer })
  }

  to { response }
}
```

Step results are available as `step.<name>` in CEL expressions. See the [steps example](../examples/steps) for more patterns.

---

## Named Operations

Named operations let you define reusable parameterized queries on a connector. Instead of repeating SQL or API calls across flows, you define them once on the connector and reference them by name.

```hcl
connector "db" {
  type   = "database"
  driver = "postgres"

  operation "find_by_email" {
    query  = "SELECT * FROM users WHERE email = $1"
    params = [{ name = "email", type = "string", required = true }]
  }
}
```

Then in a flow:

```hcl
flow "lookup_user" {
  from { connector = "api", operation = "GET /users/lookup" }
  to   { connector = "db", operation = "find_by_email" }
}
```

See the [named-operations example](../examples/named-operations) for full patterns.

---

## Auth

Mycel provides a declarative authentication system with enterprise-grade security defaults. Instead of implementing auth from scratch, you choose a **preset** (`strict`, `standard`, `relaxed`, `development`) and customize what you need.

The auth system includes: JWT token management, password hashing (Argon2id), session management, brute force protection, rate limiting, and optional MFA (TOTP, WebAuthn, recovery codes).

```hcl
auth {
  preset = "standard"

  jwt {
    secret     = env("JWT_SECRET")
    access_ttl = "15m"
  }

  storage {
    users    = "connector.db"
    sessions = "connector.redis"
  }
}
```

This automatically exposes login, logout, register, refresh, and password endpoints. See the [auth example](../examples/auth) for full configuration including MFA setup.

---

## Aspects

Aspects implement Aspect-Oriented Programming (AOP) — cross-cutting concerns that apply to multiple flows without modifying each one. You define **when** (before, after, around, on_error), **which flows** (glob patterns), and **what action** to perform.

Use aspects for audit logging, automatic caching, metrics collection, or any behavior that should apply uniformly across flows matching a pattern.

```hcl
aspect "audit_log" {
  when = "after"
  on   = ["flows/**/create_*.hcl", "flows/**/update_*.hcl"]

  action {
    connector.audit_db = {
      operation = "INSERT audit_logs"
      data = {
        flow   = "${flow.name}"
        user   = "${context.user.id}"
        action = "${flow.operation}"
      }
    }
  }
}
```

See the [aspects example](../examples/aspects) for more patterns including cache aspects and error handling.

---

## Validators

Validators define custom validation rules for type fields that go beyond built-in constraints. Three types are available:

- **regex** — validate against a regular expression pattern
- **cel** — evaluate a CEL expression that returns true/false
- **wasm** — run a compiled WebAssembly module for complex validation logic

```hcl
validator "email_domain" {
  type    = "cel"
  expr    = "value.endsWith('@company.com')"
  message = "Must be a company email"
}

type "employee" {
  email = string { validate = validator.email_domain }
}
```

See the [validators example](../examples/validators) for regex, CEL, and WASM validator patterns.

---

## WASM

WebAssembly (WASM) extends Mycel with custom logic written in any language that compiles to WASM (Rust, Go, C, AssemblyScript). Two use cases:

- **Functions** — custom functions usable in CEL transforms (pricing calculations, scoring algorithms, encoding)
- **Validators** — complex validation rules that can't be expressed in regex or CEL

```hcl
functions "pricing" {
  wasm    = "./wasm/pricing.wasm"
  exports = ["calculate_price", "apply_discount"]
}

# Then in any transform:
transform {
  output.total = "calculate_price(input.items)"
}
```

See the [wasm-functions example](../examples/wasm-functions) and [wasm-validator example](../examples/wasm-validator) for implementation details.

---

## Plugins

Plugins add entirely new connector types to Mycel via WASM modules. Use plugins to integrate systems not natively supported — Salesforce, SAP, proprietary protocols, or custom internal systems.

```hcl
plugin "salesforce" {
  source  = "./plugins/salesforce"
  version = "1.0.0"
}

connector "sf" {
  type         = "salesforce"
  instance_url = env("SF_URL")
}
```

Manage plugins via CLI: `mycel plugin install <name>`, `mycel plugin list`, `mycel plugin remove <name>`. See the [plugin example](../examples/plugin) for a complete walkthrough.

---

## Mocks

Mocks provide test data for development and testing without connecting to real services. Define JSON files that Mycel returns instead of calling the actual connector.

Enable selectively with CLI flags: `--mock=connector_name` to mock specific connectors, or `--no-mock=connector_name` to exclude specific ones from mocking.

```
mocks/
├── db/
│   └── users.json        # Mock data for "db" connector, "users" target
└── external_api/
    └── GET_users.json     # Mock data for "external_api" connector, "GET /users"
```

See the [mocks example](../examples/mocks) for patterns and JSON format.

---

## Synchronization

Mycel provides distributed synchronization primitives for coordinating concurrent flow executions:

- **Lock (Mutex)** — guarantees only one flow processes a specific resource at a time. Use for operations that cannot be concurrent (e.g., updating an account balance).
- **Semaphore** — limits concurrency to N simultaneous executions. Use for rate limiting toward external APIs with quotas.
- **Coordinate (Signal/Wait)** — synchronizes dependent flows. One flow signals completion, another waits for it before proceeding.

```hcl
flow "process_payment" {
  lock {
    key     = "'account:' + input.account_id"
    storage = "connector.redis"
    timeout = "30s"
  }

  from { connector = "rabbit", operation = "payments" }
  to   { connector = "db", target = "UPDATE accounts" }
}
```

See [Configuration Reference — Synchronization](CONFIGURATION.md#synchronization) and the [sync example](../examples/sync) for all primitives.

---

## Environments

Environment variables let you configure different values per deployment (development, staging, production). Reference them with `env("VAR_NAME")` in any HCL attribute.

You can also define environment-specific HCL files in an `environments/` directory that override base configuration.

```hcl
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  password = env("DB_PASS")
}
```

Runtime variables: `MYCEL_ENV` (default: development), `MYCEL_LOG_LEVEL` (default: info), `MYCEL_LOG_FORMAT` (default: text).

---

## Scheduled Jobs

Flows can run on a schedule instead of being triggered by a connector. Use the `when` attribute with a cron expression or interval shorthand.

```hcl
flow "daily_cleanup" {
  when = "0 3 * * *"  # 3 AM daily

  to {
    connector = "db"
    query     = "DELETE FROM logs WHERE created_at < now() - interval '30 days'"
  }
}

flow "health_ping" {
  when = "@every 5m"
  to   { connector = "monitoring", operation = "POST /health" }
}
```

Shortcuts: `@hourly`, `@daily`, `@weekly`, `@monthly`. Combine with `lock` to prevent duplicate execution across instances.

See [Configuration Reference — Flow Triggers](CONFIGURATION.md#flow-triggers-when) for full syntax.

---

## Configuration Structure

A Mycel service is a directory of HCL files. Mycel recursively scans all subdirectories for `.hcl` files, so you can organize however you like. The conventional structure is:

```
my-service/
├── config.hcl          # Service name, version, global settings
├── connectors/         # Database, API, queue connections
├── flows/              # Data flows (from → to)
├── transforms/         # Reusable transform blocks
├── types/              # Data schemas
├── validators/         # Custom validation rules
├── aspects/            # Cross-cutting concerns
├── auth/               # Authentication configuration
├── environments/       # Per-environment overrides
├── mocks/              # Test data (JSON files)
└── plugins/            # WASM plugin modules
```

Files can be split or combined however you prefer — Mycel identifies blocks by their HCL type (`connector`, `flow`, `type`, etc.), not by filename or directory. A single `everything.hcl` works just as well as deeply nested directories.

See [Configuration Reference](CONFIGURATION.md) for complete syntax of every block type.
