# Rate Limited API Flows

# These endpoints are rate limited (10 req/s, burst 20)
flow "list_items" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    target    = "items"
  }
}

flow "get_item" {
  from {
    connector = "api"
    operation = "GET /items/:id"
  }
  to {
    connector = "db"
    target    = "items"
  }
}

flow "create_item" {
  from {
    connector = "api"
    operation = "POST /items"
  }

  transform {
    id         = "uuid()"
    name       = "input.name"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "items"
    operation = "INSERT"
  }
}
