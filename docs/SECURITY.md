# Security

Mycel applies a multi-layer security model to every request. The core sanitization pipeline runs automatically before any flow executes and cannot be disabled. Optional HCL configuration lets you adjust thresholds and add custom sanitizers on top of the baseline.

---

## Overview

| Layer | Location | When | Configurable |
|-------|----------|------|--------------|
| [Core Sanitization](#core-sanitization-pipeline) | `internal/sanitize/sanitize.go` | Before every flow | Thresholds only |
| [Connector-Specific Rules](#connector-specific-protections) | `internal/sanitize/rules/` | Per connector type | No |
| [WASM Sanitizers](#wasm-sanitizers) | `security/*.hcl` | After core sanitization | Yes |
| [Type Validation](#request-processing-pipeline) | `validators/*.hcl` | After sanitization | Yes |

---

## Request Processing Pipeline

Every request passes through these stages in order, regardless of connector type:

```
Input arrives (REST, GraphQL, gRPC, MQ, WebSocket, etc.)
  |
  v
Core Sanitization (always active)
  UTF-8 normalization, null byte stripping, control char filtering,
  bidi override stripping, size limits, depth limits
  |
  v
Custom WASM Sanitizers (if configured in security block)
  |
  v
Filter evaluation (from.filter)
  |
  v
Deduplication check (if configured)
  |
  v
Type validation (if input type is defined)
  |
  v
Transform execution
  |
  v
Connector-specific operations
```

---

## Core Sanitization Pipeline

The core pipeline is always active. It runs before every flow execution and applies to all input regardless of its origin.

### UTF-8 Normalization

Invalid UTF-8 byte sequences are stripped from all string fields. Mycel does not attempt to repair malformed sequences — invalid bytes are removed silently.

### Null Byte Stripping

Null bytes (`\x00`) are unconditionally removed from all input. Null bytes are a common attack vector for bypassing string length checks or terminating strings prematurely in downstream systems.

### Control Character Filtering

Characters with code points below `0x20` are stripped by default, with three exceptions: tab (`\t`), newline (`\n`), and carriage return (`\r`). The allowed set can be adjusted via the `allowed_control_chars` configuration attribute.

### Bidi Override Stripping

Unicode bidirectional override characters in the range U+202A through U+2069 are stripped unconditionally. These characters can be used in homograph attacks to make malicious content appear visually identical to legitimate text in editors and diff views.

### Input Size Limits

The entire input payload is rejected if it exceeds `max_input_length` (default: 1MB). Individual string fields are rejected if they exceed `max_field_length` (default: 64KB). These limits protect against memory exhaustion and denial-of-service through large payloads.

### Field Depth Limit

Input objects nested deeper than `max_field_depth` levels (default: 20) are rejected entirely. This prevents stack overflow and CPU exhaustion from deeply nested JSON or XML structures.

---

## Connector-Specific Protections

These rules are applied automatically based on connector type. They cannot be disabled via configuration.

### XML and SOAP — XXE Protection

The XML decoder is configured with an empty entity map:

```go
decoder.Entity = map[string]string{}
```

This blocks all external entity expansion. DOCTYPE and ENTITY declarations in incoming XML are rejected. Only the five standard XML entities are recognized: `&amp;`, `&lt;`, `&gt;`, `&quot;`, `&apos;`. This mitigation applies to both the XML codec and the SOAP envelope parser.

### File Connector — Path Traversal Protection

All file paths are resolved relative to the configured `BasePath`. The following rules apply:

- Absolute paths are treated as relative (the leading `/` is stripped)
- `../` sequences are normalized before resolution
- Null bytes in paths are rejected
- The resolved path is validated to be contained within `BasePath`

A path that escapes the base directory is rejected regardless of how it was constructed.

### Exec Connector — Command Injection Protection

Shell arguments provided by user input are individually wrapped in single quotes by the `shellQuote()` function before being passed to the shell. Shell metacharacters (`;`, `|`, `&`, `` ` ``, `$`, `(`, `)`, `{`, `}`, `>`, `<`, `!`, `\`) are detected and cause the request to be rejected. SSH remote commands have each argument escaped before transmission.

The command itself (defined in HCL) is trusted. Only user-supplied values are quoted.

### SQL Connectors — Identifier Validation

Table and column names are validated against the pattern `[a-zA-Z_][a-zA-Z0-9_.]*` before use in queries. All query values are passed via parameterized queries — no user input is ever concatenated into SQL strings.

---

## What Cannot Be Disabled

The following protections are unconditional and cannot be turned off through any configuration:

- UTF-8 validation and stripping of invalid sequences
- Null byte stripping
- Bidi override character stripping
- Input size limits (the limit can be raised, not removed)
- Field depth limits (the limit can be raised, not removed)
- XXE protection in XML and SOAP
- Path containment in the file connector
- Shell argument quoting in the exec connector
- Parameterized queries in all SQL connectors

---

## CEL Injection — Not Applicable

CEL injection is not a threat vector in Mycel by design. CEL expressions come exclusively from HCL configuration files, which are compiled at startup. User input enters the CEL evaluation engine only as variable values — never as expressions. There is no way for an external caller to supply a CEL expression at runtime.

---

## HCL Configuration

The `security` block lets you adjust thresholds and register custom WASM sanitizers. It does not let you disable any core protection.

Place security configuration in `security/*.hcl` within your config directory.

### Global Thresholds

```hcl
security {
  max_input_length      = 2097152    # 2MB (default: 1048576 = 1MB)
  max_field_length      = 131072     # 128KB (default: 65536 = 64KB)
  max_field_depth       = 30         # (default: 20)
  allowed_control_chars = ["tab", "newline", "cr"]  # default: all three
}
```

### WASM Sanitizers

Custom sanitizers receive a field value, transform it, and return the cleaned value. Unlike validators, sanitizers produce output — they clean rather than reject.

```hcl
security {
  sanitizer "strip_html" {
    source     = "wasm"
    wasm       = "plugins/strip_html.wasm"
    entrypoint = "sanitize"           # default: "sanitize"
    apply_to   = ["flows/api/*"]      # glob patterns, empty = all flows
    fields     = ["body", "description"]  # empty = all string fields
  }

  sanitizer "mask_pii" {
    source = "wasm"
    wasm   = "plugins/mask_pii.wasm"
  }
}
```

### Per-Flow Overrides

Individual flows can raise their size limits or apply additional sanitizers:

```hcl
security {
  flow "bulk_import" {
    max_input_length = 10485760    # 10MB for this specific flow
    max_field_length = 524288      # 512KB
    sanitizers       = ["strip_html"]  # additional WASM sanitizers
  }
}
```

---

## Attributes Reference

### Global Security Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_input_length` | number | `1048576` (1MB) | Maximum total input size in bytes. Rejects entire input if exceeded |
| `max_field_length` | number | `65536` (64KB) | Maximum length of any single string field |
| `max_field_depth` | number | `20` | Maximum nesting depth of input objects |
| `allowed_control_chars` | list(string) | `["tab", "newline", "cr"]` | Control characters permitted to pass through |

### Sanitizer Block Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `source` | string | yes | Sanitizer type. Currently only `"wasm"` is supported |
| `wasm` | string | yes | Path to the WASM module, relative to the config directory |
| `entrypoint` | string | no | Exported function name to call (default: `"sanitize"`) |
| `apply_to` | list(string) | no | Glob patterns matching flow names. Empty means all flows |
| `fields` | list(string) | no | Field names to sanitize. Empty means all string fields |

### Flow Override Block Attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `max_input_length` | number | Override the global `max_input_length` for this flow only |
| `max_field_length` | number | Override the global `max_field_length` for this flow only |
| `sanitizers` | list(string) | Names of additional WASM sanitizers to apply to this flow |

---

## WASM Sanitizers

Sanitizers use the same WASM infrastructure as validators and custom functions. See [WASM.md](WASM.md) for memory management details, supported languages, and build instructions.

### Interface

Every WASM sanitizer module must export:

| Export | Signature | Description |
|--------|-----------|-------------|
| `alloc` | `(size: i32) -> ptr: i32` | Allocate memory |
| `free` | `(ptr: i32, size: i32)` | Free memory |
| `sanitize` | `(ptr: i32, len: i32) -> (ptr: i32, len: i32)` | Sanitize a value |

The `sanitize` function receives and returns JSON bytes:

- **Input**: JSON-encoded value (e.g., `"hello <script>alert(1)</script>"`)
- **Output**: JSON-encoded sanitized value (e.g., `"hello "`)
- **Reject**: Return `(0, 0)` to reject the input entirely (equivalent to validation failure)

### Sanitizer vs. Validator

| | Validator | Sanitizer |
|--|-----------|-----------|
| **Purpose** | Accept or reject a value | Clean and transform a value |
| **Output** | `0` (valid) or `1` (invalid) | Sanitized JSON value |
| **On failure** | Rejects the request with a validation error | Return `(0, 0)` to reject, or return cleaned value |
| **Side effects** | None | Modifies the field value in the input |

### Example: Strip HTML Tags (Rust)

```rust
#[no_mangle]
pub extern "C" fn alloc(size: usize) -> *mut u8 {
    let mut buf = Vec::with_capacity(size);
    let ptr = buf.as_mut_ptr();
    std::mem::forget(buf);
    ptr
}

#[no_mangle]
pub extern "C" fn free(ptr: *mut u8, size: usize) {
    unsafe { drop(Vec::from_raw_parts(ptr, 0, size)) }
}

#[no_mangle]
pub extern "C" fn sanitize(ptr: i32, len: i32) -> u64 {
    let input = unsafe {
        std::slice::from_raw_parts(ptr as *const u8, len as usize)
    };

    // input is JSON-encoded: "\"hello <b>world</b>\""
    let s: String = serde_json::from_slice(input).unwrap_or_default();
    let cleaned = strip_html_tags(&s);
    let output = serde_json::to_vec(&cleaned).unwrap();

    let out_ptr = alloc(output.len());
    unsafe { std::ptr::copy_nonoverlapping(output.as_ptr(), out_ptr, output.len()) }

    // Pack ptr and len into a single u64 return value
    ((out_ptr as u64) << 32) | (output.len() as u64)
}

fn strip_html_tags(s: &str) -> String {
    let mut out = String::new();
    let mut in_tag = false;
    for c in s.chars() {
        match c {
            '<' => in_tag = true,
            '>' => in_tag = false,
            _ if !in_tag => out.push(c),
            _ => {}
        }
    }
    out
}
```

```bash
cargo build --target wasm32-unknown-unknown --release
cp target/wasm32-unknown-unknown/release/strip_html.wasm plugins/
```

---

## Vulnerability Mitigations Summary

| Vulnerability | Mitigation | Layer |
|---------------|-----------|-------|
| XXE (XML External Entity) | Entity map set to empty; DOCTYPE/ENTITY blocked | Connector (XML/SOAP) |
| Command injection (shell) | `shellQuote()` wraps all user args in single quotes; metacharacters rejected | Connector (exec) |
| Command injection (SSH) | User-provided args escaped before remote execution | Connector (exec) |
| Path traversal | Absolute paths stripped; `../` normalized; containment validated against BasePath | Connector (file) |
| SQL injection | All query values use parameterized queries; identifiers validated against safe pattern | Connector (database) |
| CEL injection | Not possible — CEL expressions come from HCL only; user input enters as variables | Runtime (by design) |
| Homograph / Bidi attacks | U+202A–U+2069 stripped unconditionally | Core sanitization |
| Null byte injection | `\x00` stripped unconditionally from all input | Core sanitization |
| Oversized payloads (DoS) | Input and field size limits enforced before any processing | Core sanitization |
| Deeply nested input (DoS) | Field depth limit enforced before any processing | Core sanitization |
| Invalid UTF-8 | Invalid sequences stripped before processing | Core sanitization |

---

## Reporting Security Issues

Do not create public GitHub issues for security vulnerabilities. Send a description of the issue, affected versions, and reproduction steps to the project maintainers privately. Include as much detail as possible to help reproduce and assess the impact.
