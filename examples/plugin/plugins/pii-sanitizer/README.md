# pii-sanitizer

A sanitiser Mycel does not ship, written as a plugin.

The built-in pipeline refuses what everybody has to refuse — SQL fragments, XML
entities, null bytes, control characters. What it cannot know is what counts as
sensitive in your business. This one masks card numbers and refuses anything
carrying a New Zealand IRD number, because those must not reach a log, a queue
or a third party.

## What it does

| Input | Answer |
|---|---|
| `{"card": "4111 1111 1111 1111"}` | `{"card": "****1111"}` |
| `{"tax_number": "123-456-789"}` | refused — the request is turned away |
| anything else | unchanged |

It walks the whole value, so a card number nested under `customer.payment` is
masked as surely as one at the top.

## Building it

```bash
rustup target add wasm32-unknown-unknown
cargo build --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/pii_sanitizer.wasm sanitizer.wasm
```

`sanitizer.wasm` is committed, so this runs without a Rust toolchain.

## The interface

```
sanitize(ptr: i32, len: i32) -> i64
```

The module receives the value as JSON and answers with the value to use
instead — a pointer and a length packed into one i64, `ptr << 32 | len`.

**A length of zero is a refusal**, and the request is turned away before any
flow runs. That is the difference between this and filtering a response: what
is refused here never reaches a flow, a log or a downstream system.

Memory is the module's: `alloc` and `free` are exported and the host allocates
through them. See [../../../../docs/advanced/wasm.md](../../../../docs/advanced/wasm.md)
for the full interface.
