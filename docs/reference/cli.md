# CLI Reference

## Commands

### `mycel start`

Start the Mycel runtime with the given configuration.

```bash
mycel start [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config`, `-c` | `.` (current directory) | Path to the config directory |
| `--env` | `development` | Environment name (overrides `MYCEL_ENV`) |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `--log-format` | `text` | Log format: `text` or `json` |
| `--mock` | — | Enable mock for specific connector (repeatable) |
| `--no-mock` | — | Disable mock for specific connector (repeatable) |

**Examples:**

```bash
# Start with current directory as config
mycel start

# Start with specific config directory
mycel start --config ./my-service

# Start in production mode
mycel start --env production --log-format json

# Start with all connectors mocked
mycel start --mock=db --mock=external_api

# Start with all mocks except payment service
mycel start --no-mock=stripe
```

### `mycel validate`

Validate configuration files without starting the service. Reports HCL syntax errors, undefined references, and expression compilation errors.

```bash
mycel validate [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config`, `-c` | `.` | Path to the config directory |

**Example:**

```bash
mycel validate --config ./my-service
# Config validation successful: 2 connectors, 5 flows, 3 types
```

Also reports **readability advice** when a single file passes eight
declarations — never a failure, and never shown by `mycel start`, since where a
declaration lives changes nothing at runtime.

### `mycel check`

Check connectivity to all configured connectors. Useful before deployment to verify all services are reachable.

```bash
mycel check [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config`, `-c` | `.` | Path to the config directory |
| `--timeout` | `10s` | Per-connector timeout |

Every connector is built, connected and health-checked. Connectors are checked
concurrently, each with its own timeout, so one unreachable host does not stall
the rest. Unlike startup, a failure does not stop the sweep — the point is a
complete picture of what is reachable.

**Example:**

```bash
mycel check --config ./my-service
#   – api (rest): listens, nothing to reach
#   ✓ orders_db (database/postgres): connected in 12ms
#   ✓ cache (cache/redis): connected in 3ms
#   ✗ payments_api (http): no response within 10s
#
# Error: 1 of 4 connectors unreachable
```

Exits non-zero when any connector is unreachable, so it works as a deploy gate.

Connectors that **listen** rather than dial — REST, GraphQL, gRPC, SOAP and TCP
servers, plus SSE and WebSocket — are reported with `–` and never fail the
check. They have no endpoint to reach, and they are not started here, so their
health check would only ever report "not started". They are still built, which
is where a bad port or a malformed TLS config surfaces.

The distinction in the failure message matters: `connection refused` means
something answered and said no — usually a wrong port or a service that is
down. `no response within <timeout>` means nothing answered at all — usually a
firewall or a wrong host. Errors from building the connector are reported the
same way, including the missing environment variable when an `env()` call
resolved to nothing:

```
  ✗ products_api (http): factory failed to create connector products_api: http connector requires base_url
      → Missing environment variable "MERCURY_PRODUCTS_URL", required by connector "products_api" (base_url)
```

### `mycel init`

Scaffold a new project in the recommended layout.

```bash
mycel init my-service     # creates ./my-service and scaffolds into it
mycel init                # scaffolds into the current directory
```

```
  created my-service/config.mycel
  created my-service/connectors/api.mycel
  created my-service/flows/status.mycel
  created my-service/.gitignore
  created my-service/.env.example
```

The generated service runs as-is — start it and `GET /status` answers. The
service name comes from the directory, since it reaches logs, metric labels and
health output.

Mycel does not require this layout; it reads every `.mycel` file under the
config directory and merges them, so a single file behaves identically. The
scaffold hands you the shape that stays readable as a service grows. See
[Project Structure](../getting-started/project-structure.md).

Refuses to overwrite existing files, and writes nothing at all if any would
clash.

### `mycel add`

Add a connector or flow to an existing project, each in its own file.

```bash
mycel add connector orders_db --type database --driver postgres
mycel add connector rabbit --type mq --driver rabbitmq
mycel add flow order_created --from rabbit --to orders_db

mycel add connector --list       # available types
```

| Flag | Applies to | Description |
|------|-----------|-------------|
| `--type` | `connector` | Connector type — required |
| `--driver` | `connector` | Driver, for types that have one |
| `--list` | `connector` | List available types and exit |
| `--from` | `flow` | Source connector |
| `--to` | `flow` | Destination connector |

Files land in `connectors/<name>.mycel` and `flows/<name>.mycel` under
`--config`. Mycel merges every `.mycel` file regardless of location, so this is
a readability default, not a requirement — see
[Project Structure](../getting-started/project-structure.md).

The connector skeleton is generated from that connector's **own schema**, so
required attributes are the ones the runtime actually requires and cannot drift
from it. Required attributes are emitted with a placeholder; optional ones are
listed as comments, so the file doubles as a reference:

```hcl
connector "orders_db" {
  type   = "database"
  driver = "postgres"

  // Database name
  database = env("DATABASE") // TODO

  // Optional:
  //   host — Database server host
  //   port — Database server port
  //   sslmode — SSL mode (disable, require, verify-ca, verify-full)
}
```

Required string attributes default to `env("NAME")` rather than a literal:
these are usually hosts and credentials, and a committed literal is how secrets
reach a repository.

Three things are refused before anything is written:

- a name already used by another connector or flow, since
  [names are global](../getting-started/project-structure.md#names-are-global)
  across every file
- a flow wired to a connector that does not exist — the error lists the ones
  that do
- overwriting an existing file

### `mycel version`

Print the Mycel version, build commit, Go toolchain and platform.

```bash
mycel version
# mycel 2.12.0 (commit: 64be3eb, go1.25.0, linux/amd64)
```

The same information appears in the startup banner. This command exists for the
case where the banner has already rolled out of a pod's log buffer and you need
to know what is running without restarting it.

### `mycel export`

Export auto-generated API documentation.

```bash
mycel export openapi [flags]     # Export OpenAPI 3.0 spec
mycel export graphql-schema [flags]  # Export GraphQL SDL
mycel export asyncapi [flags]    # Export AsyncAPI spec
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config`, `-c` | `.` | Path to the config directory |
| `--output`, `-o` | stdout | Output file path |

**Examples:**

```bash
# Export OpenAPI spec to file
mycel export openapi --config ./my-service --output openapi.json

# Export GraphQL schema
mycel export graphql-schema --output schema.graphql
```

### `mycel plugin`

Manage WASM plugins.

```bash
mycel plugin install              # Install all declared plugins
mycel plugin list                 # List installed plugins
mycel plugin remove <name>        # Remove a plugin
mycel plugin update               # Update all plugins to latest compatible versions
```

**Examples:**

```bash
mycel plugin install
# Installing salesforce v1.2.0... done
# Installing stripe v3.0.1... done

mycel plugin list
# NAME          VERSION  SOURCE
# salesforce    1.2.0    github.com/acme/mycel-salesforce
# stripe        3.0.1    github.com/acme/mycel-stripe

mycel plugin remove salesforce
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MYCEL_ENV` | `development` | Environment name |
| `MYCEL_LOG_LEVEL` | `info` | Log level |
| `MYCEL_LOG_FORMAT` | `text` | Log format |
| `NO_COLOR` | unset | Disable colored output |
| `MYCEL_PLUGIN_CACHE` | unset | Plugin cache directory |

CLI flags take precedence over environment variables.

## Priority Chain

```
CLI flags > env vars > .env file > defaults
```
