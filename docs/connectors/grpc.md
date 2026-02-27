# gRPC

Expose gRPC services (server) or call external gRPC endpoints (client). Uses Protocol Buffers for schema definition and supports TLS, load balancing, and all standard gRPC patterns.

## Server Configuration

```hcl
connector "grpc_api" {
  type   = "grpc"
  driver = "server"
  port   = 50051

  proto {
    path    = "./proto/service.proto"
    service = "MyService"
  }

  tls {
    cert_file = "/path/to/cert.pem"
    key_file  = "/path/to/key.pem"
  }
}
```

## Client Configuration

```hcl
connector "grpc_service" {
  type    = "grpc"
  driver  = "client"
  address = "localhost:50051"

  proto {
    path    = "./proto/service.proto"
    service = "MyService"
  }
}
```

## Common Options

| Option | Type | Description |
|--------|------|-------------|
| `driver` | string | `server` or `client` |
| `port` | int | Listen port (server) |
| `address` | string | Target address (client) |
| `proto.path` | string | Path to `.proto` file |
| `proto.service` | string | Service name in the proto |
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
