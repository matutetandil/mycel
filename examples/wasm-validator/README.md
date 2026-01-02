# WASM Validator Example

This example demonstrates how to create a custom validator using WebAssembly.

## Prerequisites

To build WASM validators, you need:
- Rust with `wasm32-unknown-unknown` target
- Or any language that compiles to WASM

```bash
# Install Rust WASM target
rustup target add wasm32-unknown-unknown
```

## Creating a WASM Validator

### 1. Create a Rust Project

```bash
cargo new --lib cuit_validator
cd cuit_validator
```

### 2. Configure Cargo.toml

```toml
[package]
name = "cuit_validator"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"

[profile.release]
opt-level = "s"
lto = true
```

### 3. Implement the Validator (src/lib.rs)

```rust
use serde::Deserialize;
use std::alloc::{alloc, dealloc, Layout};
use std::slice;

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
    if ptr.is_null() {
        return;
    }
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { dealloc(ptr, layout) }
}

/// Validate function - entry point called by Mycel
/// Returns 0 if valid, 1 if invalid
#[no_mangle]
pub extern "C" fn validate(ptr: *const u8, len: i32) -> i32 {
    // Read input from memory
    let slice = unsafe { slice::from_raw_parts(ptr, len as usize) };
    
    let input: Input = match serde_json::from_slice(slice) {
        Ok(v) => v,
        Err(_) => return 1, // Parse error = invalid
    };

    // Validate Argentine CUIT
    if validate_cuit(&input.value) {
        0 // Valid
    } else {
        1 // Invalid
    }
}

fn validate_cuit(cuit: &str) -> bool {
    // Remove hyphens and spaces
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

### 4. Build the WASM Module

```bash
cargo build --target wasm32-unknown-unknown --release
cp target/wasm32-unknown-unknown/release/cuit_validator.wasm ./validators/
```

### 5. Use in Mycel Configuration

```hcl
# validators.hcl
validator "argentina_cuit" {
  type       = "wasm"
  wasm       = "./validators/cuit_validator.wasm"
  entrypoint = "validate"
  message    = "Invalid Argentine CUIT"
}

# types.hcl
type "company" {
  cuit = string { validate = "validator.argentina_cuit" }
}
```

## WASM Interface Specification

### Required Exports

Your WASM module MUST export these functions:

| Function | Signature | Description |
|----------|-----------|-------------|
| `alloc` | `(size: i32) -> ptr: i32` | Allocate memory |
| `free` | `(ptr: i32, size: i32)` | Free memory |
| `validate` | `(ptr: i32, len: i32) -> i32` | Validate function |

### Validate Function

- **Input**: JSON-encoded `{"value": <value>}` in linear memory
- **Output**: Status code
  - `0` = Valid
  - `1` = Invalid (use default message)
  - `>1` = Can be pointer to custom error message (advanced)

### Memory Management

1. Mycel calls `alloc(size)` to allocate memory
2. Mycel writes input JSON to that memory
3. Mycel calls `validate(ptr, len)`
4. WASM validates and returns status
5. Mycel calls `free(ptr, size)` to clean up

## Testing Your WASM Validator

```bash
# Build
cargo build --target wasm32-unknown-unknown --release

# Test with wasmtime (optional)
wasmtime run --invoke validate target/wasm32-unknown-unknown/release/cuit_validator.wasm

# Use in Mycel
mycel validate --config ./examples/wasm-validator
```

## Tips

1. **Keep it small**: WASM modules are loaded into memory
2. **No I/O**: WASM validators can't do network calls or file I/O
3. **Pure functions**: Validators should be pure (no side effects)
4. **Error handling**: Return 1 for any error condition
