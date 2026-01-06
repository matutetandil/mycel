# gRPC Load Balancing Example
# Demonstrates client-side load balancing with multiple gRPC backends

service {
  name = "grpc-lb-client"
  version = "1.0.0"
}

# gRPC client with round-robin load balancing
connector "backend_pool" {
  type   = "grpc"
  driver = "client"

  # Primary target (can be DNS name that resolves to multiple IPs)
  target = env("GRPC_TARGET", "dns:///backend.service.local:50051")

  # Load balancing configuration
  load_balancing {
    # Policy: round_robin or pick_first (default)
    policy = "round_robin"

    # Enable client-side health checking
    health_check = true

    # Additional static targets (optional, for non-DNS scenarios)
    # targets = ["backend1:50051", "backend2:50051", "backend3:50051"]
  }

  # Connection settings
  timeout         = "30s"
  connect_timeout = "5s"
  wait_for_ready  = true

  # Keep-alive for long-lived connections
  keep_alive {
    time    = "30s"
    timeout = "10s"
  }

  # Retry configuration
  retry_count   = 3
  retry_backoff = "100ms"

  # Proto files for service definitions
  proto_path = "./protos"
}

# gRPC client with pick_first (sticky) - for stateful services
connector "stateful_backend" {
  type   = "grpc"
  driver = "client"
  target = env("STATEFUL_TARGET", "backend-stateful:50051")

  load_balancing {
    policy = "pick_first"  # Sticks to one backend until it fails
  }

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
