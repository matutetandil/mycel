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
    key     = "'product:' + input.params.id"
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

### Cache Key Expressions

The cache key must uniquely identify the request:

```hcl
# Simple ID-based key
key = "'product:' + input.params.id"

# Multiple parameters
key = "'users:' + input.params.id + ':orders:' + input.query.status"

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
    key           = "'user:' + input.params.id"
    invalidate_on = ["user.updated:${input.params.id}", "user.deleted:${input.params.id}"]
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
      keys     = ["product:${input.params.id}"]
      patterns = ["products:list:*"]
    }
  }
}
```

`keys` invalidates exact keys. `patterns` invalidates all matching keys (glob-style).

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
    key     = "'product:' + input.params.id"
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
      keys    = ["product:${input.params.id}"]
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
- [Examples: Cache](../../examples/cache)
