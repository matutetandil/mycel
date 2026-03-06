# WebSocket server (future v2 testing)
connector "ws" {
  type   = "websocket"
  driver = "server"

  host = "0.0.0.0"
  port = 3001
  path = "/ws"
}
