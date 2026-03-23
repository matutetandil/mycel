// Mycel Validators Example
// This example demonstrates custom validators using regex and CEL expressions.

service {
  name    = "validators-demo"
  version = "1.0.0"
}

// REST API connector
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}

// SQLite database
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/validators_demo.db"
}
