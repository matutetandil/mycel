# WebSocket

Standalone bidirectional real-time communication, independent of GraphQL. Use it for live dashboards, chat, notifications, IoT data streams, or any scenario where you need persistent connections with push capabilities.

## Configuration

```hcl
connector "ws" {
  type = "websocket"
  port = 3001
  path = "/ws"

  ping_interval = "30s"
  pong_timeout  = "10s"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | int | — | Listen port |
| `host` | string | `"0.0.0.0"` | Bind address |
| `path` | string | `"/ws"` | WebSocket endpoint path |
| `ping_interval` | duration | `"30s"` | How often to ping clients |
| `pong_timeout` | duration | `"10s"` | How long to wait for a pong |

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `message` | source | Receive messages from clients |
| `connect` | source | Trigger when a client connects |
| `disconnect` | source | Trigger when a client disconnects |
| `broadcast` | target | Send to all connected clients |
| `send_to_room` | target | Send to clients in a specific room |
| `send_to_user` | target | Send to a specific user |

## Client Protocol

Clients communicate using JSON messages:

| Client sends | Description |
|-------------|-------------|
| `{"type": "message", "data": {...}}` | Send a message |
| `{"type": "join_room", "room": "orders"}` | Join a room |
| `{"type": "leave_room", "room": "orders"}` | Leave a room |

| Server sends | Description |
|-------------|-------------|
| `{"type": "message", "data": {...}}` | Data payload |
| `{"type": "error", "message": "..."}` | Error message |

## Example

```hcl
# Receive messages from clients
flow "handle_chat" {
  from {
    connector = "ws"
    operation = "message"
  }
  to {
    connector = "db"
    target    = "messages"
  }
}

# Broadcast to all connected clients
flow "live_orders" {
  from {
    connector = "rabbit"
    operation = "order.updated"
  }
  to {
    connector = "ws"
    operation = "broadcast"
  }
}

# Send to clients in a specific room
flow "room_notification" {
  from {
    connector = "rabbit"
    operation = "room.event"
  }
  to {
    connector = "ws"
    operation = "send_to_room"
    target    = "input.room"
  }
}
```

See the [websocket example](../../examples/websocket/) for a complete working setup.

---

> **Full configuration reference:** See [WebSocket](../reference/configuration.md#websocket) in the Configuration Reference.
