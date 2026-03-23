# Database Read Replicas Example
# Demonstrates automatic read/write routing for PostgreSQL and MySQL

service {
  name    = "read-replicas-example"
  version = "1.0.0"
}

# ============================================
# PostgreSQL with Read Replicas
# ============================================
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("PG_PRIMARY_HOST", "pg-primary")
  port     = 5432
  user     = env("PG_USER", "mycel")
  password = env("PG_PASSWORD", "secret")
  database = env("PG_DATABASE", "app")

  pool_size = 20
}

# ============================================
# MySQL with Read Replicas
# ============================================
connector "mysql" {
  type     = "database"
  driver   = "mysql"
  host     = env("MYSQL_PRIMARY_HOST", "mysql-primary")
  port     = 3306
  user     = env("MYSQL_USER", "mycel")
  password = env("MYSQL_PASSWORD", "secret")
  database = env("MYSQL_DATABASE", "app")
}

# REST API
connector "api" {
  type = "rest"
  port = 8080
}

# Types
type "user" {
  id         = string
  name       = string
  email      = string
  created_at = string
}
