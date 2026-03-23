# Extending Mycel

When built-in features aren't enough, Mycel can be extended with custom logic via WebAssembly (WASM). Three extension points are available: **validators**, **custom functions**, and **plugins**.

## Validators

Custom validators add field-level validation rules to type definitions. Three types are supported: `regex`, `cel`, and `wasm`.

### Regex Validator

```hcl
validator "cuit" {
  type    = "regex"
  pattern = "^[0-9]{2}-[0-9]{8}-[0-9]$"
  message = "Must be a valid CUIT (XX-XXXXXXXX-X)"
}
```

### CEL Validator

```hcl
validator "company_email" {
  type    = "cel"
  expr    = "value.endsWith('@company.com')"
  message = "Must use a company email address"
}
```

CEL validators receive `value` (the field value) and must return a boolean.

### WASM Validator

For complex validation logic that can't be expressed in CEL:

```hcl
validator "luhn_check" {
  type       = "wasm"
  wasm       = "./wasm/validators.wasm"
  entrypoint = "validate_credit_card"
  message    = "Invalid credit card number"
}
```

### Using Validators in Types

```hcl
type "invoice" {
  number       = string { validate = "luhn_check" }
  supplier_tax = string { validate = "cuit" }
  contact      = string { validate = "company_email" }
}
```

## Custom Functions (WASM)

WASM functions extend the CEL transform engine with custom logic. Write in Rust, Go/TinyGo, C, C++, AssemblyScript, or Zig.

```hcl
functions "pricing" {
  wasm    = "./wasm/pricing.wasm"
  exports = ["calculate_price", "apply_discount"]
}
```

Then use in any transform:

```hcl
transform {
  total    = "calculate_price(input.items)"
  adjusted = "apply_discount(output.total, input.coupon_code)"
}
```

### WASM Interface

Functions must implement the standard Mycel WASM interface:

**Memory management (required by host):**
```
alloc(size: i32) -> i32
free(ptr: i32, size: i32)
```

**Function exports:**
```
# Input/output via shared memory: host writes JSON input at ptr, reads JSON result
function_name(ptr: i32, len: i32) -> i32  # returns result pointer
```

See [WASM Documentation](../advanced/wasm.md) for complete interface spec and language-specific examples.

## Mocks

Mocks provide test data without connecting to real services. Place JSON files in `mocks/` following the naming convention, then enable with CLI flags.

### Directory Structure

```
mocks/
├── db/
│   ├── users.json           # Mock for connector "db", target "users"
│   └── products.json
└── external_api/
    ├── GET_users.json        # Mock for GET /users
    └── POST_orders.json      # Mock for POST /orders
```

### Mock Data Format

```json
[
  {
    "id": "mock-id-1",
    "email": "alice@example.com",
    "name": "Alice"
  },
  {
    "id": "mock-id-2",
    "email": "bob@example.com",
    "name": "Bob"
  }
]
```

### Enabling Mocks

```bash
# Mock all connectors
mycel start --config ./my-service

# Mock specific connectors only
mycel start --mock=db --mock=external_api

# Mock all except specific connectors
mycel start --no-mock=stripe
```

### How Mocks Work

When a mock is enabled for a connector, all reads from that connector return mock data. Writes are silently discarded. This lets you test transforms and flow logic without real database or API access.

## Plugins

Plugins add new connector types to Mycel via WASM modules. Useful for integrating systems not natively supported.

```hcl
plugin "salesforce" {
  source  = "github.com/acme/mycel-salesforce"
  version = "^1.0"
}
```

After declaring the plugin, use its connector like any built-in connector:

```hcl
connector "sf" {
  type         = "salesforce"
  instance_url = env("SF_INSTANCE_URL")
  token        = env("SF_TOKEN")
}

flow "sync_contacts" {
  from {
    connector = "api"
    operation = "POST /sync"
  }
  to {
    connector = "sf"
    operation = "upsert_contact"
  }
}
```

### Plugin Sources

| Format | Example |
|--------|---------|
| GitHub | `github.com/org/repo` |
| GitLab | `gitlab.com/org/repo` |
| Local path | `./plugins/my-plugin` |
| Any git URL | `https://git.internal.com/repo` |

### Version Constraints

| Constraint | Meaning |
|------------|---------|
| `"^1.0"` | Compatible with 1.x (>= 1.0, < 2.0) |
| `"~2.0"` | Patch-level updates (>= 2.0, < 2.1) |
| `">= 1.0, < 3.0"` | Explicit range |
| `"latest"` | Latest release |

### Plugin Management

```bash
mycel plugin install             # Install all plugins from config
mycel plugin list                # Show installed plugins
mycel plugin remove salesforce   # Remove a plugin
mycel plugin update              # Update all plugins
```

Plugins are cached in `mycel_plugins/` (add to `.gitignore`). Reproducible builds via `plugins.lock`.

### Plugin Manifest

Plugin authors create a `plugin.mycel` file:

```hcl
plugin {
  name    = "salesforce"
  version = "1.0.0"
}

provides {
  connector "salesforce" {
    wasm = "connector.wasm"
  }

  validator "sf_id" {
    wasm       = "validators.wasm"
    entrypoint = "validate_sf_id"
    message    = "Invalid Salesforce ID"
  }

  sanitizer "pii_filter" {
    wasm       = "sanitizers.wasm"
    entrypoint = "filter_pii"
    apply_to   = ["flows/api/*"]
    fields     = ["email", "phone"]
  }
}
```

## Aspects

Aspects implement cross-cutting concerns that apply across multiple flows via pattern matching. Use aspects for audit logging, metrics, caching policies, and error alerting.

```hcl
aspect "audit_log" {
  when = "after"
  on   = ["create_*", "update_*"]

  action {
    connector = "audit_db"
    operation = "INSERT audit_logs"
    transform {
      flow      = "_flow"
      user_id   = "ctx.user_id"
      action    = "_operation"
      timestamp = "_timestamp"
    }
  }
}
```

### Aspect Timing

| `when` | Description |
|--------|-------------|
| `before` | Run before the flow executes |
| `after` | Run after the flow succeeds |
| `around` | Wrap the entire flow execution |
| `on_error` | Run when the flow fails |

### Aspect Variables

In aspect action transforms:

| Variable | Description |
|----------|-------------|
| `_flow` | Flow name |
| `_operation` | HTTP method or operation name |
| `_target` | Target connector/resource |
| `_timestamp` | Unix timestamp |
| `result` | Flow result (after/on_error) |
| `error.message` | Error message string (on_error) |
| `error.code` | HTTP status code, e.g. 404, 500 (on_error) |
| `error.type` | Error category: `http`, `timeout`, `connection`, `validation`, `not_found`, `auth`, `flow`, `unknown` (on_error) |

### Flow Invocation

Aspect actions can invoke flows directly instead of writing to connectors. Use `flow` instead of `connector` in the action block:

```hcl
aspect "notify_on_create" {
  when = "after"
  on   = ["create_*"]

  action {
    flow = "send_notification"
    transform {
      message = "'Created: ' + _flow"
      user_id = "input.user_id"
    }
  }
}
```

The invoked flow receives the transform output as its input. This is useful for:
- **Flow orchestration** — chain flows through aspects without coupling them directly
- **Internal flows** — flows without a `from` block that are only invocable from aspects
- **Error handling** — invoke recovery flows on failure

`connector` and `flow` are mutually exclusive in an action block.

### Response Enrichment

After aspects can include a `response` block to inject fields into every row of the flow result. This is useful for API versioning, deprecation notices, or adding metadata without modifying individual flows:

```hcl
aspect "v1_deprecation" {
  when = "after"
  on   = ["*_v1"]

  response {
    headers = {
      Deprecation = "true"
      Sunset      = "Thu, 01 Jun 2026 00:00:00 GMT"
    }

    _warning = "'This API version is deprecated. Migrate to v2.'"
  }
}
```

The `response` block supports two types of enrichment:
- **Body fields** — CEL expressions merged into every row of the response. Have access to `result.data`, `result.affected`, `input`, `_flow`, and `_operation`
- **Headers** — key-value pairs set as HTTP headers (or protocol equivalent for gRPC metadata, etc.). Values are literal strings

The `response` block is only valid for `after` aspects. An aspect can have both an `action` and a `response` block — the action runs as a side-effect and the response enriches the output.

### Pattern Matching

The `on` attribute accepts glob patterns matching flow names:

```hcl
on = ["*"]                          # All flows
on = ["create_*", "update_*"]       # All create and update flows
on = ["get_product*"]               # Flows starting with "get_product"
```

## See Also

- [WASM Documentation](../advanced/wasm.md) — complete WASM interface and language examples
- [Plugins example](../../examples/plugin)
- [Validators example](../../examples/validators)
- [Aspects example](../../examples/aspects)
- [Mocks example](../../examples/mocks)
