# gRPC Example Configuration
# This example shows how to expose and consume gRPC services

service {
  name    = "grpc-example"
  version = "1.0.0"
}

# gRPC Server - Expose a gRPC service
connector "grpc_api" {
  type   = "grpc"
  driver = "server"

  host = "0.0.0.0"
  port = 50051

  # Proto file configuration
  proto_path  = "./protos"
  proto_files = ["user.proto"]
  reflection  = true  # Enable for grpcurl/grpcui tools

  # Optional TLS configuration
  # tls {
  #   ca_cert     = "/path/to/ca.crt"
  #   client_cert = "/path/to/server.crt"
  #   client_key  = "/path/to/server.key"
  # }
}

# gRPC Client - Call external gRPC services
connector "user_service" {
  type   = "grpc"
  driver = "client"

  host     = "user-service"
  port     = 50051
  timeout  = "30s"
  target   = "user-service:50051"
  insecure = true  # Set to false in production

  # Proto file configuration
  proto_path     = "./protos"
  proto_files    = ["user.proto"]
  wait_for_ready = true

  # Keep-alive settings
  keep_alive {
    time    = "10s"
    timeout = "5s"
  }

  # Optional TLS configuration for secure connections
  # tls {
  #   ca_cert     = "/path/to/ca.crt"
  #   client_cert = "/path/to/client.crt"
  #   client_key  = "/path/to/client.key"
  # }
}

# Database for storing data
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

# Flow: Handle GetUser gRPC call
flow "grpc_get_user" {
  from {
    connector = "grpc_api"
    operation = "UserService/GetUser"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

# Flow: Handle ListUsers gRPC call
flow "grpc_list_users" {
  from {
    connector = "grpc_api"
    operation = "UserService/ListUsers"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

# Flow: Handle CreateUser gRPC call with transform
flow "grpc_create_user" {
  from {
    connector = "grpc_api"
    operation = "UserService/CreateUser"
  }

  transform {
    id         = "uuid()"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "users"
    operation = "INSERT"
  }
}
