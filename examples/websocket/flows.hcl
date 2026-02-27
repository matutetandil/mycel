# Store chat messages received via WebSocket
flow "handle_chat" {
  from {
    connector = "ws"
    operation = "message"
  }

  transform {
    output.id         = "uuid()"
    output.text       = "input.text"
    output.room       = "input.room"
    output.created_at = "now()"
  }

  to {
    connector = "db"
    target    = "messages"
  }
}

# Broadcast order updates from REST to all WebSocket clients
flow "broadcast_order" {
  from {
    connector = "api"
    operation = "POST /orders/notify"
  }

  to {
    connector = "ws"
    operation = "broadcast"
  }
}

# Send notification to a specific room
flow "room_notification" {
  from {
    connector = "api"
    operation = "POST /rooms/:room/notify"
  }

  to {
    connector = "ws"
    operation = "send_to_room"
    target    = "input.room"
  }
}
