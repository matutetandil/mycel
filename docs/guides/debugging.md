# Debugging

Mycel provides a complete debugging toolkit for HCL flows — from simple CLI tracing to full IDE integration with breakpoints. Since there's no traditional code to step through, debugging in Mycel means **tracing the data pipeline**: seeing what data enters each stage, how it's transformed, where it's validated, and what gets written.

All debug features (breakpoints, DAP, verbose flow) are **development-only** — they are automatically disabled in staging and production with a warning log. This ensures zero overhead and zero risk in production.

---

## Table of Contents

- [Quick Start](#quick-start)
- [mycel trace](#mycel-trace)
- [Dry-Run Mode](#dry-run-mode)
- [Interactive Breakpoints (CLI)](#interactive-breakpoints-cli)
- [DAP Server (IDE Integration)](#dap-server-ide-integration)
  - [How It Works](#how-it-works)
  - [VS Code](#vs-code)
  - [IntelliJ / WebStorm](#intellij--webstorm)
  - [Neovim](#neovim)
  - [Supported DAP Commands](#supported-dap-commands)
- [Studio Debug Protocol](#studio-debug-protocol)
  - [Connecting](#connecting)
  - [Setup Handshake](#setup-handshake)
  - [Methods](#methods)
  - [debug.consume](#debugconsume)
  - [Events](#events)
  - [Per-CEL-Rule Debugging](#per-cel-rule-debugging)
  - [Automatic Debug Throttling](#automatic-debug-throttling)
  - [Start Suspended Mode](#start-suspended-mode)
- [Verbose Flow Logging](#verbose-flow-logging)
- [Log-Level Debugging](#log-level-debugging)
- [Local vs Docker](#local-vs-docker)
- [Troubleshooting](#troubleshooting)
- [CLI Reference](#cli-reference)

---

## Quick Start

```bash
# See what a flow does, step by step
mycel trace get_users

# Simulate a write without touching the database
mycel trace create_user --input '{"email":"test@x.com","name":"Test"}' --dry-run

# Interactive debugging — pause at each stage
mycel trace create_user --input '{"email":"test@x.com"}' --breakpoints

# IDE debugging — connect VS Code, IntelliJ, or Neovim
mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711
```

---

## mycel trace

The `trace` command executes a single flow and shows a step-by-step trace of the entire data pipeline.

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

### Common Issues Diagnosed by Trace

| Problem | What trace shows |
|---------|-----------------|
| Validation error | Exact field and constraint that failed at VALIDATE INPUT |
| Transform bug | Input vs output data at TRANSFORM — see exactly what changed |
| Missing data | Which ENRICH or STEP failed to return expected fields |
| Wrong query | Filters passed to READ stage |
| Permission error | Error at WRITE stage with the exact operation attempted |
| Sanitization issue | Before/after at SANITIZE — see what the sanitizer changed |

---

## Dry-Run Mode

With `--dry-run`, write operations (INSERT, UPDATE, DELETE) are simulated without executing. Reads still run against real data so you can trace the full pipeline end-to-end.

```bash
# See what would be written without actually writing
mycel trace create_user --input '{"email":"test@x.com","name":"Test","age":25}' --dry-run

# Works for updates too
mycel trace update_user --input '{"id":"123","name":"New Name"}' --dry-run

# And deletes
mycel trace delete_user --params id=123 --dry-run
```

Dry-run output marks write stages with `[dry-run]` and shows the payload that would have been sent:

```
  5. WRITE → users  [dry-run]
     INSERT → users
     {"id":"a1b2c3d4","email":"test@x.com","name":"Test","age":25}
```

Dry-run is safe to run against production data sources — no data is modified. It works for:
- **INSERT** — shows the payload that would be inserted
- **UPDATE** — shows the payload and filters (which rows would be affected)
- **DELETE** — shows the filters (which rows would be deleted)
- **Multi-destination writes** — shows what would be written to each destination

---

## Interactive Breakpoints (CLI)

The `--breakpoints` flag enables interactive step-by-step debugging directly in your terminal. Execution pauses at every pipeline stage, showing the current data state and waiting for your command.

> **Dev only.** Breakpoints are automatically disabled outside development mode.

```bash
# Pause at every pipeline stage
mycel trace create_user --input '{"email":"test@x.com","name":"Test"}' --breakpoints

# Pause only at specific stages (faster iteration)
mycel trace create_user --input '{"email":"test@x.com"}' --break-at=transform,write
```

### Commands

When paused at a breakpoint, you can use these commands:

| Command | Shortcut | Description |
|---------|----------|-------------|
| `next` | `n` or Enter | Step to the next stage |
| `continue` | `c` | Run until the next explicit breakpoint |
| `print` | `p` | Re-print the current data snapshot |
| `quit` | `q` | Abort flow execution immediately |
| `help` | `h` | Show available commands |

### Example Session

```
$ mycel trace create_user --input '{"email":"TEST@X.COM","name":"Test"}' --breakpoints

  ⏸  BREAKPOINT at input
     {
       "email": "TEST@X.COM",
       "name": "Test"
     }

  debug> n

  ⏸  BREAKPOINT at sanitize
     {
       "email": "TEST@X.COM",
       "name": "Test"
     }

  debug> n

  ⏸  BREAKPOINT at transform
     {
       "email": "TEST@X.COM",
       "name": "Test"
     }

  debug> p
     {
       "email": "TEST@X.COM",
       "name": "Test"
     }

  debug> c

  ⏸  BREAKPOINT at write
     {
       "id": "a1b2c3d4",
       "email": "test@x.com",
       "name": "Test",
       "created_at": "2026-03-10T14:30:00Z"
     }

  debug> q

  ✗ execution aborted by user
```

### Available Stages for --break-at

| Stage | Description |
|-------|-------------|
| `input` | Raw input data as received |
| `sanitize` | After input sanitization (XSS, injection protection) |
| `filter` | Filter expression evaluation (accept/reject) |
| `dedupe` | Deduplication check |
| `validate` | Input validation against type schema |
| `enrich` | Data enrichment from other connectors |
| `transform` | CEL transformation (the most common breakpoint) |
| `step` | Individual step execution in multi-step flows |
| `read` | Database/API read operation |
| `write` | Database/API write operation |

**Tip:** For most debugging sessions, `--break-at=transform,write` is the sweet spot — you see the data right before and after transformation, and right before it's written.

---

## DAP Server (IDE Integration)

The `--dap` flag starts a **Debug Adapter Protocol** server that lets IDE debugger panels (variables, call stack) connect to a running trace session.

> **Dev only.** The DAP server is automatically disabled outside development mode.

> **Note: Gutter breakpoints.** Currently, you cannot click the gutter of an HCL file to set breakpoints — IDEs only enable that for file types with a registered debug adapter. Dedicated extensions for VS Code and IntelliJ that enable gutter breakpoints on HCL files are planned alongside the [Mycel Language Server](#). For now, use `--break-at` on the CLI or the `breakAt` property in your launch config.

### How It Works

1. Start `mycel trace` with `--dap`:
   ```bash
   mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711
   ```
2. Your IDE connects to `localhost:4711` as a DAP client
3. The IDE sends `launch` (with `breakAt` stages) — Mycel executes the flow
4. When a breakpoint stage is reached, the IDE shows variables and call stack
5. **F10** (Step Over) advances to the next stage, **F5** (Continue) runs to the next breakpoint

### VS Code

Create `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Mycel Flow",
      "type": "node",
      "request": "attach",
      "debugServer": 4711
    }
  ]
}
```

> VS Code's `debugServer` option connects directly to any running DAP server over TCP — no extension required.

Start the DAP server, then press **F5**. When a breakpoint hits:
- **Variables** panel shows data at the current stage
- **Call Stack** shows pipeline stages executed so far
- **F10** → next stage, **F5** → continue, **Shift+F5** → abort

### IntelliJ / WebStorm

IntelliJ does not natively support connecting to arbitrary DAP servers. Options:

1. **DAP Plugin**: Install "Debug Adapter Protocol" from JetBrains Marketplace → **Run → Edit Configurations → DAP Remote Debug** → host `localhost`, port `4711`
2. **Terminal**: Use `mycel trace --break-at=transform,write` directly in IntelliJ's terminal (recommended until a dedicated plugin is available)

### Neovim

Neovim has excellent DAP support through [nvim-dap](https://github.com/mfussenegger/nvim-dap):

```lua
local dap = require('dap')

dap.adapters.mycel = function(callback, config)
  callback({ type = 'server', host = '127.0.0.1', port = config.port or 4711 })
end

dap.configurations.mycel = {
  {
    type = 'mycel',
    request = 'launch',
    name = 'Debug Mycel Flow',
    flow = '${input:flow}',
    port = 4711,
    breakAt = { 'transform', 'write' },
  },
}
```

Start the DAP server, then `:lua require('dap').continue()` (**F5**).

### Supported DAP Commands

| Command | Description |
|---------|-------------|
| `initialize` | Capability negotiation |
| `launch` | Start flow execution (supports `input`, `dryRun`, `breakAt`) |
| `setBreakpoints` | Set breakpoints at pipeline stages |
| `configurationDone` | Signal IDE configuration complete |
| `threads` | List debug threads (one per flow) |
| `stackTrace` | Pipeline stages as call stack |
| `scopes` / `variables` | Inspect data at current breakpoint |
| `continue` / `next` | Resume or step to next stage |
| `disconnect` | Stop debugging and abort flow |

---

## Studio Debug Protocol

The Studio Debug Protocol provides a WebSocket-based debug interface designed for **Mycel Studio** (the desktop IDE) and any WebSocket-capable client. Unlike the DAP server which debugs a single trace, Studio connects to a **running service** and debugs requests in real time — similar to Chrome DevTools or IntelliJ's debugger.

The protocol uses **JSON-RPC 2.0** over WebSocket at `:9090/debug` (the admin server port).

### Connecting

```javascript
// From Electron/Tauri IDE or any WebSocket client
const ws = new WebSocket("ws://localhost:9090/debug");

// Attach to get session ID and flow list
ws.send(JSON.stringify({
  jsonrpc: "2.0", id: 1,
  method: "debug.attach",
  params: { clientName: "mycel-studio 1.0" }
}));
// Response: { jsonrpc: "2.0", id: 1, result: { sessionId: "s1", flows: ["create_user", "get_users"] } }
```

### Setup Handshake

Before any messages are consumed, Studio must complete a setup handshake. This eliminates race conditions where messages arrive before breakpoints are configured.

```
Studio                           Mycel
  │                                │
  ├─── debug.attach ──────────────►│  Session created, debug throttling enabled
  │◄── { sessionId, flows } ──────┤
  │                                │
  ├─── debug.setBreakpoints ──────►│  Breakpoints registered (repeat per flow)
  │◄── { breakpoints } ───────────┤
  │                                │
  ├─── debug.ready ───────────────►│  Suspended connectors start (topology only)
  │◄── { ok, sources } ───────────┤  Returns event source capabilities
  │                                │
  ├─── debug.consume ─────────────►│  Pull ONE message from queue (RabbitMQ/Kafka)
  │    ... breakpoint hit ...      │
  │◄── event.stopped ─────────────┤
  │                                │
  ├─── debug.continue ────────────►│  Resume paused thread
  │◄── event.continued ───────────┤
  │◄── { ok } ────────────────────┤  Message fully processed
  │                                │
  ├─── debug.consume ─────────────►│  Pull next message (repeat as needed)
  │    ...                         │
```

**`debug.ready` response** includes event source capabilities:

```json
{
  "jsonrpc": "2.0", "id": 3,
  "result": {
    "ok": true,
    "sources": [
      {
        "connector": "rabbit",
        "type": "rabbitmq",
        "source": "orders.q",
        "manualConsume": true
      },
      {
        "connector": "mqtt_sensors",
        "type": "mqtt",
        "source": "sensors/#",
        "manualConsume": false
      }
    ]
  }
}
```

- **`manualConsume: true`** — queue-based connectors (RabbitMQ, Kafka) that support `debug.consume`. No messages are consumed until Studio explicitly requests them.
- **`manualConsume: false`** — push-based connectors (Redis Pub/Sub, MQTT, CDC, File, WebSocket) where messages arrive in real time. These use automatic debug throttling (one at a time) instead.

### Methods

| Method | Purpose |
|---|---|
| `debug.attach` | Connect debugger, get session + flow list |
| `debug.detach` | Disconnect cleanly |
| `debug.setBreakpoints` | Set breakpoints (stage + rule-level + conditional) |
| `debug.ready` | Signal setup complete; returns event source capabilities |
| `debug.consume` | Fetch one message from a queue connector (RabbitMQ/Kafka) |
| `debug.continue` | Resume paused thread |
| `debug.next` | Step to next pipeline stage |
| `debug.stepInto` | Step per-CEL-rule within transform |
| `debug.evaluate` | Evaluate arbitrary CEL in current context |
| `debug.variables` | Get variables at current breakpoint |
| `debug.threads` | List active debug threads |
| `inspect.flows` | List all flows with configs |
| `inspect.flow` | Full flow config detail |
| `inspect.connectors` | List connectors |
| `inspect.types` | List type schemas |
| `inspect.transforms` | List named transforms |

### debug.consume

For queue-based connectors (RabbitMQ, Kafka), `debug.consume` fetches and processes exactly **one message** from the queue. The request blocks until the message is fully processed (including hitting any breakpoints along the way).

```json
{
  "jsonrpc": "2.0", "id": 10,
  "method": "debug.consume",
  "params": { "connector": "rabbit" }
}
```

How it works per connector:
- **RabbitMQ**: Uses AMQP `Basic.Get` (pull one message). If the queue is empty, polls until a message arrives or the request is cancelled.
- **Kafka**: Uses `FetchMessage` to pull one message, then commits the offset after processing.

This gives Studio full control over when messages are consumed, making it easy to debug event-driven flows step by step. The IDE can show a "Consume Next Message" button that triggers `debug.consume`.

Push-based connectors (MQTT, Redis Pub/Sub, CDC, etc.) don't support `debug.consume` — they receive messages in real time via automatic debug throttling.

### Events

Events are JSON-RPC notifications (no `id`, no response expected) pushed from the runtime to the IDE:

| Event | Purpose |
|---|---|
| `event.stopped` | Thread hit breakpoint (stage or rule-level) |
| `event.continued` | Thread resumed |
| `event.stageEnter` | Pipeline stage starting (with input data) |
| `event.stageExit` | Pipeline stage completed (with output, duration, error) |
| `event.ruleEval` | Individual CEL rule evaluated (target, expression, result) |
| `event.flowStart` | Request entered flow |
| `event.flowEnd` | Request completed flow |

### Per-CEL-Rule Debugging

Set a rule-level breakpoint to pause at a specific CEL expression within a transform:

```json
{
  "jsonrpc": "2.0", "id": 2,
  "method": "debug.setBreakpoints",
  "params": {
    "flow": "create_user",
    "breakpoints": [
      { "stage": "transform", "ruleIndex": -1 },
      { "stage": "transform", "ruleIndex": 1, "condition": "input.email != \"\"" }
    ]
  }
}
```

- `ruleIndex: -1` pauses at the transform stage (before any rules execute)
- `ruleIndex: 0` pauses before the first CEL rule
- `condition` is a CEL expression evaluated against the current activation — only pauses when true

Use `debug.stepInto` to step through rules one at a time, and `debug.evaluate` to run ad-hoc CEL expressions against the paused thread's data.

**Zero-cost when idle**: When no Studio client is connected, the overhead is zero. When connected but no breakpoints set, only lightweight event streaming occurs. Breakpoints add pause/resume overhead only to flows that have them.

### Automatic Debug Throttling

When a debugger connects, all event-driven connectors automatically switch to **single-message processing**. This applies to push-based connectors where messages arrive in real time:

- **Redis Pub/Sub**: Messages are gated one at a time
- **MQTT**: All topic callbacks are serialized through a shared gate
- **CDC**: Database change events are processed one at a time
- **File watch**: File events are processed one at a time
- **WebSocket**: Incoming client messages are serialized

Queue-based connectors (**RabbitMQ**, **Kafka**) go further: in debug suspend mode, they don't consume at all until Studio sends `debug.consume` (see [Manual Consume](#debugconsume)).

When the debugger disconnects, original concurrency settings are restored automatically. This ensures you can step through messages one by one without a flood of concurrent events interfering with your debugging session.

No configuration is needed — throttling is enabled automatically via the `DebugThrottler` interface on each connector.

**DAP coexistence**: Studio protocol and DAP are fully independent. Both implement `trace.BreakpointController` but use different transports (WebSocket vs TCP) and lifecycle models (long-lived vs one-shot).

### Start Suspended Mode

When debugging event-driven flows (message queues, CDC, etc.), there's a timing problem: if Mycel starts consuming before your debugger connects, messages may be processed before you can set breakpoints.

**Start Suspended** solves this by deferring `Start()` on event-driven connectors until a debugger completes the setup handshake (`debug.ready`):

```bash
# Via CLI flag
mycel start --debug-suspend

# Via environment variable
MYCEL_DEBUG_SUSPEND=true mycel start
```

**What gets suspended:**
- RabbitMQ, Kafka, Redis Pub/Sub, MQTT — no messages consumed
- CDC — no change events captured
- File watch — no file events processed
- WebSocket — no client connections accepted

**What starts normally** (needed for health checks and admin API):
- REST, gRPC, GraphQL, SOAP, TCP, SSE

**Lifecycle:**
1. Mycel starts — event-driven connectors are registered but not started
2. Studio connects to `:9090/debug` and sends `debug.attach`
3. Studio sets breakpoints with `debug.setBreakpoints`
4. Studio sends `debug.ready` — suspended connectors connect to brokers and set up topology
5. For queue-based connectors (RabbitMQ, Kafka): Studio sends `debug.consume` to pull one message at a time
6. For push-based connectors (MQTT, CDC, etc.): messages arrive automatically, throttled to one at a time

> **Dev only.** Like all debug features, `--debug-suspend` is automatically disabled outside development mode with a warning log.

---

## Verbose Flow Logging

For runtime debugging without stopping the service, use `--verbose-flow` to log every pipeline stage for all requests as structured log entries:

> **Dev only.** Verbose flow logging is automatically disabled outside development mode.

```bash
# Start with per-request pipeline tracing
mycel start --verbose-flow

# Combine with debug logging for maximum detail
mycel start --verbose-flow --log-level debug
```

Example log output (each request generates multiple log lines):

```
DBG trace stage=input flow=create_user data={"email":"test@x.com"}
DBG trace stage=sanitize flow=create_user duration=0.1ms data={"email":"test@x.com"}
DBG trace stage=validate_input flow=create_user duration=0.2ms
DBG trace stage=transform flow=create_user duration=0.3ms data={"id":"abc","email":"test@x.com"}
DBG trace stage=write flow=create_user duration=5.1ms data={"affected":1}
```

This is useful for:
- **Diagnosing intermittent issues** in a running service
- **Comparing requests** that succeed vs. fail
- **Verifying transforms** without stopping the service
- **Monitoring pipeline performance** (each stage is timed)

---

## Log-Level Debugging

For runtime debugging without pipeline tracing, adjust the log level:

```bash
# Start with debug logging (shows all internal operations)
mycel start --log-level debug

# Or via environment variable
MYCEL_LOG_LEVEL=debug mycel start
```

In development mode (`MYCEL_ENV=development`), the default log level is already `debug`.

Log levels from most to least verbose:

| Level | What's logged |
|-------|--------------|
| `debug` | Everything: connector operations, query details, cache hits/misses, config parsing |
| `info` | Service lifecycle, request summaries, connector status |
| `warn` | Deprecations, configuration issues, retry attempts |
| `error` | Failures only |

---

## Local vs Docker

For the best debugging experience, **run Mycel locally**:

```bash
# Install locally
go install github.com/matutetandil/mycel/cmd/mycel@latest

# Trace directly
mycel trace create_user --input '{"email":"test@x.com"}'

# Interactive breakpoints
mycel trace create_user --input '{"email":"test@x.com"}' --breakpoints
```

Debugging also works from Docker — pass the `trace` command directly:

```bash
# Simple trace from Docker
docker run -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace get_users

# Dry-run from Docker
docker run -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace create_user --input '{"email":"test@x.com"}' --dry-run

# Interactive breakpoints from Docker (requires -it for stdin)
docker run -it -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace create_user --input '{"email":"test@x.com"}' --breakpoints

# DAP server from Docker (expose the port)
docker run -p 4711:4711 -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace create_user --input '{"email":"test@x.com"}' --dap=4711
```

---

## Troubleshooting

### "debug features are only available in development mode"

Debug flags (`--breakpoints`, `--break-at`, `--dap`, `--verbose-flow`) only work when `MYCEL_ENV=development` (the default). If you see this warning:

```bash
# Set explicitly
MYCEL_ENV=development mycel trace create_user --breakpoints

# Or via .env file
echo "MYCEL_ENV=development" >> .env
```

### "DAP server listening..." but IDE won't connect

1. Verify the port is correct: `mycel trace ... --dap=4711` means connect to `localhost:4711`
2. Check for firewalls or port conflicts: `lsof -i :4711`
3. Make sure you're connecting **after** the "listening" message appears
4. Some IDEs need a brief delay — if it fails on first try, wait 1 second and retry

### Breakpoints not hitting

1. Make sure you're using valid stage names (see [Pipeline Stages](#pipeline-stages) table)
2. Not all stages execute for every flow — e.g., `enrich` only runs if the flow has enrichments configured
3. Use `--breakpoints` (all stages) first to see which stages your flow actually goes through

### "flow not found"

```bash
# List available flows
mycel trace --list --config ./my-service
```

The `--config` flag must point to the directory containing your HCL files.

### Docker: breakpoints don't work

You need `-it` flags for interactive mode (stdin access):

```bash
# Wrong (no stdin)
docker run -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel trace ... --breakpoints

# Correct
docker run -it -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel trace ... --breakpoints
```

### Docker: DAP server unreachable

Expose the DAP port with `-p`:

```bash
docker run -p 4711:4711 -v $(pwd):/etc/mycel ghcr.io/matutetandil/mycel \
  trace ... --dap=4711
```

---

## CLI Reference

```
mycel trace <flow-name> [flags]

Flags:
  --input string      JSON input data for the flow
  --params string     Key=value parameters (comma-separated, e.g., id=123,status=active)
  --dry-run           Simulate write operations without executing them
  --breakpoints       Pause at every pipeline stage for interactive debugging (dev only)
  --break-at string   Pause at specific stages (dev only, comma-separated)
                      Valid stages: input, sanitize, filter, accept, dedupe, validate,
                      transform, step, read, write
  --dap int           Start DAP server on this port for IDE debugging (dev only)
  --list              List all available flows

mycel start [flags]

Flags:
  --verbose-flow      Log all flow pipeline stages per request (dev only)
  --debug-suspend     Defer event-driven connector start until debugger connects (dev only)

Environment Variables:
  MYCEL_DEBUG_SUSPEND=true   Same as --debug-suspend

Global Flags:
  -c, --config string   Configuration directory (default ".")
  -e, --env string      Environment (dev, staging, prod)
  --log-level string    Log level: debug, info, warn, error
  --log-format string   Log format: text, json

Admin Server (always available on :9090):
  /health             Health check endpoints
  /metrics            Prometheus metrics
  /debug              Studio Debug Protocol (WebSocket JSON-RPC 2.0)
```
