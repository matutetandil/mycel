# Batch Processing and Scheduled Jobs

## Batch Processing

The `batch` block processes large datasets in chunks within a flow. Instead of loading all records into memory, it reads from a source connector in pages, optionally transforms each item, and writes each chunk to a target. Use it for data migrations, ETL jobs, reindexing, or any operation that needs to iterate over thousands of records safely.

### Basic Batch Flow

```hcl
flow "migrate_users" {
  from { connector = "api", operation = "POST /admin/migrate" }

  batch {
    source     = "old_db"
    query      = "SELECT * FROM users ORDER BY id"
    chunk_size = 100
    on_error   = "continue"

    transform {
      email     = "input.email.lowerAscii()"
      name      = "input.name"
      migrated  = "true"
    }

    to {
      connector = "new_db"
      target    = "users"
      operation = "INSERT"
    }
  }
}
```

### Batch with Parameters

Pass runtime parameters from the triggering request into the batch query:

```hcl
flow "reindex_products" {
  from { connector = "api", operation = "POST /admin/reindex" }

  batch {
    source     = "postgres"
    query      = "SELECT * FROM products WHERE updated_at > :since ORDER BY id"
    params     = { since = "input.since" }
    chunk_size = 500

    to {
      connector = "es"
      target    = "products"
      operation = "index"
    }
  }
}
```

The `input._batch_input` variable contains the original flow input for use in parameterized queries.

### Batch Attributes

| Attribute | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `source` | string | yes | — | Connector to read from |
| `query` | string | yes | — | SQL query to paginate over |
| `chunk_size` | int | no | 100 | Records per chunk |
| `params` | map | no | — | Query parameters (CEL expressions as values) |
| `on_error` | string | no | `"stop"` | `"stop"` or `"continue"` on chunk failure |
| `transform` | block | no | — | Transform applied to each item |
| `to` | block | yes | — | Target connector for each chunk |

### Error Handling

- `on_error = "stop"` (default) — halt on the first failed chunk
- `on_error = "continue"` — skip failed chunks and keep going

The flow response includes batch stats:

```json
{
  "processed": 950,
  "failed": 50,
  "chunks": 10,
  "errors": ["Chunk 3: duplicate key violation on item 45"]
}
```

### Transform in Batch

Each item's fields are available as `input.*` inside the batch transform:

```hcl
batch {
  source     = "postgres"
  query      = "SELECT * FROM legacy_users"
  chunk_size = 200

  transform {
    id         = "input.user_id"              # Rename field
    email      = "lower(trim(input.email))"   # Normalize
    status     = "input.active ? 'active' : 'inactive'"
    migrated   = "true"
    migrated_at = "now()"
  }

  to {
    connector = "new_db"
    target    = "users"
    operation = "INSERT"
  }
}
```

### Batch with Delta Processing

Process only records changed since last run:

```hcl
flow "incremental_sync" {
  from { connector = "api", operation = "POST /sync" }

  step "last_sync" {
    connector = "db"
    query     = "SELECT value FROM sync_state WHERE key = 'last_sync_at'"
  }

  batch {
    source     = "source_db"
    query      = "SELECT * FROM records WHERE updated_at > :last_sync ORDER BY updated_at"
    params     = { last_sync = "step.last_sync.value" }
    chunk_size = 1000

    to {
      connector = "target_db"
      target    = "records"
      operation = "INSERT"
    }
  }

  to {
    connector = "db"
    target    = "UPDATE sync_state"
    set       = { value = "now()" }
    where     = { key = "last_sync_at" }
  }
}
```

## Scheduled Jobs

Run flows on a schedule instead of from a connector event. Use the `when` attribute with a cron expression or shorthand.

### Cron Syntax

```hcl
flow "daily_cleanup" {
  when = "0 3 * * *"    # Every day at 3:00 AM UTC

  to {
    connector = "db"
    query     = "DELETE FROM sessions WHERE expires_at < now()"
  }
}
```

Standard cron format: `minute hour day-of-month month day-of-week`

| Field | Range | Examples |
|-------|-------|---------|
| minute | 0-59 | `0`, `*/15` |
| hour | 0-23 | `3`, `*/6` |
| day-of-month | 1-31 | `1`, `*/2` |
| month | 1-12 | `*`, `6` |
| day-of-week | 0-6 (0=Sunday) | `*`, `1-5` |

### Shorthand Schedules

| Shorthand | Equivalent | Description |
|-----------|-----------|-------------|
| `@hourly` | `0 * * * *` | Every hour at minute 0 |
| `@daily` | `0 0 * * *` | Every day at midnight |
| `@weekly` | `0 0 * * 0` | Every Sunday at midnight |
| `@monthly` | `0 0 1 * *` | First day of each month |
| `@every 5m` | — | Every 5 minutes |
| `@every 1h` | — | Every hour |
| `@every 30s` | — | Every 30 seconds |

### Examples

```hcl
# Health ping every 5 minutes
flow "health_ping" {
  when = "@every 5m"
  to   { connector = "monitoring", operation = "POST /heartbeat" }
}

# Weekly report
flow "weekly_report" {
  when = "0 9 * * 1"  # Every Monday at 9 AM

  step "stats" {
    connector = "db"
    query     = "SELECT count(*) as users, sum(revenue) as revenue FROM weekly_stats"
  }

  to {
    connector = "slack"
    operation = "chat.postMessage"
    transform {
      channel = "'#metrics'"
      text    = "'Weekly stats: ' + string(step.stats.users) + ' users, $' + string(step.stats.revenue)"
    }
  }
}

# Monthly billing cycle
flow "monthly_billing" {
  when = "0 0 1 * *"  # First of every month

  batch {
    source     = "db"
    query      = "SELECT * FROM active_subscriptions"
    chunk_size = 100

    to {
      connector = "stripe"
      operation = "POST /charges"
    }
  }
}
```

### Preventing Duplicate Execution

Use `lock` to ensure a scheduled job doesn't run concurrently on multiple instances:

```hcl
flow "daily_sync" {
  when = "@daily"

  lock {
    storage = "connector.redis"
    key     = "'daily_sync_lock'"
    timeout = "1h"
    wait    = false  # Skip if another instance is running
  }

  batch {
    source     = "source_db"
    query      = "SELECT * FROM records WHERE synced = false"
    chunk_size = 500

    to {
      connector = "target_db"
      target    = "records"
    }
  }
}
```

## See Also

- [Core Concepts: Flows](../core-concepts/flows.md) — flow trigger (`when`) reference
- [Guides: Synchronization](synchronization.md) — lock for preventing duplicate jobs
- [Examples: Batch](../../examples/batch)
