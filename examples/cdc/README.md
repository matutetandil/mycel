# CDC (Change Data Capture) Example

This example demonstrates PostgreSQL Change Data Capture using logical replication. Database changes (INSERT/UPDATE/DELETE) are streamed in real-time and dispatched as flow events.

## Prerequisites

PostgreSQL 10+ with logical replication enabled:

```sql
-- 1. Enable logical replication (requires PostgreSQL restart)
ALTER SYSTEM SET wal_level = logical;

-- 2. Create a replication user
CREATE ROLE replication_user WITH REPLICATION LOGIN PASSWORD 'secret';

-- 3. Grant access to the database
GRANT CONNECT ON DATABASE myapp TO replication_user;
GRANT USAGE ON SCHEMA public TO replication_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO replication_user;

-- 4. Create a publication (or let Mycel auto-create it)
CREATE PUBLICATION mycel_pub FOR ALL TABLES;
```

After changing `wal_level`, restart PostgreSQL.

## Operation Format

Operations use `TRIGGER:TABLE` format:

| Operation | Matches |
|-----------|---------|
| `INSERT:users` | Inserts on the `users` table |
| `UPDATE:orders` | Updates on the `orders` table |
| `DELETE:sessions` | Deletes on the `sessions` table |
| `*:products` | Any change on the `products` table |
| `INSERT:*` | Inserts on any table |
| `*:*` | All changes on all tables |

## Input Format

Flow handlers receive:

```json
{
  "trigger": "INSERT",
  "table": "users",
  "schema": "public",
  "new": {"id": 1, "email": "alice@example.com", "name": "Alice"},
  "old": null,
  "timestamp": "2026-02-27T10:30:00Z"
}
```

- **INSERT**: `new` populated, `old` is null
- **UPDATE**: both `new` and `old` populated
- **DELETE**: `old` populated, `new` is null

## Run

```bash
export DB_PASSWORD=secret
mycel start --config ./examples/cdc
```

## How It Works

1. Mycel connects to PostgreSQL as a logical replication client
2. Creates a replication slot and publication (if they don't exist)
3. Streams WAL changes via the `pgoutput` plugin
4. Decodes changes into structured events
5. Dispatches events to matching flow handlers
