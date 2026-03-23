# MongoDB CRUD via REST

flow "mongo_list_users" {
  from {
    connector = "api"
    operation = "GET /mongo/users"
  }
  to {
    connector = "mongodb"
    target    = "users"
  }
}

flow "mongo_get_user" {
  from {
    connector = "api"
    operation = "GET /mongo/users/:id"
  }
  to {
    connector = "mongodb"
    target    = "users"
  }
}

flow "mongo_create_user" {
  from {
    connector = "api"
    operation = "POST /mongo/users"
  }
  to {
    connector = "mongodb"
    target    = "users"
  }
}

flow "mongo_delete_user" {
  from {
    connector = "api"
    operation = "DELETE /mongo/users/:id"
  }
  to {
    connector = "mongodb"
    target    = "users"
  }
}
