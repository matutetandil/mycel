# Installation

## Docker (Recommended)

The official Docker image is published to GitHub Container Registry and Docker Hub:

```bash
# From GitHub Container Registry
docker run -v $(pwd):/etc/mycel -p 3000:3000 ghcr.io/matutetandil/mycel

# From Docker Hub
docker run -v $(pwd):/etc/mycel -p 3000:3000 mdenda/mycel
```

Mount your configuration directory at `/etc/mycel`. Mycel scans it recursively for `.mycel` files.

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

Installing the CLI locally is worth it even if you deploy with Docker: it is
what you run `mycel validate` and `mycel check` with while writing config.

Requires Go 1.21 or later.

### Install

```bash
go install github.com/matutetandil/mycel/v2/cmd/mycel@latest
```

The binary lands in `$GOBIN`, or `$(go env GOPATH)/bin` if `GOBIN` is unset.
Make sure that directory is on your `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

!!! warning "The `/v2` suffix is required"

    Mycel is released at major version 2, and Go's [semantic import
    versioning](https://go.dev/ref/mod#major-version-suffixes) requires the
    module path to say so. Omitting it does not fail — it silently installs the
    newest **v1** release, which is many versions old:

    ```bash
    go install github.com/matutetandil/mycel/cmd/mycel@latest   # ← wrong: installs v1.x
    ```

    If you installed Mycel before this was fixed, `mycel version` will report a
    `1.x` number. Reinstall with the `/v2` path.

### Updating

There is no separate update command, and nothing updates itself in the
background. **The install command is also the update command** — re-run it and
Go replaces the binary with the newest release:

```bash
go install github.com/matutetandil/mycel/v2/cmd/mycel@latest
```

`@latest` resolves to the highest published release tag each time it runs, so
this is safe to repeat. To pin a specific version instead — in a CI image, or
to match what is deployed:

```bash
go install github.com/matutetandil/mycel/v2/cmd/mycel@v2.13.0
```

Check what you have with `mycel version`, and compare against the
[latest release](https://github.com/matutetandil/mycel/releases/latest).

Docker and Helm have their own update path: change the image tag. Pin an exact
version there rather than tracking `latest`, so a restart never changes the
runtime underneath a running service.

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
# mycel 2.13.0 (commit: ed24e66, go1.25.0, linux/amd64)
```

The version and commit come from the build metadata Go embeds, so this reports
what you are actually running: the module version for a `go install` binary,
and the git revision — with a `-dirty` suffix for uncommitted changes — for a
build from a checkout.

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

See [helm/mycel/README.md](https://github.com/matutetandil/mycel/blob/main/helm/mycel/README.md) for the full values reference.

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
