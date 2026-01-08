# Mycel

**Declarative Microservice Framework**

Mycel is an open-source framework for creating microservices through HCL configuration, without writing code. It works as a single runtime (like nginx) that interprets configuration files and exposes services.

> **Philosophy:** Configuration, not code. You define WHAT you want, Mycel handles HOW.

## Quick Start

### 1. Create Your Configuration

Create a directory for your service and add these files:

```bash
mkdir my-service && cd my-service
```

**`connectors.hcl`** - Define your data sources:
```hcl
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"
}
```

**`flows.hcl`** - Define how data moves:
```hcl
flow "list_users" {
  from { connector = "api", operation = "GET /users" }
  to   { connector = "db", target = "users" }
}

flow "create_user" {
  from { connector = "api", operation = "POST /users" }
  transform {
    id         = "uuid()"
    email      = "lower(input.email)"
    created_at = "now()"
  }
  to { connector = "db", target = "users" }
}
```

### 2. Run Your Service

**With Docker (recommended):**
```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

**From source:**
```bash
go install github.com/matutetandil/mycel/cmd/mycel@latest
mycel start
```

### 3. Test the API

```bash
# Create a user
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","name":"Test"}'

# List users
curl http://localhost:3000/users
```

That's it! You have a REST API connected to a database without writing any code.

> **Next steps:** See [Getting Started Guide](docs/GETTING_STARTED.md) for a complete tutorial.

## CLI

```bash
mycel start [--config=<path>] [--env=<env>] [--log-level=<level>] [--log-format=<format>] [--hot-reload]
mycel validate [--config=<path>]
mycel check [--config=<path>]
mycel version
```

## Kubernetes (Helm)

Deploy Mycel to Kubernetes using Helm:

```bash
# Install from GHCR (recommended)
helm install my-api oci://ghcr.io/matutetandil/charts/mycel

# Install specific version
helm install my-api oci://ghcr.io/matutetandil/charts/mycel --version 1.0.0

# With custom configuration
helm install my-api oci://ghcr.io/matutetandil/charts/mycel \
  --set replicaCount=3 \
  --set autoscaling.enabled=true \
  --set ingress.enabled=true

# Or from local chart (for development)
helm install my-api ./helm/mycel -f values.yaml
```

Create a `values.yaml`:

```yaml
replicaCount: 2

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: api.example.com
      paths:
        - path: /
          pathType: Prefix

mycel:
  env: production
  config:
    service: |
      service {
        name = "my-api"
        port = 8080
      }
    connectors: |
      connector "api" {
        type = "rest"
        port = 8080
      }
    flows: |
      # Your flows here

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10

metrics:
  serviceMonitor:
    enabled: true  # For Prometheus Operator
```

See [helm/mycel/README.md](helm/mycel/README.md) for full documentation.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MYCEL_ENV` | `development` | Environment to load (development, staging, production) |
| `MYCEL_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `MYCEL_LOG_FORMAT` | `text` | Log format: text, json (use json for production) |

Flags take precedence over environment variables.

## Features

| Feature | Status | Example |
|---------|--------|---------|
| REST API | ✅ | [examples/basic](examples/basic) |
| SQLite / PostgreSQL / MySQL | ✅ | [examples/basic](examples/basic) |
| MongoDB | ✅ | [examples/mongodb](examples/mongodb) |
| GraphQL Server & Client | ✅ | [examples/graphql](examples/graphql) |
| gRPC Server & Client | ✅ | [examples/grpc](examples/grpc) |
| RabbitMQ / Kafka | ✅ | [examples/mq](examples/mq) |
| TCP Server & Client | ✅ | [examples/tcp](examples/tcp) |
| Files (local) | ✅ | [examples/files](examples/files) |
| S3 / MinIO | ✅ | [examples/s3](examples/s3) |
| Cache (Memory / Redis) | ✅ | [examples/cache](examples/cache) |
| Data Enrichment | ✅ | [examples/enrich](examples/enrich) |
| Rate Limiting | ✅ | [examples/rate-limit](examples/rate-limit) |
| Circuit Breaker | ✅ | - |
| Hot Reload | ✅ | - |
| Health Checks | ✅ | `/health`, `/health/live`, `/health/ready` |
| Prometheus Metrics | ✅ | `/metrics` |

## What Mycel Can (and Cannot) Do

Mycel is designed to replace the **"plumbing" code** that makes up 70-80% of typical microservices. It excels at data transformation, protocol bridging, and integration patterns—but it's not a silver bullet.

### ✅ Perfect Fit (Use Mycel)

| Use Case | Examples |
|----------|----------|
| **Data APIs** | CRUD REST/GraphQL, API gateways, BFF (Backend for Frontend) |
| **Event Processing** | Queue consumers, webhook handlers, event routing |
| **Data Integration** | DB ↔ API sync, ETL pipelines, data enrichment |
| **Protocol Bridging** | REST → gRPC, GraphQL → REST, TCP → Queue |
| **Scheduled Tasks** | Cron jobs, cleanup tasks, report generation |
| **Notifications** | Email, Slack, SMS, Push notifications |
| **Auth & Security** | JWT auth, rate limiting, circuit breaker |

### ⚠️ Partial Fit (Mycel + External Services)

| Use Case | Mycel Handles | External Service Handles |
|----------|---------------|--------------------------|
| **Search APIs** | REST/GraphQL API, caching, auth | Elasticsearch queries |
| **Recommendations** | API layer, caching, response formatting | ML model inference |
| **Image Processing** | Upload/download, S3 storage | ImageMagick, Cloudinary |
| **Complex Workflows** | Multi-step orchestration | Stateful saga coordination |

### ❌ Not a Fit (Use Custom Code)

| Use Case | Why Not Mycel | Better Alternative |
|----------|---------------|-------------------|
| **ML/AI Inference** | Requires GPU, complex models | Python + TensorFlow/PyTorch |
| **Video/Audio Processing** | Heavy computation | FFmpeg, AWS MediaConvert |
| **Custom Protocols** | Proprietary binary formats | Go/Rust custom service |
| **Real-time Gaming** | Sub-millisecond latency, UDP | Custom game server |
| **Blockchain/DeFi** | Specialized cryptographic ops | Dedicated blockchain node |

### The Right Mental Model

Think of Mycel like **nginx for microservices**:
- nginx handles HTTP routing, SSL, rate limiting—you don't write that code
- Mycel handles data flows, transformations, integrations—you don't write that code
- Both let you focus on what makes your application unique

**Philosophy:**
> Use Mycel for everything you can. Write custom code only for what truly requires it.

In a typical enterprise, this means:
- **70% of microservices** → Mycel (CRUD APIs, integrations, event handlers)
- **20% of microservices** → Mycel + external services (ML, search, complex processing)
- **10% of microservices** → Custom code (proprietary algorithms, heavy computation)

## Service Configuration

```hcl
# config.hcl
service {
  name    = "my-api"
  version = "1.0.0"

  # Optional: Enable rate limiting
  rate_limit {
    requests_per_second = 100
    burst               = 200
    key_extractor       = "ip"  # or "header:X-API-Key"
  }
}
```

## Documentation

- **[Concepts](docs/CONCEPTS.md)** - What is a connector, flow, transform, and more
- **[Configuration Reference](docs/CONFIGURATION.md)** - Complete HCL syntax reference
- **[Integration Patterns](docs/integration-patterns.md)** - Common use cases
- **[Transformations](docs/transformations.md)** - CEL transformation guide
- **[Roadmap](docs/ROADMAP.md)** - Project status and future plans

## Examples

| Example | Description |
|---------|-------------|
| [basic](examples/basic) | REST API + SQLite |
| [graphql](examples/graphql) | GraphQL server with schema |
| [grpc](examples/grpc) | gRPC server and client |
| [mq](examples/mq) | RabbitMQ and Kafka |
| [tcp](examples/tcp) | TCP server with protocols |
| [cache](examples/cache) | Memory and Redis caching |
| [files](examples/files) | Local file operations |
| [s3](examples/s3) | AWS S3 / MinIO |
| [rate-limit](examples/rate-limit) | Rate limiting configuration |
| [enrich](examples/enrich) | Data enrichment from services |
| [mongodb](examples/mongodb) | MongoDB NoSQL operations |
| [exec](examples/exec) | Execute shell commands |

## Requirements

**Docker** (recommended) or **Go 1.24+** (for building from source)

## License

MIT

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a pull request.
