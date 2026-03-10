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
  - [Pipeline Stages](#pipeline-stages)
  - [VS Code Setup](#vs-code-setup)
  - [IntelliJ / WebStorm Setup](#intellij--webstorm-setup)
  - [Neovim Setup](#neovim-setup)
  - [Other DAP Clients](#other-dap-clients)
  - [Supported DAP Commands](#supported-dap-commands)
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

The `--dap` flag starts a **Debug Adapter Protocol** server that enables full IDE debugging — breakpoints, stepping, variable inspection, and call stack navigation — directly in your editor.

> **Dev only.** The DAP server is automatically disabled outside development mode.

```bash
# Start DAP server on port 4711
mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711
```

Output:
```
DAP server listening on port 4711 — connect your IDE debugger
```

### How It Works

```
┌──────────────┐         TCP          ┌──────────────┐
│   Your IDE   │ ◄──────────────────► │  Mycel DAP   │
│  (VS Code,   │   DAP Protocol       │   Server     │
│   IntelliJ,  │   (JSON over TCP)    │  :4711       │
│   Neovim)    │                      │              │
└──────────────┘                      └──────┬───────┘
                                             │
                                     ┌───────▼───────┐
                                     │  Flow Engine   │
                                     │  input →       │
                                     │  sanitize →    │
                                     │  validate →    │
                                     │  transform →   │
                                     │  write         │
                                     └───────────────┘
```

1. You start `mycel trace --dap=4711` — the server waits for a connection
2. Your IDE connects to `localhost:4711` as a DAP client
3. The IDE sends `setBreakpoints` — mapping pipeline stages to virtual line numbers
4. The IDE sends `launch` — Mycel starts executing the flow
5. When a breakpoint is hit, the IDE receives a `stopped` event
6. You inspect variables (data at current stage), view the pipeline as a call stack, and step through
7. `continue` runs to the next breakpoint, `next` advances one stage

### Pipeline Stages

DAP uses **line numbers** for breakpoints. Mycel maps each pipeline stage to a virtual line number. When you set a breakpoint on line 7 in your IDE, you're breaking at the `transform` stage.

| Line | Stage | What happens here |
|------|-------|-------------------|
| 1 | `input` | Raw data received from the connector |
| 2 | `sanitize` | XSS/injection sanitization applied |
| 3 | `filter` | Filter expression evaluated (accept/reject) |
| 4 | `dedupe` | Duplicate message check |
| 5 | `validate_input` | Schema validation against type definition |
| 6 | `enrich` | Data enrichment from other connectors |
| 7 | `transform` | CEL transformation rules applied |
| 8 | `step` | Multi-step flow: individual step execution |
| 9 | `validate_output` | Output schema validation |
| 10 | `read` | Database/API read operation |
| 11 | `write` | Database/API write operation |

To create a visual reference file that your IDE can use for breakpoint placement, create a file called `pipeline.mycel` in your project root:

```
input
sanitize
filter
dedupe
validate_input
enrich
transform
step
validate_output
read
write
```

Then open this file in your IDE and set breakpoints on the lines corresponding to the stages you want to debug.

---

### VS Code Setup

#### Step 1: Start the DAP Server

```bash
mycel trace create_user --input '{"email":"test@x.com","name":"Test"}' --dap=4711
```

#### Step 2: Configure launch.json

Create `.vscode/launch.json` in your project:

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

> **How this works:** VS Code's `debugServer` option connects directly to a running DAP server over TCP. This works without a dedicated Mycel extension because the DAP protocol is standard — any debug adapter that speaks DAP over TCP can be used this way.

#### Step 3: Set Breakpoints

Open `pipeline.mycel` and click in the gutter to set breakpoints on the stages you want to debug (e.g., line 7 for `transform`, line 11 for `write`).

#### Step 4: Start Debugging

Press **F5** or use the Run & Debug panel. VS Code connects to the DAP server and the flow starts executing. When a breakpoint is hit:

- The **Variables** panel shows the data at the current stage
- The **Call Stack** panel shows the pipeline stages executed so far
- Use **F10** (Step Over) to advance to the next stage
- Use **F5** (Continue) to run to the next breakpoint
- Use **Shift+F5** (Stop) to abort

#### Alternative: Raw DAP Client

For advanced users, VS Code also supports connecting via the Debug Console. Install the [DAP Client](https://marketplace.visualstudio.com/items?itemName=nickclaw.dap-client) extension for raw protocol access.

---

### IntelliJ / WebStorm Setup

IntelliJ IDEA and WebStorm do not natively support connecting to arbitrary DAP servers via TCP out of the box. However, there are two approaches:

#### Option A: DAP Plugin (Recommended)

Install the **Debug Adapter Protocol** plugin from the JetBrains Marketplace:

1. Go to **Settings → Plugins → Marketplace**
2. Search for "Debug Adapter Protocol" or "DAP"
3. Install and restart the IDE

Then create a run configuration:

1. **Run → Edit Configurations → + → DAP Remote Debug**
2. Set **Host**: `localhost`
3. Set **Port**: `4711`
4. Click **Debug**

#### Option B: External Tool

Configure Mycel trace as an **External Tool**:

1. **Settings → Tools → External Tools → +**
2. **Name**: `Mycel Trace`
3. **Program**: `mycel`
4. **Arguments**: `trace $Prompt$ --dap=4711 --config $ProjectFileDir$`
5. **Working directory**: `$ProjectFileDir$`

Run it via **Tools → External Tools → Mycel Trace**. Enter the flow name when prompted, then connect your debugger to port 4711.

#### Option C: Terminal + Debug Console

The simplest approach — run the DAP server in IntelliJ's terminal:

```bash
mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711
```

Then use any DAP client to connect. The output and breakpoints appear in the terminal.

---

### Neovim Setup

Neovim has excellent DAP support through [nvim-dap](https://github.com/mfussenegger/nvim-dap).

#### Step 1: Install nvim-dap

Using [lazy.nvim](https://github.com/folke/lazy.nvim):

```lua
{
  "mfussenegger/nvim-dap",
  dependencies = {
    "rcarriga/nvim-dap-ui",     -- optional: visual UI
    "nvim-neotest/nvim-nio",    -- required by dap-ui
  },
}
```

#### Step 2: Configure the Mycel Adapter

Add this to your Neovim configuration (e.g., `~/.config/nvim/lua/dap-mycel.lua`):

```lua
local dap = require('dap')

-- Register the Mycel DAP adapter
dap.adapters.mycel = function(callback, config)
  callback({
    type = 'server',
    host = config.host or '127.0.0.1',
    port = config.port or 4711,
  })
end

-- Debug configuration
dap.configurations.hcl = {
  {
    type = 'mycel',
    request = 'launch',
    name = 'Debug Mycel Flow',
    flow = '${input:flow}',           -- prompts for flow name
    port = 4711,
    input = {},                        -- override per-project
  },
}

-- Optional: map pipeline stages to the visual file
-- Open pipeline.mycel and set breakpoints on lines 1-11
```

#### Step 3: Debug

1. Start the DAP server in a terminal split:
   ```bash
   mycel trace create_user --input '{"email":"test@x.com"}' --dap=4711
   ```

2. Open `pipeline.mycel` in Neovim

3. Set breakpoints with `:lua require('dap').toggle_breakpoint()` (or your keybinding, commonly `<leader>db`)

4. Start debugging with `:lua require('dap').continue()` (commonly `<F5>`)

5. When a breakpoint hits:
   - `:lua require('dap').step_over()` — next stage (`<F10>`)
   - `:lua require('dap').continue()` — run to next breakpoint (`<F5>`)
   - `:lua require('dap.ui.widgets').hover()` — inspect variable under cursor
   - `:lua require('dap').terminate()` — abort flow

#### Recommended Keybindings

```lua
vim.keymap.set('n', '<F5>', function() require('dap').continue() end)
vim.keymap.set('n', '<F10>', function() require('dap').step_over() end)
vim.keymap.set('n', '<leader>db', function() require('dap').toggle_breakpoint() end)
vim.keymap.set('n', '<leader>dr', function() require('dap').repl.open() end)
vim.keymap.set('n', '<leader>dl', function() require('dap').run_last() end)
```

If you use [nvim-dap-ui](https://github.com/rcarriga/nvim-dap-ui), the variables, call stack, and breakpoints panels appear automatically when a debug session starts.

---

### Other DAP Clients

Any tool that speaks the Debug Adapter Protocol over TCP can connect to Mycel's DAP server. Some options:

| Client | How to Connect |
|--------|---------------|
| **Emacs (dap-mode)** | `(dap-register-debug-provider "mycel" ...)` with `:host "localhost" :port 4711` |
| **Sublime Text (Debugger)** | Install the Debugger package, configure a custom adapter pointing to `localhost:4711` |
| **Eclipse** | DAP4E plugin supports custom debug adapters |
| **Command line** | Use any DAP client library (Node.js: `@vscode/debugadapter`, Python: `debugpy`) to connect to `localhost:4711` |

The DAP protocol is an open standard — see the [full specification](https://microsoft.github.io/debug-adapter-protocol/specification).

---

### Supported DAP Commands

| Command | Description |
|---------|-------------|
| `initialize` | Capability negotiation (called once on connect) |
| `launch` | Start flow execution with optional input and dry-run |
| `setBreakpoints` | Set breakpoints at pipeline stages (by line number 1-11) |
| `configurationDone` | Signal that IDE configuration is complete |
| `threads` | List debug threads (one per flow execution) |
| `stackTrace` | Show pipeline stages as a call stack |
| `scopes` | List variable scopes for a stack frame |
| `variables` | Inspect data values at the current breakpoint |
| `continue` | Resume execution until next breakpoint |
| `next` | Step to the next pipeline stage |
| `disconnect` | Stop debugging and abort the flow |

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

1. Make sure you're using the correct line numbers (see [Pipeline Stages](#pipeline-stages) table)
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
                      Valid stages: input, sanitize, filter, dedupe, validate,
                      transform, step, read, write
  --dap int           Start DAP server on this port for IDE debugging (dev only)
  --list              List all available flows

mycel start [flags]

Flags:
  --verbose-flow      Log all flow pipeline stages per request (dev only)

Global Flags:
  -c, --config string   Configuration directory (default ".")
  -e, --env string      Environment (dev, staging, prod)
  --log-level string    Log level: debug, info, warn, error
  --log-format string   Log format: text, json
```
