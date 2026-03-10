# SSE (Server-Sent Events)

Unidirectional push from server to clients over standard HTTP. Clients open a `GET` request and receive a continuous `text/event-stream` response — no WebSocket handshake, no custom protocol, just plain HTTP that works through proxies and firewalls. Use it for live feeds, progress tracking, notification banners, or any scenario where the server pushes updates and clients only listen.

## Configuration

```hcl
connector "sse" {
  type = "sse"
  port = 3002
  path = "/events"

  heartbeat_interval = "30s"

  cors {
    allowed_origins = ["https://app.example.com"]
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | int | — | Listen port |
| `host` | string | `"0.0.0.0"` | Bind address |
| `path` | string | `"/events"` | SSE endpoint path |
| `heartbeat_interval` | duration | `"30s"` | Keepalive comment interval |
| `cors.allowed_origins` | list | — | Allowed CORS origins |

## Operations

Target only — clients connect via GET, the server pushes events.

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `broadcast` | target | Send to all connected clients |
| `send_to_room` | target | Send to clients in a specific room |
| `send_to_user` | target | Send to a specific user |

## Client Connection

Clients connect via `GET /events` with optional query parameters:

| Parameter | Description |
|-----------|-------------|
| `?room=orders` | Join a single room |
| `?rooms=orders,inventory` | Join multiple rooms |
| `?user_id=42` | Per-user targeting |

The connector sends periodic heartbeat comments (`: keepalive`) to keep connections alive through proxies.

## Example

```hcl
# Broadcast to all connected clients
flow "live_feed" {
  from { connector = "rabbit", operation = "feed.item" }
  to   { connector = "sse", operation = "broadcast" }
}

# Push only to clients subscribed to a specific room
flow "room_updates" {
  from { connector = "rabbit", operation = "room.event" }
  to   { connector = "sse", operation = "send_to_room", target = "input.room" }
}

# Push to a single user
flow "user_notification" {
  from { connector = "rabbit", operation = "notification.personal" }
  to   { connector = "sse", operation = "send_to_user", target = "input.user_id" }
}
```

See the [sse example](../../examples/sse/) for a complete working setup.

---

> **Full configuration reference:** See [SSE](../reference/configuration.md#sse) in the Configuration Reference.
