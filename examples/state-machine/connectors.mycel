connector "api" {
  type = "rest"
  port = 3000
}

connector "orders_db" {
  type   = "database"
  driver = "sqlite"
  source = ":memory:"
}

connector "notifications" {
  type    = "http"
  base_url = "http://localhost:6000"
}
