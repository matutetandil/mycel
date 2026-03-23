connector "postgres" {
  type   = "database"
  driver = "postgres"
  source = env("DATABASE_URL", "postgres://mycel:mycel@localhost:5432/orders?sslmode=disable")
}

connector "shipping_api" {
  type     = "http"
  base_url = env("SHIPPING_API_URL", "http://localhost:4000")
}

connector "notifications" {
  type     = "http"
  base_url = env("NOTIFICATION_API_URL", "http://localhost:5000")
}
