# SSE (Server-Sent Events) Example

Demonstrates unidirectional server-to-client push using SSE.

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
```

Send events via the REST API:

```bash
# Broadcast to all clients
curl -X POST http://localhost:8080/notify \
  -H 'Content-Type: application/json' \
  -d '{"message": "Hello everyone!"}'

# Send to a specific room
curl -X POST http://localhost:8080/notify/orders \
  -H 'Content-Type: application/json' \
  -d '{"message": "New order received!"}'
```

## SSE Wire Format

Events are delivered in standard SSE format:

```
id: 1
event: message
data: {"message": "Hello everyone!"}

```

Heartbeat comments (`: keepalive`) are sent every 30 seconds to keep the connection alive.
