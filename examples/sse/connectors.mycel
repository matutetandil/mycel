# REST API to trigger events
connector "api" {
  type = "rest"
  port = 8080
}

# SSE connector for server-to-client push
connector "events" {
  type = "sse"
  port = 3002
  path = "/events"

  heartbeat_interval = "30s"

  cors {
    origins = ["*"]
  }
}
