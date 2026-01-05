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
| `config.hcl` | Service configuration |
| `connectors.hcl` | REST API, SQLite, and Cache connectors |
| `caches.hcl` | Named cache definitions (reusable) |
| `flows.hcl` | Flow definitions with various caching patterns |

## Quick Start

```bash
# Start the service
mycel start --config ./examples/cache

# The service runs on http://localhost:3000
```

## Caching Patterns

### Pattern 1: Inline Cache

Define cache settings directly in the flow:

```hcl
flow "get_product" {
  from { connector = "api", operation = "GET /products/:id" }
  to   { connector = "db", target = "products" }

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
# In caches.hcl
cache "products" {
  storage = "memory_cache"
  ttl     = "10m"
  prefix  = "products"
}

# In flows.hcl
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
  from { connector = "api", operation = "PUT /products/:id" }
  to   { connector = "db", target = "products" }

  after {
    invalidate {
      storage = "memory_cache"
      keys     = ["product:${input.id}"]      # Specific keys
      patterns = ["lists:products:*"]          # Wildcard patterns
    }
  }
}
```

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
| `${input.query.page}` | Query parameter | `?page=2` → `products:page=2` |
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

1. Uncomment the Redis connector in `connectors.hcl`:

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
mycel start --config ./examples/cache
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
