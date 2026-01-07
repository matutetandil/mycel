# GraphQL Server Connector
# Exposes a GraphQL API on port 4000

connector "graphql_api" {
  type   = "graphql"
  driver = "server"

  port       = 4000
  endpoint   = "/graphql"
  playground = true

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
    headers = ["Content-Type", "Authorization"]
  }
}

# SQLite Database Connector
# Stores user data

connector "database" {
  type   = "database"
  driver = "sqlite"

  database = "./data/users.db"

  # Note: init_sql is not supported - create tables manually or use setup.sql
}

# GraphQL Client Connector (optional)
# Example of connecting to an external GraphQL API

# connector "external_api" {
#   type     = "graphql"
#   driver   = "client"
#   endpoint = "https://api.example.com/graphql"
#
#   auth {
#     type  = "bearer"
#     token = env("EXTERNAL_API_TOKEN")
#   }
#
#   timeout     = "30s"
#   retry_count = 3
#   retry_delay = "1s"
# }
