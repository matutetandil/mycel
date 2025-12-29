# TCP Server - Listens for incoming TCP connections
connector "tcp_api" {
  type   = "tcp"
  driver = "server"

  host     = "0.0.0.0"
  port     = 9000
  protocol = "json"

  max_connections = 100
  read_timeout    = "30s"
  write_timeout   = "30s"
}

# Database connector for persistence
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/tcp_example.db"
}

# REST API for HTTP access (optional, for testing)
connector "api" {
  type = "rest"
  port = 3000

  cors {
    enabled = true
    origins = ["*"]
  }
}
