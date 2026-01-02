# Phase 5: Extensibility System (Validators, Functions, Plugins)

This document specifies the extensibility system for Mycel, enabling custom validators, CEL functions, and connectors via regex, CEL expressions, and WASM.

## Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      WASM Runtime                            │
│                       (wazero)                               │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Validators  │  │  Functions  │  │  Connectors         │  │
│  │             │  │  (CEL ext)  │  │  (Plugins)          │  │
│  │ validate()  │  │ fn1(), fn2()│  │ read/write/call()   │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Order

1. **Validators regex/CEL** (no WASM, quick win)
2. **WASM runtime base** (wazero integration)
3. **Validators WASM**
4. **Functions WASM** (CEL extensions)
5. **Plugins** (connectors WASM)

---

## 1. Validators

Validators provide reusable validation logic for type fields. Three types supported:

### 1.1 Regex Validators

Simple pattern matching:

```hcl
validator "phone_ar" {
  type    = "regex"
  pattern = "^\\+54[0-9]{10,11}$"
  message = "Invalid Argentine phone number"
}

validator "email" {
  type    = "regex"
  pattern = "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
  message = "Invalid email format"
}

validator "uuid" {
  type    = "regex"
  pattern = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
  message = "Invalid UUID format"
}
```

### 1.2 CEL Validators

Expression-based validation with access to the value:

```hcl
validator "adult_age" {
  type    = "cel"
  expr    = "value >= 18 && value <= 120"
  message = "Age must be between 18 and 120"
}

validator "strong_password" {
  type = "cel"
  expr = <<-CEL
    size(value) >= 8 &&
    value.matches("[A-Z]") &&
    value.matches("[a-z]") &&
    value.matches("[0-9]") &&
    value.matches("[!@#$%^&*]")
  CEL
  message = "Password must have 8+ chars, upper, lower, number, and symbol"
}

validator "future_date" {
  type    = "cel"
  expr    = "timestamp(value) > now()"
  message = "Date must be in the future"
}

validator "valid_status" {
  type    = "cel"
  expr    = "value in ['pending', 'active', 'completed', 'cancelled']"
  message = "Invalid status value"
}
```

### 1.3 WASM Validators

Complex validation logic compiled to WebAssembly:

```hcl
validator "argentina_cuit" {
  type       = "wasm"
  wasm       = "./validators/cuit.wasm"
  entrypoint = "validate"
  message    = "Invalid CUIT"
}

validator "credit_card" {
  type       = "wasm"
  wasm       = "./validators/luhn.wasm"
  entrypoint = "validate_card"
  message    = "Invalid credit card number"
}
```

### 1.4 Usage in Types

```hcl
type "usuario" {
  phone    = string { validate = "validator.phone_ar" }
  age      = number { validate = "validator.adult_age" }
  password = string { validate = "validator.strong_password" }
  cuit     = string { validate = "validator.argentina_cuit" }
  email    = string { validate = "validator.email" }
}

type "order" {
  status      = string { validate = "validator.valid_status" }
  delivery_at = string { validate = "validator.future_date" }
}
```

### 1.5 Validator Interface (Internal)

```go
type Validator interface {
    Name() string
    Validate(value interface{}) error
}

type RegexValidator struct {
    name    string
    pattern *regexp.Regexp
    message string
}

type CELValidator struct {
    name    string
    program cel.Program
    message string
}

type WASMValidator struct {
    name       string
    module     wazero.CompiledModule
    entrypoint string
    message    string
}
```

### 1.6 WASM Validator Interface

The WASM module must export a function with this signature:

```
validate(value_ptr: i32, value_len: i32) -> i32
```

Returns:
- `0` = valid
- `1` = invalid (use default message)
- `ptr` = pointer to error message string (custom message)

Input is JSON-encoded value passed via linear memory.

---

## 2. Functions (CEL Extensions)

Custom functions that extend CEL, available in all transform expressions.

### 2.1 HCL Definition

```hcl
# functions.hcl
functions "pricing" {
  wasm    = "./functions/pricing.wasm"
  exports = ["calculate_price", "apply_discount", "tax_for_country"]
}

functions "geo" {
  wasm    = "./functions/geo.wasm"
  exports = ["distance_km", "in_polygon", "nearest_location"]
}

functions "crypto" {
  wasm    = "./functions/crypto.wasm"
  exports = ["encrypt_aes", "decrypt_aes", "sign_hmac"]
}
```

### 2.2 Usage in Transforms

```hcl
flow "checkout" {
  from {
    connector = "api"
    operation = "POST /checkout"
  }

  transform {
    # Custom functions from WASM
    subtotal = "calculate_price(input.items)"
    discount = "apply_discount(subtotal, input.coupon_code)"
    tax      = "tax_for_country(discount, input.shipping.country)"
    total    = "discount + tax"

    # Mix with built-in CEL
    order_id   = "uuid()"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

flow "find_nearest_store" {
  transform {
    user_location = "input.location"
    nearest       = "nearest_location(user_location, enriched.stores)"
    distance      = "distance_km(user_location, nearest.location)"
  }
}
```

### 2.3 WASM Function Interface

Each exported function receives JSON and returns JSON:

```
function_name(input_ptr: i32, input_len: i32) -> (result_ptr: i32, result_len: i32)
```

Input JSON:
```json
{
  "args": [arg1, arg2, ...]
}
```

Output JSON:
```json
{
  "result": <value>,
  "error": null
}
// or
{
  "result": null,
  "error": "error message"
}
```

### 2.4 Registration in CEL

Functions are registered at startup:

```go
// For each exported function
celEnv.AddFunction(
    functionName,
    cel.Overload(
        functionName+"_impl",
        []*cel.Type{cel.DynType}, // variadic args
        cel.DynType,              // return type
        cel.FunctionBinding(func(args ...ref.Val) ref.Val {
            return callWASMFunction(module, functionName, args)
        }),
    ),
)
```

---

## 3. Plugins (WASM Connectors)

Plugins provide custom connectors for systems not built into Mycel.

### 3.1 Plugin Structure

```
plugins/
└── salesforce/
    ├── plugin.hcl       # Manifest
    ├── connector.wasm   # Connector implementation
    └── README.md        # Documentation (optional)
```

### 3.2 Plugin Manifest

```hcl
# plugin.hcl
plugin {
  name        = "salesforce"
  version     = "1.0.0"
  description = "Salesforce CRM connector"
  author      = "Acme Corp"
  license     = "MIT"
}

provides {
  connector "salesforce" {
    wasm       = "connector.wasm"

    # Configuration schema
    config {
      instance_url  = string { required = true }
      client_id     = string { required = true }
      client_secret = string { required = true, sensitive = true }
      api_version   = string { default = "v58.0" }
    }
  }

  # Optional: custom functions
  functions {
    wasm    = "functions.wasm"
    exports = ["sf_format_id", "sf_parse_date"]
  }
}
```

### 3.3 Plugin Declaration (Declarative)

```hcl
# plugins.hcl
plugins {
  # Local plugin (already in filesystem)
  salesforce {
    source = "./plugins/salesforce"
  }

  # Git plugin (auto-downloaded)
  sap {
    source  = "github.com/acme/mycel-sap"
    version = "1.0.0"
  }

  # Registry plugin (future)
  stripe {
    source  = "registry.mycel.dev/stripe"
    version = "~> 2.0"
  }
}
```

### 3.4 Plugin Usage

```hcl
# connectors.hcl
connector "crm" {
  type = "salesforce"  # Plugin name

  instance_url  = env("SF_URL")
  client_id     = env("SF_CLIENT_ID")
  client_secret = env("SF_SECRET")
}

# flows.hcl
flow "sync_leads" {
  from {
    connector = "api"
    operation = "POST /leads"
  }

  transform {
    sf_id = "sf_format_id(input.external_id)"  # Plugin function
    name  = "input.name"
    email = "input.email"
  }

  to {
    connector = "crm"
    target    = "Lead"
  }
}
```

### 3.5 WASM Connector Interface

```
┌─────────────────────────────────────────────────────────────┐
│                    Connector Interface                       │
├─────────────────────────────────────────────────────────────┤
│ Lifecycle:                                                   │
│   init(config_ptr, config_len) -> status                    │
│   health() -> status                                         │
│   close()                                                    │
├─────────────────────────────────────────────────────────────┤
│ Operations:                                                  │
│   read(query_ptr, query_len) -> (result_ptr, result_len)    │
│   write(data_ptr, data_len) -> (result_ptr, result_len)     │
│   call(op_ptr, op_len, params_ptr, params_len) -> (r, len)  │
└─────────────────────────────────────────────────────────────┘
```

**Status codes:**
- `0` = success
- `1` = error (check error buffer)

**Query JSON:**
```json
{
  "target": "Contact",
  "filter": {"email": "john@example.com"},
  "limit": 10,
  "offset": 0
}
```

**Write JSON:**
```json
{
  "target": "Lead",
  "operation": "insert",
  "data": {"name": "John", "email": "john@example.com"}
}
```

**Result JSON:**
```json
{
  "data": [...],
  "error": null,
  "metadata": {
    "affected": 1,
    "id": "001xx000003DGXXX"
  }
}
```

### 3.6 Plugin Loading Flow

```
┌─────────────────────────────────────────────────────────────┐
│                     mycel start                              │
│                          │                                   │
│                          ▼                                   │
│               ┌─────────────────────┐                       │
│               │  Parse plugins.hcl  │                       │
│               └──────────┬──────────┘                       │
│                          │                                   │
│         ┌────────────────┼────────────────┐                 │
│         ▼                ▼                ▼                 │
│    ┌─────────┐     ┌──────────┐    ┌───────────┐           │
│    │ Local   │     │   Git    │    │ Registry  │           │
│    │ (exists)│     │ (clone)  │    │ (download)│           │
│    └─────────┘     └──────────┘    └───────────┘           │
│         │                │                │                 │
│         └────────────────┴────────────────┘                 │
│                          │                                   │
│                          ▼                                   │
│               ┌─────────────────────┐                       │
│               │ Parse plugin.hcl    │                       │
│               │ Load WASM modules   │                       │
│               │ Register connectors │                       │
│               │ Register functions  │                       │
│               └─────────────────────┘                       │
│                          │                                   │
│                          ▼                                   │
│                    Runtime ready                             │
└─────────────────────────────────────────────────────────────┘
```

### 3.7 Plugin Cache (for Git/Registry)

```
/var/cache/mycel/plugins/
├── github.com/
│   └── acme/
│       ├── mycel-sap@1.0.0/
│       └── mycel-stripe@2.1.0/
└── registry.mycel.dev/
    └── salesforce@1.0.0/
```

---

## 4. WASM Runtime (wazero)

### 4.1 Why wazero

- **Pure Go**: No CGO dependencies (aligns with Mycel philosophy)
- **Fast**: Near-native performance
- **Secure**: Sandboxed execution
- **WASI support**: For I/O operations if needed

### 4.2 Memory Management

WASM linear memory is used for passing data:

```go
// Allocate memory in WASM
allocFn := module.ExportedFunction("alloc")
ptr, _ := allocFn.Call(ctx, uint64(len(data)))

// Write data to WASM memory
module.Memory().Write(uint32(ptr[0]), data)

// Call function
resultPtr, resultLen := fn.Call(ctx, ptr[0], uint64(len(data)))

// Read result from WASM memory
result, _ := module.Memory().Read(uint32(resultPtr[0]), uint32(resultLen[0]))

// Free memory in WASM
freeFn := module.ExportedFunction("free")
freeFn.Call(ctx, ptr[0], uint64(len(data)))
```

### 4.3 Required WASM Exports

Every WASM module must export:

```
alloc(size: i32) -> ptr: i32      # Allocate memory
free(ptr: i32, size: i32)          # Free memory
```

Plus the specific functions for the module type (validate, read, write, etc.)

### 4.4 Error Handling

Errors are returned as JSON in the result:

```json
{
  "error": "Connection refused",
  "code": "CONNECTION_ERROR",
  "details": {
    "host": "api.salesforce.com",
    "port": 443
  }
}
```

---

## 5. CLI Commands

### Development Helpers (not for production)

```bash
# Plugin management (development only)
mycel plugin install ./plugins/salesforce
mycel plugin install github.com/acme/mycel-sap@1.0.0
mycel plugin list
mycel plugin info salesforce
mycel plugin remove salesforce

# Validate WASM module
mycel wasm validate ./validators/cuit.wasm --type=validator
mycel wasm validate ./functions/pricing.wasm --type=functions
mycel wasm validate ./plugins/salesforce/connector.wasm --type=connector

# Test validator
mycel validator test argentina_cuit "20-12345678-9"
mycel validator test strong_password "weakpass"
```

---

## 6. Docker Integration

### 6.1 Volume Mounts

```yaml
services:
  mycel:
    image: mycel:latest
    volumes:
      - ./config:/etc/mycel
      - ./plugins:/etc/mycel/plugins      # Local plugins
      - ./validators:/etc/mycel/validators # WASM validators
      - ./functions:/etc/mycel/functions   # WASM functions
    environment:
      - MYCEL_ENV=production
```

### 6.2 Custom Image with Plugins

```dockerfile
FROM mycel:latest

# Copy plugins
COPY plugins/ /etc/mycel/plugins/

# Copy WASM validators and functions
COPY validators/ /etc/mycel/validators/
COPY functions/ /etc/mycel/functions/
```

### 6.3 Plugin Auto-download

For Git-based plugins, Mycel downloads to cache on first start:

```yaml
services:
  mycel:
    image: mycel:latest
    volumes:
      - ./config:/etc/mycel
      - plugin-cache:/var/cache/mycel/plugins  # Persist downloads

volumes:
  plugin-cache:
```

---

## 7. File Structure

```
/etc/mycel/
├── config.hcl           # Service configuration
├── plugins.hcl          # Plugin declarations
├── validators/          # Validator definitions + WASM
│   ├── validators.hcl   # Regex/CEL validators
│   ├── cuit.wasm
│   └── luhn.wasm
├── functions/           # WASM functions
│   ├── functions.hcl    # Function declarations
│   ├── pricing.wasm
│   └── geo.wasm
├── plugins/             # Local plugins
│   └── salesforce/
│       ├── plugin.hcl
│       └── connector.wasm
├── connectors/
├── flows/
└── types/
```

---

## 8. Implementation Checklist

### Phase 5a: Validators (regex/CEL)
- [ ] Define `ValidatorConfig` struct
- [ ] Parser for `validator` blocks
- [ ] `RegexValidator` implementation
- [ ] `CELValidator` implementation
- [ ] Integration with type validation
- [ ] Tests for regex validators
- [ ] Tests for CEL validators
- [ ] Example: `examples/validators/`

### Phase 5b: WASM Runtime
- [ ] Add wazero dependency
- [ ] Create `internal/wasm/runtime.go`
- [ ] Memory management helpers (alloc/free)
- [ ] JSON serialization for WASM I/O
- [ ] Error handling
- [ ] Tests for WASM runtime

### Phase 5c: Validators WASM
- [ ] `WASMValidator` implementation
- [ ] Parser for `type = "wasm"` validators
- [ ] Hot reload for .wasm files
- [ ] Tests for WASM validators
- [ ] Example validator in Rust

### Phase 5d: Functions WASM
- [ ] Parser for `functions` blocks
- [ ] Function registration in CEL
- [ ] Dynamic function calling
- [ ] Tests for WASM functions
- [ ] Example functions in Rust

### Phase 5e: Plugins
- [ ] Parser for `plugins.hcl`
- [ ] Parser for `plugin.hcl` manifest
- [ ] Plugin loader (local)
- [ ] Plugin loader (git)
- [ ] WASM connector adapter
- [ ] Plugin registry in runtime
- [ ] CLI: `mycel plugin install/list/remove`
- [ ] Tests for plugin system
- [ ] Example plugin

---

## 9. Example: Complete Validator in Rust

```rust
// validators/cuit/src/lib.rs
use serde::{Deserialize, Serialize};
use std::alloc::{alloc, dealloc, Layout};

#[derive(Deserialize)]
struct Input {
    value: String,
}

// Memory allocation for host
#[no_mangle]
pub extern "C" fn alloc(size: i32) -> *mut u8 {
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { alloc(layout) }
}

#[no_mangle]
pub extern "C" fn free(ptr: *mut u8, size: i32) {
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { dealloc(ptr, layout) }
}

#[no_mangle]
pub extern "C" fn validate(ptr: *const u8, len: i32) -> i32 {
    // Read input from memory
    let slice = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    let input: Input = match serde_json::from_slice(slice) {
        Ok(v) => v,
        Err(_) => return 1,
    };

    // Validate CUIT
    if validate_cuit(&input.value) {
        0 // valid
    } else {
        1 // invalid
    }
}

fn validate_cuit(cuit: &str) -> bool {
    // Remove hyphens
    let clean: String = cuit.chars().filter(|c| c.is_numeric()).collect();

    if clean.len() != 11 {
        return false;
    }

    // CUIT validation algorithm
    let multipliers = [5, 4, 3, 2, 7, 6, 5, 4, 3, 2];
    let digits: Vec<u32> = clean.chars()
        .filter_map(|c| c.to_digit(10))
        .collect();

    if digits.len() != 11 {
        return false;
    }

    let sum: u32 = digits.iter()
        .take(10)
        .zip(multipliers.iter())
        .map(|(d, m)| d * m)
        .sum();

    let remainder = sum % 11;
    let check_digit = if remainder == 0 { 0 } else { 11 - remainder };

    check_digit == digits[10]
}
```

Build:
```bash
cd validators/cuit
cargo build --target wasm32-unknown-unknown --release
cp target/wasm32-unknown-unknown/release/cuit.wasm ../cuit.wasm
```

---

## 10. Summary

| Extension | Regex | CEL | WASM |
|-----------|-------|-----|------|
| **Validators** | ✅ Patterns | ✅ Expressions | ✅ Complex logic |
| **Transforms** | ❌ | ✅ Built-in | ✅ Custom functions |
| **Connectors** | ❌ | ❌ | ✅ Plugins |

**Philosophy:** Scale complexity gradually. 80% of cases are solved with regex/CEL, the remaining 20% with WASM.
