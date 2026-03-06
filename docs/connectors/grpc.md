# gRPC

Expose gRPC services (server) or call external gRPC endpoints (client). Uses Protocol Buffers for schema definition and supports TLS, load balancing, and all standard gRPC patterns.

## Server Configuration

```hcl
connector "grpc_api" {
  type   = "grpc"
  driver = "server"
  port   = 50051

  proto_path  = "./proto"
  proto_files = ["service.proto"]
  reflection  = true

  tls {
    cert_file = "/path/to/cert.pem"
    key_file  = "/path/to/key.pem"
  }
}
```

## Client Configuration

```hcl
connector "grpc_service" {
  type   = "grpc"
  driver = "client"
  target = "localhost:50051"

  proto_path  = "./proto"
  proto_files = ["service.proto"]
}
```

## Common Options

| Option | Type | Description |
|--------|------|-------------|
| `driver` | string | `server` or `client` |
| `port` | int | Listen port (server) |
| `target` | string | Target address `host:port` (client) |
| `proto_path` | string | Directory containing `.proto` files |
| `proto_files` | list | Specific `.proto` files to load |
| `reflection` | bool | Enable gRPC reflection (default: `true`) |
| `insecure` | bool | Disable TLS (client, default: `false`) |
| `tls.cert_file` | string | TLS certificate path |
| `tls.key_file` | string | TLS key path |

## Operations

**Server (source):** RPC method names as defined in the proto file — e.g., `GetUser`, `ListUsers`.

**Client (target):** Same RPC method names, resolved against the target service.

## Example

```hcl
flow "get_user" {
  from { connector = "grpc_api", operation = "GetUser" }

  step "user" {
    connector = "db"
    operation = "query"
    query     = "SELECT * FROM users WHERE id = ?"
    params    = [input.id]
  }

  transform { output = "step.user" }
  to { response }
}
```

See the [grpc example](../../examples/grpc/) and [grpc-loadbalancing example](../../examples/grpc-loadbalancing/) for complete setups.
