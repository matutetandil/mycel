# Connectors for mock example

connector "api" {
  type   = "rest"
  driver = "server"
  port   = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
