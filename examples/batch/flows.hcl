# Migrate users from old database to new one
flow "migrate_users" {
  from {
    connector = "api"
    operation = "POST /admin/migrate"
  }

  batch {
    source     = "old_db"
    query      = "SELECT * FROM users ORDER BY id"
    chunk_size = 100
    on_error   = "continue"

    transform {
      output.email       = "input.email.lowerAscii()"
      output.name        = "input.name"
      output.migrated    = "true"
    }

    to {
      connector = "new_db"
      target    = "users"
      operation = "INSERT"
    }
  }
}

# Reindex products from database to Elasticsearch
flow "reindex_products" {
  from {
    connector = "api"
    operation = "POST /admin/reindex"
  }

  batch {
    source     = "old_db"
    query      = "SELECT * FROM products ORDER BY id"
    chunk_size = 500

    to {
      connector = "es"
      target    = "products"
      operation = "index"
    }
  }
}

# Export recent orders
flow "export_orders" {
  from {
    connector = "api"
    operation = "POST /admin/export-orders"
  }

  batch {
    source     = "old_db"
    query      = "SELECT * FROM orders WHERE created_at > :since ORDER BY id"
    params     = { since = "input.since" }
    chunk_size = 200
    on_error   = "stop"

    to {
      connector = "new_db"
      target    = "orders_archive"
      operation = "INSERT"
    }
  }
}
