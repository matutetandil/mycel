# Environments

Mycel supports environment-specific configuration for running the same service with different settings across development, staging, and production.

## Runtime Environment Variables

Mycel reads these variables at startup:

| Variable | Default | Description |
|----------|---------|-------------|
| `MYCEL_ENV` | `development` | Active environment name. Selects the overlay from `environments/` |
| `MYCEL_LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `MYCEL_LOG_FORMAT` | `text` | Log format: `text` (human-readable) or `json` (structured) |
| `NO_COLOR` | unset | Set to any value to disable colored output |
| `MYCEL_PLUGIN_CACHE` | unset | Directory to cache downloaded WASM plugins |

CLI flags override environment variables. Priority chain:

```
CLI flags > existing env vars > .env file > environment defaults > hardcoded defaults
```

## Environment-Aware Defaults

Mycel automatically adjusts behavior based on the active environment. These are *defaults* — any explicit configuration always takes precedence.

| Behavior | development | staging | production |
|----------|-------------|---------|------------|
| **Log level** | `debug` | `info` | `warn` |
| **Log format** | `text` | `json` | `json` |
| **Hot reload** | enabled | enabled | disabled |
| **GraphQL Playground** | enabled | enabled | disabled |
| **Detailed health** | latencies + messages | latencies + messages | status only |
| **Rate limiting** | disabled | enabled (100 req/s) | enabled (100 req/s) |
| **CORS** | permissive (all origins) | explicit config only | explicit config only |
| **Error responses** | verbose (internal details) | verbose | minimal (no internals) |

### Startup Warnings

In production and staging, Mycel logs warnings for potentially unsafe configurations:

- SQLite used in production (suggest PostgreSQL/MySQL)
- No authentication configured in production

### Examples

```bash
# Development: debug logs, hot reload, permissive CORS, verbose errors
mycel start

# Production: warn logs (JSON), no hot reload, strict CORS, minimal errors
mycel start --env production

# Override a default: production but with debug logging
mycel start --env production --log-level debug
```

## Using env() in HCL

Reference environment variables with the `env()` function anywhere in HCL:

```hcl
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  password = env("DB_PASSWORD")
}
```

You can also provide a default value:

```hcl
connector "db" {
  host = env("DB_HOST", "localhost")
  port = 5432
}
```

## .env File

Mycel loads a `.env` file automatically during `start`, `validate`, and `check` commands.

**Search order:**
1. `<config-dir>/.env` (next to your HCL files)
2. `./.env` (current working directory)

Variables in `.env` do **not** override already-set environment variables — they only fill in missing ones.

```bash
# .env (never commit this file)
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=secret

REDIS_ADDRESS=localhost:6379

MYCEL_ENV=development
MYCEL_LOG_LEVEL=debug

# External APIs
STRIPE_SECRET_KEY=sk_test_123
SENDGRID_API_KEY=SG.abc123
```

Add `.env` to `.gitignore`. Provide a `.env.example` with placeholder values:

```bash
# .env.example (commit this file)
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=CHANGE_ME

MYCEL_ENV=development
MYCEL_LOG_LEVEL=debug
```

## Environment Overlays

The `environments/` directory holds per-environment HCL files that override base configuration. When `MYCEL_ENV=staging` is set, Mycel loads `environments/staging.hcl` after loading all base files.

```
my-service/
├── config.hcl          # Base configuration
├── connectors.hcl      # Base connectors
├── environments/
│   ├── dev.hcl         # Development overrides
│   ├── staging.hcl     # Staging overrides
│   └── prod.hcl        # Production overrides
```

Example `environments/prod.hcl`:

```hcl
# Override the database connector with production settings
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("PROD_DB_HOST")
  database = "app_prod"
  user     = env("PROD_DB_USER")
  password = env("PROD_DB_PASSWORD")
  ssl_mode = "verify-full"

  pool {
    max = 50
    min = 10
  }
}
```

## Starting with a Specific Environment

```bash
# Via CLI flag
mycel start --env production --config ./my-service

# Via environment variable
MYCEL_ENV=production mycel start --config ./my-service

# In Docker
docker run \
  -v ./config:/etc/mycel \
  -e MYCEL_ENV=production \
  -e MYCEL_LOG_FORMAT=json \
  ghcr.io/matutetandil/mycel
```

## Kubernetes / Production

In production, use environment variables or Kubernetes Secrets instead of `.env` files:

```yaml
# Kubernetes Secret
apiVersion: v1
kind: Secret
metadata:
  name: my-service-secrets
stringData:
  DB_PASSWORD: "prod-password"
  STRIPE_SECRET_KEY: "sk_live_..."
```

```yaml
# Deployment
env:
  - name: MYCEL_ENV
    value: production
  - name: MYCEL_LOG_FORMAT
    value: json
  - name: DB_HOST
    value: postgres.internal
envFrom:
  - secretRef:
      name: my-service-secrets
```

See the [Deployment Guide](../deployment/kubernetes.md) for complete Kubernetes setup.
