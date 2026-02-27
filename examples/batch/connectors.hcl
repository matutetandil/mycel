connector "api" {
  type = "rest"
  port = 3000
}

connector "old_db" {
  type   = "database"
  driver = "sqlite"
  dsn    = "old.db"
}

connector "new_db" {
  type   = "database"
  driver = "sqlite"
  dsn    = "new.db"
}

connector "es" {
  type  = "elasticsearch"
  nodes = ["http://localhost:9200"]
  index = "products"
}
