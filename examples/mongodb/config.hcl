# MongoDB Example Configuration
# This example shows how to work with MongoDB

service {
  name    = "mongodb-example"
  version = "1.0.0"
}

# REST API
connector "api" {
  type = "rest"
  port = 3000
}

# MongoDB connection
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  uri      = env("MONGO_URI")
  database = "myapp"

  pool {
    max             = 100
    min             = 10
    connect_timeout = 30
  }
}
