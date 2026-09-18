# Database

Relational and document database connectors. All use `type = "database"` with a `driver` to select the backend. Supports query, insert, update, and delete operations.

## SQLite

```hcl
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"
}
```

Mycel opens the database in [WAL mode](https://www.sqlite.org/wal.html) with a
five-second busy timeout and foreign keys on, so concurrent requests queue for
the write lock instead of failing, and readers do not block the writer. WAL
keeps two files next to the database — `app.db-wal` and `app.db-shm` — which
belong in `.gitignore` along with the database itself.

An in-memory database (`":memory:"`) is per connection, and the pool opens
several: what one request writes, the next one will not find. Use a file.

## PostgreSQL

```hcl
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("PG_HOST")
  port     = 5432
  database = env("PG_DATABASE")
  user     = env("PG_USER")
  password = env("PG_PASSWORD")
  ssl_mode = "require"    # "disable", "require", "verify-full"

  pool {
    max          = 100
    min          = 10
    max_lifetime = 300    # seconds
  }
}
```

## MySQL

```hcl
connector "mysql" {
  type     = "database"
  driver   = "mysql"
  host     = env("MYSQL_HOST")
  port     = 3306
  database = env("MYSQL_DATABASE")
  user     = env("MYSQL_USER")
  password = env("MYSQL_PASSWORD")
  charset  = "utf8mb4"

  pool {
    max          = 100
    min          = 10
    max_lifetime = 300
  }
}
```

## MongoDB

```hcl
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  uri      = env("MONGO_URI")
  database = "myapp"

  pool {
    max             = 200
    min             = 10
    connect_timeout = 30
  }
}
```

## Common Options

| Option | Type | Required | Drivers | Description |
|--------|------|----------|---------|-------------|
| `driver` | string | **yes** | all | `sqlite`, `postgres`, `mysql`, `mongodb` |
| `database` | string | **yes** | all | Database name or file path |
| `host` | string | optional | pg/mysql | Database host (default: `localhost`) |
| `port` | int | optional | pg/mysql | Database port (default: `5432`/`3306`) |
| `user` | string | **yes** | pg/mysql | Username |
| `password` | string | optional | pg/mysql | Password |
| `ssl_mode` | string | optional | pg | `disable`, `require`, `verify-full` (default: `disable`) |
| `charset` | string | optional | mysql | Character set (default: `utf8mb4`) |
| `uri` | string | optional | mongo | Full connection URI (alternative to host/port/user/password) |
| `pool.max` | int | optional | pg/mysql/mongo | Max connections (default: `25`) |
| `pool.min` | int | optional | pg/mysql/mongo | Min idle connections (default: `5`) |
| `pool.max_lifetime` | int | optional | pg/mysql | Max connection lifetime in seconds |
| `pool.connect_timeout` | int | optional | mongo | Connection timeout in seconds |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `query` / `SELECT ...` | read | Query rows |
| `INSERT` | write | Insert rows |
| `UPDATE` | write | Update rows |
| `DELETE` | write | Delete rows |
| target name (table) | read/write | Auto-detect from flow context |

## The record that is already there (MongoDB)

A destination writing to Mongo can say which fields identify the record it writes and what to do when the collection already holds it, instead of spelling out a filter document, an update document and an upsert param that have to agree:

```hcl
to {
  connector    = "archive"
  target       = "payload_archive"
  conflict_key = "sku"          # or ["store", "sku"] for a composite key
  on_conflict  = "replace"
  ttl          = "30d"
}
```

| Attribute | Description |
|-----------|-------------|
| `conflict_key` | Field, or list of fields, that identifies the record. Without it every write is a new document. |
| `on_conflict` | What to do when the record is already stored: `update` (default), `replace`, `skip`, `error` |
| `ttl` | How long the record stays: `"30d"`, `"12h"`, `"90m"` |

| `on_conflict` | What the store does | When you want it |
|---------------|--------------------|------------------|
| `update` | Merges the payload into the stored document; fields it does not mention survive | Messages carry part of the record |
| `replace` | The new document wins whole; fields it does not mention are gone | Messages carry the whole record — "the last payload per SKU" |
| `skip` | Keeps what is stored, writes only if the record is new | First value wins |
| `error` | Fails the write. Backed by a unique index, so it is the store refusing rather than a race between two consumers | A duplicate is a problem to report |

The result says which of those happened, in `outcome`: `inserted`, `updated`, `replaced`, `skipped` or `unchanged`. The affected count cannot tell them apart — an upsert that inserted and one that overwrote a stored record both report one.

A payload that does not carry the fields `conflict_key` names fails the write. Writing it anyway would identify every message by `{sku: null}`, so the collection would hold one document: whichever message arrived last, under no key at all.

### Expiry

`ttl` makes the **store** expire the record; Mycel never deletes anything for it. On the first write to a collection Mycel creates a TTL index (`mycel_ttl`, with `expireAfterSeconds: 0`) and every document it writes carries its own deadline in `_mycel_expires_at`.

Two things follow from that, and both are deliberate:

- **The deadline lives in the document, not in the index.** `expireAfterSeconds` cannot be changed by `createIndex` — it needs `collMod` — so an index built from the configured duration would keep the duration it was first created with, and lowering a `ttl` from a month to a day would go on keeping documents for a month with nothing said.
- **The deadline is a BSON date.** A TTL index reads nothing else: a timestamp written by `now()` is a string, and a TTL index over a string field expires nothing, silently. This is why the field is Mycel's own rather than one of yours.

Mongo's TTL monitor runs about once a minute, so a document disappears shortly after its deadline rather than exactly on it.

### From an aspect

An archive of what arrived is usually written from an aspect, and the same three attributes work there:

```hcl
aspect "archive_last_payload" {
  on   = ["update_sku", "create_sku"]
  when = "after"

  action {
    connector    = "archive"
    target       = "payload_archive"
    conflict_key = "sku"
    on_conflict  = "replace"
    ttl          = "30d"

    transform {
      sku      = "string(input.sku)"
      received = "now()"
      payload  = "input"     # the whole message, headers included
    }
  }
}
```

### SQL

These three are refused at startup on a SQL destination, because nothing there would act on them: a `to` block is open, so they would be swept into the connector params and ignored — a file saying "the last payload per SKU, kept for a month" while the service appends a row per message, forever.

In SQL, write the upsert as the query it is, and let the database expire the rows:

```hcl
to {
  connector = "store"
  target    = "payload_archive"
  query     = "INSERT INTO payload_archive (sku, payload, updated_at) VALUES (:sku, :payload, :updated_at) ON CONFLICT (sku) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at"
}
```

Retention there is a scheduled `DELETE` (a `when` flow, `pg_cron`, or partitioning by day) rather than an attribute.

## Transactional writes

To write several statements **atomically** on a single pinned connection (with
`LAST_INSERT_ID` / `SELECT` capture and per-element iteration), use the
`to { transaction { } }` block instead of a single `query`/`target`. See
[Flows → Transactional write](../core-concepts/flows.md#transactional-write-transaction)
and the [transactional-write example](https://github.com/matutetandil/mycel/tree/main/examples/transactional-write).

## Example

```hcl
flow "list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  step "user" {
    connector = "db"
    query     = "SELECT * FROM users WHERE id = :id"
    params = {
      id = "input.id"
    }
  }

  response {
    user = "step.user"
  }
}
```

See the [basic example](https://github.com/matutetandil/mycel/tree/main/examples/basic) (SQLite) and [mongodb example](https://github.com/matutetandil/mycel/tree/main/examples/mongodb) for complete setups.

---

> **Full configuration reference:** See [Database](../reference/configuration.md#database) in the Configuration Reference.
