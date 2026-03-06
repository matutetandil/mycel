# Mycel Plugin Example

This example demonstrates how to create a custom connector plugin for Mycel using WebAssembly (WASM).

## Plugin Structure

A Mycel plugin is a directory containing:

```
my-plugin/
├── plugin.hcl          # Plugin manifest (required)
├── connector.wasm      # WASM connector module (required for connectors)
└── functions.wasm      # WASM functions module (optional)
```

## Plugin Manifest (plugin.hcl)

The `plugin.hcl` file describes the plugin and what it provides:

```hcl
plugin {
  name        = "salesforce"
  version     = "1.0.0"
  description = "Salesforce CRM connector for Mycel"
  author      = "Your Name"
  license     = "MIT"
}

provides {
  connector "salesforce" {
    wasm = "connector.wasm"

    config {
      instance_url = "string"
      client_id    = "string"
      client_secret = {
        type      = "string"
        required  = true
        sensitive = true
      }
      username = "string"
      password = {
        type      = "string"
        sensitive = true
      }
    }
  }

  # Optional: provide custom functions for CEL expressions
  functions {
    wasm    = "functions.wasm"
    exports = ["sf_format_id", "sf_parse_date"]
  }
}
```

## Using Plugins

Declare plugins in your Mycel configuration:

```hcl
# plugins.hcl
plugin "salesforce" {
  source = "./plugins/salesforce"
}

# For git-hosted plugins (future):
# plugin "stripe" {
#   source  = "github.com/acme/mycel-stripe"
#   version = "~> 1.0"
# }
```

Then use the plugin connector like any built-in connector:

```hcl
# connectors/salesforce.hcl
connector "sf_crm" {
  type = "salesforce"  # Uses the plugin connector type

  instance_url  = env("SF_INSTANCE_URL")
  client_id     = env("SF_CLIENT_ID")
  client_secret = env("SF_CLIENT_SECRET")
  username      = env("SF_USERNAME")
  password      = env("SF_PASSWORD")
}
```

Use it in flows:

```hcl
# flows/get_accounts.hcl
flow "get_accounts" {
  from {
    connector = "api"
    operation = "GET /accounts"
  }

  to {
    connector = "sf_crm"
    target    = "Account"
    operation = "SELECT"
  }
}
```

## WASM Connector Interface

Your WASM connector must export these functions:

### Required Functions

- `init(config: JSON) -> JSON` - Initialize with configuration
- `read(query: JSON) -> JSON` - Read/query data
- `write(data: JSON) -> JSON` - Write/modify data

### Optional Functions

- `health() -> JSON` - Health check
- `close() -> JSON` - Cleanup resources
- `call(operation: JSON) -> JSON` - RPC-style operations

### Input/Output Format

All functions receive and return JSON:

**init input:**
```json
{
  "instance_url": "https://myorg.salesforce.com",
  "client_id": "...",
  "client_secret": "..."
}
```

**read input:**
```json
{
  "target": "Account",
  "operation": "SELECT",
  "filters": {"Industry": "Technology"},
  "fields": ["Id", "Name", "Industry"],
  "pagination": {"limit": 100, "offset": 0}
}
```

**write input:**
```json
{
  "target": "Account",
  "operation": "INSERT",
  "payload": {"Name": "Acme Corp", "Industry": "Technology"}
}
```

**Response format:**
```json
{
  "data": [...],
  "metadata": {
    "affected": 1,
    "id": "001xxx..."
  }
}
```

**Error response:**
```json
{
  "error": "Failed to connect: invalid credentials"
}
```

## Building WASM Connectors

### Using Go (with TinyGo)

```go
//go:build tinygo

package main

import (
    "encoding/json"
)

//export init
func Init(configPtr, configLen uint32) uint64 {
    config := readMemory(configPtr, configLen)
    // Initialize connector...
    return writeResult(map[string]interface{}{"status": "ok"})
}

//export read
func Read(queryPtr, queryLen uint32) uint64 {
    query := readMemory(queryPtr, queryLen)
    // Execute query...
    return writeResult(map[string]interface{}{
        "data": []map[string]interface{}{...},
    })
}

//export write
func Write(dataPtr, dataLen uint32) uint64 {
    data := readMemory(dataPtr, dataLen)
    // Execute write...
    return writeResult(map[string]interface{}{
        "affected": 1,
    })
}

func main() {}
```

Build with TinyGo:
```bash
tinygo build -o connector.wasm -target wasi main.go
```

### Using Rust

```rust
use serde_json::{Value, json};

#[no_mangle]
pub extern "C" fn init(config_ptr: u32, config_len: u32) -> u64 {
    let config = read_memory(config_ptr, config_len);
    // Initialize...
    write_result(json!({"status": "ok"}))
}

#[no_mangle]
pub extern "C" fn read(query_ptr: u32, query_len: u32) -> u64 {
    let query = read_memory(query_ptr, query_len);
    // Query...
    write_result(json!({
        "data": []
    }))
}

#[no_mangle]
pub extern "C" fn write(data_ptr: u32, data_len: u32) -> u64 {
    let data = read_memory(data_ptr, data_len);
    // Write...
    write_result(json!({
        "affected": 1
    }))
}
```

Build with cargo:
```bash
cargo build --target wasm32-wasi --release
```

## Memory Management

WASM modules communicate with Mycel via shared memory:

1. **Input**: Mycel writes JSON to WASM memory and passes (pointer, length)
2. **Output**: WASM function returns a packed u64: `(pointer << 32) | length`
3. **Allocation**: Export `alloc(size: u32) -> u32` for Mycel to allocate memory
4. **Deallocation**: Export `free(ptr: u32, size: u32)` for cleanup

## Testing Plugins Locally

```bash
# Start Mycel with your plugin
mycel start --config ./examples/plugin

# The plugin connector is now available
curl http://localhost:3000/accounts
```

## Plugin Distribution

Plugins can be distributed as:

1. **Local directory**: `source = "./plugins/my-plugin"`
2. **Git repository**: `source = "github.com/org/mycel-plugin"` (planned)
3. **Plugin registry**: `source = "registry.mycel.dev/plugin"` (planned)

## See Also

- [WASM Documentation](../../docs/WASM.md) — Supported languages, interface spec, examples in Rust/Go/C/C++/AssemblyScript/Zig
- [Custom Functions](../wasm-functions/)
- [Custom Validators](../wasm-validator/)
