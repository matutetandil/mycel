# PostgreSQL CDC connector - streams WAL changes in real-time
connector "pg_cdc" {
  type   = "cdc"
  driver = "postgres"

  host     = "localhost"
  port     = 5432
  database = "myapp"
  user     = "replication_user"
  password = env("DB_PASSWORD")

  slot_name   = "mycel_slot"
  publication = "mycel_pub"
}

# REST API to expose endpoints
connector "api" {
  type = "rest"
  port = 3000
}

# SQLite for local event log
connector "events_db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/events.db"
}
