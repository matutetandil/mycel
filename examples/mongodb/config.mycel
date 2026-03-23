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

# MongoDB connection using URI
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  uri      = "mongodb://admin:secret@localhost:27017/myapp?authSource=admin"
  database = "myapp"

  pool {
    max = 100
    min = 10
  }
}

# Alternative: MongoDB connection using individual parameters
# connector "mongo_alt" {
#   type     = "database"
#   driver   = "mongodb"
#   host     = "localhost"
#   port     = 27017
#   database = "myapp"
#   user     = "admin"
#   password = "secret"
#   auth_source = "admin"
#   replica_set = "rs0"  # For replica sets
# }
