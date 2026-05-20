# Dedupe Example

Content-based, biphasic message deduplication. Drops no-op messages
**before** forwarding to a slow downstream, in milliseconds.

## The problem

An upstream publisher re-sends update messages periodically — sometimes
because of replay-on-error semantics, sometimes because the upstream
"emits on touch" rather than "emits on change." Many of those messages
contain identical content. Each one takes seconds to process downstream
(Magento, an enterprise ERP, a slow REST API, ...). The consumer wastes
hours on duplicated work, and the queue accumulates.

A naive solution — "track which message IDs we've seen and skip
duplicates" — does not help, because each re-send has a new ID.

This example uses the `dedupe {}` block to compare the **content** of
the message against the last successfully processed content for the same
resource. If they are byte-equal, the downstream call is skipped.

## How it works

```
from(rabbit) → ... → transform → dedupe → to(magento)
                                  ↓
                          ┌───────┴────────┐
                          │ Phase A        │
                          │  GET stored fp │
                          │  compare bytes │
                          │  match? → DROP │
                          └────────────────┘
                                  ↓ no match
                              to(magento)
                                  ↓ success
                          ┌───────────────────┐
                          │ Phase B           │
                          │  SET new fp       │
                          │  (best effort)    │
                          └───────────────────┘
```

Phase B runs **only on `to` success**, so a failed-then-retried message
will not self-discard. The primitive self-locks per-key for the duration
of (Phase A + `to` + Phase B), so two workers cannot both pass Phase A
with identical fingerprints and double-call the downstream.

## The fingerprint

The fingerprint is a **canonical** encoding of a user-specified
projection of the message:

- Map keys are sorted alphabetically.
- Array elements are sorted by their encoded bytes (treated as sets).
- Every value carries a type tag and a length prefix, so e.g. the
  string `"a,b"` cannot collide with the array `["a","b"]`.
- Whole-number floats and integers normalize to the same bytes.

This guarantees:

- **Zero false discards**: two projections with the same content
  produce identical fingerprints regardless of map ordering.
- **Zero false matches**: different content always produces different
  fingerprints. Length-prefix + type-tag prevent serialization
  collisions.

## The projection is explicit

`fingerprint {}` must list every field that counts. There is no implicit
default — silent defaults would risk dropping real changes when the
author forgets to enumerate a persisted field.

In this example we fingerprint the SKU, name, parent SKU, the
per-storeview price map, and the website-visibility flags. If a future
field starts being persisted (e.g. `description`), the author must add
it to `fingerprint {}` so changes to that field are not silently
swallowed.

## Composition with other primitives

The flow combines several primitives that together make dedupe
maximally effective:

| Primitive | What it does |
|---|---|
| `lock { key = "sku_lock:..." }` | Serializes all workers across the cluster on the same SKU |
| `sequence_guard { ... }` | Drops out-of-order messages (older jobId) |
| `transform { ... }` | Computes the canonical projection |
| `dedupe { ... }` | Drops messages whose projection equals the last persisted one |
| `to { connector.magento = "POST ..." }` | The slow downstream call we are protecting |

The dedupe primitive's **internal** lock handles in-process
serialization. The user's **outer** `lock {}` block handles
cross-process serialization (multiple Mycel pods). Both are needed for
full effectiveness in a clustered deployment.

## Files

| File | Description |
|------|-------------|
| `config.mycel` | Service configuration |
| `connectors.mycel` | RabbitMQ, Magento HTTP, and the in-memory cache for fingerprints |
| `flows.mycel` | The `item_update_with_dedupe` flow |

## Run locally

```bash
export RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
export MAGENTO_URL="https://your-magento.example.com"
mycel start --config ./examples/dedupe
```

For production, swap the cache driver from `memory` to `redis` in
`connectors.mycel` so the fingerprint store survives restarts and is
shared across consumer pods:

```hcl
connector "fp_cache" {
  type   = "cache"
  driver = "redis"
  host   = env("REDIS_HOST")
  port   = env("REDIS_PORT", 6379)
  db     = env("REDIS_DB", 0)
}
```

## Tuning notes

- **TTL**: `30d` is the recommended baseline. Too short and you lose
  dedupe effectiveness for slow-changing resources. Too long and your
  cache leaks stale entries for retired SKUs.

- **on_duplicate**: `ack` is correct for MQ consumers — a fingerprint
  match means the message is fully consumed and the broker should
  release it. Use `requeue` only if you specifically want
  upstream-side retry handling for duplicates.

- **Fingerprint coverage**: re-audit `fingerprint {}` whenever a new
  field starts being persisted. The cost of omitting a field is silent
  data loss; the cost of including too many is one extra `Set` per
  message.

## What the dedupe primitive does NOT do

- It does not validate or transform the message — those are separate
  blocks.
- It does not retry — that is `error_handling { retry { ... } }`.
- It does not handle ordering — that is `sequence_guard`.
- It does not call the downstream — that is `to`.

Each primitive does one thing. Dedupe's one thing is "drop no-ops in
milliseconds." Composing it with the rest of the pipeline gives the
full effect.
