# Get product pricing from the currently active profile
# The response format is normalized regardless of which backend is used
flow "get_product_price" {
  from {
    connector = "api"
    operation = "GET /products/:id/price"
  }

  to {
    connector = "pricing"
    target    = "products"  # For database profiles
    # For HTTP profiles, this would be used as the path suffix
  }
}

# Get all products with pricing
flow "get_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  to {
    connector = "pricing"
    target    = "products"
  }
}

# Health check endpoint
flow "health" {
  from {
    connector = "api"
    operation = "GET /health"
  }

  transform {
    status  = "'healthy'"
    service = "'pricing-service'"
  }
}
