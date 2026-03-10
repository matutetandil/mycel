# Caching

Mycel provides two flow-level caching mechanisms: **cache** for avoiding repeated calls by storing results, and **dedupe** for preventing duplicate processing of the same message or request.

## Cache Setup

First, define a cache connector:

```hcl
# Redis (recommended for production and multi-instance)
connector "redis_cache" {
  type    = "cache"
  driver  = "redis"
  address = env("REDIS_ADDRESS")
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
  from { connector = "api", operation = "GET /products/:id" }

  cache {
    storage = "redis_cache"
    ttl     = "5m"
    key     = "'product:' + input.params.id"
  }

  to { connector = "db", target = "products WHERE id = :id" }
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
  from { connector = "api", operation = "GET /products/:id" }
  cache = cache.products
  to   { connector = "db", target = "products WHERE id = :id" }
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
  from { connector = "api", operation = "GET /users/:id" }

  cache {
    storage       = "redis_cache"
    ttl           = "15m"
    key           = "'user:' + input.params.id"
    invalidate_on = ["user.updated:${input.params.id}", "user.deleted:${input.params.id}"]
  }

  to { connector = "db", target = "users WHERE id = :id" }
}
```

### `after` block (explicit, per-mutation flow)

Explicitly invalidate keys after a write operation:

```hcl
flow "update_product" {
  from { connector = "api", operation = "PUT /products/:id" }
  to   { connector = "db", target = "UPDATE products" }

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

The `dedupe` block prevents processing the same message or request more than once. Essential for message queue consumers where messages can be delivered more than once.

```hcl
flow "process_payment" {
  from { connector = "rabbit", operation = "payments" }

  dedupe {
    storage      = "redis_cache"
    key          = "input.payment_id"
    ttl          = "24h"
    on_duplicate = "skip"
  }

  to { connector = "db", target = "payments" }
}
```

### Dedupe Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage` | string | yes | — | Cache connector name |
| `key` | string | yes | — | CEL expression to compute the unique message key |
| `ttl` | string | no | — | How long to remember processed message IDs |
| `on_duplicate` | string | no | `"skip"` | What to do with duplicates: `"skip"` or `"error"` |

### Dedupe with Complex Keys

```hcl
dedupe {
  storage      = "redis_cache"
  key          = "'order:' + input.order_id + ':' + input.event_type"
  ttl          = "1h"
  on_duplicate = "skip"
}
```

## Caching vs Deduplication

| | Cache | Dedupe |
|--|-------|--------|
| Purpose | Avoid redundant reads | Prevent duplicate processing |
| Applies to | Read flows | Write flows (especially MQ consumers) |
| Cache miss | Execute `to` block, cache result | Process normally |
| Cache hit | Return cached value immediately | Skip the flow execution |
| Key source | Request parameters | Message content |

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
  address = env("REDIS_ADDRESS")
  prefix  = "catalog:"
}

# Cache product reads for 10 minutes
flow "get_product" {
  from { connector = "api", operation = "GET /products/:id" }

  cache {
    storage = "redis_cache"
    ttl     = "10m"
    key     = "'product:' + input.params.id"
  }

  to { connector = "db", target = "products WHERE id = :id" }
}

# Invalidate on update
flow "update_product" {
  from { connector = "api", operation = "PUT /products/:id" }
  to   { connector = "db", target = "UPDATE products" }

  after {
    invalidate {
      storage = "redis_cache"
      keys    = ["product:${input.params.id}"]
    }
  }
}

# Deduplicate webhook events
flow "handle_inventory_update" {
  from { connector = "rabbit", operation = "inventory.updated" }

  dedupe {
    storage      = "redis_cache"
    key          = "input.event_id"
    ttl          = "1h"
    on_duplicate = "skip"
  }

  to { connector = "db", target = "UPDATE products" }
}
```

## See Also

- [Connectors: Cache](../connectors/cache.md)
- [Guides: Error Handling](error-handling.md)
- [Examples: Cache](../../examples/cache)
