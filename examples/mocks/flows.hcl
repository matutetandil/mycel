# Flows for mock example

flow "get_users" {
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
  to {
    connector = "db"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
