# Full-text search with query DSL
flow "search_products" {
  from {
    connector = "api"
    operation = "GET /search"
  }

  step "results" {
    connector = "es"
    operation = "search"
    target    = "products"
    body = {
      "query" = {
        "multi_match" = {
          "query"  = "input.query.q"
          "fields" = ["name^2", "description"]
        }
      }
    }
  }

  transform {
    output.results = "step.results"
  }

  to { response }
}

# Get single product by ID
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  to {
    connector = "es"
    target    = "products"
    operation = "get"
  }
}

# Index a new product
flow "index_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  to {
    connector = "es"
    target    = "products"
    operation = "index"
  }
}

# Update a product
flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }

  to {
    connector = "es"
    target    = "products"
    operation = "update"
  }
}

# Delete a product
flow "delete_product" {
  from {
    connector = "api"
    operation = "DELETE /products/:id"
  }

  to {
    connector = "es"
    target    = "products"
    operation = "delete"
  }
}

# Count products by status
flow "count_active" {
  from {
    connector = "api"
    operation = "GET /products/count"
  }

  to {
    connector = "es"
    target    = "products"
    operation = "count"
  }
}
