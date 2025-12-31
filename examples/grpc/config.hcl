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

  host       = "0.0.0.0"
  port       = 50051
  proto_path = "./protos"
  reflection = true  # Enable for grpcurl/grpcui tools

  # Optional TLS configuration
  # tls {
  #   enabled   = true
  #   cert_file = "/path/to/server.crt"
  #   key_file  = "/path/to/server.key"
  # }
}

# gRPC Client - Call external gRPC services
connector "user_service" {
  type   = "grpc"
  driver = "client"

  target     = "user-service:50051"
  proto_path = "./protos"
  insecure   = true  # Set to false in production

  timeout         = "30s"
  connect_timeout = "10s"
  retry_count     = 3
  retry_backoff   = "100ms"

  # Optional TLS configuration for secure connections
  # tls {
  #   enabled     = true
  #   ca_file     = "/path/to/ca.crt"
  #   server_name = "user-service"
  # }

  # Optional mTLS (client certificate)
  # tls {
  #   enabled   = true
  #   ca_file   = "/path/to/ca.crt"
  #   cert_file = "/path/to/client.crt"
  #   key_file  = "/path/to/client.key"
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

# Flow: Call external gRPC service (enrichment example)
flow "get_user_with_profile" {
  from {
    connector = "grpc_api"
    operation = "UserService/GetUserWithProfile"
  }

  # Get user from local DB
  to {
    connector = "db"
    target    = "users"
  }

  # Enrich with data from external service
  enrich "profile" {
    connector = "user_service"
    operation = "ProfileService/GetProfile"
    params {
      user_id = "input.id"
    }
  }

  transform {
    user    = "output"
    profile = "enriched.profile"
  }
}
