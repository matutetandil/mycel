# Mycel

**Declarative Microservice Framework**

Mycel is an open-source framework for creating microservices through HCL configuration, without writing code. It works as a single runtime (like nginx) that interprets configuration files and exposes services.

> **Philosophy:** Configuration, not code. You define WHAT you want, Mycel handles HOW.

## Quick Start

### With Docker (recommended)

```bash
# Using pre-built image (Docker Hub)
docker run -v ./my-service:/etc/mycel -p 3000:3000 mdenda/mycel

# Or from GitHub Container Registry
docker run -v ./my-service:/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

Or use in your `docker-compose.yml`:

```yaml
services:
  my-api:
    image: mdenda/mycel:latest
    volumes:
      - ./config:/etc/mycel:ro
    ports:
      - "3000:3000"
    environment:
      # Mycel runtime configuration
      - MYCEL_ENV=production
      - MYCEL_LOG_LEVEL=info
      - MYCEL_LOG_FORMAT=json
      # Your app config (accessible via env() in HCL)
      - DB_HOST=postgres
    depends_on:
      - postgres

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: mycel
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: mycel
```

### From Source

```bash
# Clone and build
git clone https://github.com/matutetandil/mycel.git
cd mycel
go build -o mycel ./cmd/mycel

# Run the basic example
./mycel start --config ./examples/basic
```

### Test the API
```bash
# List users
curl http://localhost:3000/users

# Create user
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","name":"Test"}'
```

## How It Works

### 1. Define Connectors

```hcl
# connectors.hcl
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

### 2. Define Flows

```hcl
# flows.hcl
flow "get_users" {
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

### 3. Run

```bash
mycel start --config ./my-service
```

That's it. You have a REST API connected to a database.

## CLI

```bash
mycel start [--config=<path>] [--env=<env>] [--log-level=<level>] [--log-format=<format>] [--hot-reload]
mycel validate [--config=<path>]
mycel check [--config=<path>]
mycel version
```

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
