# Dynamic API Key Validation Example
# Validates API keys against a database instead of static configuration
# NOTE: Dynamic API key validation is a planned feature

service {
  name    = "api-with-dynamic-keys"
  version = "1.0.0"
}

# Database storing API keys
connector "keys_db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST", "localhost")
  port     = 5432
  user     = env("DB_USER", "mycel")
  password = env("DB_PASSWORD", "secret")
  database = env("DB_NAME", "api_keys")
}

# REST API
connector "api" {
  type = "rest"
  port = 8080

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}

# Types for the API
type "user" {
  id    = string
  name  = string
  email = string
  role  = string
}
