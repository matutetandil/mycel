// Service configuration for WASM functions example
service {
  name    = "wasm-functions-example"
  version = "1.0.0"
}

// REST API connector
connector "api" {
  type   = "rest"
  driver = "server"
  port   = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
  }
}

// SQLite database for orders
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./orders.db"
}
