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

  transform {
    output.room    = input.params.room
    output.message = input.body.message
  }

  to {
    connector = "events"
    operation = "send_to_room"
    target    = "input.params.room"
  }
}

# Clients connect to /events?room=orders to receive order updates
# Clients connect to /events?rooms=orders,inventory for multiple rooms
# Clients connect to /events?user_id=123 for per-user targeting
