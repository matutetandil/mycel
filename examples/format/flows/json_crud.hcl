# Standard JSON CRUD flows
# No format declaration needed - JSON is the default

# List all products
flow "get_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  to {
    connector = "sqlite"
    target    = "products"
  }
}

# Get product by ID
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  to {
    connector = "sqlite"
    target    = "products"
  }
}

# Create product
flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  to {
    connector = "sqlite"
    target    = "products"
  }
}
