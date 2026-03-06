# PostgreSQL CRUD via REST

flow "pg_list_users" {
  from {
    connector = "api"
    operation = "GET /pg/users"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}

flow "pg_get_user" {
  from {
    connector = "api"
    operation = "GET /pg/users/:id"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}

flow "pg_create_user" {
  from {
    connector = "api"
    operation = "POST /pg/users"
  }
  validate {
    input = "type.user"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}

flow "pg_create_item" {
  from {
    connector = "api"
    operation = "POST /pg/items"
  }
  to {
    connector = "postgres"
    target    = "items"
  }
}

flow "pg_delete_user" {
  from {
    connector = "api"
    operation = "DELETE /pg/users/:id"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}
