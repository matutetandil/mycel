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

## Example

```hcl
# React to new user inserts
flow "on_user_created" {
  from { connector = "pg_cdc", operation = "INSERT:users" }
  transform {
    output.event = "'user.created'"
    output.data  = "input.new"
  }
  to { connector = "events_db", target = "events" }
}

# Track order status changes
flow "on_order_updated" {
  from { connector = "pg_cdc", operation = "UPDATE:orders" }
  transform {
    output.event  = "'order.updated'"
    output.before = "input.old"
    output.after  = "input.new"
  }
  to { connector = "rabbit", operation = "PUBLISH", target = "order.events" }
}

# Monitor all changes on a table
flow "audit_products" {
  from { connector = "pg_cdc", operation = "*:products" }
  to   { connector = "audit_db", target = "change_log" }
}
```

See the [cdc example](../../examples/cdc/) for a complete working setup.
