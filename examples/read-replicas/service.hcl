# Database Read Replicas Example
# Demonstrates automatic read/write routing for PostgreSQL and MySQL

service {
  name = "read-replicas-example"
  version = "1.0.0"
}

# ============================================
# PostgreSQL with Read Replicas
# ============================================
connector "postgres" {
  type   = "database"
  driver = "postgres"

  # Primary (writes)
  host     = env("PG_PRIMARY_HOST", "pg-primary")
  port     = 5432
  user     = env("PG_USER", "mycel")
  password = env("PG_PASSWORD", "secret")
  database = env("PG_DATABASE", "app")

  # Read replicas for SELECT queries
  replicas {
    hosts = [
      env("PG_REPLICA_1", "pg-replica-1:5432"),
      env("PG_REPLICA_2", "pg-replica-2:5432"),
    ]

    # Load balancing strategy
    strategy = "round_robin"  # or "random", "least_conn"

    # Replica-specific settings
    max_lag = "1s"  # Skip replicas with high replication lag
  }

  # Connection pool
  pool_size = 20
}

# ============================================
# MySQL with Read Replicas
# ============================================
connector "mysql" {
  type   = "database"
  driver = "mysql"

  # Primary (writes)
  host     = env("MYSQL_PRIMARY_HOST", "mysql-primary")
  port     = 3306
  user     = env("MYSQL_USER", "mycel")
  password = env("MYSQL_PASSWORD", "secret")
  database = env("MYSQL_DATABASE", "app")

  # Read replicas
  replicas {
    hosts = [
      env("MYSQL_REPLICA_1", "mysql-replica-1:3306"),
      env("MYSQL_REPLICA_2", "mysql-replica-2:3306"),
    ]

    strategy = "random"
  }
}

# ============================================
# Using Connector Profiles for More Control
# ============================================
connector "products" {
  # Use profile to explicitly control read vs write
  select  = "env('DB_MODE')"
  default = "read"

  profile "read" {
    type   = "database"
    driver = "postgres"
    host   = env("PG_REPLICA_1", "pg-replica-1")
    port   = 5432
    user   = env("PG_USER", "mycel")
    password = env("PG_PASSWORD", "secret")
    database = env("PG_DATABASE", "app")
  }

  profile "write" {
    type   = "database"
    driver = "postgres"
    host   = env("PG_PRIMARY_HOST", "pg-primary")
    port   = 5432
    user   = env("PG_USER", "mycel")
    password = env("PG_PASSWORD", "secret")
    database = env("PG_DATABASE", "app")
  }
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
  email      = string { format = "email" }
  created_at = timestamp
}
