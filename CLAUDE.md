# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Mycel** is an open-source declarative microservice framework. Instead of writing code, you define configuration files (HCL2) and Mycel runs as a runtime that interprets them - similar to nginx or Apache.

**Philosophy:** Configuration, not code. The user defines WHAT they want, Mycel handles the HOW.

**Tech Stack:** Go + HCL2 (HashiCorp Configuration Language v2)

## Build and Test Commands

```bash
# Build
go build ./...

# Run all tests
go test ./...

# Run specific package tests
go test ./internal/connector/...

# Start Mycel
mycel start --config ./examples/basic

# Validate configuration
mycel validate --config ./examples/basic

# Check connector connectivity
mycel check --config ./examples/basic
```

## Project Structure

```
mycel/
├── cmd/mycel/           # CLI entry point
├── internal/
│   ├── banner/          # ASCII banner and colored output
│   ├── config/          # HCL config loading and parsing
│   ├── connector/       # All connector implementations
│   │   ├── database/    # SQLite, PostgreSQL
│   │   ├── http/        # HTTP client
│   │   ├── mq/          # Message queues (RabbitMQ, Kafka)
│   │   ├── rest/        # REST server
│   │   └── tcp/         # TCP server/client (JSON, msgpack, NestJS)
│   ├── runtime/         # Core runtime and flow registry
│   ├── transform/       # CEL-based transformation engine
│   └── validate/        # Type validation system
├── pkg/hcl/             # HCL parsing utilities and functions
├── examples/            # Example configurations
│   ├── basic/           # Simple REST + SQLite
│   ├── tcp/             # TCP server/client examples
│   └── mq/              # RabbitMQ/Kafka examples
└── docs/                # Documentation
```

## Architecture

Mycel is a **runtime** that reads HCL configuration and exposes services:

```
┌─────────────────────────────────────────┐
│           mycel (binary)                │
│  ┌─────────────────────────────────┐    │
│  │  Reads /etc/mycel or ./config   │    │
│  │  • connectors/*.hcl             │    │
│  │  • flows/*.hcl                  │    │
│  │  • types/*.hcl                  │    │
│  │  • transforms/*.hcl             │    │
│  │  • validators/*.hcl             │    │
│  │  • aspects/*.hcl                │    │
│  │  • auth/config.hcl              │    │
│  │  • mocks/**/*.json              │    │
│  │  • environments/*.hcl           │    │
│  │  • plugins/**/*                 │    │
│  │  • config.hcl                   │    │
│  └─────────────────────────────────┘    │
│                 ↓                       │
│         Running Microservice            │
└─────────────────────────────────────────┘
```

Mycel generates standard microservices that speak standard protocols (REST, GraphQL, gRPC, etc). A microservice built with Mycel is indistinguishable from one built in NestJS, Go, or any other language.

## Key Concepts

### Connectors
Bidirectional adapters that can read (source) or write/expose (target):
- **database**: SQLite, PostgreSQL, MySQL, MongoDB
- **rest**: HTTP server/client
- **graphql**: GraphQL server/client
- **queue**: RabbitMQ, Kafka
- **tcp**: TCP server/client (JSON, msgpack, NestJS protocol)
- **grpc**: gRPC server/client
- **file/s3**: File system, S3

### Flows
Define how data flows from one connector to another:
```hcl
flow "get_users" {
  from { connector.my_api = "GET /users" }
  to   { connector.postgres = "users" }
}
```

### Transforms
CEL-based data transformations (inline or reusable):
```hcl
transform {
  output.id    = uuid()
  output.email = lower(input.email)
}
```

### Types
Schema validation for inputs/outputs:
```hcl
type "user" {
  email = string { format = "email" }
  age   = number { min = 0, max = 150 }
}
```

### Aspects (AOP)
Cross-cutting concerns applied via pattern matching:
```hcl
aspect "audit_log" {
  when = "after"
  on   = ["flows/**/create_*.hcl"]
  action { ... }
}
```

## Development Guidelines

1. **Pure Go**: No CGO dependencies. All connectors must be pure Go.
2. **HCL First**: All configuration is HCL. The binary is the same, configuration differs.
3. **Recursive Scanning**: All directories are scanned recursively for .hcl files.
4. **Hot Reload**: Configuration changes apply without restart.
5. **Standard Protocols**: Always expose/consume standard protocols (REST, gRPC, etc).

## Current Implementation Status

### ✅ Completed (Phases 1-3.1)
- REST Server + Client
- SQLite + PostgreSQL
- TCP Server + Client (JSON, msgpack, NestJS protocol)
- RabbitMQ + Kafka
- CEL Transform Engine
- Type Validation
- Environment Support
- CLI (start, validate, check)

### 🔜 Pending (Phases 3-5)
- GraphQL server/client
- gRPC server/client
- Files/S3
- Hot Reload
- Metrics + Health Checks
- Auth System (enterprise-grade)
- Aspects (AOP)
- Validators (custom WASM)
- Plugins System
- Mocks/Testing
- Doc Generation (OpenAPI, GraphQL schema)

## CLI Commands

```bash
mycel start [--env=<env>] [--config=<path>] [--mock=<n>] [--no-mock=<n>]
mycel validate [--config=<path>]
mycel check [--config=<path>]
mycel version

mycel plugin install <name>
mycel plugin list
mycel plugin remove <name>

mycel export openapi
mycel export graphql-schema
```

## Testing

All packages must have tests. Run with:
```bash
go test ./...
```

For integration tests that require external services (Postgres, RabbitMQ, Kafka), use build tags or skip if services are not available.
