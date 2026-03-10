# Installation

## Docker (Recommended)

The official Docker image is published to GitHub Container Registry and Docker Hub:

```bash
# From GitHub Container Registry
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel

# From Docker Hub
docker run -v $(pwd):/etc/mycel -p 3000:3000 mdenda/mycel
```

Mount your configuration directory at `/etc/mycel`. Mycel scans it recursively for `.hcl` files.

### Supported platforms

The official image supports `linux/amd64` and `linux/arm64` (Apple Silicon, Graviton).

### With environment variables

```bash
docker run \
  -v $(pwd):/etc/mycel \
  -p 3000:3000 \
  -e MYCEL_ENV=production \
  -e MYCEL_LOG_FORMAT=json \
  -e DB_HOST=db.example.com \
  -e DB_PASSWORD=secret \
  ghcr.io/matutetandil/mycel
```

### Building a custom image

If your service includes static assets or WASM plugins, embed the configuration:

```dockerfile
FROM ghcr.io/matutetandil/mycel:latest
COPY ./config /etc/mycel
```

```bash
docker build -t my-service .
docker run -p 3000:3000 my-service
```

## Go Binary

Requires Go 1.21 or later.

### Install from source

```bash
go install github.com/matutetandil/mycel/cmd/mycel@latest
```

### Build from repository

```bash
git clone https://github.com/matutetandil/mycel.git
cd mycel
go build -o mycel ./cmd/mycel
./mycel start --config ./examples/basic
```

### Verify installation

```bash
mycel version
# mycel v1.7.0 (go1.21)
```

## Helm (Kubernetes)

The official Helm chart installs Mycel on any Kubernetes cluster:

```bash
helm install my-api oci://ghcr.io/matutetandil/charts/mycel
```

### With ConfigMap

```bash
# Create ConfigMap from local HCL files
kubectl create configmap my-api-config --from-file=./config/

# Create Secret for credentials
kubectl create secret generic my-api-secrets \
  --from-literal=PG_PASSWORD=secret \
  --from-literal=API_TOKEN=sk-prod-token

# Install
helm install my-api oci://ghcr.io/matutetandil/charts/mycel \
  --set existingConfigMap=my-api-config \
  --set envFrom[0].secretRef.name=my-api-secrets
```

See [helm/mycel/README.md](../../helm/mycel/README.md) for the full values reference.

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
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      retries: 5

  redis:
    image: redis:7-alpine

volumes:
  pgdata:
```

`.env` for local development:

```bash
PG_DATABASE=myapp
PG_USER=postgres
PG_PASSWORD=secret
REDIS_PASSWORD=
MYCEL_LOG_LEVEL=debug
```

Run with:

```bash
docker compose up
```

## Runtime Environment Variables

Mycel reads these environment variables at startup:

| Variable | Default | Description |
|----------|---------|-------------|
| `MYCEL_ENV` | `development` | Environment name, selects overlay from `environments/` |
| `MYCEL_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `MYCEL_LOG_FORMAT` | `text` | `text` (human-readable) or `json` (structured) |
| `NO_COLOR` | unset | Set to any value to disable colored output |
| `MYCEL_PLUGIN_CACHE` | unset | Directory to cache downloaded WASM plugins |

CLI flags override environment variables. Priority chain:

```
CLI flags > existing env vars > .env file > defaults
```

## .env File

Mycel loads a `.env` file automatically on startup (for the `start`, `validate`, and `check` commands). This simplifies local development — no need to export variables manually.

Mycel looks for `<config-dir>/.env` first, then `./.env` in the current working directory. Variables in `.env` do not override already-set environment variables.

```bash
# .env
DB_HOST=localhost
DB_PORT=5432
DB_PASSWORD=secret
MYCEL_LOG_LEVEL=debug
```

Add `.env` to your `.gitignore`. Provide a `.env.example` with placeholder values for new developers.

## Next Steps

- [Quick Start](quick-start.md) — build and run your first service
- [Deployment Guide](../deployment/docker.md) — production deployment patterns
