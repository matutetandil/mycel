# Flow configurations with enrichment

# Example 1: Enrich at flow level
# Get product with price from external pricing service
flow "get_product_with_price" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  # Enrich with pricing data from external service
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  # Transform combines input data with enriched data
  transform {
    id       = "input.id"
    name     = "input.name"
    price    = "enriched.pricing.price"
    currency = "enriched.pricing.currency"
  }

  to {
    connector = "products_db"
    target    = "products"
  }
}

# Example 2: Multiple enrichments
# Get product with price AND inventory
flow "get_product_full" {
  from {
    connector = "api"
    operation = "GET /products/:id/full"
  }

  # Enrich with pricing
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  # Enrich with inventory
  enrich "inventory" {
    connector = "inventory_service"
    operation = "GET /stock"
    params {
      sku = "input.sku"
    }
  }

  transform {
    id              = "input.id"
    name            = "input.name"
    price           = "enriched.pricing.price"
    currency        = "enriched.pricing.currency"
    stock_available = "enriched.inventory.available"
    stock_reserved  = "enriched.inventory.reserved"
    in_stock        = "enriched.inventory.available > 0"
  }

  to {
    connector = "products_db"
    target    = "products"
  }
}

# Example 3: Enrich inside transform (reusable)
# Uses a named transform that includes enrichment
flow "get_product_with_transform" {
  from {
    connector = "api"
    operation = "GET /products/:id/v2"
  }

  transform {
    use = "transform.with_pricing"

    # Additional inline mappings
    fetched_at = "now()"
  }

  to {
    connector = "products_db"
    target    = "products"
  }
}
