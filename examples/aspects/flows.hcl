# Flows for Aspects Example
# Note: Aspects are defined separately in aspects.hcl
# They are automatically applied to flows based on pattern matching.

# Get all products - cache aspect will be applied automatically
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

# Get single product by ID - cache aspect will be applied
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }
  to {
    connector = "db"
    target    = "products"
  }
}

# Create a product - audit aspect will log this operation
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }
  transform {
    id         = "uuid()"
    created_at = "now()"
  }
  to {
    connector = "db"
    target    = "products"
  }
}

# Update a product - audit aspect will log this
flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  transform {
    updated_at = "now()"
  }
  to {
    connector = "db"
    target    = "products"
  }
}

# Delete a product - audit aspect will log this
flow "delete_product" {
  from {
    connector = "api"
    operation = "DELETE /products/:id"
  }
  to {
    connector = "db"
    target    = "products"
  }
}

# Get all users
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

# Create a user
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  transform {
    id         = "uuid()"
    created_at = "now()"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
