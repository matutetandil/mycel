# Security Example

Demonstrates Mycel's secure-by-default input sanitization. All protections are always active — this example shows how to customize thresholds.

## What's Protected Automatically

Every request passes through the sanitization pipeline before reaching your flows. This cannot be disabled.

| Protection | Description |
|---|---|
| Null bytes | Stripped from all string values |
| Invalid UTF-8 | Rejected |
| Control characters | Stripped (except tab, newline, cr by default) |
| Bidi overrides | Unicode directional overrides removed |
| Size limits | Max input size (default 1MB), max field length (default 64KB) |
| Depth limits | Max nesting depth (default 20 levels) |
| XXE blocking | XML/SOAP external entity expansion blocked |
| Path traversal | `../` sequences blocked in file connector paths |
| Shell injection | Dangerous characters escaped in exec connector |
| SQL identifiers | Table/column names validated against injection |

## Quick Start

```bash
mycel start --config ./examples/security
```

## Testing the Protections

### 1. Normal request (passes)

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "alice@example.com"}'
```

### 2. Oversized payload (blocked)

This example configures `max_input_length = 524288` (512KB). A payload exceeding that limit is rejected:

```bash
# Generate a 1MB payload
python3 -c "import json; print(json.dumps({'name': 'x' * 1048576}))" | \
  curl -X POST http://localhost:3000/users \
    -H "Content-Type: application/json" \
    -d @-
```

Expected: `413` or sanitization error.

### 3. Deeply nested JSON (blocked)

This example sets `max_field_depth = 10`. Nesting beyond that is rejected:

```bash
# Generate 15 levels of nesting
python3 -c "
d = {'name': 'Alice'}
for i in range(15):
    d = {'nested': d}
import json; print(json.dumps(d))
" | curl -X POST http://localhost:3000/users \
    -H "Content-Type: application/json" \
    -d @-
```

Expected: sanitization error (depth exceeded).

### 4. Null bytes in input (stripped)

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice\u0000Bob", "email": "alice@example.com"}'
```

Expected: null byte is stripped; `name` stored as `"AliceBob"`.

### 5. Control characters (stripped)

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice\u0007Bell", "email": "alice@example.com"}'
```

Expected: bell character (`\u0007`) is stripped; `name` stored as `"AliceBell"`.

## File Structure

```
security/
├── config.hcl              # Service name and version
├── security.hcl            # Custom security thresholds
├── connectors/
│   ├── api.hcl             # REST API on port 3000
│   └── database.hcl        # SQLite database
└── flows/
    └── users.hcl           # User CRUD (security applies automatically)
```

## Customizing Thresholds

In `security.hcl`:

```hcl
security {
  max_input_length = 524288   # Max total input (bytes)
  max_field_length = 8192     # Max single string field (bytes)
  max_field_depth  = 10       # Max JSON nesting depth

  allowed_control_chars = ["tab", "newline"]  # Allow only these

  # Raise limits for specific flows
  flow "bulk_import" {
    max_input_length = 10485760  # 10MB
  }
}
```

### Adding a WASM Sanitizer

For custom sanitization logic (e.g., stripping HTML tags):

```hcl
security {
  sanitizer "strip_html" {
    source     = "wasm"
    wasm       = "plugins/strip_html.wasm"
    entrypoint = "sanitize"
    apply_to   = ["flows/*"]          # Glob pattern for flows
    fields     = ["name", "bio"]      # Only these fields
  }
}
```

See [docs/WASM.md](../../docs/WASM.md) for writing WASM sanitizers.

## Defaults Reference

| Setting | Default | This Example |
|---|---|---|
| `max_input_length` | 1MB (1048576) | 512KB (524288) |
| `max_field_length` | 64KB (65536) | 8KB (8192) |
| `max_field_depth` | 20 | 10 |
| `allowed_control_chars` | tab, newline, cr | tab, newline |

## Next Steps

- Add type validation: See [examples/basic](../basic)
- Add authentication: See [docs/CONFIGURATION.md](../../docs/CONFIGURATION.md)
- Write WASM sanitizers: See [docs/WASM.md](../../docs/WASM.md)
