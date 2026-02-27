connector "api" {
  type = "rest"
  port = 8080
}

connector "events" {
  type = "sse"
  port = 3002
  path = "/events"

  heartbeat_interval = "30s"

  cors {
    origins = ["*"]
  }
}
