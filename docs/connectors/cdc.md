# CDC (Change Data Capture)

Stream database changes in real time via logical replication. Instead of polling, Mycel connects as a replication client and receives INSERT, UPDATE, and DELETE events the moment they happen. Use it for event sourcing, audit trails, cache invalidation, or cross-service synchronization.

Currently supports PostgreSQL (pgoutput plugin).

## Configuration

```hcl
connector "pg_cdc" {
  type   = "cdc"
  driver = "postgres"

  host        = "localhost"
  port        = 5432
  database    = "myapp"
  user        = "replication_user"
  password    = env("DB_PASSWORD")
  slot_name   = "mycel_slot"
  publication = "mycel_pub"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | — | Database driver (`postgres`) |
| `host` | string | `"localhost"` | Database host |
| `port` | int | `5432` | Database port |
| `database` | string | — | Database name |
| `user` | string | — | Replication user |
| `password` | string | — | Password |
| `slot_name` | string | `"mycel_slot"` | Replication slot name |
| `publication` | string | `"mycel_pub"` | PostgreSQL publication name |

**Prerequisites:** PostgreSQL must have `wal_level = logical` and the user must have `REPLICATION` privilege.

## Operations

Operations use `TRIGGER:TABLE` format. Source only — CDC does not support write operations.

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `INSERT:table` | source | New row inserted |
| `UPDATE:table` | source | Row updated |
| `DELETE:table` | source | Row deleted |
| `*:table` | source | Any change on a table |
| `INSERT:*` | source | Inserts on any table |
| `*:*` | source | All changes on all tables |

The flow handler receives: `input.trigger`, `input.table`, `input.schema`, `input.new` (new row for INSERT/UPDATE), `input.old` (old row for UPDATE/DELETE), and `input.timestamp`.

`input.old` carries only the key columns unless the table is set to
`ALTER TABLE <name> REPLICA IDENTITY FULL`. Without it, a flow comparing what a
row was against what it now is sees only the id.

## Delivery

The replication slot is what makes this different from polling: PostgreSQL
keeps the WAL a slot has not confirmed, so changes written while Mycel is down
are still there when it reconnects, and streaming resumes from the slot rather
than from the present.

That resumption is **at-least-once**. How far a consumer has got is reported to
the server periodically, so a change already handled but not yet confirmed when
the connection dropped is delivered again. A flow that must not act twice on the
same change says so with a [`dedupe` block](../core-concepts/flows.md) keyed on
something stable in the row.

The other side of a slot is that it costs disk. PostgreSQL cannot recycle WAL a
slot still needs, so a slot belonging to a service that is never coming back
grows the database's disk until it is dropped:

```sql
SELECT slot_name, active, pg_size_pretty(
  pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained
FROM pg_replication_slots;

SELECT pg_drop_replication_slot('mycel_slot');
```

## Example

```hcl
# React to new user inserts
flow "on_user_created" {
  from {
    connector = "pg_cdc"
    operation = "INSERT:users"
  }
  transform {
    event = "'user.created'"
    data  = "input.new"
  }
  to {
    connector = "events_db"
    target    = "events"
  }
}

# Track order status changes
flow "on_order_updated" {
  from {
    connector = "pg_cdc"
    operation = "UPDATE:orders"
  }
  transform {
    event  = "'order.updated'"
    before = "input.old"
    after  = "input.new"
  }
  to {
    connector = "rabbit"
    operation = "PUBLISH"
    target    = "order.events"
  }
}

# Monitor all changes on a table
flow "audit_products" {
  from {
    connector = "pg_cdc"
    operation = "*:products"
  }
  to {
    connector = "audit_db"
    target    = "change_log"
  }
}
```

See the [cdc example](https://github.com/matutetandil/mycel/tree/main/examples/cdc) for a complete working setup.

---

> **Full configuration reference:** See [CDC](../reference/configuration.md#cdc) in the Configuration Reference.
