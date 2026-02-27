# TCP

Raw TCP server and client with pluggable codecs. Use it for low-level communication, custom binary protocols, or interop with NestJS TCP microservices.

## Server Configuration

```hcl
connector "tcp_server" {
  type   = "tcp"
  driver = "server"
  host   = "0.0.0.0"
  port   = 9000
  codec  = "json"    # "json", "msgpack", "raw", "nestjs"
}
```

## Client Configuration

```hcl
connector "tcp_client" {
  type    = "tcp"
  driver  = "client"
  address = "localhost:9000"
  codec   = "json"
  timeout = "10s"
}
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `driver` | string | — | `server` or `client` |
| `host` | string | `"0.0.0.0"` | Bind address (server) |
| `port` | int | — | Listen port (server) |
| `address` | string | — | Target address (client) |
| `codec` | string | `"json"` | Wire format: `json`, `msgpack`, `raw`, `nestjs` |
| `timeout` | duration | `"10s"` | Connection timeout (client) |

## Operations

**Server (source):** Message pattern matching — incoming messages are routed by their `pattern` field.

**Client (target):** Send messages to the remote TCP server.

## Example

```hcl
flow "handle_tcp_message" {
  from { connector = "tcp_server", operation = "get_users" }
  to   { connector = "db", target = "users" }
}

flow "forward_to_tcp" {
  from { connector = "api", operation = "POST /send" }
  to   { connector = "tcp_client", operation = "process" }
}
```

See the [tcp example](../../examples/tcp/) for a complete working setup.
