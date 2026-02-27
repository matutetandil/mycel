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

| Option | Type | Drivers | Description |
|--------|------|---------|-------------|
| `driver` | string | all | `sqlite`, `postgres`, `mysql`, `mongodb` |
| `database` | string | all | Database name or file path |
| `host` | string | pg/mysql | Database host |
| `port` | int | pg/mysql | Database port |
| `user` | string | pg/mysql/mongo | Username |
| `password` | string | pg/mysql/mongo | Password |
| `ssl_mode` | string | pg | SSL mode |
| `charset` | string | mysql | Character set |
| `uri` | string | mongo | Full connection URI |
| `pool.max` | int | pg/mysql/mongo | Max connections |
| `pool.min` | int | pg/mysql/mongo | Min connections |
| `pool.max_lifetime` | int | pg/mysql | Max connection lifetime (seconds) |

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
