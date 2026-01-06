# Dynamic API Key Validation Example
# Validates API keys against a database instead of static configuration

service {
  name = "api-with-dynamic-keys"
  version = "1.0.0"
}

# Database storing API keys
connector "keys_db" {
  type   = "database"
  driver = "postgres"
  host   = env("DB_HOST", "localhost")
  port   = 5432
  user   = env("DB_USER", "mycel")
  password = env("DB_PASSWORD", "secret")
  database = env("DB_NAME", "api_keys")
}

# REST API with dynamic API key authentication
connector "api" {
  type = "rest"
  port = 8080

  auth {
    type = "api_key"

    api_key {
      header = "X-API-Key"

      # Dynamic validation against database
      validate {
        connector = "connector.keys_db"
        query     = "SELECT user_id, metadata FROM api_keys WHERE key_hash = :key AND active = true AND (expires_at IS NULL OR expires_at > NOW())"
      }
    }

    # Public endpoints (no auth required)
    public = ["/health", "/docs/*"]
  }

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE"]
  }
}

# Types for the API
type "api_key_record" {
  key_hash   = string
  user_id    = string
  metadata   = object
  active     = bool
  expires_at = timestamp { optional = true }
  created_at = timestamp
}

type "user" {
  id    = string
  name  = string
  email = string { format = "email" }
  role  = string
}
