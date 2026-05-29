# Reusable Inline Blocks

> Since v2.6.0

Many flows share the same deduplication policy, the same retry/error strategy,
the same lock backend, the same response envelope. Instead of copy-pasting those
blocks into every flow, declare each one **once** at the top level with a name,
then **reference** it from any flow with `use = "<kind>.<name>"`.

This is the same mechanism `transform` and `cache` already used — now extended
to every inline block.

```hcl
# Declare once, top level:
dedupe "standard" {
  cache = "fingerprints"
  key   = "'item:' + input.id"
  ttl   = "30d"
  fingerprint {
    id    = "output.id"
    price = "output.price"
  }
}

# Reference from any number of flows:
flow "ingest_products" {
  # ...
  dedupe { use = "dedupe.standard" }
  # ...
}
```

## Overriding

A referencing block can override individual attributes inline. Anything it does
not mention is inherited from the named base.

```hcl
flow "ingest_orders" {
  # ...
  dedupe {
    use = "dedupe.standard"
    key = "'order:' + input.id"   # override just the key
    ttl = "7d"                    # and the retention
    # cache + fingerprint inherited from "standard"
  }
}
```

The merge rules depend on the field type:

| Field type | Override behavior |
|---|---|
| Scalar (string, number, bool) | Inline value wins when set; otherwise inherits the base. |
| Map (e.g. dedupe `fingerprint`, `response` mappings) | Merged key by key — inline keys win, base-only keys are preserved. |
| Sub-block (e.g. lock `storage`, error_handling `retry`) | Replaced **wholesale** when the inline block defines one — no deep merge. |

> Note on booleans: because a bool cannot distinguish "unset" from "false", an
> inline `wait = true` overrides the base, but an omitted/`false` `wait` inherits
> the base value. To force the opposite of a base, define a separate named block.

## What's reusable

| Kind | Where the reference goes | Notes |
|---|---|---|
| `dedupe` | `flow { dedupe { use = … } }` | fingerprint map merges key by key |
| `retry` | `error_handling { retry { use = … } }` | lives inside `error_handling` |
| `lock` | `flow { lock { use = … } }` | `storage` sub-block replaced wholesale |
| `semaphore` | `flow { semaphore { use = … } }` | `storage` sub-block replaced wholesale |
| `sequence_guard` | `flow { sequence_guard { use = … } }` | `storage` sub-block replaced wholesale |
| `coordinate` | `flow { coordinate { use = … } }` | wait/signal/preflight replaced wholesale |
| `transaction` | `to { transaction { use = … } }` | inline statements replace the base's wholesale |
| `error_handling` | `flow { error_handling { use = … } }` | sub-blocks replaced wholesale; may itself reference a named `retry` |
| `accept` | `flow { accept { use = … } }` | |
| `response` | `flow { response { use = … } }` | mappings merge key by key |
| `transform` | `flow { transform { use = … } }` | (since earlier; mappings merge key by key) |
| `cache` | `flow { cache { use = … } }` | (since earlier) |

`error_response`, `on_timeout`, and `on_error` are **not** independently
nameable: they live inside `error_handling`, which holds a single one of each,
so reusing the whole named `error_handling` already covers them.

## Nesting

A named `error_handling` can itself reference a named `retry`:

```hcl
retry "resilient" {
  attempts = 5
  backoff  = "exponential"
}

error_handling "resilient" {
  retry { use = "retry.resilient" }
  on_timeout { action = "ack" }
}

flow "x" {
  error_handling { use = "error_handling.resilient" }   # resolves both levels
}
```

The references are resolved outer-first, so the nested `retry` reference is
folded in after the `error_handling` is materialized onto the flow.

## CEL scope

A named block's CEL expressions (`input.x`, `output.y`, `step.z`,
`captured.w`) are evaluated in the **consuming flow's** scope. A named block is
therefore only portable across flows that share the relevant input/output shape
— this is the author's responsibility; Mycel does not infer types across the
reference.

## Validation

References are resolved and validated at config load (parse time), not at
runtime. A `use` that names a block that does not exist fails `mycel validate`
immediately, with a message listing the available names:

```
flow "x": dedupe references unknown name "standar" (available: standard)
```

## Backward compatibility

Strictly additive. Every existing inline block — written without `use` — behaves
exactly as before. Names live in a per-kind namespace, so `flow "x"` and
`dedupe "x"` never collide.

See the runnable example in [`examples/reusable-blocks/`](../../examples/reusable-blocks/).
