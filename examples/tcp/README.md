# TCP Example

This example demonstrates how to use TCP connectors in Mycel.

## Overview

The service exposes:
- **TCP Server** on port 9000 (JSON protocol)
- **REST API** on port 3000 (for testing/comparison)
- **SQLite database** for persistence

## Setup

```bash
# Create data directory and initialize database
mkdir -p data
sqlite3 data/tcp_example.db < setup.sql

# Run the service
mycel start --config ./examples/tcp
```

## TCP Protocol

Messages use length-prefixed JSON framing:

```
[4-byte length (big-endian)][JSON payload]
```

### Message Format

```json
{
  "type": "create_user",
  "id": "req-123",
  "data": {
    "email": "user@example.com",
    "name": "User Name"
  }
}
```

### Response Format

```json
{
  "type": "response",
  "id": "req-123",
  "data": {
    "id": "uuid-here",
    "success": true
  }
}
```

## Testing with netcat

### Create User

```bash
# Prepare message (47 bytes)
MSG='{"type":"create_user","id":"1","data":{"email":"test@example.com","name":"Test"}}'

# Send with length prefix (using printf for binary)
(printf '\x00\x00\x00\x52'; echo -n "$MSG") | nc localhost 9000
```

### List Users

```bash
MSG='{"type":"list_users","id":"2","data":{}}'
(printf '\x00\x00\x00\x28'; echo -n "$MSG") | nc localhost 9000
```

## Testing with Python

```python
import socket
import json
import struct

def send_tcp_message(host, port, message):
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.connect((host, port))

    # Encode message
    data = json.dumps(message).encode()

    # Send length prefix + data
    sock.sendall(struct.pack('>I', len(data)) + data)

    # Read response
    length_bytes = sock.recv(4)
    length = struct.unpack('>I', length_bytes)[0]
    response = sock.recv(length)

    sock.close()
    return json.loads(response)

# Create user
response = send_tcp_message('localhost', 9000, {
    'type': 'create_user',
    'id': 'req-1',
    'data': {
        'email': 'python@example.com',
        'name': 'Python User'
    }
})
print(response)

# List users
response = send_tcp_message('localhost', 9000, {
    'type': 'list_users',
    'id': 'req-2',
    'data': {}
})
print(response)
```

## Comparing with REST

The same operations are available via REST:

```bash
# Create user via REST
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email":"rest@example.com","name":"REST User"}'

# List users via REST
curl http://localhost:3000/users
```

## Message Types

| Type | Description |
|------|-------------|
| `create_user` | Create a new user |
| `get_user` | Get user by ID (pass `id` in data) |
| `list_users` | List all users |

## NestJS Protocol Support

Mycel supports the NestJS TCP protocol, allowing you to connect to existing NestJS microservices!

### NestJS Wire Format

NestJS uses a different wire format: `{length}#{json}`

```
75#{"pattern":"cache","data":{"key":"foo"},"id":"uuid-here"}
```

### Connecting to NestJS Microservices

```hcl
connector "cache_service" {
  type     = "tcp"
  driver   = "client"
  host     = "localhost"
  port     = 3001
  protocol = "nestjs"  # Use NestJS protocol!
}
```

### NestJS Message Format

```json
{
  "pattern": "cache",
  "data": {
    "action": "get",
    "key": "mykey"
  },
  "id": "request-uuid"
}
```

### NestJS Response Format

```json
{
  "id": "request-uuid",
  "response": {
    "value": "cached_data"
  },
  "isDisposed": true
}
```

### Pattern Types

NestJS supports two pattern formats:

1. **String pattern**: `"cache"` → Routes to `@MessagePattern('cache')`
2. **Object pattern**: `{"cmd": "sum"}` → Routes to `@MessagePattern({cmd: 'sum'})`

Both are automatically handled by Mycel.

## Verify It Works

### 1. Start the service

```bash
mycel start --config ./examples/tcp
```

You should see:
```
INFO  Starting service: tcp-example
INFO  Loaded 3 connectors: api, tcp_server, sqlite
INFO  Registered 5 flows
INFO  TCP server listening on :9000
INFO  REST server listening on :3000
```

### 2. Test REST endpoint (simpler)

```bash
curl http://localhost:3000/users
```

Expected response:
```json
[]
```

### 3. Create a user via REST

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email": "test@example.com", "name": "Test User"}'
```

Expected response:
```json
{"id":"<uuid>","email":"test@example.com","name":"Test User"}
```

### 4. Test TCP with Python

```bash
python3 -c "
import socket, json, struct
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.connect(('localhost', 9000))
msg = json.dumps({'type': 'list_users', 'id': '1', 'data': {}}).encode()
sock.sendall(struct.pack('>I', len(msg)) + msg)
length = struct.unpack('>I', sock.recv(4))[0]
print(json.loads(sock.recv(length)))
sock.close()
"
```

Expected output (users from previous REST call):
```json
{"type": "response", "id": "1", "data": [{"id": "...", "email": "test@example.com", "name": "Test User"}]}
```

### What to check in logs

```
INFO  TCP connection from 127.0.0.1:xxxxx
INFO    Message type: list_users
INFO    Flow: list_users → sqlite:users
INFO  TCP response sent
```

### Common Issues

**"Connection refused" on port 9000**

The TCP server is not running. Check if the service started correctly:
```bash
lsof -i :9000
```

**"Invalid message format"**

Ensure you're sending the correct length-prefixed JSON. The length must be a 4-byte big-endian integer.

**NestJS protocol not working**

Make sure you set `protocol = "nestjs"` in the connector config. The wire format is different (`75#{"pattern":"..."}`)
