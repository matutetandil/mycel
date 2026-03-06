# SSE server (future v2 testing)
connector "sse" {
  type   = "sse"
  driver = "server"

  host = "0.0.0.0"
  port = 3002
  path = "/events"
}
