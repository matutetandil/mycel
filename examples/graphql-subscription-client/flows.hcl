# GraphQL Subscription Client Flows
# Demonstrates subscribing to an external GraphQL API's real-time events.

# =============================================================================
# SUBSCRIPTION FLOWS (External GraphQL → Local DB)
# =============================================================================

# Subscribe to product price changes from the external products API.
# Every time a price changes, this flow stores it locally.
#
# This is the client-side counterpart to the server-side subscription in
# examples/graphql-federation. The external server publishes events;
# this service receives them.

flow "track_price_changes" {
  from {
    connector = "products_api"
    operation = "Subscription.priceChanged"
  }

  transform {
    id         = "uuid()"
    sku        = "input.sku"
    old_price  = "input.oldPrice"
    new_price  = "input.newPrice"
    changed_at = "input.changedAt"
    tracked_at = "now()"
  }

  to {
    connector = "db"
    target    = "price_history"
  }
}

# Subscribe to new product events
flow "track_new_products" {
  from {
    connector = "products_api"
    operation = "Subscription.productCreated"
  }

  transform {
    id         = "uuid()"
    sku        = "input.sku"
    name       = "input.name"
    price      = "input.price"
    category   = "input.category"
    created_at = "input.createdAt"
    tracked_at = "now()"
  }

  to {
    connector = "db"
    target    = "tracked_products"
  }
}

# =============================================================================
# REST API FLOWS (Expose stored data)
# =============================================================================

# Query the local price history
flow "get_price_history" {
  from {
    connector = "api"
    operation = "GET /prices/:sku/history"
  }

  to {
    connector = "db"
    target    = "price_history"
    query     = "SELECT * FROM price_history WHERE sku = :sku ORDER BY changed_at DESC"
  }
}

# Query all tracked products
flow "get_tracked_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  to {
    connector = "db"
    target    = "tracked_products"
  }
}
