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
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
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
| `tls.cert` | string | Certificate this connector presents |
| `tls.key` | string | Private key for `tls.cert` |
| `tls.ca_cert` | string | CA used to verify the other side |
| `tls.server_name` | string | Expected server name, overriding the address (SNI) |
| `tls.insecure_skip_verify` | bool | Skip certificate verification. Development only |

See [TLS](../core-concepts/connectors.md#tls) for the shared block, including the
older `cert_file` / `key_file` / `ca_file` / `skip_verify` names, which are still accepted.

## Operations

**Server (source):** RPC method names as defined in the proto file — e.g., `GetUser`, `ListUsers`.

**Client (target):** Same RPC method names, resolved against the target service.

## Example

```hcl
flow "get_user" {
  from {
    connector = "grpc_api"
    operation = "GetUser"
  }

  step "user" {
    connector = "db"
    query     = "SELECT * FROM users WHERE id = :id"
    params = {
      id = "input.id"
    }
  }

  response {
    user = "step.user"
  }
}
```

See the [grpc example](https://github.com/matutetandil/mycel/tree/main/examples/grpc) and [grpc-loadbalancing example](https://github.com/matutetandil/mycel/tree/main/examples/grpc-loadbalancing) for complete setups.

---

> **Full configuration reference:** See [gRPC](../reference/configuration.md#grpc) in the Configuration Reference.
