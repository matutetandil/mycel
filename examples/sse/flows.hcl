# Broadcast a notification to all SSE clients
flow "broadcast_notification" {
  from {
    connector = "api"
    operation = "POST /notify"
  }

  to {
    connector = "events"
    operation = "broadcast"
  }
}

# Send to a specific room
flow "notify_room" {
  from {
    connector = "api"
    operation = "POST /notify/:room"
  }

  to {
    connector = "events"
    operation = "send_to_room"
    target    = "input.params.room"
  }
}

# Send to a specific user
flow "notify_user" {
  from {
    connector = "api"
    operation = "POST /notify/user/:user_id"
  }

  to {
    connector = "events"
    operation = "send_to_user"
    target    = "input.params.user_id"
  }
}
