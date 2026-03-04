# Deployment Guide

How to run Mycel in different environments: local development, Docker, Docker Compose, and Kubernetes.

## Environment Variables

Mycel reads the following environment variables at startup:

| Variable | Default | Description |
|----------|---------|-------------|
| `MYCEL_ENV` | `development` | Environment name (`development`, `staging`, `production`). Selects the environment overlay from `environments/` |
| `MYCEL_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `MYCEL_LOG_FORMAT` | `text` | Log format: `text` (human-readable), `json` (structured) |
| `NO_COLOR` | _(unset)_ | Set to any value to disable colored output |
| `MYCEL_PLUGIN_CACHE` | _(unset)_ | Directory to cache downloaded WASM plugins |

CLI flags always take precedence over environment variables.

### Priority Chain

```
CLI flags  >  existing env vars  >  .env file  >  defaults
```

For example, `--log-level=debug` overrides `MYCEL_LOG_LEVEL=info` in a `.env` file, which overrides the default `info`.

## .env File

Mycel automatically loads a `.env` file on startup (`start`, `validate`, `check` commands). This is useful for local development so you don't need to export variables manually.

### How It Works

1. Mycel looks for `<config-dir>/.env` first (next to your HCL files)
2. Falls back to `./.env` in the current directory
3. Variables in `.env` do **not** override already-set environment variables
4. If no `.env` file is found, Mycel continues silently (normal for production/Docker)

### Example `.env` File

```bash
# Database
PG_HOST=localhost
PG_PORT=5432
PG_DATABASE=myapp
PG_USER=postgres
PG_PASSWORD=secret

# Redis
REDIS_ADDRESS=localhost:6379
REDIS_PASSWORD=

# Mycel
MYCEL_ENV=development
MYCEL_LOG_LEVEL=debug

# External APIs
API_TOKEN=sk-dev-token-123
```

### Best Practices

- **Never commit `.env` files** to version control. Add `.env` to your `.gitignore`.
- Provide a `.env.example` file with placeholder values for new developers.
- In production, use real environment variables (Docker `-e`, Kubernetes secrets, CI/CD variables) instead of `.env` files.

## Docker

### Basic Usage

Mount your configuration directory at `/etc/mycel`:

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

Or from Docker Hub:

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 mdenda/mycel
```

### With Environment Variables

```bash
docker run \
  -v $(pwd):/etc/mycel \
  -p 3000:3000 \
  -e MYCEL_ENV=production \
  -e MYCEL_LOG_FORMAT=json \
  -e PG_HOST=db.example.com \
  -e PG_PASSWORD=secret \
  ghcr.io/matutetandil/mycel
```

### Without REST Connector

Services that only consume from queues or CDC don't expose a REST port. Mycel starts a standalone admin server on port 9090 for health checks and metrics:

```bash
docker run \
  -v $(pwd):/etc/mycel \
  -p 9090:9090 \
  -e RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/ \
  ghcr.io/matutetandil/mycel
```

Health checks are always available at `/health`, `/health/live`, `/health/ready` and metrics at `/metrics`.

### Custom Admin Port

```bash
docker run \
  -v $(pwd):/etc/mycel \
  -p 3000:3000 \
  -p 8080:8080 \
  ghcr.io/matutetandil/mycel
```

Where `config.hcl` contains:

```hcl
service {
  name       = "my-service"
  version    = "1.0.0"
  admin_port = 8080
}
```

### Building a Custom Image

If your service includes static assets or WASM plugins, build a custom image:

```dockerfile
FROM ghcr.io/matutetandil/mycel:latest
COPY ./config /etc/mycel
```

```bash
docker build -t my-service .
docker run -p 3000:3000 my-service
```

## Docker Compose

Full example with PostgreSQL and Redis:

```yaml
version: "3.8"

services:
  api:
    image: ghcr.io/matutetandil/mycel:latest
    volumes:
      - ./config:/etc/mycel
    ports:
      - "3000:3000"
    env_file: .env
    environment:
      - MYCEL_ENV=development
      - PG_HOST=postgres
      - REDIS_ADDRESS=redis:6379
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_started
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: secret
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  pgdata:
```

`.env` for this setup:

```bash
PG_DATABASE=myapp
PG_USER=postgres
PG_PASSWORD=secret
REDIS_PASSWORD=
MYCEL_LOG_LEVEL=debug
```

Run:

```bash
docker compose up
```

## Kubernetes

Mycel provides an official Helm chart:

```bash
helm install my-api oci://ghcr.io/matutetandil/charts/mycel
```

The Helm chart supports:
- ConfigMap-based HCL configuration (`existingConfigMap` or `--set-file`)
- Environment variables via `env` and `envFrom` (for Secrets)
- Autoscaling (HPA)
- Ingress
- Health check probes (pre-configured)
- Resource limits

See [helm/mycel/README.md](../helm/mycel/README.md) for full documentation, values reference, and examples.

### Quick Kubernetes Example

```bash
# Create ConfigMap from local HCL files
kubectl create configmap my-api-config --from-file=./config/

# Create Secret for credentials
kubectl create secret generic my-api-secrets \
  --from-literal=PG_PASSWORD=secret \
  --from-literal=API_TOKEN=sk-prod-token

# Install with Helm
helm install my-api oci://ghcr.io/matutetandil/charts/mycel \
  --set existingConfigMap=my-api-config \
  --set envFrom[0].secretRef.name=my-api-secrets
```
