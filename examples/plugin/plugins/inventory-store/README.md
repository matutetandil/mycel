# inventory-store

A connector type Mycel does not ship, written as a plugin.

The runtime knows nothing about stock levels. This plugin adds a connector type
called `inventory_store`, and from there a flow names it exactly like `postgres`
or `rabbitmq`:

```hcl
connector "stock" {
  type      = "inventory_store"
  warehouse = "auckland"
}
```

`warehouse` is declared in `plugin.mycel`, not in Mycel — that is what a plugin
manifest's `config` block is for.

## What it does

| Function | Reached by | What it does |
|---|---|---|
| `init` | startup | Receives the connector block's attributes; opening stock |
| `read` | a flow reading from it | Answers with stock, optionally filtered by `sku` |
| `write` | a flow writing to it | Adjusts a SKU by `delta` |
| `call` | a `step` naming an `operation` | `reserve`, which refuses to take more than there is |

## Building it

```bash
rustup target add wasm32-unknown-unknown
cargo build --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/inventory_store.wasm connector.wasm
```

`connector.wasm` is committed next to the manifest, so the example runs without
a Rust toolchain.

## The interface

Memory is the plugin's: `alloc` and `free` are exported, the host allocates
through them, and a function receives `(ptr, len)` pointing at JSON.

An answer is a pointer and a length **packed into one i64** — `ptr << 32 | len`.
Two separate results is the other form the host accepts, and no toolchain can
emit it: both Rust and TinyGo lower a two-word return through the C ABI, which
becomes a pointer argument rather than two results. The packed form is what a
plugin in any language can actually return.

A read answers `{"rows": [...]}` (`{"data": [...]}` is accepted too), a write
`{"affected": n, "rows": [...]}`, a call `{"data": {...}}`, and any of them
`{"error": "..."}` — which reaches the flow as a failure, as `reserve` does
when there is not enough stock.
