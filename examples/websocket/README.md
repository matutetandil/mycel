# WebSocket Example

Bidirectional real-time communication using the WebSocket connector.

## What This Demonstrates

- **Source:** Receive messages from WebSocket clients and store them in a database
- **Target:** Broadcast data and send to rooms from REST triggers
- **Rooms:** Clients join/leave rooms; messages can target specific rooms

## Run

```bash
mycel start --config ./examples/websocket
```

## Test

Connect with a WebSocket client (e.g., [websocat](https://github.com/vi/websocat)):

```bash
# Connect
websocat ws://localhost:3001/ws

# Send a message
{"type": "message", "data": {"text": "hello", "room": "general"}}

# Join a room
{"type": "join_room", "room": "orders"}

# Leave a room
{"type": "leave_room", "room": "orders"}
```

Broadcast from REST:

```bash
curl -X POST http://localhost:3000/orders/notify \
  -H "Content-Type: application/json" \
  -d '{"status": "shipped", "order_id": "123"}'
```

Send to a specific room:

```bash
curl -X POST http://localhost:3000/rooms/orders/notify \
  -H "Content-Type: application/json" \
  -d '{"message": "New order received"}'
```

## Message Protocol

```json
// Client → Server
{"type": "message", "data": {...}}
{"type": "join_room", "room": "room_name"}
{"type": "leave_room", "room": "room_name"}

// Server → Client
{"type": "message", "data": {...}}
{"type": "message", "data": {...}, "room": "room_name"}
{"type": "error", "message": "..."}
```

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `message` | Source | Receive client messages |
| `connect` | Source | Client connected |
| `disconnect` | Source | Client disconnected |
| `broadcast` | Target | Send to all clients |
| `send_to_room` | Target | Send to clients in a room |
| `send_to_user` | Target | Send to a specific user |
