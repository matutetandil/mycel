# MySQL CRUD via REST

flow "mysql_list_users" {
  from {
    connector = "api"
    operation = "GET /mysql/users"
  }
  to {
    connector = "mysql"
    target    = "users"
  }
}

flow "mysql_get_user" {
  from {
    connector = "api"
    operation = "GET /mysql/users/:id"
  }
  to {
    connector = "mysql"
    target    = "users"
  }
}

flow "mysql_create_user" {
  from {
    connector = "api"
    operation = "POST /mysql/users"
  }
  validate {
    input = "type.user"
  }
  to {
    connector = "mysql"
    target    = "users"
  }
}

flow "mysql_delete_user" {
  from {
    connector = "api"
    operation = "DELETE /mysql/users/:id"
  }
  to {
    connector = "mysql"
    target    = "users"
  }
}
