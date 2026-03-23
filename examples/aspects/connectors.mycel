# Connectors for Aspects Example

# REST API connector
connector "api" {
  type   = "rest"
  driver = "server"
  port   = 3000
}

# Main application database
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "aspects_demo.db"
}

# Audit log database (separate for compliance)
connector "audit_db" {
  type     = "database"
  driver   = "sqlite"
  database = "audit_logs.db"
}

# In-memory cache
connector "cache" {
  type        = "cache"
  driver      = "memory"
  max_items   = 10000
  default_ttl = "5m"
}
