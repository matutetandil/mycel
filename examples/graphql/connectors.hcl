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

  init_sql = <<-SQL
    CREATE TABLE IF NOT EXISTS users (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      email TEXT UNIQUE NOT NULL,
      name TEXT NOT NULL,
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
  SQL
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
