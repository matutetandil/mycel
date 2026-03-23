# Connectors for GraphQL Optimization Demo

# GraphQL API Server
connector "api" {
  type   = "graphql"
  driver = "server"
  port   = 4000

  endpoint   = "/graphql"
  playground = true

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
  }
}

# SQLite Database
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./demo.db"
}

# External Pricing API (simulated)
# In real scenarios, this would be an HTTP client to an external service
connector "pricing_api" {
  type     = "database"
  driver   = "sqlite"
  database = "./demo.db"
}

# External Inventory API (simulated)
connector "inventory_api" {
  type     = "database"
  driver   = "sqlite"
  database = "./demo.db"
}

# External Reviews API (simulated)
connector "reviews_api" {
  type     = "database"
  driver   = "sqlite"
  database = "./demo.db"
}
