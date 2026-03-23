# Elasticsearch flows

# Index document via to block with operation override
# (handleCreate defaults to "INSERT", but ES needs "index")
flow "es_index" {
  from {
    connector = "api"
    operation = "POST /es/products"
  }
  to {
    connector = "es"
    target    = "products"
    operation = "index"
  }
}

flow "es_get" {
  from {
    connector = "api"
    operation = "GET /es/products/:id"
  }
  to {
    connector = "es"
    target    = "products"
    operation = "get"
  }
}

flow "es_search" {
  from {
    connector = "api"
    operation = "GET /es/search"
  }
  step "results" {
    connector = "es"
    operation = "search"
    target    = "products"
    body = {
      "query" = {
        "multi_match" = {
          "query"  = "input.query.q"
          "fields" = ["name", "description"]
        }
      }
    }
  }
  transform {
    data = "step.results"
  }
  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "es"
    target    = "products"
  }
}

flow "es_delete" {
  from {
    connector = "api"
    operation = "DELETE /es/products/:id"
  }
  to {
    connector = "es"
    target    = "products"
    operation = "delete"
  }
}
