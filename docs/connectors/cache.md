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
  type     = "cache"
  driver   = "redis"
  url      = "redis://localhost:6379"
  prefix   = "myapp:"

  pool {
    max_connections = 100
    min_idle        = 10
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | — | Redis connection URL (`redis://host:port`) |
| `host` | string | `"localhost"` | Redis host (alternative to `url`) |
| `port` | int | `6379` | Redis port (used with `host`) |
| `password` | string | — | Redis password |
| `db` | int | `0` | Redis database number |
| `prefix` | string | — | Prefix for all keys |
| `default_ttl` | duration | — | Default time-to-live for entries |
| `pool.max_connections` | int | `100` | Max pool size |
| `pool.min_idle` | int | `10` | Connections kept open when idle |

## How a cache is used

A cache connector is not a flow's source or destination — a flow naming one in
`to` is answered "destination connector does not support required operation".
It is somewhere other blocks keep things, and it is named by them:

| Written in | What it does |
|------------|--------------|
| [`cache { storage = "..." }`](../core-concepts/flows.md) on a flow | Answers a repeated read without going to the destination |
| [`after { invalidate { storage = "..." } }`](../core-concepts/flows.md) | Drops what a write made stale |
| [`dedupe { cache = "..." }`](../core-concepts/flows.md) | Holds the fingerprints that decide whether a message was already handled |
| [`idempotency { storage = "..." }`](../guides/resilience.md) | Remembers the answer already given to a request |
| An aspect's `cache` block | The same, applied by flow name rather than written into each flow |

Reading and writing entries directly — a get, a set, a delete of your own — is
not something a flow can ask for.

## The wire format is the flow's, not the connector's

Nothing here selects how entries are encoded. That belongs to the block using
the cache, because one connector can hold namespaces owned by different things:
a flow's `cache {}` block declares its own with
[`encoding`](../guides/caching.md#sharing-a-namespace-with-another-service),
and a named `cache "..."` definition can carry one for every flow that
references it.

It only comes up when the namespace is shared with something that is not Mycel
— which is the normal state of affairs during a migration, and the case where
getting it wrong is not incompatibility but two services taking turns
overwriting each other's entries.

Neither `dedupe` nor `idempotency` is affected: they store their own bytes and
read them back themselves.

## Example

```hcl
flow "get_user_cached" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  cache {
    storage = "redis_cache"
    key     = "'user:' + input.id"
    ttl     = "10m"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
```

See the [cache example](https://github.com/matutetandil/mycel/tree/main/examples/cache) and [redis-cluster example](https://github.com/matutetandil/mycel/tree/main/examples/redis-cluster) for complete setups.

---

> **Full configuration reference:** See [Cache](../reference/configuration.md#cache) in the Configuration Reference.
