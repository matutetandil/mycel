# Connectors configuration

# REST API connector (exposes endpoints)
connector "api" {
  type = "rest"
  port = 3000
}

# Product database
connector "products_db" {
  type   = "database"
  driver = "sqlite"

  database = "./products.db"
}

# Pricing service (TCP)
# This would connect to an external pricing microservice
connector "pricing_service" {
  type   = "tcp"
  driver = "client"

  host     = "localhost"
  port     = 9001
  protocol = "json"
}

# Inventory service (HTTP client)
# This would call an external inventory API
connector "inventory_service" {
  type = "http"

  base_url = "http://localhost:8080/api"
  timeout  = "5s"
}
