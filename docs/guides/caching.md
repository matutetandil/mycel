# Caching

Mycel provides two flow-level caching mechanisms: **cache** for avoiding repeated reads by storing results, and **dedupe** (since v2.1.0) for dropping **no-op** writes whose persisted projection is byte-identical to the last one processed for the same key.

## Cache Setup

First, define a cache connector:

```hcl
# Redis (recommended for production and multi-instance)
connector "redis_cache" {
  type    = "cache"
  driver  = "redis"
  url     = env("REDIS_URL", "redis://localhost:6379")
  prefix  = "myapp:"
}

# In-memory (development or single-instance)
connector "local_cache" {
  type      = "cache"
  driver    = "memory"
  max_items = 10000
  eviction  = "lru"
}
```

## Inline Cache Block

Add a `cache` block directly in a flow:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  cache {
    storage = "redis_cache"
    ttl     = "5m"
    key     = "'product:' + input.id"
  }

  to {
    connector = "db"
    target    = "products WHERE id = :id"
  }
}
```

When a request comes in:
1. Mycel computes the cache key
2. If the key exists in the cache, return the cached value immediately (no `to` block executes)
3. If not, execute the `to` block and store the result in the cache

### Cache Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `storage` | string | yes | Cache connector name |
| `ttl` | string | no | Time-to-live: `"5m"`, `"1h"`, `"24h"` |
| `key` | string | no | CEL expression for cache key (default: auto-generated from request) |
| `invalidate_on` | list | no | Event patterns that invalidate this cache entry |
| `use` | string | no | Reference a named cache (`use = "cache.<name>"`); its storage, ttl, prefix and encoding come with it |
| `encoding` | list | no | Wire format for entries, applied in order on the way out and reversed on the way in. Default `["json"]`. See [Sharing a namespace](#sharing-a-namespace-with-another-service) |

### Cache Key Expressions

The cache key must uniquely identify the request:

```hcl
# Simple ID-based key
key = "'product:' + input.id"

# Multiple parameters
key = "'users:' + input.id + ':orders:' + input.status"

# Context-aware (per-user cache)
key = "'user_data:' + ctx.user_id"
```

## Named Caches

Define a named cache block to reuse cache configuration across multiple flows:

```hcl
cache "products" {
  storage       = "redis_cache"
  ttl           = "10m"
  prefix        = "products"
  invalidate_on = ["product.updated", "product.deleted"]
}
```

Reference it in a flow:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  cache = cache.products
  to {
    connector = "db"
    target    = "products WHERE id = :id"
  }
}
```

### Named Cache Attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `storage` | string | Cache connector name |
| `ttl` | string | Default TTL for entries |
| `prefix` | string | Key prefix for namespacing |
| `invalidate_on` | list | Event patterns that trigger invalidation |
| `encoding` | list | Wire format for entries in this cache, inherited by any flow that references it |

## Cache Invalidation

### `invalidate_on` (automatic)

Invalidate cache entries when specific events happen. This uses event pattern matching:

```hcl
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  cache {
    storage       = "redis_cache"
    ttl           = "15m"
    key           = "'user:' + input.id"
    invalidate_on = ["user.updated:${input.id}", "user.deleted:${input.id}"]
  }

  to {
    connector = "db"
    target    = "users WHERE id = :id"
  }
}
```

### `after` block (explicit, per-mutation flow)

Explicitly invalidate keys after a write operation:

```hcl
flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  to {
    connector = "db"
    target    = "UPDATE products"
  }

  after {
    invalidate {
      storage  = "redis_cache"
      keys     = ["product:${input.id}"]
      patterns = ["products:list:*"]
    }
  }
}
```

`keys` invalidates exact keys. `patterns` invalidates all matching keys (glob-style). Both are templates: `${input.id}` is replaced when the flow runs.

### A key set whose size depends on the data

`keys` is one key out per template in. The *values* in a key vary with the message; the *number* of keys is fixed when the configuration is parsed. When the set is whatever a query returned — every store view a product appears in, every rewrite path it has had, every variant of a parent — that number is a function of the data, and there is nothing to write.

`keys_from` and `patterns_from` take a CEL expression yielding a list of strings:

```hcl
flow "republish_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id/republish"
  }

  step "affected" {
    connector = "db"
    query     = "SELECT store_code FROM product_stores WHERE product_id = :id"
    params    = { id = "input.id" }
  }

  to {
    connector = "db"
    target    = "products"
  }

  after {
    invalidate {
      storage   = "redis_cache"
      keys      = ["products:item"]
      keys_from = "step.affected.map(r, 'product:' + input.id + ':' + r.store_code)"
    }
  }
}
```

`input.*`, `output.*` and `step.*` are in scope, because the list almost always comes from a query the flow just ran. The result is unioned with the static list and deduplicated, so a fixed key and a computed set can be named together.

!!! warning "A wildcard is not a substitute"
    Not when the members diverge. Rewrite paths drift from the URL key through redirects and history, so a prefix broad enough to catch them all also deletes entries for unrelated products, and one narrow enough to be safe misses exactly the paths that most need dropping — over-invalidating and under-invalidating at the same time.

    Aiming a `${...}` template at a list does not fan out either: it renders Go syntax into the key (`url-rewrite-[a b c]`), deletes that, and reports success. It now warns and points here.

### When an invalidation does not happen

A flow whose invalidation failed has already done and committed its own work, so it answers 200 either way — and the cache is now serving what that write made stale, with nothing to correct it. The two ways it can fail want different answers:

| What failed | Response |
|---|---|
| The cache could not be reached (`keys`, `patterns`) | Logged at warn, counted as `mycel_cache_invalidate_errors_total`. **The request still succeeds** — the write is committed, and failing it afterwards would be wrong |
| `keys_from` / `patterns_from` could not be evaluated, or did not yield a list of strings | Logged, counted, **and the request fails**. This is not transient: it will fail identically on every message for as long as the flow is deployed, invalidating nothing while reporting success, and `mycel validate` does not evaluate CEL so it is not caught beforehand either |

```
WARN cache invalidation did not happen
     flow=invalidate_products cache=redis_cache attr=keys_from fatal=true
     error="invalidate keys_from: CEL eval error: no such key: nope"
```

This matters most for a flow whose *only* job is invalidation — an endpoint a consumer calls after writing elsewhere, with steps and an `after` block and no `to`. There is nothing else to observe, so a silent no-op would answer 200 forever and the only symptom would be stale reads somewhere else entirely, hours later. `examples/cache` shows that shape as Pattern 7.

## Sharing a namespace with another service

A flow's cache entries are `["json"]` unless the block says otherwise, and that is the right answer while Mycel owns the namespace. It stops being the right answer during a migration — which is exactly when a cache is most likely to be shared, because the service being replaced is still up and still reading and writing the same keys.

Getting it wrong is not incompatibility, it is mutual destruction:

1. Mycel reads a key the other service wrote and cannot decode it.
2. That reads as a miss, so the flow does the work.
3. The flow then writes **its** format over that key.
4. The other service reads the same key next and its own decode throws.

They take turns destroying each other's entries, and the only visible symptom is a cache that never seems to hit.

`encoding` declares the format. The codecs listed apply left to right on the way out and reversed on the way in:

```hcl
cache {
  storage  = "redis_cache"
  ttl      = "5m"
  key      = "'product:' + input.id"
  # Reads and writes what a service storing gzip(base64(JSON.stringify(v))) does
  encoding = ["json", "base64", "gzip"]
}
```

| Codec | Position | What it does |
|-------|----------|--------------|
| `json` | first, required | The value ↔ bytes |
| `base64` | after | Bytes ↔ bytes, standard alphabet with padding. Tolerant on the way in of missing padding and wrapped lines |
| `gzip` | after | Bytes ↔ bytes |

A chain that could never be applied — one that does not start with `json`, or names a codec that does not exist — fails when the configuration is read, not on the first cache write.

A named cache can carry it, so flows sharing a namespace share its format without restating it; a flow declaring its own wins.

### Knowing when the format is wrong

A found entry that cannot be decoded is **not** a hit. It gets its own counter — `mycel_cache_decode_errors_total` — because it is neither a hit (the flow is about to do the work) nor a miss the cache could fix by being warmer, and it is logged at warn naming the flow, the cache and the key:

```
WARN cache entry could not be decoded; treating as a miss and doing the work
     flow=get_product cache=redis_cache key=product:42 bytes=180
```

That line is the only signal that the cache holds something this flow cannot use. Note that a hit rate computed as `hits / (hits + misses)` will not show this — see [Observability](observability.md#cache-metrics).

## Deduplication

Since v2.1.0 the `dedupe` block is **content-based** and runs in two phases. Phase A (after `transform`, before `to`) computes a canonical fingerprint over the projection the operator declares and compares it byte-for-byte to the stored fingerprint for the same key; on match the message is dropped according to `on_duplicate` without invoking `to`. Phase B (after `to` succeeds) stores the new fingerprint, so a failed-then-retried message will not self-discard.

The primitive self-locks per key (in-process via the memory-backed `SyncManager`) so two workers cannot both pass Phase A with identical fingerprints and double-call the downstream. For cross-process serialization across multiple Mycel pods, compose with an outer `lock {}` block on the same resource key.

The typical use case is an MQ consumer where the upstream re-sends "update" messages even when nothing relevant changed: every redelivery hits a slow downstream and the queue accumulates. With dedupe, only messages whose persisted projection actually differs reach the downstream.

```hcl
connector "fp_cache" {
  type   = "cache"
  driver = "redis"   # or "memory" for tests / single-pod
}

flow "process_payment" {
  from {
    connector = "rabbit"
    operation = "payments"
  }

  transform {
    payment_id = "input.payment_id"
    account_id = "input.account_id"
    amount     = "input.amount"
  }

  dedupe {
    cache        = "fp_cache"
    key          = "'payment:' + input.payment_id"
    ttl          = "24h"
    on_duplicate = "ack"
    fingerprint {
      payment_id = "output.payment_id"
      account_id = "output.account_id"
      amount     = "output.amount"
    }
  }

  to {
    connector = "db"
    target    = "payments"
  }
}
```

### Dedupe Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `cache` | string | yes | — | Name of a `connector { type = "cache" }`. The connector pool is initialized once at startup; the hot path does not pay a registry lookup per message |
| `key` | string | yes | — | CEL expression for the per-resource fingerprint key (evaluated against `input.*`) |
| `fingerprint {}` | block | yes | — | Named CEL expressions whose values form the projection. Both `input.*` and `output.*` (transform result) are in scope. Must list every persisted field — omitting one would silently drop real changes |
| `ttl` | string | no | — | How long to keep stored fingerprints. Supports `"30d"` and `"2w"` plus stdlib units (`s`/`m`/`h`); malformed values fail the parse |
| `on_duplicate` | string | no | `"ack"` | Behavior on fingerprint match: `"ack"`, `"reject"`, `"requeue"`. Matches the `sequence_guard` vocabulary so MQ consumers handle it uniformly |
| `compare_when` | string | no | — | CEL predicate gating Phase A **only**. False: the stored fingerprint is not consulted, so the message cannot be dropped; Phase B still commits after a successful write. `input.*` and `output.*` in scope. See [When the record can vanish](#when-the-record-can-vanish) |

### When the record can vanish

A stored fingerprint says the content was written once. It does not say it is still written. Nothing in dedupe observes the downstream record being removed by a path the flow never sees — a manual delete in an admin UI, a restore, a data fix — and there is usually no flow to clear the fingerprint when that happens. The re-send meant to repair the damage then matches a fingerprint describing a record that no longer exists, and is dropped.

`compare_when` gates the comparison, and only the comparison:

```hcl
step "check_present" {
  connector = "db"
  query     = "SELECT CAST(COALESCE((SELECT 1 FROM products WHERE sku = :sku LIMIT 1), 0) AS SIGNED) AS row_exists"
  params    = { sku = "input.body.sku" }
  on_error  = "fail"
}

transform {
  row_exists = "int(step.check_present.row_exists)"
  name       = "input.body.name"
}

dedupe {
  cache        = "fp_cache"
  key          = "'sku_fp:' + input.body.sku"
  ttl          = "30d"
  compare_when = "output.row_exists == 1"
  fingerprint {
    name = "output.name"
  }
}
```

- **false** → Phase A is skipped entirely: no `GET`, no comparison, no drop. Phase B still runs, so the write commits a fresh fingerprint and the next message *can* be suppressed. Gating both phases would leave the cache empty forever and the primitive permanently inert on the flow.
- **true** or absent → unchanged behavior.
- Fails **open**: a predicate that cannot be evaluated, or that does not return a boolean, logs a warning and processes the message. Same direction as the cache-error path — one extra downstream call is recoverable, a silently swallowed message is not.

!!! warning "The existence check goes in `compare_when`, not in `fingerprint {}`"
    Adding `row_exists` to the projection looks like it should work — record gone, projection differs, message reprocessed — and it does the opposite in both directions.

    Phase A and Phase B share one projection: the fingerprint Phase B stores is the one Phase A computed, i.e. the **pre-write** reading. On a create, "does this record exist" is `0` by definition, so the stored value is `0` forever while every later message computes `1`.

    | situation | stored | computed | result |
    |---|---|---|---|
    | record exists, duplicate re-send | 0 | 1 | mismatch → **duplicate reaches the destination** |
    | record deleted externally, re-send | 0 | 0 | match → **dropped**, which is the case it was added for |

    It breaks suppression exactly where suppression worked, and stays inert exactly where invalidation was needed.

### Pipeline order

The `dedupe` block runs **after** `transform`. The fingerprint expressions reference `output.*` (the transformed payload), so transform must run first. Earlier versions (≤ 2.0.0) had a key-based dedupe block that ran before transform; see CHANGELOG v2.1.0 for migration.

### Array order-insensitivity

The canonical encoder sorts array elements before serialization, treating them as **order-insensitive sets**. This is appropriate for projections like "list of attribute values" or "set of website flags," but **lossy** for fields where order is semantically meaningful (e.g. a ranked list where position encodes priority).

For order-sensitive arrays, reshape them in `transform` before dedupe sees them — join with a delimiter into a single string:

```hcl
transform {
  # Bad: ranked_tags as an array would lose order in the fingerprint.
  # Good: join into a string so order is part of the encoded value.
  ranked_tags = "input.ranked_tags.map(t, t).join(',')"
}
```

## Caching vs Deduplication

| | Cache | Dedupe |
|--|-------|--------|
| Purpose | Avoid redundant downstream reads | Drop no-op writes |
| Applies to | Read flows | Write flows (especially MQ consumers) |
| Cache miss | Execute `to`, cache result | Process normally; store fingerprint after `to` success |
| Cache hit | Return cached value immediately | Drop without invoking `to` |
| Compares | Key only | Canonical content fingerprint |
| Pipeline position | Before `to` (read path) | After `transform`, before `to` |

## Production Considerations

- Use Redis for multi-instance deployments. In-memory cache is not shared across instances.
- Set TTLs appropriate to your data freshness requirements. Stale cache is worse than no cache for critical data.
- Use `invalidate_on` or the `after` block to invalidate caches on writes.
- Monitor cache hit rates with the `/metrics` endpoint (Prometheus).
- Use `prefix` or `key` expressions to prevent key collisions between services sharing a Redis instance.

## Example: Read-Through Cache for Product Catalog

```hcl
connector "redis_cache" {
  type    = "cache"
  driver  = "redis"
  url     = env("REDIS_URL", "redis://localhost:6379")
  prefix  = "catalog:"
}

# Cache product reads for 10 minutes
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  cache {
    storage = "redis_cache"
    ttl     = "10m"
    key     = "'product:' + input.id"
  }

  to {
    connector = "db"
    target    = "products WHERE id = :id"
  }
}

# Invalidate on update
flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  to {
    connector = "db"
    target    = "UPDATE products"
  }

  after {
    invalidate {
      storage = "redis_cache"
      keys    = ["product:${input.id}"]
    }
  }
}

# Deduplicate no-op inventory updates by content
flow "handle_inventory_update" {
  from {
    connector = "rabbit"
    operation = "inventory.updated"
  }

  transform {
    product_id  = "input.product_id"
    stock_qty   = "input.stock_qty"
    reorder_at  = "input.reorder_at"
  }

  dedupe {
    cache        = "redis_cache"
    key          = "'inv_fp:' + input.product_id"
    ttl          = "1h"
    on_duplicate = "ack"
    fingerprint {
      product_id = "output.product_id"
      stock_qty  = "output.stock_qty"
      reorder_at = "output.reorder_at"
    }
  }

  to {
    connector = "db"
    target    = "UPDATE products"
  }
}
```

## See Also

- [Connectors: Cache](../connectors/cache.md)
- [Guides: Error Handling](error-handling.md)
- [Examples: Cache](https://github.com/matutetandil/mycel/tree/main/examples/cache)
