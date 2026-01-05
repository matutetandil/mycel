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

## Verify It Works

### 1. Start the service

```bash
mycel start --config ./examples/grpc
```

You should see:
```
INFO  Starting service: grpc-example
INFO  Loaded 2 connectors: grpc_api, database
INFO  gRPC server listening on :50051
INFO  gRPC reflection enabled
```

### 2. List available services

```bash
grpcurl -plaintext localhost:50051 list
```

Expected output:
```
UserService
grpc.reflection.v1alpha.ServerReflection
```

### 3. Describe a service

```bash
grpcurl -plaintext localhost:50051 describe UserService
```

Expected output:
```
UserService is a service:
service UserService {
  rpc CreateUser ( CreateUserRequest ) returns ( User );
  rpc GetUser ( GetUserRequest ) returns ( User );
  rpc ListUsers ( Empty ) returns ( UserList );
}
```

### 4. List users (empty initially)

```bash
grpcurl -plaintext localhost:50051 UserService/ListUsers
```

Expected output:
```json
{
  "users": []
}
```

### 5. Create a user

```bash
grpcurl -plaintext -d '{"email": "test@example.com", "name": "Test User"}' \
  localhost:50051 UserService/CreateUser
```

Expected output:
```json
{
  "id": "<uuid>",
  "email": "test@example.com",
  "name": "Test User"
}
```

### What to check in logs

```
INFO  gRPC request: UserService/ListUsers
INFO    Flow: list_users → database:users
INFO  gRPC response sent in 2ms
```

### Common Issues

**"grpcurl: command not found"**

Install grpcurl:
```bash
# macOS
brew install grpcurl

# Linux
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

**"Failed to dial: connection refused"**

The gRPC server is not running. Check if port 50051 is in use:
```bash
lsof -i :50051
```

**"Server does not support reflection"**

Ensure `reflection = true` is set in the gRPC connector config.

**"Proto file not found"**

Check that `proto_path` points to a valid directory with .proto files.

## See Also

- [TCP Example](../tcp) - Alternative to gRPC for simpler protocols
