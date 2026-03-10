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

## Example

```hcl
flow "list_users" {
  from { connector = "api", operation = "GET /users" }
  to   { connector = "db", target = "users" }
}

flow "get_user" {
  from { connector = "api", operation = "GET /users/:id" }

  step "user" {
    connector = "db"
    operation = "query"
    query     = "SELECT * FROM users WHERE id = ?"
    params    = [input.params.id]
  }

  transform { output = "step.user" }
  to { response }
}
```

See the [basic example](../../examples/basic/) (SQLite) and [mongodb example](../../examples/mongodb/) for complete setups.

---

> **Full configuration reference:** See [Database](../reference/configuration.md#database) in the Configuration Reference.
