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
# Use host/port/user/password instead of uri (uri is handled by factory but not in parser schema)
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  host     = "localhost"
  port     = 27017
  database = "myapp"
  user     = "admin"
  password = "secret"

  pool {
    max = 100
    min = 10
  }
}
