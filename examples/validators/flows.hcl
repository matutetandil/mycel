// Flow Definitions for Validators Example

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  validate {
    input = "type.user"
  }

  transform {
    id         = "uuid()"
    username   = "input.username"
    email      = "lower(input.email)"
    password   = "input.password"  // In real app, would be hashed
    age        = "input.age"
    phone      = "input.phone"
    status     = "'pending'"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "users"
  }
}

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

flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  validate {
    input = "type.product"
  }

  transform {
    id         = "uuid()"
    name       = "input.name"
    slug       = "lower(replace(input.name, ' ', '-'))"
    price      = "input.price"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "products"
  }
}

flow "get_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }
  to {
    connector = "db"
    target    = "products"
  }
}
