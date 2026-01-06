# gRPC Load Balancing Example

This example demonstrates client-side load balancing for gRPC connections in Mycel.

## Load Balancing Policies

### round_robin

Distributes requests evenly across all healthy backends.

```hcl
connector "backend_pool" {
  type   = "grpc"
  driver = "client"
  target = "dns:///backend.service.local:50051"

  load_balancing {
    policy       = "round_robin"
    health_check = true
  }
}
```

**Use when:**
- Stateless services
- Need even distribution
- High availability is critical

### pick_first

Sticks to one backend until it becomes unhealthy.

```hcl
connector "stateful_backend" {
  type   = "grpc"
  driver = "client"
  target = "backend:50051"

  load_balancing {
    policy = "pick_first"
  }
}
```

**Use when:**
- Stateful services
- Session affinity needed
- Caching per-connection state

## Target Formats

### DNS-based (Recommended)

```hcl
target = "dns:///backend.service.local:50051"
```

DNS resolution happens automatically. Works with:
- Kubernetes headless services
- Consul DNS
- Any DNS that returns multiple A records

### Direct Address

```hcl
target = "backend:50051"
```

Single backend, no load balancing.

### Static Targets

```hcl
load_balancing {
  policy  = "round_robin"
  targets = ["backend1:50051", "backend2:50051", "backend3:50051"]
}
```

For environments without DNS-based discovery.

## Health Checking

Enable client-side health checking:

```hcl
load_balancing {
  policy       = "round_robin"
  health_check = true
}
```

This uses the gRPC Health Checking Protocol. Backends must implement:

```protobuf
service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}
```

## Kubernetes Example

```yaml
# Headless service for DNS-based discovery
apiVersion: v1
kind: Service
metadata:
  name: backend
spec:
  clusterIP: None  # Headless
  selector:
    app: backend
  ports:
    - port: 50051
---
# In Mycel config
# target = "dns:///backend.default.svc.cluster.local:50051"
```

## Connection Settings

Optimize for load-balanced scenarios:

```hcl
connector "backend_pool" {
  type   = "grpc"
  driver = "client"
  target = "dns:///backend:50051"

  load_balancing {
    policy       = "round_robin"
    health_check = true
  }

  # Wait for at least one backend to be ready
  wait_for_ready = true

  # Keep connections alive
  keep_alive {
    time    = "30s"
    timeout = "10s"
  }

  # Retry on transient failures
  retry_count   = 3
  retry_backoff = "100ms"
}
```

## Running

```bash
# Start gRPC backends (example with 3 replicas)
docker-compose up -d backend

# Set target
export GRPC_TARGET="dns:///backend:50051"

# Start Mycel
mycel start --config ./examples/grpc-loadbalancing

# Test - requests distributed across backends
for i in {1..10}; do
  curl http://localhost:8080/users/123
done
```

## Monitoring

Check which backends are being used:

```bash
# Prometheus metrics
curl http://localhost:8080/metrics | grep grpc

# Example metrics:
# mycel_grpc_client_requests_total{target="backend1:50051"} 42
# mycel_grpc_client_requests_total{target="backend2:50051"} 41
# mycel_grpc_client_requests_total{target="backend3:50051"} 43
```
