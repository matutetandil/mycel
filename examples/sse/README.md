# SSE (Server-Sent Events) Example

Unidirectional server-to-client push over standard HTTP using the SSE connector.

## What This Demonstrates

- **Broadcast:** Push events to all connected SSE clients
- **Rooms:** Clients join rooms via query params; messages can target specific rooms
- **Per-user:** Target events to a specific user via `user_id` query param

## Run

```bash
mycel start --config ./examples/sse
```

## Test

Connect an SSE client:

```bash
# Receive all broadcasts
curl -N http://localhost:3002/events

# Join a specific room
curl -N "http://localhost:3002/events?room=orders"

# Join multiple rooms
curl -N "http://localhost:3002/events?rooms=orders,inventory"

# Per-user targeting
curl -N "http://localhost:3002/events?user_id=42"
```

Broadcast from REST:

```bash
curl -X POST http://localhost:8080/notify \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello everyone!"}'
```

Send to a specific room:

```bash
curl -X POST http://localhost:8080/notify/orders \
  -H "Content-Type: application/json" \
  -d '{"message": "New order received!"}'
```

Send to a specific user:

```bash
curl -X POST http://localhost:8080/notify/user/42 \
  -H "Content-Type: application/json" \
  -d '{"message": "Your order shipped!"}'
```

## SSE Wire Format

Events are delivered in standard SSE format:

```
id: 1
event: message
data: {"message": "Hello everyone!"}

```

Heartbeat comments (`: keepalive`) are sent every 30 seconds to keep the connection alive.

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `broadcast` | Target | Send to all clients |
| `send_to_room` | Target | Send to clients in a room |
| `send_to_user` | Target | Send to a specific user |
