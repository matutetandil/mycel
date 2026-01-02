# Phase 5: Extensibility & Documentation

This phase adds cross-cutting concerns (Aspects), testing infrastructure (Mocks), and documentation generation.

## 5.1 Aspects (AOP)

Aspects allow applying common logic to multiple flows based on pattern matching, without modifying each flow individually.

### Use Cases

1. **Audit Logging** - Log all create/update/delete operations
2. **Caching** - Cache all read operations (refactor current cache system)
3. **Rate Limiting** - Apply rate limits per endpoint pattern
4. **Data Enrichment** - Add common fields to all responses
5. **Authorization** - Check permissions before flow execution
6. **Metrics** - Custom metrics for specific operations

### HCL Syntax

```hcl
aspect "audit_log" {
  # Pattern matching on flow files (glob patterns)
  on = ["flows/**/create_*.hcl", "flows/**/delete_*.hcl"]

  # When to execute: before, after, around
  when = "after"

  # Condition (optional) - CEL expression
  if = "result.affected > 0"

  # Action to perform
  action {
    connector = "audit_db"
    target    = "audit_logs"

    transform {
      action     = "input._operation"
      flow       = "input._flow"
      user_id    = "input.user_id ?? 'anonymous'"
      resource   = "input._target"
      timestamp  = "now()"
      request    = "jsonEncode(input)"
      response   = "jsonEncode(result)"
    }
  }
}
```

### Cache as Aspect

Refactor current cache system to use aspects:

```hcl
aspect "cache_products" {
  on   = ["flows/**/get_product*.hcl"]
  when = "around"

  cache {
    storage = "redis"
    ttl     = "10m"
    key     = "products:${input.id}"
  }
}

aspect "invalidate_products" {
  on   = ["flows/**/update_product*.hcl", "flows/**/delete_product*.hcl"]
  when = "after"
  if   = "result.affected > 0"

  invalidate {
    storage  = "redis"
    keys     = ["products:${input.id}"]
    patterns = ["products:list:*"]
  }
}
```

### When Values

| Value | Description |
|-------|-------------|
| `before` | Execute before the flow, can modify input or abort |
| `after` | Execute after the flow, has access to result |
| `around` | Wrap the flow (for caching, retry, etc.) |

### Context Variables

Available in aspect expressions:

| Variable | Description |
|----------|-------------|
| `input` | Original flow input |
| `input._flow` | Flow name |
| `input._operation` | HTTP method or operation type |
| `input._target` | Target connector/table |
| `result` | Flow result (only in `after`) |
| `result.affected` | Rows affected |
| `result.data` | Result data |
| `error` | Error if flow failed (only in `after` with errors) |

### Aspect Order

When multiple aspects match the same flow:
1. `before` aspects execute in definition order
2. `around` aspects wrap from outside-in (first defined = outermost)
3. `after` aspects execute in reverse definition order

### Implementation Plan

1. **Parser**: Add `aspect` block parsing
2. **Registry**: Store aspects, match patterns to flows
3. **Executor**: Execute aspects at appropriate times
4. **Integration**: Hook into FlowRegistry execution
5. **Refactor**: Migrate cache system to use aspects (optional, maintain backward compat)

---

## 5.2 Mock System

Mock connectors for testing without external dependencies.

### Directory Structure

```
mocks/
├── connectors/
│   ├── postgres/
│   │   ├── users.json        # Mock for "users" table
│   │   └── orders.json       # Mock for "orders" table
│   └── external_api/
│       └── GET_users.json    # Mock for GET /users
└── flows/
    └── get_users.json        # Override entire flow response
```

### Mock File Format

```json
// mocks/connectors/postgres/users.json
{
  "data": [
    {"id": 1, "email": "john@example.com", "name": "John"},
    {"id": 2, "email": "jane@example.com", "name": "Jane"}
  ],
  "metadata": {
    "total": 2
  }
}
```

With conditions:

```json
// mocks/connectors/postgres/users.json
{
  "responses": [
    {
      "when": "input.id == 1",
      "data": {"id": 1, "email": "john@example.com"}
    },
    {
      "when": "input.id == 2",
      "data": {"id": 2, "email": "jane@example.com"}
    },
    {
      "default": true,
      "error": "User not found",
      "status": 404
    }
  ]
}
```

### CLI Usage

```bash
# All connectors mocked (default in test env)
mycel start --env=test

# Only specific connectors mocked
mycel start --env=test --mock=postgres,external_api

# Everything except specific connectors
mycel start --env=test --no-mock=redis

# Disable all mocks
mycel start --env=test --no-mock
```

### HCL Configuration

```hcl
# environments/test.hcl
environment "test" {
  mocks {
    enabled = true
    path    = "./mocks"

    # Optional: connector-specific settings
    connectors {
      postgres = {
        latency = "50ms"  # Simulate latency
      }
    }
  }
}
```

### Implementation Plan

1. **Mock Loader**: Load JSON files from mocks directory
2. **Mock Connector**: Wrapper that returns mock data
3. **Pattern Matching**: Match requests to mock files
4. **CLI Flags**: --mock, --no-mock flags
5. **Latency Simulation**: Optional delay for realistic testing

---

## 5.3 Documentation Generation

Generate API documentation from configuration.

### OpenAPI Export

```bash
mycel export openapi --output ./docs/openapi.yaml
mycel export openapi --format json --output ./docs/openapi.json
```

Generated from:
- REST connector endpoints
- Flow definitions (from/to)
- Type definitions
- Transform schemas

### AsyncAPI Export

```bash
mycel export asyncapi --output ./docs/asyncapi.yaml
```

Generated from:
- Message queue connectors (RabbitMQ, Kafka)
- Queue/topic definitions
- Message schemas from types

### Implementation Plan

1. **Schema Collector**: Gather all endpoint/queue definitions
2. **OpenAPI Generator**: Generate OpenAPI 3.0 spec
3. **AsyncAPI Generator**: Generate AsyncAPI 2.0 spec
4. **CLI Commands**: `mycel export openapi`, `mycel export asyncapi`

---

## 5.4 Custom Validators (WASM)

User-defined validation logic via WebAssembly.

### HCL Syntax

```hcl
validator "argentina_cuit" {
  wasm = "./validators/cuit.wasm"

  # Or inline regex for simple cases
  pattern = "^(20|23|24|27|30|33|34)\\d{9}$"
  message = "Invalid CUIT format"
}

type "company" {
  cuit = string {
    validate = validator.argentina_cuit
  }
}
```

### WASM Interface

```go
// Validator WASM must export:
// - validate(input: string) -> bool
// - message() -> string (optional, for error message)
```

### Implementation Plan

1. **WASM Runtime**: Integrate wazero or wasmer
2. **Validator Interface**: Define WASM contract
3. **Validator Registry**: Load and cache validators
4. **Type Integration**: Use validators in type definitions

---

## 5.5 Plugin System

Extend Mycel with custom connectors and transforms.

### Plugin Structure

```
plugins/
└── salesforce/
    ├── plugin.hcl
    └── connector.wasm
```

### Plugin Manifest

```hcl
# plugins/salesforce/plugin.hcl
plugin "salesforce" {
  version = "1.0.0"
  type    = "connector"
  wasm    = "./connector.wasm"

  config {
    instance_url = string { required = true }
    api_version  = string { default = "v58.0" }
  }
}
```

### CLI Commands

```bash
mycel plugin install salesforce
mycel plugin list
mycel plugin remove salesforce
```

### Implementation Plan

1. **Plugin Loader**: Load plugin manifests
2. **WASM Connector**: Execute connector logic via WASM
3. **Plugin Registry**: Manage installed plugins
4. **CLI Commands**: install, list, remove

---

## Implementation Order

1. **Aspects (AOP)** - Foundation for cross-cutting concerns
2. **Mock System** - Essential for testing
3. **Doc Generation** - OpenAPI/AsyncAPI export
4. **Custom Validators** - WASM validators
5. **Plugin System** - Full extensibility

## Dependencies

- `github.com/tetratelabs/wazero` - WASM runtime (zero dependencies)
- `github.com/getkin/kin-openapi` - OpenAPI spec generation
- `github.com/asyncapi/parser-go` - AsyncAPI spec generation (if available)
