# Steps Example - Connectors
# Demonstrates multi-step flow orchestration with intermediate connector calls.

# REST API Server
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
  }
}

# Main database (orders, products)
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

# External pricing service (simulated as same database for demo)
connector "pricing_db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

# Inventory service (simulated as same database for demo)
connector "inventory_db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
