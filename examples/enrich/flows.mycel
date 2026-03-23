# Flow configurations
# NOTE: enrich blocks are a planned feature (not yet implemented)

# Example: Get product
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  transform {
    id   = "input.id"
    name = "input.name"
  }

  to {
    connector = "products_db"
    operation = "products"
  }
}

# Example: Get product with transform
flow "get_product_with_transform" {
  from {
    connector = "api"
    operation = "GET /products/:id/v2"
  }

  transform {
    use = "transform.normalize_product"

    # Additional inline mappings
    fetched_at = "now()"
  }

  to {
    connector = "products_db"
    operation = "products"
  }
}
