# Docker Deployment

## Basic Usage

Mount your configuration directory at `/etc/mycel`:

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel
```

Or from Docker Hub:

```bash
docker run -v $(pwd):/etc/mycel -p 3000:3000 mdenda/mycel
```

## With Environment Variables

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

## Services Without a REST Connector

Queue workers, CDC pipelines, and other event-driven services don't expose a REST port. Mycel starts a standalone admin server on port 9090 for health checks and metrics:

```bash
docker run \
  -v $(pwd):/etc/mycel \
  -p 9090:9090 \
  -e RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/ \
  ghcr.io/matutetandil/mycel
```

Health checks are always available at `/health`, `/health/live`, `/health/ready` and metrics at `/metrics`.

## Custom Admin Port

```hcl
# config.hcl
service {
  name       = "my-service"
  version    = "1.0.0"
  admin_port = 8081
}
```

```bash
docker run \
  -v $(pwd):/etc/mycel \
  -p 3000:3000 \
  -p 8081:8081 \
  ghcr.io/matutetandil/mycel
```

## Building a Custom Image

If your service includes static assets or WASM plugins, embed the configuration:

```dockerfile
FROM ghcr.io/matutetandil/mycel:latest
COPY ./config /etc/mycel
```

```bash
docker build -t my-service:latest .
docker run -p 3000:3000 my-service:latest
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
      start_period: 10s

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

`.env` file for local development:

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

## Supported Platforms

The official image supports:
- `linux/amd64`
- `linux/arm64` (Apple Silicon, AWS Graviton)

## Image Tags

| Tag | Description |
|-----|-------------|
| `latest` | Latest stable release |
| `v1.7.0` | Specific version |
| `main` | Latest main branch build |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MYCEL_ENV` | `development` | Environment name |
| `MYCEL_LOG_LEVEL` | `info` | Log level |
| `MYCEL_LOG_FORMAT` | `text` | Log format (`text` or `json`) |
| `NO_COLOR` | unset | Disable colored output |

## See Also

- [Kubernetes Deployment](kubernetes.md)
- [Production Checklist](production.md)
- [Environment Configuration](../core-concepts/environments.md)
