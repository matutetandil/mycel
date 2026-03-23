# Authentication Service Example
# This example demonstrates a complete authentication microservice using Mycel
# NOTE: The auth block is a planned feature - this is a reference implementation

service {
  name    = "auth-service"
  version = "1.0.0"
}

# Database for user storage
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = 5432
  database = env("DB_NAME", "auth")
  user     = env("DB_USER", "postgres")
  password = env("DB_PASSWORD", "postgres")
}

# REST API connector (exposes auth endpoints)
connector "api" {
  type = "rest"
  port = 8080

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    headers = ["Authorization", "Content-Type"]
  }
}

# NOTE: The auth block below is a planned feature (Phase 5.1)
# It is commented out until full implementation is complete
#
# auth {
#   preset = "standard"
#
#   users {
#     connector = "postgres"
#     table     = "users"
#   }
#
#   jwt {
#     secret           = env("JWT_SECRET", "change-this-in-production")
#     access_lifetime  = "1h"
#     refresh_lifetime = "7d"
#   }
#
#   endpoints {
#     prefix = "/auth"
#   }
# }
