# Introduction to Mycel

Mycel is a **declarative microservice framework**. Instead of writing code, you write HCL configuration files that describe what data sources to connect, how data flows between them, and what transformations to apply. Mycel runs as a binary that interprets those files — the same binary for every service, only the configuration differs.

Think of it like nginx: one binary, different configuration files for different services.

## The Problem Mycel Solves

Most microservices follow the same patterns:

- Accept an HTTP request, query a database, return the result
- Consume a message from a queue, transform it, write it to a database
- Call an external API, reshape the response, forward it somewhere else

Writing this as code means boilerplate: HTTP handlers, database connection pools, retry logic, error handling, serialization, validation — for every service, every time. Mycel handles all of that. You describe the pattern, Mycel runs it.

## The Core Model

Mycel has two core building blocks: **connectors** and **flows**. Everything else builds on them.

```
Connector (source) ──> Flow ──> Connector (target)
```

A **connector** is anything Mycel can talk to — a database, a REST API, a message queue, a file system, a cache. A **flow** wires two connectors together, moving data from one to the other.

On top of this foundation, you can add:
- **Transforms** — reshape data with CEL expressions
- **Types** — validate schemas before processing
- **Steps** — multi-step orchestration with intermediate calls
- **Auth** — JWT, sessions, MFA, without writing any auth code
- **Aspects** — cross-cutting concerns (audit logs, metrics) applied via patterns

Every feature ultimately serves the same pattern: data enters through a connector, gets processed, exits through another connector.

## A Concrete Example

A REST API that reads from PostgreSQL:

```hcl
# config.hcl
service {
  name    = "users-api"
  version = "1.0.0"
}

# connectors.hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  database = "myapp"
  user     = env("DB_USER")
  password = env("DB_PASSWORD")
}

# flows.hcl
flow "get_users" {
  from { connector = "api", operation = "GET /users" }
  to   { connector = "db", target = "users" }
}

flow "create_user" {
  from { connector = "api", operation = "POST /users" }

  transform {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    created_at = "now()"
  }

  to { connector = "db", target = "users" }
}
```

That is a complete REST API with a PostgreSQL backend. No code. No HTTP handlers. No SQL boilerplate.

Run it:

```bash
mycel start
# REST server listening on :3000
```

## What Mycel Produces

A running Mycel service is indistinguishable from a service built in Go, NestJS, or any other language. It speaks standard protocols — REST, GraphQL, gRPC, TCP, WebSockets — and connects to standard systems. Other services cannot tell they are talking to Mycel.

## Configuration Structure

A Mycel service is a directory of HCL files. Mycel scans all subdirectories recursively. The conventional layout:

```
my-service/
├── config.hcl          # Service name, version, global settings
├── connectors/         # Database, API, queue connections
├── flows/              # Data flows (from → to)
├── transforms/         # Reusable transform blocks
├── types/              # Data schemas and validation rules
├── validators/         # Custom validation logic
├── aspects/            # Cross-cutting concerns
├── auth/               # Authentication configuration
├── environments/       # Per-environment overrides
├── mocks/              # Test data (JSON files)
└── plugins/            # WASM plugin modules
```

Files can be split or combined however you prefer. Mycel identifies blocks by their HCL type (`connector`, `flow`, `type`), not by filename or directory.

## Next Steps

- [Quick Start](quick-start.md) — build and run a service in 10 minutes
- [Installation](installation.md) — Docker, Go binary, Helm
- [Core Concepts: Connectors](../core-concepts/connectors.md) — all available connector types
- [Core Concepts: Flows](../core-concepts/flows.md) — the complete flow model
