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

### ⚠️ Arrays are treated as order-insensitive sets

The canonical encoder sorts array elements before serialization, so two
projections with the same set of array values in different order produce
identical fingerprints. This is appropriate for "list of attribute
values" projections (image URLs, dynamic attributes, websites) where
order is presentational, but **lossy** if order is semantically
meaningful.

If your projection includes an array where order matters (e.g. an
ordered list of pipeline steps, or a ranked tag list where position
encodes priority), reshape it in `transform` before dedupe sees it:
join with a delimiter into a string so the dedupe encoder treats it as
a single ordered value.

```hcl
transform {
  // Bad: ranked_tags as an array would lose order in the fingerprint.
  // Good: join into a string so order is part of the encoded value.
  ranked_tags = "input.ranked_tags.map(t, t).join(',')"
}
```

Audit every array field in your `fingerprint {}` for order sensitivity
before going to production.

## When the record can vanish: `compare_when`

A stored fingerprint says this content was written once. It does not say it is
still written.

Nothing in dedupe observes the downstream record being removed by a path the
flow never sees — a manual delete, a restore from an older backup, a data fix —
and there is usually no delete flow to clear the fingerprint when it happens.
The re-send meant to repair the damage then matches a fingerprint describing a
record that no longer exists, and is dropped in milliseconds having written
nothing. Anything downstream that was waiting on that write proceeds against a
record that is not there.

`item_create_with_existence_gate.mycel` closes that hole. A step asks the one
question the message cannot answer, and `compare_when` gates the comparison on
it:

```hcl
step "check_present" {
  connector = "catalog"
  query     = "SELECT CAST(COALESCE((SELECT 1 FROM catalog_items WHERE sku = :sku LIMIT 1), 0) AS SIGNED) AS row_exists"
  params    = { sku = "input.body.payload.productItemId" }
  on_error  = "fail"
}

transform {
  row_exists = "int(step.check_present.row_exists)"
  # ... the fields actually written downstream
}

dedupe {
  cache        = "fp_cache"
  key          = "'sku_fp:' + input.body.payload.productItemId"
  compare_when = "output.row_exists == 1"
  fingerprint {
    sku  = "output.sku"
    name = "output.name"
  }
}
```

| situation | gate | outcome |
|---|---|---|
| record present, identical content | compare | dropped as a duplicate |
| record present, content changed | compare | fingerprint differs → processed |
| first write, record absent | skip | processed → the new fingerprint is stored |
| record deleted externally, identical content | skip | **processed and rewritten** |

Two properties are load-bearing:

- **Only the comparison is gated.** The new fingerprint is still committed
  after a successful write. Gating that too would leave the cache empty
  forever, so no later message could be suppressed either and the primitive
  would be permanently inert on this flow.
- **It fails open.** A predicate that cannot be evaluated, or that returns
  something other than a boolean, logs a warning and processes the message.
  One extra downstream call is recoverable; a silently swallowed message is
  not.

### ⚠️ The existence check does not go in `fingerprint {}`

Adding `row_exists` to the projection looks like it should do the same job —
record gone, projection differs, message reprocessed — and it does the opposite
in both directions.

Phase A and Phase B share one projection: the fingerprint Phase B stores is the
one Phase A computed, i.e. the **pre-write** reading. On a create, "does this
record exist" is `0` by definition, so the stored value stays `0` forever while
every later message computes `1`.

| situation | stored | computed | result |
|---|---|---|---|
| record exists, duplicate re-send | 0 | 1 | mismatch → **the duplicate reaches the downstream** |
| record deleted externally, re-send | 0 | 0 | match → **dropped**, which is the case it was added for |

It breaks suppression exactly where suppression was working, and stays inert
exactly where invalidation was needed. Put the check in `compare_when`.

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
| `connectors.mycel` | RabbitMQ, Magento HTTP, the in-memory cache for fingerprints, and the catalog the existence gate reads |
| `item_update_with_dedupe.mycel` | The `item_update_with_dedupe` flow |
| `item_create_with_existence_gate.mycel` | The `item_create_with_existence_gate` flow — dedupe with `compare_when` |
| `migrations/001_catalog.sql` | The catalog table the existence gate reads |

## Run locally

```bash
export RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
export MAGENTO_URL="https://your-magento.example.com"

# Creates the catalog table the existence gate reads. Without it the
# check_present step fails and the create flow retries instead of starting.
mycel migrate --config ./examples/dedupe

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
