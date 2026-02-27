# Cache

In-memory (LRU) and Redis caching. Use cache connectors to store frequently accessed data, reduce database load, or share state across flows.

## Memory Cache

```hcl
connector "cache" {
  type        = "cache"
  driver      = "memory"
  max_items   = 10000
  eviction    = "lru"
  default_ttl = "5m"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_items` | int | `10000` | Maximum cached items |
| `eviction` | string | `"lru"` | Eviction policy |
| `default_ttl` | duration | `"5m"` | Default time-to-live |

## Redis Cache

```hcl
connector "redis_cache" {
  type       = "cache"
  driver     = "redis"
  address    = "localhost:6379"
  password   = env("REDIS_PASSWORD")
  db         = 0
  key_prefix = "myapp:"

  pool {
    max_connections = 100
    min_connections = 10
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `address` | string | `"localhost:6379"` | Redis address |
| `password` | string | — | Redis password |
| `db` | int | `0` | Redis database number |
| `key_prefix` | string | — | Prefix for all keys |
| `pool.max_connections` | int | `100` | Max pool size |
| `pool.min_connections` | int | `10` | Min pool size |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `get` | read | Read a cached value by key |
| `set` | write | Write a value with optional TTL |
| `delete` | write | Remove a cached key |

## Example

```hcl
flow "get_user_cached" {
  from { connector = "api", operation = "GET /users/:id" }

  cache {
    connector = "redis_cache"
    key       = "'user:' + input.params.id"
    ttl       = "10m"
  }

  to { connector = "db", target = "users" }
}
```

See the [cache example](../../examples/cache/) and [redis-cluster example](../../examples/redis-cluster/) for complete setups.
