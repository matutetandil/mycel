connector "ws" {
  type = "websocket"
  port = 3001
  path = "/ws"

  ping_interval = "30s"
  pong_timeout  = "10s"
}

connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/chat.db"
}
