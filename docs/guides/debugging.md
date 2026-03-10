# Debugging

Mycel provides built-in tools for debugging your HCL flows without writing code or modifying configuration. Since there's no traditional code to step through, debugging in Mycel means **tracing the data pipeline** — seeing what data enters, how it's transformed, where it's validated, and what gets written.

## mycel trace

The `trace` command executes a single flow and shows a step-by-step trace of the data pipeline.

```bash
# Trace a read flow
mycel trace get_users --config ./my-service

# Trace a write flow with JSON input
mycel trace create_user --input '{"email":"test@example.com","name":"Test","age":25}'

# Trace with path parameters
mycel trace get_user --params id=123

# List all available flows
mycel trace --list
```

### Example Output

```
  Flow: create_user
  Duration: 6.2ms
  ──────────────────────────────────────────────────

  1. INPUT
     {"email":"TEST@Example.COM","name":"Test","age":25}

  2. SANITIZE  0.1ms
     {"email":"TEST@Example.COM","name":"Test","age":25}

  3. VALIDATE INPUT  0.2ms

  4. TRANSFORM  0.3ms
     {"id":"a1b2c3d4","email":"test@example.com","name":"Test","age":25,"created_at":"2026-03-10T14:30:00Z"}

  5. WRITE → users  5.4ms
     INSERT → users
     {"affected":1,"last_id":42}

  ✓ completed successfully
```

Each stage shows:
- **Stage name** and timing
- **Data snapshot** at that point in the pipeline
- **Errors** with the exact stage where they occurred
- **Skipped** stages (e.g., validation when no schema is configured)

### Dry-Run Mode

With `--dry-run`, write operations (INSERT, UPDATE, DELETE) are simulated without executing. Reads still run against real data so you can trace the full pipeline.

```bash
# See what would be written without actually writing
mycel trace create_user --input '{"email":"test@x.com","name":"Test","age":25}' --dry-run
```

Dry-run output marks write stages with `[dry-run]` and shows the payload that would have been sent:

```
  5. WRITE → users  [dry-run]
     INSERT → users
```

This is safe to run against production data sources — no data is modified. Dry-run works for all write operations: INSERT, UPDATE, DELETE, and multi-destination writes.

## Interactive Breakpoints

The `--breakpoints` flag enables interactive step-by-step debugging. Execution pauses at every pipeline stage, showing the current data state and waiting for your command.

```bash
# Pause at every pipeline stage
mycel trace create_user --input '{"email":"test@x.com","name":"Test"}' --breakpoints

# Pause only at specific stages
mycel trace create_user --input '{"email":"test@x.com"}' --break-at=transform,write
```

### Breakpoint Commands

When paused at a breakpoint, you can use:

| Command | Description |
|---------|-------------|
| `n`, `next` | Step to the next stage (default — just press Enter) |
| `c`, `continue` | Run until the next explicit breakpoint |
| `p`, `print` | Re-print the current data snapshot |
| `q`, `quit` | Abort flow execution |
| `h`, `help` | Show available commands |

### Example Session

```
  ⏸  BREAKPOINT at input
     {
       "email": "test@x.com",
       "name": "Test"
     }

  debug> n

  ⏸  BREAKPOINT at sanitize
     {
       "email": "test@x.com",
       "name": "Test"
     }

  debug> c

  ⏸  BREAKPOINT at write
     {
       "id": "a1b2c3d4",
       "email": "test@x.com",
       "name": "Test"
     }

  debug> q

  ✗ execution aborted by user
```

### Available Stages

Stages you can use with `--break-at`:

| Stage | Description |
|-------|-------------|
| `input` | Raw input data received |
| `sanitize` | After input sanitization |
| `filter` | Filter evaluation |
| `dedupe` | Deduplication check |
| `validate` | Input validation against type schema |
| `enrich` | Data enrichment from other connectors |
| `transform` | CEL transformation |
| `step` | Individual step execution |
| `read` | Database/API read operation |
| `write` | Database/API write operation |

## Verbose Flow Logging

For runtime debugging without stopping the service, use `--verbose-flow` to log every pipeline stage for all requests:

```bash
# Start with per-request pipeline tracing
mycel start --verbose-flow

# Combine with debug logging for maximum detail
mycel start --verbose-flow --log-level debug
```

This logs each pipeline stage (sanitize, validate, transform, read/write) as structured log entries at debug level. Useful for diagnosing issues in running services without attaching a debugger.

### Debugging Common Issues

| Problem | What trace shows |
|---------|-----------------|
| Validation error | Exact field and constraint that failed at VALIDATE INPUT |
| Transform bug | Input vs output data at TRANSFORM stage |
| Missing data | Which ENRICH or STEP failed to return expected fields |
| Wrong query | Filters passed to READ stage |
| Permission error | Error at WRITE stage with the exact operation |

## Local vs Docker

For the best debugging experience, run Mycel locally:

```bash
# Install locally
go install github.com/matutetandil/mycel/cmd/mycel@latest

# Trace directly
mycel trace create_user --input '{"email":"test@x.com"}'
```

Debugging also works from Docker — pass the `trace` command directly:

```bash
# Trace from Docker
docker run -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel trace get_users

# Trace with input
docker run -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace create_user --input '{"email":"test@x.com","name":"Test","age":25}' --dry-run

# Interactive breakpoints from Docker (requires -it for stdin)
docker run -it -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace create_user --input '{"email":"test@x.com"}' --breakpoints
```

## DAP Server (IDE Integration)

The `--dap` flag starts a Debug Adapter Protocol server that allows your IDE (VS Code, IntelliJ, Neovim) to control flow debugging with breakpoints, stepping, and variable inspection.

```bash
# Start DAP server on port 4711
mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711
```

The server waits for an IDE to connect via TCP, then executes the flow with breakpoints controlled by the IDE.

### How It Works

1. Mycel starts a DAP server on the specified port
2. Your IDE connects as a DAP client
3. The IDE sends `setBreakpoints` to pause at specific pipeline stages
4. The IDE sends `launch` to start the flow
5. At each breakpoint, the IDE receives a `stopped` event
6. You can inspect variables (flow data), view the pipeline stack, and step through stages

### Pipeline Stage to Line Mapping

DAP uses line numbers for breakpoints. Mycel maps pipeline stages to virtual lines:

| Line | Stage |
|------|-------|
| 1 | input |
| 2 | sanitize |
| 3 | filter |
| 4 | dedupe |
| 5 | validate_input |
| 6 | enrich |
| 7 | transform |
| 8 | step |
| 9 | validate_output |
| 10 | read |
| 11 | write |

### VS Code Configuration

To use with VS Code, create a `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "type": "mycel",
      "request": "launch",
      "name": "Debug Flow",
      "flow": "create_user",
      "input": {"email": "test@example.com", "name": "Test"},
      "dryRun": true
    }
  ]
}
```

> **Note:** A VS Code extension that registers the "mycel" debug adapter type is planned. In the meantime, you can connect to the DAP server using any DAP-compatible client or the [DAP protocol](https://microsoft.github.io/debug-adapter-protocol/) directly.

### Supported DAP Commands

| Command | Description |
|---------|-------------|
| `initialize` | Capability negotiation |
| `launch` | Start flow execution |
| `setBreakpoints` | Set breakpoints at pipeline stages (by line number) |
| `configurationDone` | Signal configuration is complete |
| `threads` | List flow execution threads |
| `stackTrace` | Show pipeline stages as call stack |
| `scopes` | List variable scopes (data at current stage) |
| `variables` | Inspect data values at current breakpoint |
| `continue` | Resume execution until next breakpoint |
| `next` | Step to next pipeline stage |
| `disconnect` | Stop debugging and abort flow |

> **Important:** The DAP server is only available in development mode (`MYCEL_ENV=development`). In staging or production, the `--dap` flag is ignored with a warning.

## Log-Level Debugging

For runtime debugging without stopping the service, use log levels:

```bash
# Start with debug logging (shows all internal operations)
mycel start --log-level debug

# Or via environment variable
MYCEL_LOG_LEVEL=debug mycel start
```

In development mode (`MYCEL_ENV=development`), the default log level is already `debug`.

## CLI Reference

```
mycel trace <flow-name> [flags]

Flags:
  --input string      JSON input data for the flow
  --params string     Key=value parameters (comma-separated, e.g., id=123,status=active)
  --dry-run           Simulate write operations without executing them
  --breakpoints       Pause at every pipeline stage for interactive debugging (dev only)
  --break-at string   Pause at specific stages (dev only, comma-separated: input,sanitize,validate,transform,step,read,write)
  --dap int           Start DAP server on this port for IDE debugging (dev only, e.g., --dap=4711)
  --list              List all available flows

mycel start [flags]

Flags:
  --verbose-flow      Log all flow pipeline stages per request (debug)

Global Flags:
  -c, --config string   Configuration directory (default ".")
  -e, --env string      Environment (dev, staging, prod)
  --log-level string    Log level: debug, info, warn, error
  --log-format string   Log format: text, json
```
