# Synchronization

Mycel provides three distributed synchronization primitives for coordinating concurrent flow executions. Each primitive owns its own storage configuration via an inline `storage {}` block — no separate cache connector is needed.

## When to Use Synchronization

- **Lock**: Exactly one flow must process a resource at a time (e.g., deducting from an account balance, updating inventory count)
- **Semaphore**: Limit concurrency to N simultaneous executions (e.g., external API with rate limits)
- **Coordinate**: One flow must wait for another to complete (e.g., a consumer waiting for a producer)

## Lock (Mutex)

A lock guarantees only one flow instance processes a specific resource at a time. Any concurrent flow that tries to acquire the same lock will wait (or timeout).

```hcl
flow "process_payment" {
  from {
    connector = "rabbit"
    operation = "payments"
  }

  lock {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'account:' + input.account_id"
    timeout = "30s"
    wait    = true
    retry   = "100ms"
  }

  to {
    connector = "db"
    target    = "UPDATE accounts"
  }
}
```

### Lock Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage` | block | yes | — | Inline storage config (`driver`, `url` or `host`/`port`) |
| `key` | string | yes | — | CEL expression for the lock key (scopes the lock to a resource) |
| `timeout` | string | no | `"30s"` | Maximum time to hold the lock |
| `wait` | bool | no | `true` | Block until the lock is available |
| `retry` | string | no | `"100ms"` | Interval between lock acquisition attempts |

The `key` expression determines lock granularity. Using `"account:" + input.account_id` means only flows for the same account are serialized — flows for different accounts run in parallel.

### Lock Example: Inventory Reservation

```hcl
flow "reserve_inventory" {
  from {
    connector = "api"
    operation = "POST /reservations"
  }

  lock {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
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
  from {
    connector = "api"
    operation = "POST /analyze"
  }

  semaphore {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'ai_api_quota'"
    limit   = 5        # Max 5 concurrent calls
    timeout = "10s"    # Wait up to 10s for a slot
  }

  to {
    connector = "ai_service"
    operation = "POST /analyze"
  }
}
```

### Semaphore Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage` | block | yes | — | Inline storage config (`driver`, `url` or `host`/`port`) |
| `key` | string | yes | — | Semaphore name |
| `limit` | int | yes | — | Maximum concurrent flows allowed |
| `timeout` | string | no | `"30s"` | Maximum wait time for a slot |

### Semaphore Example: External API Rate Limiting

```hcl
flow "geocode_address" {
  from {
    connector = "api"
    operation = "POST /geocode"
  }

  semaphore {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'google_maps_quota'"
    limit   = 20        # Google Maps allows 50 QPS, leave buffer
    timeout = "5s"
  }

  to {
    connector = "maps_api"
    operation = "POST /geocode/json"
  }
}
```

## Coordinate (Signal/Wait)

Coordinate synchronizes dependent flows. One flow signals completion, another waits for that signal before proceeding. Uses CEL expressions for conditional logic — both signal emission and wait behavior are controlled by `when` conditions evaluated at runtime.

```hcl
# Producer: signals when a parent entity is ready
flow "create_style" {
  from {
    connector = "rabbit"
    operation = "entities"
  }

  to {
    connector = "db"
    target    = "styles"
  }

  coordinate {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }

    signal {
      when = "true"
      emit = "'parent_ready:' + input.sku"
      ttl  = "24h"
    }
  }
}

# Consumer: waits for signal before proceeding
flow "create_item" {
  from {
    connector = "rabbit"
    operation = "entities"
  }

  # Check if parent already exists in DB
  step "check_parent" {
    connector = "db"
    query     = "SELECT entity_id FROM products WHERE sku = ?"
    params    = [input.parent_sku]
    on_error  = "default"
    default   = []
  }

  coordinate {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    timeout    = "5m"
    on_timeout = "fail"

    # Only wait if parent doesn't exist yet (fast-path skip)
    wait {
      when = "size(step.check_parent) == 0"
      for  = "'parent_ready:' + input.parent_sku"
    }
  }

  to {
    connector = "db"
    target    = "items"
  }
}
```

### Coordinate Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage` | block | yes | — | Inline storage config (`driver`, `url` or `host`/`port`) |
| `timeout` | duration | no | `"60s"` | Maximum time to wait for a signal |
| `on_timeout` | string | no | `"fail"` | Behavior on timeout: `"fail"`, `"retry"`, `"skip"`, `"pass"` |
| `max_retries` | int | no | `3` | Max retries when `on_timeout` is `"retry"` |
| `max_concurrent_waits` | int | no | `0` (unlimited) | Limit simultaneous waiting processes |

### wait sub-block

Defines when and what to wait for. The `when` condition is evaluated per message — if false, the wait is skipped entirely (fast-path).

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `when` | string | yes | CEL expression — wait only if this evaluates to `true` |
| `for` | string | yes | CEL expression for the signal key to wait for |

### signal sub-block

Defines when and what to signal. Emitted after the flow completes successfully.

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `when` | string | yes | CEL expression — signal only if this evaluates to `true` |
| `emit` | string | yes | CEL expression for the signal key to emit |
| `ttl` | duration | no | How long the signal remains valid |

### preflight sub-block

Defines a database check to run before waiting. If the check finds results, waiting is skipped. This is an alternative to using a `step` + `when` condition on the `wait` block.

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `connector` | string | yes | Connector for the check query |
| `query` | string | yes | SQL query or operation to execute |
| `params` | map | no | Parameter map (CEL expressions) |
| `if_exists` | string | no | `"pass"` (skip waiting) or `"fail"` (return error) |

### Coordinate with Preflight

Instead of using a separate `step` + conditional `wait`, you can use `preflight` for a self-contained check:

```hcl
coordinate {
  storage {
    driver = "redis"
    url    = env("REDIS_URL", "redis://localhost:6379")
  }
  timeout    = "5m"
  on_timeout = "fail"

  # Skip waiting if parent already exists in DB
  preflight {
    connector = "db"
    query     = "SELECT entity_id FROM products WHERE sku = ?"
    params    = { sku = "input.parent_sku" }
    if_exists = "pass"
  }

  wait {
    when = "true"
    for  = "'parent_ready:' + input.parent_sku"
  }

  signal {
    when = "true"
    emit = "'parent_ready:' + input.sku"
    ttl  = "24h"
  }
}
```

A `coordinate` block can have `wait`, `signal`, or both (for flows that both produce and consume entities).

## Combining Synchronization

You can combine synchronization primitives in a single flow:

```hcl
flow "critical_payment" {
  from {
    connector = "rabbit"
    operation = "payments"
  }

  # Deduplicate first
  dedupe {
    storage      = "connector.redis"
    key          = "input.payment_id"
    ttl          = "24h"
    on_duplicate = "skip"
  }

  # Then lock the account
  lock {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'account:' + input.account_id"
    timeout = "30s"
  }

  # Limit concurrent external payment API calls
  semaphore {
    storage {
      driver = "redis"
      url    = env("REDIS_URL", "redis://localhost:6379")
    }
    key     = "'payment_gateway'"
    limit   = 10
    timeout = "10s"
  }

  to {
    connector = "payment_gateway"
    operation = "POST /charge"
  }
}
```

## Setup Requirements

Each synchronization primitive owns its own storage via an inline `storage {}` block. No separate cache connector is needed — the sync block connects to Redis directly.

### Using a URL

```hcl
lock {
  storage {
    driver = "redis"
    url    = env("REDIS_URL", "redis://localhost:6379")
  }
  key = "'my_lock'"
}
```

### Using host / port

```hcl
lock {
  storage {
    driver   = "redis"
    host     = env("REDIS_HOST", "localhost")
    port     = env("REDIS_PORT", "6379")
    password = env("REDIS_PASSWORD", "")
    db       = env("REDIS_DB", "0")
  }
  key = "'my_lock'"
}
```

`port` and `db` accept either a numeric literal (`port = 6379`) or a string (`port = "6379"`), so values sourced from `env()` — which always returns strings — work without conversion.

Both forms (`url` and `host`/`port`) work for `lock`, `semaphore`, and `coordinate`. Use whichever matches your environment.

## See Also

- [Examples: Sync](../../examples/sync)
- [Guides: Caching](caching.md)
