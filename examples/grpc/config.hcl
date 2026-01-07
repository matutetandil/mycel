# gRPC Example Configuration
# This example shows how to expose and consume gRPC services

service {
  name    = "grpc-example"
  version = "1.0.0"
}

# gRPC Server - Expose a gRPC service
#
# NOTE: The gRPC connector expects .proto files in the working directory.
# Configure proto_path and reflection in the connector factory directly
# (see internal/connector/grpc for implementation details).
connector "grpc_api" {
  type   = "grpc"
  driver = "server"

  host = "0.0.0.0"
  port = 50051

  # NOTE: proto_path and reflection are handled by the connector internally
  # proto_path = "./protos"
  # reflection = true  # Enable for grpcurl/grpcui tools

  # Optional TLS configuration
  # tls {
  #   enabled   = true
  #   cert_file = "/path/to/server.crt"
  #   key_file  = "/path/to/server.key"
  # }
}

# gRPC Client - Call external gRPC services
#
# NOTE: Client options like target, proto_path, insecure, and retry settings
# are handled internally by the connector factory.
connector "user_service" {
  type   = "grpc"
  driver = "client"

  host    = "user-service"
  port    = 50051
  timeout = "30s"

  # NOTE: These settings are configured internally:
  # target     = "user-service:50051"
  # proto_path = "./protos"
  # insecure   = true  # Set to false in production
  # connect_timeout = "10s"
  # retry_count     = 3
  # retry_backoff   = "100ms"

  # Optional TLS configuration for secure connections
  # tls {
  #   enabled     = true
  #   ca_file     = "/path/to/ca.crt"
  #   server_name = "user-service"
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
  }
}

# =========================================
# Advanced Features (Documented, Need Parser Support)
# =========================================
# The following patterns require parser enhancements:
#
# 1. 'operation' in 'to' block (e.g., operation = "INSERT"):
#    to {
#      connector = "db"
#      target    = "users"
#      operation = "INSERT"  # Not supported in to block
#    }
#
# 2. Enrichment with external gRPC service:
#    enrich "profile" {
#      connector = "user_service"
#      operation = "ProfileService/GetProfile"
#      params {
#        user_id = "input.id"
#      }
#    }
#
# See docs/INTEGRATION-PATTERNS.md for full documentation.
