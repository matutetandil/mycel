# Flows for testing aspect response enrichment

# v1 endpoint — matched by deprecation aspect
flow "get_products_v1" {
  from {
    connector = "api"
    operation = "GET /aspects/v1/products"
  }
  to {
    connector = "sqlite"
    target    = "products"
  }
}

# v2 endpoint — NOT matched by deprecation aspect
flow "get_products_v2" {
  from {
    connector = "api"
    operation = "GET /aspects/v2/products"
  }
  to {
    connector = "sqlite"
    target    = "products"
  }
}

# Init endpoint — seed data for aspect tests
flow "aspects_init" {
  from {
    connector = "api"
    operation = "POST /aspects/init"
  }
  step "create_table" {
    connector = "sqlite"
    query     = "CREATE TABLE IF NOT EXISTS products (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, price REAL NOT NULL)"
  }
  transform {
    name  = "'Widget'"
    price = "9.99"
  }
  to {
    connector = "sqlite"
    target    = "products"
  }
}

# Create product — matched by audit aspect (create_*)
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /aspects/products"
  }
  to {
    connector = "sqlite"
    target    = "products"
  }
}

# List endpoint — matched by count aspect (list_*)
flow "list_products" {
  from {
    connector = "api"
    operation = "GET /aspects/products"
  }
  to {
    connector = "sqlite"
    target    = "products"
  }
}
