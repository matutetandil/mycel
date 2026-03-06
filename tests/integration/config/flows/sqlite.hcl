# SQLite initialization (create table if not exists)
flow "sqlite_init" {
  from {
    connector = "api"
    operation = "POST /sqlite/init"
  }
  step "create_table" {
    connector = "sqlite"
    query     = "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, email TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)"
  }
  transform {
    name  = "'__init__'"
    email = "'init@setup'"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

# SQLite CRUD via REST

flow "sqlite_list_users" {
  from {
    connector = "api"
    operation = "GET /sqlite/users"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "sqlite_get_user" {
  from {
    connector = "api"
    operation = "GET /sqlite/users/:id"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "sqlite_create_user" {
  from {
    connector = "api"
    operation = "POST /sqlite/users"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}

flow "sqlite_delete_user" {
  from {
    connector = "api"
    operation = "DELETE /sqlite/users/:id"
  }
  to {
    connector = "sqlite"
    target    = "users"
  }
}
