# Cache Example

This example demonstrates caching with Mycel using both in-memory and Redis cache drivers.

## Features Demonstrated

- **In-memory cache** with LRU eviction
- **Redis cache** (optional, for production)
- **Inline cache configuration** in flows
- **Named cache definitions** for reusability
- **Cache invalidation** after write operations
- **Pattern-based invalidation** with wildcards
- **Cache key interpolation** with variables

## Files

| File | Description |
|------|-------------|
| `config.mycel` | Service configuration |
| `connectors.mycel` | REST API, SQLite, and Cache connectors |
| `caches.mycel` | Named cache definitions (reusable) |
| `flows.mycel` | Flow definitions with various caching patterns |

## Quick Start

The database file is created where the service is started from, so run these
from this directory.

```bash
cd examples/cache

# Create the products and users tables
mycel migrate --config .

# Start the service
mycel start --config .

# The service runs on http://localhost:3000
```

## Caching Patterns

### Pattern 1: Inline Cache

Define cache settings directly in the flow:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  to {
    connector = "db"
    target    = "products"
  }

  cache {
    storage = "memory_cache"
    ttl     = "5m"
    key     = "product:${input.id}"
  }
}
```

### Pattern 2: Named Cache Reference

Define once, use everywhere:

```hcl
# In caches.mycel
cache "products" {
  storage = "memory_cache"
  ttl     = "10m"
  prefix  = "products"
}

# In flows.mycel
flow "get_product" {
  from { ... }
  to   { ... }

  cache {
    use = "products"
    key = "product:${input.id}"
  }
}
```

### Pattern 3: Cache Invalidation

Invalidate cache after write operations:

```hcl
flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  to {
    connector = "db"
    target    = "products"
  }

  after {
    invalidate {
      storage = "memory_cache"
      keys     = ["product:${input.id}"]      # Specific keys
      patterns = ["lists:products:*"]          # Wildcard patterns
    }
  }
}
```

### Pattern 5: Sharing a namespace with another service

The cache is not always ours alone. During a migration the service being replaced is still up, still reading and writing the same keys, and it encodes them its own way. `encoding` says which way:

```hcl
cache {
  storage  = "memory_cache"
  key      = "shared:product:${input.id}"
  encoding = ["json", "base64", "gzip"]   # gzip(base64(JSON.stringify(v)))
}
```

Codecs apply left to right on the way out and reversed on the way in. Absent is `["json"]`, which is what every cache did before the attribute existed.

Without it this is not merely incompatible, it is destructive: the entry cannot be decoded, that reads as a miss, the flow does the work and writes plain JSON over the key, and the other service fails on its next read. They take turns destroying each other's entries, and the only visible symptom is a cache that never seems to hit. A found entry that cannot be decoded is now counted as `mycel_cache_decode_errors_total` and logged with its key, which is the signal that the format is wrong.

### Pattern 6: Dropping a key set the configuration cannot count

`keys` is one key out per template in: the values vary with the message, the number does not. When the set is whatever a query returned — one entry per store view a product appears in — its size is a function of the data, and `keys_from` is how to say so:

```hcl
step "affected" {
  connector = "db"
  query     = "SELECT store_code FROM product_stores WHERE product_id = :id"
  params    = { id = "input.id" }
}

after {
  invalidate {
    storage   = "memory_cache"
    keys      = ["products:item"]
    keys_from = "step.affected.map(r, 'product:' + input.id + ':' + r.store_code)"
  }
}
```

`input.*`, `output.*` and `step.*` are in scope, and the result is unioned with `keys` and deduplicated. `patterns_from` is the same for wildcards.

A wildcard is not a substitute when the members diverge: one broad enough to catch them all also deletes entries for unrelated products, and one narrow enough to be safe misses exactly the ones that matter.

## Testing the API

```bash
# Create a product
curl -X POST http://localhost:3000/products \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 29.99}'

# Get product (first request - cache miss, hits database)
curl http://localhost:3000/products/1
# Response time: ~5ms

# Get product again (cache hit - much faster!)
curl http://localhost:3000/products/1
# Response time: ~0.5ms

# Update product (invalidates cache)
curl -X PUT http://localhost:3000/products/1 \
  -H "Content-Type: application/json" \
  -d '{"name": "Super Widget", "price": 39.99}'

# Get product (cache miss after invalidation)
curl http://localhost:3000/products/1
# Fresh data from database, then cached again

# Delete product (invalidates cache)
curl -X DELETE http://localhost:3000/products/1
```

## Cache Key Interpolation

Cache keys support variable interpolation from the input:

| Variable | Description | Example |
|----------|-------------|---------|
| `${input.id}` | Path parameter | `/products/:id` → `product:123` |
| `${input.page}` | Query parameter | `?page=2` → `products:page=2` |
| `${input.data.field}` | Request body field | `{"category": "toys"}` → `category:toys` |
| `${result.id}` | Result field (invalidation only) | After insert → `product:456` |

Example with pagination:
```hcl
cache {
  key = "products:page=${input.page}:limit=${input.limit}"
}
# Results in keys like: "products:page=1:limit=10"
```

## Using Redis (Production)

For production deployments, use Redis for distributed caching:

1. Uncomment the Redis connector in `connectors.mycel`:

```hcl
connector "redis_cache" {
  type   = "cache"
  driver = "redis"
  url    = env("REDIS_URL", "redis://localhost:6379")
  prefix = "myapp"

  pool {
    max_connections = 10
    min_idle       = 2
  }
}
```

2. Update cache definitions and flows to use `"redis_cache"` instead of `"memory_cache"`.

## Cache Drivers Comparison

| Feature | Memory | Redis |
|---------|--------|-------|
| Speed | Fastest | Fast |
| Persistence | No | Yes |
| Distributed | No | Yes |
| Max Items | Configurable (LRU) | Unlimited* |
| TTL | Supported | Supported |
| Pattern Delete | Iterates all keys | SCAN (efficient) |
| Best For | Dev/Test/Single instance | Production/Multi-instance |

## Configuration Reference

### Memory Cache Connector

```hcl
connector "cache" {
  type        = "cache"
  driver      = "memory"
  max_items   = 10000      # Maximum items before LRU eviction
  eviction    = "lru"      # Eviction policy
  default_ttl = "5m"       # Default TTL for entries
}
```

### Redis Cache Connector

```hcl
connector "cache" {
  type        = "cache"
  driver      = "redis"
  url         = "redis://localhost:6379"
  prefix      = "myapp"    # Namespace prefix for all keys
  default_ttl = "5m"

  pool {
    max_connections = 10   # Maximum connections
    min_idle       = 2     # Minimum idle connections
    max_idle_time  = "30s" # Close idle connections after
    connect_timeout = "5s" # Connection timeout
  }
}
```

### Named Cache Definition

```hcl
cache "name" {
  storage       = "cache_connector_name"
  ttl           = "10m"
  prefix        = "optional_prefix"
  invalidate_on = ["event:pattern"]  # Future: event-driven invalidation
}
```

### Flow Cache Block

```hcl
flow "name" {
  # ... from/to ...

  cache {
    storage = "connector"  # Required if not using 'use'
    use     = "named"      # Required if not using 'storage'
    ttl     = "5m"         # Override named cache TTL
    key     = "key:${var}" # Cache key template
  }
}
```

### Cache Invalidation Block

```hcl
flow "name" {
  # ... from/to ...

  after {
    invalidate {
      storage  = "connector"              # Cache connector name
      keys     = ["key1", "key2"]         # Specific keys to delete
      patterns = ["prefix:*", "other:*"]  # Wildcard patterns
    }
  }
}

## Verify It Works

### 1. Start the service

```bash
cd examples/cache
mycel migrate --config .
mycel start --config .
```

You should see:
```
INFO  Starting service: cache-example
INFO  Loaded 3 connectors: api, db, memory_cache
INFO  Memory cache initialized (max_items: 10000)
INFO  Registered 5 flows with caching
INFO  REST server listening on :3000
```

### 2. Create a product

```bash
curl -X POST http://localhost:3000/products \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 29.99}'
```

Expected response:
```json
{"id":1,"name":"Widget","price":29.99}
```

### 3. First GET (cache MISS)

```bash
time curl http://localhost:3000/products/1
```

Expected:
- Response time: ~5-10ms
- Log shows: `Cache MISS for key: product:1`

### 4. Second GET (cache HIT)

```bash
time curl http://localhost:3000/products/1
```

Expected:
- Response time: <1ms
- Log shows: `Cache HIT for key: product:1`

### 5. Update product (invalidates cache)

```bash
curl -X PUT http://localhost:3000/products/1 \
  -H "Content-Type: application/json" \
  -d '{"name": "Super Widget", "price": 39.99}'
```

Log shows:
```
INFO  Cache INVALIDATED: product:1
```

### 6. Next GET (cache MISS again)

```bash
curl http://localhost:3000/products/1
```

Response contains updated data, log shows cache miss.

### What to check in logs

```
INFO  GET /products/1
INFO    Cache MISS for key: product:1
INFO    Querying database...
INFO    Result cached for 5m
INFO  Response sent in 8ms

INFO  GET /products/1
INFO    Cache HIT for key: product:1
INFO  Response sent in 0.5ms
```

### Common Issues

**Cache not working (always MISS)**

Check that the cache connector is loaded:
```bash
curl http://localhost:3000/health
# Should show cache connector as healthy
```

**"Unknown cache storage"**

The `storage` name in the flow must match a cache connector name:
```hcl
cache {
  storage = "memory_cache"  # Must match connector name
}
```

**Redis connection failed**

For Redis cache, ensure Redis is running:
```bash
redis-cli ping
# Should respond: PONG
```
