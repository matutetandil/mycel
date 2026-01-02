# WASM Functions Example

This example demonstrates how to extend Mycel with custom functions written in WebAssembly (WASM).

## Overview

WASM functions allow you to:
- Add custom business logic as CEL functions
- Use any language that compiles to WASM (Rust, Go, AssemblyScript, etc.)
- Call functions directly in transform expressions

## Function Interface

Each WASM function must:
1. Export `alloc(size: i32) -> *mut u8` for memory allocation
2. Export `free(ptr: *mut u8, size: i32)` for memory deallocation
3. Export the actual function(s) with signature: `fn(ptr: i32, len: i32) -> (ptr: i32, len: i32)`

### Input JSON Format

```json
{
  "args": [arg1, arg2, ...]
}
```

### Output JSON Format

```json
{
  "result": <value>,
  "error": null
}
// or on error:
{
  "result": null,
  "error": "error message"
}
```

## Example: Pricing Functions in Rust

### Rust Code (`src/lib.rs`)

```rust
use serde::{Deserialize, Serialize};
use std::alloc::{alloc, dealloc, Layout};

#[derive(Deserialize)]
struct Input {
    args: Vec<serde_json::Value>,
}

#[derive(Serialize)]
struct Output {
    result: Option<serde_json::Value>,
    error: Option<String>,
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

fn parse_input(ptr: *const u8, len: i32) -> Result<Input, String> {
    let slice = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    serde_json::from_slice(slice).map_err(|e| e.to_string())
}

fn write_output(output: Output) -> (i32, i32) {
    let json = serde_json::to_vec(&output).unwrap();
    let len = json.len() as i32;
    let ptr = alloc(len);
    unsafe {
        std::ptr::copy_nonoverlapping(json.as_ptr(), ptr, json.len());
    }
    (ptr as i32, len)
}

/// Calculate total price from array of items
/// Input: { "args": [items] } where items = [{"price": f64, "quantity": i64}, ...]
/// Output: { "result": total_price }
#[no_mangle]
pub extern "C" fn calculate_price(ptr: *const u8, len: i32) -> (i32, i32) {
    let input = match parse_input(ptr, len) {
        Ok(v) => v,
        Err(e) => return write_output(Output { result: None, error: Some(e) }),
    };

    if input.args.is_empty() {
        return write_output(Output {
            result: None,
            error: Some("calculate_price requires 1 argument (items array)".to_string()),
        });
    }

    let items = match input.args[0].as_array() {
        Some(arr) => arr,
        None => return write_output(Output {
            result: None,
            error: Some("first argument must be an array of items".to_string()),
        }),
    };

    let mut total = 0.0;
    for item in items {
        let price = item.get("price").and_then(|v| v.as_f64()).unwrap_or(0.0);
        let quantity = item.get("quantity").and_then(|v| v.as_i64()).unwrap_or(1) as f64;
        total += price * quantity;
    }

    write_output(Output {
        result: Some(serde_json::json!(total)),
        error: None,
    })
}

/// Apply discount to a price
/// Input: { "args": [price, discount_percent] }
/// Output: { "result": discounted_price }
#[no_mangle]
pub extern "C" fn apply_discount(ptr: *const u8, len: i32) -> (i32, i32) {
    let input = match parse_input(ptr, len) {
        Ok(v) => v,
        Err(e) => return write_output(Output { result: None, error: Some(e) }),
    };

    if input.args.len() < 2 {
        return write_output(Output {
            result: None,
            error: Some("apply_discount requires 2 arguments (price, discount_percent)".to_string()),
        });
    }

    let price = input.args[0].as_f64().unwrap_or(0.0);
    let discount = input.args[1].as_f64().unwrap_or(0.0);

    let discounted = price * (1.0 - discount / 100.0);

    write_output(Output {
        result: Some(serde_json::json!(discounted)),
        error: None,
    })
}

/// Calculate tax for a country
/// Input: { "args": [price, country_code] }
/// Output: { "result": tax_amount }
#[no_mangle]
pub extern "C" fn tax_for_country(ptr: *const u8, len: i32) -> (i32, i32) {
    let input = match parse_input(ptr, len) {
        Ok(v) => v,
        Err(e) => return write_output(Output { result: None, error: Some(e) }),
    };

    if input.args.len() < 2 {
        return write_output(Output {
            result: None,
            error: Some("tax_for_country requires 2 arguments (price, country_code)".to_string()),
        });
    }

    let price = input.args[0].as_f64().unwrap_or(0.0);
    let country = input.args[1].as_str().unwrap_or("");

    // Simple tax rates by country
    let tax_rate = match country.to_uppercase().as_str() {
        "AR" => 0.21,  // Argentina IVA 21%
        "US" => 0.0875, // US average ~8.75%
        "UK" | "GB" => 0.20, // UK VAT 20%
        "DE" => 0.19,  // Germany 19%
        "FR" => 0.20,  // France 20%
        "BR" => 0.17,  // Brazil ~17%
        "MX" => 0.16,  // Mexico 16%
        _ => 0.0,      // No tax for unknown
    };

    let tax = price * tax_rate;

    write_output(Output {
        result: Some(serde_json::json!(tax)),
        error: None,
    })
}
```

### Cargo.toml

```toml
[package]
name = "mycel-pricing"
version = "1.0.0"
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

### Build

```bash
# Install WASM target
rustup target add wasm32-unknown-unknown

# Build
cargo build --target wasm32-unknown-unknown --release

# Copy to Mycel config
cp target/wasm32-unknown-unknown/release/mycel_pricing.wasm ./functions/pricing.wasm
```

## Mycel Configuration

### functions.hcl

```hcl
functions "pricing" {
  wasm    = "./functions/pricing.wasm"
  exports = ["calculate_price", "apply_discount", "tax_for_country"]
}
```

### flows.hcl

```hcl
flow "checkout" {
  from {
    connector = "api"
    operation = "POST /checkout"
  }

  transform {
    // Use WASM functions in CEL expressions
    subtotal = "calculate_price(input.items)"
    discount = "apply_discount(subtotal, input.discount_percent)"
    tax      = "tax_for_country(discount, input.shipping_country)"
    total    = "discount + tax"

    // Mix with built-in functions
    order_id   = "uuid()"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
```

## Testing

```bash
# Start Mycel
mycel start --config ./examples/wasm-functions

# Test checkout
curl -X POST http://localhost:3000/checkout \
  -H "Content-Type: application/json" \
  -d '{
    "items": [
      {"name": "Widget", "price": 9.99, "quantity": 2},
      {"name": "Gadget", "price": 24.99, "quantity": 1}
    ],
    "discount_percent": 10,
    "shipping_country": "AR"
  }'
```

## Notes

- WASM functions are loaded once at startup and cached
- Functions can be used anywhere CEL expressions are valid
- Hot reload will reload WASM modules when they change
- The wazero runtime is pure Go (no CGO required)
