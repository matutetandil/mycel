connector "api" {
  type = "rest"
  port = 3000
}

connector "orders_db" {
  type   = "database"
  driver = "sqlite"
  source = ":memory:"
}

connector "payments_api" {
  type    = "http"
  base_url = "http://localhost:4000"
}

connector "inventory_api" {
  type    = "http"
  base_url = "http://localhost:5000"
}

connector "notifications" {
  type    = "http"
  base_url = "http://localhost:6000"
}
