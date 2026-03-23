# PostgreSQL
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("PG_HOST", "localhost")
  port     = env("PG_PORT", 5432)
  user     = env("PG_USER", "mycel")
  password = env("PG_PASS", "mycel")
  database = env("PG_DB", "mycel_test")
}

# MySQL
connector "mysql" {
  type     = "database"
  driver   = "mysql"
  host     = env("MYSQL_HOST", "localhost")
  port     = env("MYSQL_PORT", 3306)
  user     = env("MYSQL_USER", "mycel")
  password = env("MYSQL_PASS", "mycel")
  database = env("MYSQL_DB", "mycel_test")
}

# MongoDB
connector "mongodb" {
  type     = "database"
  driver   = "mongodb"
  uri      = env("MONGO_URI", "mongodb://mongo:mycel@localhost:27017/mycel_test?authSource=admin")
  database = "mycel_test"
}

# SQLite (embedded)
connector "sqlite" {
  type     = "database"
  driver   = "sqlite"
  database = "/tmp/mycel/integration.db"
}
