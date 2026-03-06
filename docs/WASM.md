# WebAssembly (WASM) in Mycel

Mycel uses [wazero](https://github.com/tetratelabs/wazero), a pure Go WebAssembly runtime (no CGO), to extend functionality with custom validators, functions, and connector plugins.

## Supported Languages

Any language that compiles to WebAssembly can be used with Mycel:

| Language | Target | Toolchain | Best For |
|----------|--------|-----------|----------|
| **Rust** | `wasm32-unknown-unknown` | `cargo` + `rustup` | Performance, safety, mature WASM ecosystem |
| **Go** | `wasm32-wasi` | [TinyGo](https://tinygo.org/) | Go developers, simple logic |
| **C** | `wasm32-wasi` | Clang / Emscripten | Low-level, legacy code reuse |
| **C++** | `wasm32-wasi` | Clang / Emscripten | Complex algorithms, existing C++ libs |
| **AssemblyScript** | native WASM | `asc` compiler | TypeScript developers, quick prototyping |
| **Zig** | `wasm32-wasi` | `zig build` | Performance, C interop, no hidden allocator |

> **Note:** Standard Go (`GOOS=wasip1 GOARCH=wasm`) produces modules that include the Go runtime and garbage collector (~2MB+). Use **TinyGo** instead for small, efficient WASM modules.

---

## WASM Interface

All WASM modules communicate with Mycel through linear memory using JSON serialization.

### Required Exports

Every WASM module **must** export these memory management functions:

| Function | Signature | Description |
|----------|-----------|-------------|
| `alloc` | `(size: i32) -> ptr: i32` | Allocate `size` bytes, return pointer |
| `free` | `(ptr: i32, size: i32)` | Free previously allocated memory |

Mycel also accepts `malloc`/`dealloc` as alternative names.

### Validator Exports

```
validate(ptr: i32, len: i32) -> i32
```

- **Input**: JSON `{"value": <the_value>}` written to linear memory at `ptr`
- **Returns**: `0` = valid, `1` = invalid

### Function Exports

```
my_function(ptr: i32, len: i32) -> (ptr: i32, len: i32)
```

- **Input**: JSON `{"args": [arg1, arg2, ...]}` written to linear memory
- **Returns**: pointer and length of output JSON `{"result": <value>}` or `{"error": "message"}`

### Connector Plugin Exports

```
init(ptr: i32, len: i32) -> (ptr: i32, len: i32)   # Required
read(ptr: i32, len: i32) -> (ptr: i32, len: i32)    # Required
write(ptr: i32, len: i32) -> (ptr: i32, len: i32)   # Required
health() -> (ptr: i32, len: i32)                     # Optional
close() -> (ptr: i32, len: i32)                      # Optional
```

### Memory Flow

```
1. Host calls alloc(size) → gets ptr
2. Host writes JSON input to memory at ptr
3. Host calls function(ptr, len)
4. WASM reads input, processes, writes output to new allocation
5. WASM returns (output_ptr, output_len)
6. Host reads output from memory
7. Host calls free() to clean up
```

---

## Examples by Language

All examples implement the same validator: a modulo-11 check digit algorithm (Argentine CUIT/CUIL).

### Rust

The recommended language for WASM — smallest output, best tooling.

**Setup:**
```bash
rustup target add wasm32-unknown-unknown
cargo new --lib cuit_validator
```

**Cargo.toml:**
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

**src/lib.rs:**
```rust
use serde::Deserialize;
use std::alloc::{alloc as rust_alloc, dealloc, Layout};
use std::slice;

#[derive(Deserialize)]
struct Input {
    value: String,
}

#[no_mangle]
pub extern "C" fn alloc(size: i32) -> *mut u8 {
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { rust_alloc(layout) }
}

#[no_mangle]
pub extern "C" fn free(ptr: *mut u8, size: i32) {
    if ptr.is_null() { return; }
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { dealloc(ptr, layout) }
}

#[no_mangle]
pub extern "C" fn validate(ptr: *const u8, len: i32) -> i32 {
    let slice = unsafe { slice::from_raw_parts(ptr, len as usize) };
    let input: Input = match serde_json::from_slice(slice) {
        Ok(v) => v,
        Err(_) => return 1,
    };

    let clean: Vec<u32> = input.value.chars()
        .filter(|c| c.is_numeric())
        .filter_map(|c| c.to_digit(10))
        .collect();

    if clean.len() != 11 { return 1; }

    let weights = [5, 4, 3, 2, 7, 6, 5, 4, 3, 2];
    let sum: u32 = clean.iter().zip(weights.iter()).map(|(d, w)| d * w).sum();
    let check = match sum % 11 { 0 => 0, r => 11 - r };

    if check == clean[10] { 0 } else { 1 }
}
```

**Build:**
```bash
cargo build --target wasm32-unknown-unknown --release
cp target/wasm32-unknown-unknown/release/cuit_validator.wasm ./validators/
```

---

### Go (TinyGo)

Best option for Go developers. Requires [TinyGo](https://tinygo.org/getting-started/install/).

**main.go:**
```go
//go:build tinygo

package main

import (
	"encoding/json"
	"unsafe"
)

type Input struct {
	Value string `json:"value"`
}

//export alloc
func wasmAlloc(size int32) *byte {
	buf := make([]byte, size)
	return &buf[0]
}

//export free
func wasmFree(ptr *byte, size int32) {
	// TinyGo GC handles deallocation
}

//export validate
func validate(ptr *byte, length int32) int32 {
	// Read input from memory
	data := unsafe.Slice(ptr, length)

	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		return 1
	}

	// Extract digits
	var digits []int
	for _, c := range input.Value {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) != 11 {
		return 1
	}

	// CUIT check digit
	weights := []int{5, 4, 3, 2, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i, w := range weights {
		sum += digits[i] * w
	}
	check := 0
	if r := sum % 11; r != 0 {
		check = 11 - r
	}

	if check == digits[10] {
		return 0
	}
	return 1
}

func main() {}
```

**Build:**
```bash
tinygo build -o validator.wasm -target wasi -no-debug main.go
```

---

### C

Minimal output size, works with Clang or Emscripten.

**validator.c:**
```c
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

// Memory management exports for the host
__attribute__((export_name("alloc")))
void* wasm_alloc(int size) {
    return malloc(size);
}

__attribute__((export_name("free")))
void wasm_free(void* ptr, int size) {
    free(ptr);
}

// Minimal JSON parser — extracts the "value" string
// Looks for "value":"..." in the JSON input
static int extract_value(const char* json, int json_len, char* out, int out_max) {
    const char* key = "\"value\"";
    const char* p = json;
    const char* end = json + json_len;

    // Find "value"
    while (p < end - 7) {
        if (memcmp(p, key, 7) == 0) break;
        p++;
    }
    if (p >= end - 7) return -1;

    // Skip to the string value after the colon
    p += 7;
    while (p < end && (*p == ':' || *p == ' ' || *p == '"')) p++;
    if (p >= end) return -1;

    // Copy until closing quote
    int i = 0;
    while (p < end && *p != '"' && i < out_max - 1) {
        out[i++] = *p++;
    }
    out[i] = '\0';
    return i;
}

__attribute__((export_name("validate")))
int validate(const char* ptr, int len) {
    char value[64];
    if (extract_value(ptr, len, value, sizeof(value)) < 0) {
        return 1;
    }

    // Extract digits
    int digits[11];
    int count = 0;
    for (int i = 0; value[i] && count < 11; i++) {
        if (isdigit(value[i])) {
            digits[count++] = value[i] - '0';
        }
    }
    if (count != 11) return 1;

    // CUIT check digit
    int weights[] = {5, 4, 3, 2, 7, 6, 5, 4, 3, 2};
    int sum = 0;
    for (int i = 0; i < 10; i++) {
        sum += digits[i] * weights[i];
    }
    int check = (sum % 11 == 0) ? 0 : 11 - (sum % 11);

    return (check == digits[10]) ? 0 : 1;
}
```

**Build (Clang):**
```bash
clang --target=wasm32-wasi \
  --sysroot=/path/to/wasi-sysroot \
  -O2 -nostartfiles \
  -Wl,--no-entry \
  -Wl,--export=alloc \
  -Wl,--export=free \
  -Wl,--export=validate \
  -o validator.wasm \
  validator.c
```

**Build (Emscripten):**
```bash
emcc validator.c -O2 \
  -s STANDALONE_WASM=1 \
  -s EXPORTED_FUNCTIONS='["_alloc","_free","_validate"]' \
  --no-entry \
  -o validator.wasm
```

> **Tip:** Install the WASI SDK for the sysroot: https://github.com/WebAssembly/wasi-sdk

---

### C++

Same toolchain as C, but with C++ features. Use `extern "C"` for exports.

**validator.cpp:**
```cpp
#include <cstdlib>
#include <cstring>
#include <vector>
#include <string>

extern "C" {

__attribute__((export_name("alloc")))
void* wasm_alloc(int size) {
    return std::malloc(size);
}

__attribute__((export_name("free")))
void wasm_free(void* ptr, int size) {
    std::free(ptr);
}

// Extract "value" string from JSON input
static std::string extract_value(const char* json, int len) {
    std::string s(json, len);
    auto pos = s.find("\"value\"");
    if (pos == std::string::npos) return "";

    pos = s.find('"', pos + 7);   // skip key
    pos = s.find('"', pos + 1);   // opening quote of value
    if (pos == std::string::npos) return "";
    pos++;

    auto end = s.find('"', pos);
    if (end == std::string::npos) return "";

    return s.substr(pos, end - pos);
}

__attribute__((export_name("validate")))
int validate(const char* ptr, int len) {
    std::string value = extract_value(ptr, len);
    if (value.empty()) return 1;

    // Extract digits
    std::vector<int> digits;
    for (char c : value) {
        if (c >= '0' && c <= '9') {
            digits.push_back(c - '0');
        }
    }
    if (digits.size() != 11) return 1;

    // CUIT check digit
    int weights[] = {5, 4, 3, 2, 7, 6, 5, 4, 3, 2};
    int sum = 0;
    for (int i = 0; i < 10; i++) {
        sum += digits[i] * weights[i];
    }
    int check = (sum % 11 == 0) ? 0 : 11 - (sum % 11);

    return (check == digits[10]) ? 0 : 1;
}

} // extern "C"
```

**Build:**
```bash
clang++ --target=wasm32-wasi \
  --sysroot=/path/to/wasi-sysroot \
  -O2 -nostartfiles -fno-exceptions \
  -Wl,--no-entry \
  -Wl,--export=alloc \
  -Wl,--export=free \
  -Wl,--export=validate \
  -o validator.wasm \
  validator.cpp
```

---

### AssemblyScript

TypeScript-like syntax, compiles directly to WASM. Great for web developers.

**Setup:**
```bash
npm init -y
npm install --save-dev assemblyscript
npx asinit .
```

**assembly/index.ts:**
```typescript
// Memory exports are automatic in AssemblyScript (uses its built-in allocator)

// Helper: export alloc for Mycel host
export function alloc(size: i32): usize {
  return heap.alloc(size) as usize;
}

// Helper: export free for Mycel host
export function free(ptr: usize, size: i32): void {
  heap.free(changetype<usize>(ptr));
}

// Parse the "value" field from JSON input bytes
function extractValue(ptr: usize, len: i32): string {
  let json = String.UTF8.decodeUnsafe(ptr, len);
  let key = '"value"';
  let idx = json.indexOf(key);
  if (idx < 0) return "";

  // Find opening quote of value
  let start = json.indexOf('"', idx + key.length + 1);
  if (start < 0) return "";
  start++;

  let end = json.indexOf('"', start);
  if (end < 0) return "";

  return json.substring(start, end);
}

export function validate(ptr: usize, len: i32): i32 {
  let value = extractValue(ptr, len);
  if (value.length == 0) return 1;

  // Extract digits
  let digits: i32[] = [];
  for (let i = 0; i < value.length; i++) {
    let code = value.charCodeAt(i);
    if (code >= 48 && code <= 57) {  // '0'-'9'
      digits.push(code - 48);
    }
  }
  if (digits.length != 11) return 1;

  // CUIT check digit
  let weights: i32[] = [5, 4, 3, 2, 7, 6, 5, 4, 3, 2];
  let sum: i32 = 0;
  for (let i = 0; i < 10; i++) {
    sum += digits[i] * weights[i];
  }
  let remainder = sum % 11;
  let check = remainder == 0 ? 0 : 11 - remainder;

  return check == digits[10] ? 0 : 1;
}
```

**Build:**
```bash
npx asc assembly/index.ts \
  --target release \
  --exportRuntime \
  --outFile validator.wasm
```

---

### Zig

No hidden allocator, excellent WASM support, C-compatible ABI.

**validator.zig:**
```zig
const std = @import("std");

// Use a page allocator for WASM
var gpa = std.heap.page_allocator;

export fn alloc(size: i32) [*]u8 {
    const s: usize = @intCast(size);
    const slice = gpa.alloc(u8, s) catch return @ptrFromInt(0);
    return slice.ptr;
}

export fn free(ptr: [*]u8, size: i32) {
    const s: usize = @intCast(size);
    gpa.free(ptr[0..s]);
}

fn extractDigits(value: []const u8) ?[11]u8 {
    var digits: [11]u8 = undefined;
    var count: usize = 0;

    for (value) |c| {
        if (c >= '0' and c <= '9') {
            if (count >= 11) return null;
            digits[count] = c - '0';
            count += 1;
        }
    }

    if (count != 11) return null;
    return digits;
}

fn findValue(json: []const u8) ?[]const u8 {
    // Find "value":"..."
    const key = "\"value\"";
    var i: usize = 0;
    while (i + key.len <= json.len) : (i += 1) {
        if (std.mem.eql(u8, json[i .. i + key.len], key)) {
            // Skip to value string
            var j = i + key.len;
            while (j < json.len and (json[j] == ':' or json[j] == ' ')) : (j += 1) {}
            if (j >= json.len or json[j] != '"') return null;
            j += 1; // skip opening quote
            const start = j;
            while (j < json.len and json[j] != '"') : (j += 1) {}
            return json[start..j];
        }
    }
    return null;
}

export fn validate(ptr: [*]const u8, len: i32) i32 {
    const l: usize = @intCast(len);
    const json = ptr[0..l];

    const value = findValue(json) orelse return 1;
    const digits = extractDigits(value) orelse return 1;

    // CUIT check digit
    const weights = [10]u32{ 5, 4, 3, 2, 7, 6, 5, 4, 3, 2 };
    var sum: u32 = 0;
    for (weights, 0..) |w, i| {
        sum += @as(u32, digits[i]) * w;
    }

    const remainder = sum % 11;
    const check: u8 = if (remainder == 0) 0 else @intCast(11 - remainder);

    return if (check == digits[10]) 0 else 1;
}
```

**Build:**
```bash
zig build-lib validator.zig \
  -target wasm32-wasi \
  -OReleaseSafe \
  --name validator
# Output: validator.wasm
```

---

## HCL Configuration

### Validators

```hcl
validator "argentina_cuit" {
  type       = "wasm"
  wasm       = "./validators/cuit.wasm"
  entrypoint = "validate"
  message    = "Invalid Argentine CUIT"
}
```

### Functions

```hcl
functions "pricing" {
  wasm    = "./functions/pricing.wasm"
  exports = ["calculate_price", "apply_discount", "tax_for_country"]
}
```

Functions are available in CEL transforms:

```hcl
transform {
  subtotal = "calculate_price(input.items)"
  discount = "apply_discount(subtotal, input.coupon_code)"
  tax      = "tax_for_country(discount, input.country)"
}
```

### Plugins (Connector)

```hcl
plugin "salesforce" {
  source = "./plugins/salesforce"
}
```

See [Plugin Example](../examples/plugin/) for the full plugin manifest and connector interface.

---

## Size Comparison

Approximate `.wasm` sizes for the CUIT validator example:

| Language | Release Size | Notes |
|----------|-------------|-------|
| C | ~2 KB | Smallest, no runtime |
| Zig | ~4 KB | No hidden allocator |
| Rust | ~20 KB | With serde_json; ~3 KB without |
| AssemblyScript | ~8 KB | Includes managed runtime |
| Go (TinyGo) | ~50 KB | Includes GC + reflect |

> **Tip:** For validators that don't need JSON parsing, C and Zig produce the smallest modules. For functions that return complex JSON, Rust with serde is the most ergonomic choice.

---

## Best Practices

1. **Keep modules small** — each module is loaded into memory at startup
2. **No I/O** — WASM modules cannot make network calls or access the filesystem
3. **Pure functions** — validators and functions should have no side effects
4. **Use `--release`** — always build with optimizations enabled
5. **Test locally** — use `wasmtime` or `wasmer` to test modules before deploying
6. **Hot reload** — Mycel reloads `.wasm` files automatically when they change

## See Also

- [WASM Validator Example](../examples/wasm-validator/) — Complete Rust validator
- [WASM Functions Example](../examples/wasm-functions/) — Custom CEL functions in Rust
- [Plugin Example](../examples/plugin/) — Custom connector via WASM
- [CONCEPTS.md](./CONCEPTS.md#wasm) — WASM section in concepts overview
- [Phase 5 Spec](./PHASE-5-EXTENSIBILITY.md) — Full extensibility specification
