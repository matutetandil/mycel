# Synchronization

Mycel provides three distributed synchronization primitives for coordinating concurrent flow executions. All require a Redis (or compatible) cache connector as the backend.

## When to Use Synchronization

- **Lock**: Exactly one flow must process a resource at a time (e.g., deducting from an account balance, updating inventory count)
- **Semaphore**: Limit concurrency to N simultaneous executions (e.g., external API with rate limits)
- **Coordinate**: One flow must wait for another to complete (e.g., a consumer waiting for a producer)

## Lock (Mutex)

A lock guarantees only one flow instance processes a specific resource at a time. Any concurrent flow that tries to acquire the same lock will wait (or timeout).

```hcl
flow "process_payment" {
  from { connector = "rabbit", operation = "payments" }

  lock {
    storage = "connector.redis"
    key     = "'account:' + input.account_id"
    timeout = "30s"
    wait    = true
    retry   = "100ms"
  }

  to { connector = "db", target = "UPDATE accounts" }
}
```

### Lock Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage` | string | yes | — | Cache connector name |
| `key` | string | yes | — | CEL expression for the lock key (scopes the lock to a resource) |
| `timeout` | string | no | `"30s"` | Maximum time to hold the lock |
| `wait` | bool | no | `true` | Block until the lock is available |
| `retry` | string | no | `"100ms"` | Interval between lock acquisition attempts |

The `key` expression determines lock granularity. Using `"account:" + input.account_id` means only flows for the same account are serialized — flows for different accounts run in parallel.

### Lock Example: Inventory Reservation

```hcl
flow "reserve_inventory" {
  from { connector = "api", operation = "POST /reservations" }

  lock {
    storage = "connector.redis"
    key     = "'inventory:' + input.product_id"
    timeout = "10s"
  }

  step "current" {
    connector = "db"
    query     = "SELECT stock FROM products WHERE id = ?"
    params    = [input.product_id]
  }

  transform {
    product_id = "input.product_id"
    quantity   = "input.quantity"
    reserved   = "step.current.stock >= input.quantity"
  }

  to {
    connector = "db"
    target    = "UPDATE products"
    when      = "step.current.stock >= input.quantity"
  }
}
```

## Semaphore

A semaphore limits the number of concurrent flow executions globally. Use it when calling external services with rate limits or quotas.

```hcl
flow "call_ai_api" {
  from { connector = "api", operation = "POST /analyze" }

  semaphore {
    storage = "connector.redis"
    key     = "'ai_api_quota'"
    limit   = 5        # Max 5 concurrent calls
    timeout = "10s"    # Wait up to 10s for a slot
  }

  to { connector = "ai_service", operation = "POST /analyze" }
}
```

### Semaphore Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage` | string | yes | — | Cache connector name |
| `key` | string | yes | — | Semaphore name |
| `limit` | int | yes | — | Maximum concurrent flows allowed |
| `timeout` | string | no | `"30s"` | Maximum wait time for a slot |

### Semaphore Example: External API Rate Limiting

```hcl
flow "geocode_address" {
  from { connector = "api", operation = "POST /geocode" }

  semaphore {
    storage = "connector.redis"
    key     = "'google_maps_quota'"
    limit   = 20        # Google Maps allows 50 QPS, leave buffer
    timeout = "5s"
  }

  to { connector = "maps_api", operation = "POST /geocode/json" }
}
```

## Coordinate (Signal/Wait)

Coordinate synchronizes dependent flows. One flow signals completion, another waits for that signal before proceeding.

```hcl
# Producer: signals when data is ready
flow "produce_batch" {
  from { connector = "api", operation = "POST /batches" }
  to   { connector = "db", target = "batches" }

  coordinate {
    storage = "connector.redis"
    signal  = "batch_ready"
    key     = "input.batch_id"
  }
}

# Consumer: waits for signal
flow "process_batch" {
  from { connector = "api", operation = "POST /batches/:id/process" }

  coordinate {
    storage = "connector.redis"
    wait    = "batch_ready"
    key     = "input.params.id"
    timeout = "60s"
  }

  step "batch" {
    connector = "db"
    query     = "SELECT * FROM batches WHERE id = ?"
    params    = [input.params.id]
  }

  to { connector = "db", target = "results" }
}
```

### Coordinate Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `storage` | string | yes | Cache connector name |
| `key` | string | yes | CEL expression scoping the coordination |
| `signal` | string | — | Name of the signal to emit (use in producer flow) |
| `wait` | string | — | Name of the signal to wait for (use in consumer flow) |
| `timeout` | string | — | Maximum wait time (only on wait side) |

A `coordinate` block must have either `signal` or `wait`, not both.

## Combining Synchronization

You can combine synchronization primitives in a single flow:

```hcl
flow "critical_payment" {
  from { connector = "rabbit", operation = "payments" }

  # Deduplicate first
  dedupe {
    storage      = "connector.redis"
    key          = "input.payment_id"
    ttl          = "24h"
    on_duplicate = "skip"
  }

  # Then lock the account
  lock {
    storage = "connector.redis"
    key     = "'account:' + input.account_id"
    timeout = "30s"
  }

  # Limit concurrent external payment API calls
  semaphore {
    storage = "connector.redis"
    key     = "'payment_gateway'"
    limit   = 10
    timeout = "10s"
  }

  to { connector = "payment_gateway", operation = "POST /charge" }
}
```

## Setup Requirements

All synchronization primitives require a Redis connector:

```hcl
connector "redis" {
  type    = "cache"
  driver  = "redis"
  address = env("REDIS_ADDRESS")
}
```

Reference it in synchronization blocks as `connector.redis` or just `"redis"`.

## See Also

- [Examples: Sync](../../examples/sync)
- [Guides: Caching](caching.md)
