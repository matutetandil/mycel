# gRPC Load Balancing Example
# Demonstrates client-side load balancing with multiple gRPC backends
# NOTE: Some gRPC features are still being implemented

service {
  name    = "grpc-lb-client"
  version = "1.0.0"
}

# gRPC client with round-robin load balancing
connector "backend_pool" {
  type   = "grpc"
  driver = "client"

  # Primary target
  target = env("GRPC_TARGET", "dns:///backend.service.local:50051")

  # Connection settings
  timeout = "30s"

  # Proto files for service definitions
  proto_path = "./protos"
}

# gRPC client for stateful services
connector "stateful_backend" {
  type   = "grpc"
  driver = "client"
  target = env("STATEFUL_TARGET", "backend-stateful:50051")

  timeout = "10s"
}

# REST API to expose gRPC services
connector "api" {
  type = "rest"
  port = 8080
}

# Types
type "user_request" {
  user_id = string
}

type "user_response" {
  id    = string
  name  = string
  email = string
}
