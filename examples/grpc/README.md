# gRPC Example

This example demonstrates gRPC server and client functionality.

## Features

- gRPC Server with reflection enabled
- gRPC Client for calling external services
- Proto file configuration
- TLS/mTLS support (commented examples)
- Data enrichment from external gRPC services

## Files

- `config.hcl` - Complete configuration with server, client, and flows

## Usage

```bash
# Start the service
mycel start --config ./examples/grpc

# Test with grpcurl (requires reflection enabled)
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext localhost:50051 UserService/ListUsers
grpcurl -plaintext -d '{"id": "1"}' localhost:50051 UserService/GetUser
```

## Configuration

### Server

```hcl
connector "grpc_api" {
  type   = "grpc"
  driver = "server"
  port   = 50051

  proto_path = "./protos"
  reflection = true
}
```

### Client

```hcl
connector "user_service" {
  type       = "grpc"
  driver     = "client"
  target     = "user-service:50051"
  proto_path = "./protos"
  insecure   = true
}
```

## Flow Operations

Operations use the format `ServiceName/MethodName`:

```hcl
flow "get_user" {
  from {
    connector = "grpc_api"
    operation = "UserService/GetUser"
  }
  # ...
}
```
