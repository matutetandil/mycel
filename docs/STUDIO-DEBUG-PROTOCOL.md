# Mycel Studio Debug Protocol

Complete specification and implementation guide for the Mycel Studio Debug Protocol. This document contains **everything** needed to build a client (IDE, CLI tool, or any WebSocket-capable application) that connects to a running Mycel service and debugs it in real time.

---

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Transport](#transport)
- [Session Lifecycle](#session-lifecycle)
- [Protocol Reference](#protocol-reference)
  - [Methods (IDE → Runtime)](#methods-ide--runtime)
  - [Events (Runtime → IDE)](#events-runtime--ide)
- [Data Types](#data-types)
- [Breakpoint System](#breakpoint-system)
  - [Stage-Level Breakpoints](#stage-level-breakpoints)
  - [Per-CEL-Rule Breakpoints](#per-cel-rule-breakpoints)
  - [Conditional Breakpoints](#conditional-breakpoints)
- [Variable Inspection](#variable-inspection)
- [CEL Expression Evaluation](#cel-expression-evaluation)
- [Event Streaming](#event-streaming)
- [Runtime Inspection](#runtime-inspection)
- [Threading Model](#threading-model)
- [Pipeline Stages](#pipeline-stages)
- [Implementation Details](#implementation-details)
  - [Package Structure](#package-structure)
  - [Server Implementation](#server-implementation)
  - [Session Management](#session-management)
  - [EventStream and StudioCollector](#eventstream-and-studiocollector)
  - [StudioBreakpointController](#studiobreakpointcontroller)
  - [StudioTransformHook](#studiotransformhook)
  - [TransformHook Interface](#transformhook-interface)
  - [RuntimeInspector Interface](#runtimeinspector-interface)
  - [Runtime Integration](#runtime-integration)
  - [Flow Handler Integration](#flow-handler-integration)
- [Zero-Cost Design](#zero-cost-design)
- [DAP Coexistence](#dap-coexistence)
- [Complete Example Session](#complete-example-session)
- [Error Codes](#error-codes)
- [Testing](#testing)

---

## Overview

The Mycel Studio Debug Protocol provides a **WebSocket JSON-RPC 2.0** interface for real-time debugging of Mycel services. It enables:

1. **Runtime Inspection** — List flows, connectors, types, and transforms from a running service
2. **Event Streaming** — Watch pipeline events in real time (flow start/end, stage enter/exit, CEL rule evaluation)
3. **Stage-Level Breakpoints** — Pause execution at any pipeline stage (sanitize, validate, transform, read, write, etc.)
4. **Per-CEL-Rule Breakpoints** — Pause at individual CEL expressions within a transform block
5. **Variable Inspection** — Examine input, output, enriched data, and step results at any breakpoint
6. **CEL Evaluation** — Execute arbitrary CEL expressions against the paused thread's data context
7. **Multi-Thread Debugging** — Debug multiple concurrent requests simultaneously

The experience is similar to debugging Java in IntelliJ or JavaScript in Chrome DevTools, but for HCL configuration pipelines.

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    Mycel Runtime                          │
│                                                          │
│  ┌──────────────┐    ┌──────────────┐                    │
│  │  REST Server  │    │ Admin Server │ ← always :9090     │
│  │  :8080        │    │   /health    │                    │
│  │  (user API)   │    │   /metrics   │                    │
│  │               │    │   /debug  ←──┼── WebSocket        │
│  └──────┬───────┘    └──────────────┘    JSON-RPC 2.0    │
│         │                                     ↑           │
│         ▼                                     │           │
│  ┌──────────────────────────────────────┐     │           │
│  │          FlowHandler.HandleRequest    │     │           │
│  │                                      │     │           │
│  │  1. Check DebugServer.HasClients()   │     │           │
│  │  2. Create DebugThread               │     │           │
│  │  3. Attach StudioCollector           │ ────┘           │
│  │  4. Attach BreakpointController      │  (events)       │
│  │  5. Attach TransformHook             │                  │
│  │  6. Execute pipeline...              │                  │
│  │  7. Broadcast FlowEnd               │                  │
│  └──────────────────────────────────────┘                  │
│                                                            │
│  ┌──────────────────────────────────────┐                  │
│  │        internal/debug/ package        │                  │
│  │                                      │                  │
│  │  Server ── Session ── DebugThread    │                  │
│  │    │         │           │           │                  │
│  │    │         │    pause/resume ch    │                  │
│  │    │         │                       │                  │
│  │  EventStream ── StudioCollector      │                  │
│  │    │                                 │                  │
│  │  StudioBreakpointController          │                  │
│  │  StudioTransformHook                 │                  │
│  └──────────────────────────────────────┘                  │
└──────────────────────────────────────────────────────────┘
          ↑
          │ WebSocket (ws://localhost:9090/debug)
          │
┌─────────┴─────────┐
│   Mycel Studio     │
│   (Electron/Tauri) │
│                    │
│   or any WS client │
└────────────────────┘
```

### Key Components

| Component | Location | Purpose |
|---|---|---|
| `Server` | `internal/debug/server.go` | WebSocket handler, JSON-RPC dispatch, session management |
| `Session` | `internal/debug/session.go` | Per-client breakpoints and thread registry |
| `DebugThread` | `internal/debug/session.go` | Per-request pause/resume state |
| `EventStream` | `internal/debug/stream.go` | Fan-out events to all connected clients |
| `StudioCollector` | `internal/debug/stream.go` | trace.Collector that broadcasts to EventStream |
| `StudioBreakpointController` | `internal/debug/controller.go` | trace.BreakpointController for stage-level |
| `StudioTransformHook` | `internal/debug/controller.go` | transform.TransformHook for per-rule |
| `RuntimeInspector` | `internal/debug/inspector.go` | Interface for read-only runtime access |
| `TransformHook` | `internal/transform/hook.go` | Interface for hooking into CEL rule loops |

---

## Transport

- **Protocol**: WebSocket (RFC 6455)
- **Endpoint**: `ws://<host>:9090/debug`
- **Port**: Admin server port (default 9090, configurable via `service.admin_port` in HCL)
- **Message Format**: JSON-RPC 2.0 (UTF-8 text frames)
- **Lifecycle**: Long-lived connection. The IDE connects once and debugs multiple requests over time.

The admin server always starts (even when a REST connector is present), so `:9090/debug` is always available.

---

## Session Lifecycle

```
1. Client connects via WebSocket to ws://host:9090/debug
2. Client sends debug.attach → gets sessionId + flow list
3. Client sends inspect.* methods to explore the runtime
4. Client sends debug.setBreakpoints to configure breakpoints
5. HTTP request arrives at Mycel → creates DebugThread
6. Pipeline executes → events stream to client
7. Pipeline hits breakpoint → event.stopped sent, thread pauses
8. Client inspects variables, evaluates expressions
9. Client sends debug.continue/next/stepInto → thread resumes
10. Pipeline completes → event.flowEnd sent, thread cleaned up
11. Client sends debug.detach or disconnects
```

### Connection Cleanup

When the WebSocket connection closes (normal or abnormal):
- Event stream subscription is removed
- Session is deleted from the server
- Any active threads continue executing (breakpoints are no longer checked)

---

## Protocol Reference

### Methods (IDE → Runtime)

Every method follows JSON-RPC 2.0:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "method.name",
  "params": { ... }
}
```

Responses:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": { ... }
}
```

Or error:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": { "code": -32601, "message": "unknown method: foo" }
}
```

---

#### `debug.attach`

Establishes a debug session. Must be the first method called.

**Params:**
```json
{
  "clientName": "mycel-studio 1.0"  // optional, identifies the IDE
}
```

**Result:**
```json
{
  "sessionId": "s1",
  "flows": ["create_user", "get_users", "delete_user"]
}
```

After attach, the client receives events via JSON-RPC notifications (no `id`).

---

#### `debug.detach`

Disconnects cleanly. No params needed.

**Result:**
```json
{ "ok": true }
```

---

#### `debug.setBreakpoints`

Configures breakpoints for a specific flow. Replaces all previous breakpoints for that flow.

**Params:**
```json
{
  "flow": "create_user",
  "breakpoints": [
    { "stage": "transform", "ruleIndex": -1 },
    { "stage": "transform", "ruleIndex": 0 },
    { "stage": "transform", "ruleIndex": 2, "condition": "input.email != \"\"" },
    { "stage": "validate_input", "ruleIndex": -1 },
    { "stage": "write", "ruleIndex": -1 }
  ]
}
```

**Breakpoint fields:**

| Field | Type | Description |
|---|---|---|
| `stage` | string | Pipeline stage to break on (see [Pipeline Stages](#pipeline-stages)) |
| `ruleIndex` | int | `-1` = break at stage level. `0+` = break at specific CEL rule within transform |
| `condition` | string | Optional CEL expression. Only pauses when condition evaluates to `true` |

**Result:**
```json
{
  "breakpoints": [ /* same array echoed back */ ]
}
```

**To clear breakpoints**, send an empty array:
```json
{ "flow": "create_user", "breakpoints": [] }
```

---

#### `debug.continue`

Resumes a paused thread. Execution continues until the next breakpoint or flow completion.

**Params:**
```json
{ "threadId": "t1a2b3c4d5e6f7g8" }
```

**Result:**
```json
{ "ok": true }
```

Also disables stepInto mode if it was active.

---

#### `debug.next`

Steps to the next pipeline stage. If currently inside a transform (per-rule), exits to the next stage.

**Params:**
```json
{ "threadId": "t1a2b3c4d5e6f7g8" }
```

**Result:**
```json
{ "ok": true }
```

---

#### `debug.stepInto`

Enables per-CEL-rule stepping. After sending this, the thread will pause before each CEL rule within a transform block.

**Params:**
```json
{ "threadId": "t1a2b3c4d5e6f7g8" }
```

**Result:**
```json
{ "ok": true }
```

Step-into mode stays active until `debug.continue` is called (which disables it).

---

#### `debug.evaluate`

Evaluates a CEL expression in the paused thread's current data context. The thread **must be paused**.

**Params:**
```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "expression": "lower(input.email)"
}
```

**Result:**
```json
{
  "result": "alice@example.com",
  "type": "string"
}
```

Available variables in the expression depend on the current pipeline stage. At minimum, `input` and `output` are always available. See [CEL Expression Evaluation](#cel-expression-evaluation).

**Error if not paused:**
```json
{ "code": -32602, "message": "thread not paused" }
```

---

#### `debug.variables`

Returns all variables at the current breakpoint.

**Params:**
```json
{ "threadId": "t1a2b3c4d5e6f7g8" }
```

**Result:**
```json
{
  "input": { "email": "ALICE@EXAMPLE.COM", "name": "Alice" },
  "output": { "email": "alice@example.com" },
  "enriched": { "existing_user": null },
  "steps": { "lookup": { "count": 0 } },
  "rule": {
    "index": 1,
    "target": "name",
    "expression": "input.name",
    "result": null
  }
}
```

The `rule` field is only present when paused inside a transform (per-rule breakpoint or stepInto mode). It shows the current rule that is about to execute (in `BeforeRule`) or just executed (in `AfterRule`).

---

#### `debug.threads`

Lists all active debug threads (one per in-flight request being debugged).

**No params needed.**

**Result:**
```json
{
  "threads": [
    {
      "id": "t1a2b3c4d5e6f7g8",
      "flowName": "create_user",
      "stage": "transform",
      "name": "",
      "paused": true
    },
    {
      "id": "t9h8g7f6e5d4c3b2",
      "flowName": "get_users",
      "stage": "read",
      "name": "",
      "paused": false
    }
  ]
}
```

---

#### `inspect.flows`

Lists all flows with their configuration summaries.

**No params needed.**

**Result:**
```json
[
  {
    "name": "create_user",
    "from": { "connector": "api", "operation": "POST /users" },
    "to": { "connector": "postgres", "operation": "users" },
    "hasSteps": false,
    "stepCount": 0,
    "transform": {
      "email": "lower(input.email)",
      "name": "input.name"
    },
    "response": null,
    "validate": { "input": "user", "output": "" },
    "hasCache": false,
    "hasRetry": true
  }
]
```

---

#### `inspect.flow`

Returns detailed configuration for a single flow.

**Params:**
```json
{ "name": "create_user" }
```

**Result:** Same structure as one element of `inspect.flows`.

**Error if not found:**
```json
{ "code": -32002, "message": "flow not found" }
```

---

#### `inspect.connectors`

Lists all registered connectors.

**No params needed.**

**Result:**
```json
[
  { "name": "api", "type": "rest", "driver": "" },
  { "name": "postgres", "type": "database", "driver": "postgres" },
  { "name": "redis_cache", "type": "cache", "driver": "redis" }
]
```

---

#### `inspect.types`

Lists all type schemas.

**No params needed.**

**Result:**
```json
[
  {
    "name": "user",
    "fields": [
      { "name": "email", "type": "string", "required": true },
      { "name": "name", "type": "string", "required": true },
      { "name": "age", "type": "number", "required": false }
    ]
  }
]
```

---

#### `inspect.transforms`

Lists all named (reusable) transforms.

**No params needed.**

**Result:**
```json
[
  {
    "name": "normalize_email",
    "mappings": {
      "email": "lower(trim(input.email))"
    }
  }
]
```

---

### Events (Runtime → IDE)

Events are JSON-RPC 2.0 **notifications** (no `id`, no response expected):

```json
{
  "jsonrpc": "2.0",
  "method": "event.name",
  "params": { ... }
}
```

---

#### `event.flowStart`

Sent when a request enters a flow.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "input": { "email": "ALICE@EXAMPLE.COM", "name": "Alice" }
}
```

---

#### `event.flowEnd`

Sent when a request completes a flow.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "output": { "id": 42, "email": "alice@example.com" },
  "durationUs": 3500,
  "error": ""
}
```

`durationUs` is microseconds. `error` is empty string on success.

---

#### `event.stageEnter`

Sent when a pipeline stage starts. Contains the input data going into the stage.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "stage": "transform",
  "name": "",
  "input": { "email": "ALICE@EXAMPLE.COM" }
}
```

---

#### `event.stageExit`

Sent when a pipeline stage completes. Contains the output data and timing.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "stage": "transform",
  "name": "",
  "output": { "email": "alice@example.com", "name": "Alice" },
  "durationUs": 150,
  "error": ""
}
```

---

#### `event.ruleEval`

Sent when an individual CEL rule within a transform is evaluated. Only emitted when a debug client is connected and transform hooks are active.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "stage": "transform",
  "ruleIndex": 0,
  "target": "email",
  "expression": "lower(input.email)",
  "result": "alice@example.com",
  "error": ""
}
```

---

#### `event.stopped`

Sent when a thread hits a breakpoint and pauses.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "stage": "transform",
  "name": "",
  "rule": {
    "index": 0,
    "target": "email",
    "expression": "lower(input.email)"
  },
  "reason": "stepInto"
}
```

**Reason values:**
- `"breakpoint"` — hit a stage-level breakpoint
- `"step"` — hit the next stage after a `debug.next`
- `"stepInto"` — hit the next CEL rule in stepInto mode

The `rule` field is only present for per-CEL-rule breakpoints.

---

#### `event.continued`

Sent when a paused thread is resumed.

```json
{
  "threadId": "t1a2b3c4d5e6f7g8"
}
```

---

## Data Types

### BreakpointSpec

```json
{
  "stage": "transform",
  "ruleIndex": -1,
  "condition": ""
}
```

| Field | Type | Description |
|---|---|---|
| `stage` | `string` | Pipeline stage name (see [Pipeline Stages](#pipeline-stages)) |
| `ruleIndex` | `int` | `-1` for stage-level breakpoint, `0+` for CEL rule index |
| `condition` | `string` | Optional CEL expression; only pauses when true |

### ThreadInfo

```json
{
  "id": "t1a2b3c4d5e6f7g8",
  "flowName": "create_user",
  "stage": "transform",
  "name": "",
  "paused": true
}
```

### FlowInfo

```json
{
  "name": "create_user",
  "from": { "connector": "api", "operation": "POST /users" },
  "to": { "connector": "postgres", "operation": "users" },
  "hasSteps": true,
  "stepCount": 2,
  "transform": { "email": "lower(input.email)" },
  "response": { "total": "output.count" },
  "validate": { "input": "user", "output": "" },
  "hasCache": false,
  "hasRetry": true
}
```

### ConnectorInfo

```json
{ "name": "postgres", "type": "database", "driver": "postgres" }
```

### TypeInfo

```json
{
  "name": "user",
  "fields": [
    { "name": "email", "type": "string", "required": true }
  ]
}
```

### TransformInfo

```json
{
  "name": "normalize",
  "mappings": { "email": "lower(input.email)" }
}
```

### RuleInfo

```json
{
  "index": 0,
  "target": "email",
  "expression": "lower(input.email)",
  "result": "alice@example.com"
}
```

`result` is only present after the rule has been evaluated (in `AfterRule` or when returned via `debug.variables`).

### VariablesResult

```json
{
  "input": { "email": "ALICE@EXAMPLE.COM" },
  "output": { "email": "alice@example.com" },
  "enriched": { "lookup": { "exists": false } },
  "steps": { "check": { "count": 0 } },
  "rule": { "index": 0, "target": "email", "expression": "lower(input.email)", "result": "alice@example.com" }
}
```

---

## Breakpoint System

### Stage-Level Breakpoints

Pause execution before a pipeline stage runs. Set `ruleIndex: -1`.

```json
{ "stage": "transform", "ruleIndex": -1 }
```

This uses `trace.BreakpointController` — the same interface as DAP and CLI breakpoints. The `StudioBreakpointController` checks the session's breakpoint list and pauses the `DebugThread` via channels.

### Per-CEL-Rule Breakpoints

Pause at a specific CEL rule within a transform. Set `ruleIndex` to the 0-based index.

```json
{ "stage": "transform", "ruleIndex": 0 }
```

This uses `transform.TransformHook` — a separate interface injected into the CEL rule evaluation loops. Each rule calls `BeforeRule()` before evaluation and `AfterRule()` after. The `StudioTransformHook` checks for rule-level breakpoints or stepInto mode.

### Conditional Breakpoints

Add a `condition` CEL expression to any breakpoint:

```json
{ "stage": "transform", "ruleIndex": -1, "condition": "input.email.endsWith(\"@example.com\")" }
```

The condition is evaluated against the current activation (input, output, enriched, step variables). The breakpoint only triggers when the condition evaluates to `true`.

### How It Works Internally

```
Request arrives at FlowHandler.HandleRequest()
  │
  ├── Check: DebugServer != nil && DebugServer.HasClients()
  │     └── No → skip all debug logic (zero cost)
  │
  ├── Get active session
  ├── Generate threadID (random 16-hex string)
  ├── Create DebugThread
  ├── Register thread on session
  │
  ├── Create StudioCollector (trace.Collector)
  ├── Create trace.Context with:
  │     ├── Collector = StudioCollector
  │     └── Breakpoint = StudioBreakpointController (if breakpoints set)
  │
  ├── Create StudioTransformHook (if breakpoints set)
  ├── Attach hook to context via transform.WithTransformHook()
  │
  ├── BroadcastFlowStart
  │
  ├── Execute pipeline...
  │     │
  │     ├── trace.RecordStage() → checks BreakpointController.ShouldBreak()
  │     │     └── If true → BreakpointController.Pause() → blocks on channel
  │     │           └── IDE sends debug.continue → resumes channel
  │     │
  │     ├── CEL Transform loop (for each rule):
  │     │     ├── HookFromContext(ctx) → gets TransformHook
  │     │     ├── hook.BeforeRule() → may pause (stepInto or rule breakpoint)
  │     │     ├── prog.Eval(activation)
  │     │     └── hook.AfterRule() → broadcasts ruleEval event
  │     │
  │     └── Continue through remaining stages...
  │
  ├── BroadcastFlowEnd
  └── Unregister thread
```

---

## Variable Inspection

When paused at any breakpoint, call `debug.variables` to get the current data context:

| Variable | Available At | Description |
|---|---|---|
| `input` | All stages | The original request data |
| `output` | Transform stages | Data built up during transform (grows rule by rule) |
| `enriched` | After enrich stage | Data from external lookups |
| `steps` | After step execution | Results from intermediate connector calls |
| `rule` | Per-rule breakpoints only | Current CEL rule (index, target, expression, result) |

The activation map (`input`, `output`, `enriched`, `step`) is the **same map** that CEL expressions evaluate against. So `debug.evaluate` with expression `input.email` returns exactly what `input.email` returns in a CEL rule.

---

## CEL Expression Evaluation

`debug.evaluate` compiles and runs a CEL expression using the paused thread's activation record. This is the same CEL environment used by the transform engine, with all built-in functions available:

**String functions**: `lower()`, `upper()`, `trim()`, `split()`, `join()`, `replace()`
**Array functions**: `first()`, `last()`, `unique()`, `reverse()`, `flatten()`, `pluck()`, `sort_by()`, `sum()`, `avg()`, `min_val()`, `max_val()`
**Type functions**: `type()`, `has_field()`, `field_requested()`
**ID functions**: `uuid()`, `now()`, `now_unix()`
**Null-handling**: `default()`, `coalesce()`

**Examples:**
```
input.email                          → "ALICE@EXAMPLE.COM"
lower(input.email)                   → "alice@example.com"
size(input.items)                    → 3
output.email.contains("@")          → true
input.age > 18                      → true
type(input.score)                   → "double"
```

---

## Event Streaming

Even without breakpoints, the protocol streams pipeline events to all connected clients. This enables **live pipeline visualization** — the IDE can show requests flowing through stages in real time.

### EventStream

`EventStream` is a thread-safe pub/sub system:
- Each session subscribes to a buffered channel (256 slots)
- `Broadcast()` is non-blocking — drops events for slow clients
- `HasSubscribers()` check prevents work when no one is listening

### StudioCollector

`StudioCollector` implements `trace.Collector` (the same interface used by `MemoryCollector` for `mycel trace` and `LogCollector` for `--verbose-flow`):

- `Record(event)` → converts trace events to JSON-RPC notifications and broadcasts
- Events with `Duration` or `Output` → `event.stageExit`
- Events with `Input` but no output → `event.stageEnter`
- Also provides `BroadcastFlowStart()`, `BroadcastFlowEnd()`, `BroadcastRuleEval()`

---

## Runtime Inspection

The `RuntimeInspector` interface provides read-only access to the running service's configuration:

```go
type RuntimeInspector interface {
    ListFlows() []string
    GetFlowConfig(name string) (*flow.Config, bool)
    ListConnectors() []string
    GetConnectorConfig(name string) (*connector.Config, bool)
    ListTypes() []*validate.TypeSchema
    ListTransforms() []*transform.Config
    GetCELTransformer() *transform.CELTransformer
}
```

This is implemented by `Runtime`. The debug package never imports `runtime` — it depends only on the interface, keeping the packages decoupled.

The `inspect.*` methods return IDE-friendly summaries (not the full internal configs). The builder functions in `inspector.go` handle the conversion.

---

## Threading Model

Each concurrent HTTP request creates one `DebugThread`:

```
Thread ID: "t" + 16 random hex chars (e.g., "t1a2b3c4d5e6f7g8")

DebugThread {
    ID       string
    FlowName string

    pauseCh  chan struct{}    // signals pause to anyone waiting
    resumeCh chan resumeAction // receives resume command

    paused   atomic.Bool     // current pause state
    stage    trace.Stage     // current pipeline stage
    name     string          // sub-name (step name, enrichment name)
    activation map[string]interface{}  // CEL variables at breakpoint
    ruleInfo *RuleInfo       // current rule if in transform

    stepInto atomic.Bool     // step-into mode flag
}
```

### Pause/Resume Flow

```
1. BreakpointController.Pause() or TransformHook.BeforeRule() is called
2. Thread state is updated (stage, activation, ruleInfo)
3. event.stopped notification is broadcast
4. thread.Pause() is called:
   a. paused.Store(true)
   b. signal pauseCh (non-blocking)
   c. block on resumeCh ← waits here
5. IDE sends debug.continue/next/stepInto
6. Server calls thread.Resume(action)
   a. action is sent to resumeCh
7. thread.Pause() returns the action
8. paused.Store(false)
9. event.continued notification is broadcast
10. Execution resumes based on action:
    - actionContinue: disable stepInto, continue to next breakpoint
    - actionNext: disable stepInto, continue to next stage
    - actionStepInto: enable stepInto, continue to next rule
    - actionAbort: return false, abort execution
```

### Resume Actions

| Action | Constant | Effect |
|---|---|---|
| Continue | `actionContinue` | Resume, disable stepInto, run until next breakpoint |
| Next | `actionNext` | Resume, disable stepInto, run until next stage |
| Step Into | `actionStepInto` | Resume, enable stepInto, pause at next CEL rule |
| Abort | `actionAbort` | Stop execution, return error |

---

## Pipeline Stages

These are the `trace.Stage` constants that can be used in breakpoints:

| Stage | Constant | Description |
|---|---|---|
| `input` | `StageInput` | Raw request data received |
| `sanitize` | `StageSanitize` | Input sanitization (XSS, injection prevention) |
| `filter` | `StageFilter` | `from.filter` CEL condition evaluation |
| `dedupe` | `StageDedupe` | Deduplication check |
| `validate_input` | `StageValidateIn` | Input type validation |
| `enrich` | `StageEnrich` | Data enrichment from external sources |
| `transform` | `StageTransform` | CEL transformation rules |
| `step` | `StageStep` | Intermediate connector calls |
| `validate_output` | `StageValidateOut` | Output type validation |
| `read` | `StageRead` | Read from destination connector |
| `write` | `StageWrite` | Write to destination connector |
| `cache_hit` | `StageCacheHit` | Cache hit (data returned from cache) |
| `cache_miss` | `StageCacheMiss` | Cache miss (data fetched from source) |

### Pipeline Order

A typical flow executes stages in this order:

```
input → sanitize → filter → dedupe → validate_input → enrich → transform → step(s) → validate_output → write/read → response
```

Not all stages are present in every flow. Stages are skipped if not configured (e.g., no `validate` block → skip `validate_input`).

---

## Implementation Details

### Package Structure

```
internal/debug/
├── protocol.go     — JSON-RPC types (Request, Response, Notification, all params/results/events)
├── server.go       — WebSocket handler, mounted on admin mux at /debug
├── session.go      — Per-client state: breakpoints, threads, DebugThread
├── controller.go   — StudioBreakpointController + StudioTransformHook
├── inspector.go    — RuntimeInspector interface, builder functions
├── stream.go       — EventStream fan-out, StudioCollector
└── debug_test.go   — 29 tests
```

### Server Implementation

```go
// Server struct
type Server struct {
    inspector RuntimeInspector     // read-only runtime access
    stream    *EventStream         // fan-out events
    logger    *slog.Logger         // structured logging
    mu        sync.Mutex           // protects sessions map
    sessions  map[string]*Session  // sessionID → Session
    nextID    atomic.Uint64        // session ID counter
    upgrader  websocket.Upgrader   // gorilla/websocket
}

// Key methods:
NewServer(inspector, logger) *Server
Stream() *EventStream
GetSession(id) (*Session, bool)
ActiveSession() *Session              // returns first session (single-client shortcut)
HasClients() bool
RegisterHandlers(mux *http.ServeMux)  // mounts /debug
handleWebSocket(w, r)                 // upgrade + read loop
handleMethod(session, req) *Response  // dispatch to handlers
```

The WebSocket handler:
1. Upgrades HTTP → WebSocket via gorilla/websocket
2. Waits for `debug.attach` to create session
3. Subscribes session to EventStream
4. Starts goroutine for event forwarding (channel → WebSocket writes)
5. Enters read loop: read message → parse JSON-RPC → dispatch → write response
6. On disconnect: unsubscribe, delete session, log

Write operations are serialized via a `writeMu` mutex to prevent concurrent WebSocket writes.

### Session Management

```go
type Session struct {
    ID         string
    ClientName string
    mu          sync.Mutex
    breakpoints map[string][]BreakpointSpec  // flow name → breakpoints
    threads     map[string]*DebugThread      // thread ID → thread
}

// Key methods:
SetBreakpoints(flowName, specs)
GetBreakpoints(flowName) []BreakpointSpec
HasBreakpoints(flowName) bool
AllBreakpointFlows() []string
RegisterThread(thread)
UnregisterThread(id)
GetThread(id) (*DebugThread, bool)
ListThreads() []*DebugThread
```

### EventStream and StudioCollector

```go
type EventStream struct {
    mu          sync.RWMutex
    subscribers map[string]chan *Notification  // sessionID → buffered channel (256)
}

// Broadcast is non-blocking: drops for slow clients
func (es *EventStream) Broadcast(n *Notification) {
    es.mu.RLock()
    defer es.mu.RUnlock()
    for _, ch := range es.subscribers {
        select {
        case ch <- n:
        default: // drop
        }
    }
}
```

```go
type StudioCollector struct {
    stream   *EventStream
    threadID string
    flowName string
    mu       sync.Mutex
    events   []trace.Event
}

// Implements trace.Collector interface
func (c *StudioCollector) Record(event trace.Event) { ... }
func (c *StudioCollector) Events() []trace.Event { ... }

// Convenience broadcasters
func (c *StudioCollector) BroadcastFlowStart(input interface{}) { ... }
func (c *StudioCollector) BroadcastFlowEnd(output interface{}, duration time.Duration, err error) { ... }
func (c *StudioCollector) BroadcastRuleEval(stage, index, target, expression, result, err) { ... }
```

### StudioBreakpointController

Implements `trace.BreakpointController`:

```go
type StudioBreakpointController struct {
    session   *Session
    thread    *DebugThread
    stream    *EventStream
    collector *StudioCollector
}

func (c *StudioBreakpointController) ShouldBreak(stage trace.Stage) bool {
    // Check session breakpoints for this flow
    // Only matches stage-level breakpoints (ruleIndex < 0)
}

func (c *StudioBreakpointController) Pause(stage, name, data) bool {
    // 1. Build activation from data
    // 2. Update thread state
    // 3. Check conditional breakpoints
    // 4. Broadcast event.stopped
    // 5. thread.Pause() → blocks until resumed
    // 6. Broadcast event.continued
    // 7. Return true (continue) or false (abort)
}
```

This is the **same interface** that DAP uses (`DAPBreakpoint`) and CLI uses (`Breakpoint`). All three can coexist because they're injected per-request via `trace.Context.Breakpoint`.

### StudioTransformHook

Implements `transform.TransformHook`:

```go
type StudioTransformHook struct {
    session   *Session
    thread    *DebugThread
    stream    *EventStream
    collector *StudioCollector
    flowName  string
    stage     trace.Stage
}

func (h *StudioTransformHook) BeforeRule(ctx, index, rule, activation) bool {
    // 1. Check: stepInto mode OR rule-level breakpoint at this index
    // 2. If neither → return true (continue)
    // 3. Check conditional breakpoints
    // 4. Update thread state + ruleInfo
    // 5. Broadcast event.stopped with rule info
    // 6. thread.Pause() → blocks
    // 7. Broadcast event.continued
    // 8. Handle action (abort → false, continue → disable stepInto, etc.)
}

func (h *StudioTransformHook) AfterRule(ctx, index, rule, result, err) {
    // 1. Broadcast event.ruleEval via collector
    // 2. Update thread ruleInfo with result
}
```

### TransformHook Interface

In `internal/transform/hook.go`:

```go
type TransformHook interface {
    BeforeRule(ctx context.Context, index int, rule Rule, activation map[string]interface{}) bool
    AfterRule(ctx context.Context, index int, rule Rule, result interface{}, err error)
}

// Context key for hook injection
type transformHookKey struct{}

func WithTransformHook(ctx context.Context, hook TransformHook) context.Context {
    return context.WithValue(ctx, transformHookKey{}, hook)
}

func HookFromContext(ctx context.Context) TransformHook {
    hook, _ := ctx.Value(transformHookKey{}).(TransformHook)
    return hook
}
```

### Hook Injection in CEL Transform Loops

All three transform methods (`Transform`, `TransformResponse`, `TransformWithContext`) follow the same pattern:

```go
func (t *CELTransformer) Transform(ctx context.Context, input map[string]interface{}, rules []Rule) (map[string]interface{}, error) {
    output := make(map[string]interface{})
    activation := map[string]interface{}{
        "input":  input,
        "output": output,
        "ctx":    make(map[string]interface{}),
    }

    hook := HookFromContext(ctx)  // ← nil check (~10ns)

    for i, rule := range rules {
        // BeforeRule hook
        if hook != nil {
            if !hook.BeforeRule(ctx, i, rule, activation) {
                return nil, fmt.Errorf("transform aborted at rule %d ('%s')", i, rule.Target)
            }
        }

        prog, err := t.Compile(rule.Expression)
        if err != nil {
            if hook != nil {
                hook.AfterRule(ctx, i, rule, nil, err)
            }
            return nil, fmt.Errorf("failed to compile expression for '%s': %w", rule.Target, err)
        }

        result, _, err := prog.Eval(activation)
        if err != nil {
            if hook != nil {
                hook.AfterRule(ctx, i, rule, nil, err)
            }
            return nil, fmt.Errorf("failed to evaluate expression for '%s': %w", rule.Target, err)
        }

        value := result.Value()

        // AfterRule hook
        if hook != nil {
            hook.AfterRule(ctx, i, rule, value, nil)
        }

        setNestedValue(output, rule.Target, value)
        activation["output"] = output
    }

    return output, nil
}
```

### RuntimeInspector Interface

```go
type RuntimeInspector interface {
    ListFlows() []string
    GetFlowConfig(name string) (*flow.Config, bool)
    ListConnectors() []string
    GetConnectorConfig(name string) (*connector.Config, bool)
    ListTypes() []*validate.TypeSchema
    ListTransforms() []*transform.Config
    GetCELTransformer() *transform.CELTransformer
}
```

Implemented by `Runtime` in `internal/runtime/runtime.go`:

```go
func (r *Runtime) GetFlowConfig(name string) (*flow.Config, bool) {
    handler, ok := r.flows.Get(name)
    if !ok { return nil, false }
    return handler.Config, true
}

func (r *Runtime) ListConnectors() []string {
    return r.connectors.List()
}

func (r *Runtime) GetConnectorConfig(name string) (*connector.Config, bool) {
    for _, cfg := range r.config.Connectors {
        if cfg.Name == name { return cfg, true }
    }
    return nil, false
}

func (r *Runtime) ListTypes() []*validate.TypeSchema {
    schemas := make([]*validate.TypeSchema, 0, len(r.types))
    for _, schema := range r.types { schemas = append(schemas, schema) }
    return schemas
}

func (r *Runtime) ListTransforms() []*transform.Config {
    configs := make([]*transform.Config, 0, len(r.transforms))
    for _, cfg := range r.transforms { configs = append(configs, cfg) }
    return configs
}

func (r *Runtime) GetCELTransformer() *transform.CELTransformer {
    // Return first flow handler's transformer, or create new one
    for _, name := range r.flows.List() {
        if handler, ok := r.flows.Get(name); ok && handler.Transformer != nil {
            return handler.Transformer
        }
    }
    t, _ := transform.NewCELTransformer()
    return t
}
```

### Runtime Integration

In `runtime.go` `New()`:

```go
// Created early so flow handlers can reference it
r.debugServer = debug.NewServer(r, opts.Logger)
```

In `startAdminServer()`:

```go
mux := http.NewServeMux()
r.health.RegisterHandlers(mux)
if r.metrics != nil {
    mux.Handle("/metrics", r.metrics.Handler())
}
r.registerWorkflowEndpoints(mux)
r.debugServer.RegisterHandlers(mux)  // ← mounts /debug
```

The admin server always starts (port 9090 default), regardless of REST connector presence.

### Flow Handler Integration

In `flow_registry.go`, `FlowHandler` struct has:

```go
type FlowHandler struct {
    // ... (30+ fields)
    DebugServer *debug.Server
}
```

Set when handler is created in `registerFlows()`:

```go
handler := &FlowHandler{
    // ...
    DebugServer: r.debugServer,
}
```

In `HandleRequest()`, debug context is injected:

```go
var debugThread *debug.DebugThread
var debugCollector *debug.StudioCollector
if h.DebugServer != nil && h.DebugServer.HasClients() && !trace.IsTracing(ctx) {
    if session := h.DebugServer.ActiveSession(); session != nil {
        threadID := generateThreadID()
        debugThread = debug.NewDebugThread(threadID, h.Config.Name)
        session.RegisterThread(debugThread)
        defer session.UnregisterThread(threadID)

        stream := h.DebugServer.Stream()
        debugCollector = debug.NewStudioCollector(stream, threadID, h.Config.Name)

        tc := &trace.Context{
            FlowName:  h.Config.Name,
            Collector: debugCollector,
        }

        if session.HasBreakpoints(h.Config.Name) {
            tc.Breakpoint = debug.NewStudioBreakpointController(session, debugThread, stream, debugCollector)
        }

        ctx = trace.WithTrace(ctx, tc)

        if session.HasBreakpoints(h.Config.Name) {
            hook := debug.NewStudioTransformHook(session, debugThread, stream, debugCollector, h.Config.Name, trace.StageTransform)
            ctx = transform.WithTransformHook(ctx, hook)
        }

        debugCollector.BroadcastFlowStart(input)
    }
}

// ... pipeline executes ...

defer func() {
    if debugCollector != nil {
        debugCollector.BroadcastFlowEnd(result, time.Since(start), err)
    }
    // ... logging ...
}()
```

---

## Zero-Cost Design

When no debug client is connected, the overhead is:

1. **`h.DebugServer != nil`** — one pointer comparison per request (~1ns)
2. **`h.DebugServer.HasClients()`** — one mutex RLock + map length check (~5ns)
3. **Total per request: ~6ns** — negligible vs typical request latency (µs-ms)

When a client IS connected but no breakpoints are set:
- `StudioCollector` records events (~µs per stage)
- No pausing, no per-rule hooks

When breakpoints ARE set:
- `BreakpointController.ShouldBreak()` checked per stage (~ns)
- `TransformHook.BeforeRule()/AfterRule()` called per CEL rule (~ns when not breaking)
- Pause/resume only blocks the specific thread, not other requests

---

## DAP Coexistence

| Feature | DAP (`internal/dap/`) | Studio (`internal/debug/`) |
|---|---|---|
| Transport | TCP | WebSocket |
| Protocol | Debug Adapter Protocol | JSON-RPC 2.0 |
| Lifecycle | One-shot (`mycel trace --dap=4711`) | Long-lived (admin server) |
| Threading | Single thread | Multi-thread |
| Granularity | Stage-level only | Stage + per-CEL-rule |
| Initiated by | CLI command | IDE connects anytime |
| Implements | `trace.BreakpointController` | `trace.BreakpointController` + `transform.TransformHook` |

Both can be active simultaneously. They use the same `trace.Context.Breakpoint` hook point but different implementations. DAP creates its own trace context in the `mycel trace` command; Studio creates its in `HandleRequest()`.

---

## Complete Example Session

### 1. Connect and Inspect

```json
→ {"jsonrpc":"2.0","id":1,"method":"debug.attach","params":{"clientName":"mycel-studio"}}
← {"jsonrpc":"2.0","id":1,"result":{"sessionId":"s1","flows":["create_user","get_users"]}}

→ {"jsonrpc":"2.0","id":2,"method":"inspect.flows"}
← {"jsonrpc":"2.0","id":2,"result":[{"name":"create_user","from":{"connector":"api","operation":"POST /users"},"to":{"connector":"postgres","operation":"users"},"hasSteps":false,"stepCount":0,"transform":{"email":"lower(input.email)","name":"input.name"},"response":null,"validate":null,"hasCache":false,"hasRetry":false},{"name":"get_users","from":{"connector":"api","operation":"GET /users"},"to":{"connector":"postgres","operation":"users"},"hasSteps":false,"stepCount":0,"transform":null,"response":null,"validate":null,"hasCache":false,"hasRetry":false}]}
```

### 2. Set Breakpoints

```json
→ {"jsonrpc":"2.0","id":3,"method":"debug.setBreakpoints","params":{"flow":"create_user","breakpoints":[{"stage":"transform","ruleIndex":-1},{"stage":"transform","ruleIndex":0}]}}
← {"jsonrpc":"2.0","id":3,"result":{"breakpoints":[{"stage":"transform","ruleIndex":-1},{"stage":"transform","ruleIndex":0}]}}
```

### 3. Trigger a Request (external)

```bash
curl -X POST http://localhost:8080/users -d '{"email":"ALICE@EXAMPLE.COM","name":"Alice"}'
```

### 4. Receive Events

```json
← {"jsonrpc":"2.0","method":"event.flowStart","params":{"threadId":"t1a2b3c4","flowName":"create_user","input":{"email":"ALICE@EXAMPLE.COM","name":"Alice"}}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"sanitize","output":{"email":"ALICE@EXAMPLE.COM","name":"Alice"},"durationUs":50}}
← {"jsonrpc":"2.0","method":"event.stopped","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","reason":"breakpoint"}}
```

### 5. Inspect Variables

```json
→ {"jsonrpc":"2.0","id":4,"method":"debug.variables","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","id":4,"result":{"input":{"email":"ALICE@EXAMPLE.COM","name":"Alice"},"output":{}}}
```

### 6. Step Into Transform

```json
→ {"jsonrpc":"2.0","id":5,"method":"debug.stepInto","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","id":5,"result":{"ok":true}}
← {"jsonrpc":"2.0","method":"event.continued","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","method":"event.stopped","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","rule":{"index":0,"target":"email","expression":"lower(input.email)"},"reason":"stepInto"}}
```

### 7. Evaluate Expression

```json
→ {"jsonrpc":"2.0","id":6,"method":"debug.evaluate","params":{"threadId":"t1a2b3c4","expression":"lower(input.email)"}}
← {"jsonrpc":"2.0","id":6,"result":{"result":"alice@example.com","type":"string"}}
```

### 8. Continue

```json
→ {"jsonrpc":"2.0","id":7,"method":"debug.continue","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","id":7,"result":{"ok":true}}
← {"jsonrpc":"2.0","method":"event.continued","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","method":"event.ruleEval","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","ruleIndex":0,"target":"email","expression":"lower(input.email)","result":"alice@example.com"}}
← {"jsonrpc":"2.0","method":"event.ruleEval","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","ruleIndex":1,"target":"name","expression":"input.name","result":"Alice"}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","output":{"email":"alice@example.com","name":"Alice"},"durationUs":200}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"write","output":{"id":42,"email":"alice@example.com","name":"Alice"},"durationUs":5000}}
← {"jsonrpc":"2.0","method":"event.flowEnd","params":{"threadId":"t1a2b3c4","flowName":"create_user","output":{"id":42,"email":"alice@example.com","name":"Alice"},"durationUs":6000}}
```

### 9. Disconnect

```json
→ {"jsonrpc":"2.0","id":8,"method":"debug.detach"}
← {"jsonrpc":"2.0","id":8,"result":{"ok":true}}
```

---

## Error Codes

| Code | Name | Description |
|---|---|---|
| -32700 | Parse Error | Invalid JSON received |
| -32600 | Invalid Request | Missing `jsonrpc: "2.0"` |
| -32601 | Method Not Found | Unknown method name |
| -32602 | Invalid Params | Missing or invalid parameters |
| -32603 | Internal Error | Server-side error |
| -32000 | Session Not Found | Method called before `debug.attach` |
| -32001 | Thread Not Found | ThreadID doesn't exist |
| -32002 | Flow Not Found | Flow name doesn't exist |
| -32003 | Eval Error | CEL expression evaluation failed |

---

## Testing

The test suite (`internal/debug/debug_test.go`) contains **29 tests** covering:

| Test | What it verifies |
|---|---|
| `TestAttachDetach` | Session creation and cleanup |
| `TestMethodWithoutAttach` | Error when calling methods before attach |
| `TestInspectFlows` | Flow listing via inspect.flows |
| `TestInspectFlowDetail` | Single flow detail via inspect.flow |
| `TestInspectFlowNotFound` | Error for nonexistent flow |
| `TestInspectConnectors` | Connector listing |
| `TestInspectTypes` | Type schema listing |
| `TestInspectTransforms` | Named transform listing |
| `TestSetBreakpoints` | Breakpoint storage and retrieval |
| `TestThreadsEmpty` | Empty thread list |
| `TestUnknownMethod` | Error for unknown methods |
| `TestEventStreaming` | Event broadcast to subscribers |
| `TestStudioCollector` | Collector → EventStream pipeline |
| `TestStudioCollectorFlowEvents` | Flow start/end event broadcasting |
| `TestDebugThreadPauseResume` | Thread pause/resume via channels |
| `TestDebugThreadVariables` | Variable inspection at breakpoint |
| `TestDebugThreadEvaluateCEL` | CEL evaluation in thread context |
| `TestStudioBreakpointController` | Full breakpoint flow with events |
| `TestTransformHookIntegration` | Hook before/after calls in order |
| `TestTransformHookAbort` | Hook abort stops transform |
| `TestTransformHookNilCost` | No hook = no overhead |
| `TestTransformResponseHook` | Hook in TransformResponse method |
| `TestTransformWithContextHook` | Hook in TransformWithContext method |
| `TestSessionBreakpointManagement` | Set/clear/query breakpoints |
| `TestSessionThreadManagement` | Register/unregister/list threads |
| `TestEventStreamBroadcast` | Multi-subscriber broadcast |
| `TestHasClients` | Client detection after attach |
| `TestContinueThreadNotFound` | Error for nonexistent thread |
| `TestEvaluateNotPaused` | Error for evaluate on non-paused thread |

Run with:
```bash
go test ./internal/debug/ -v
```

All 29 pass. The full test suite (`go test ./...`) also passes — 65 packages, zero regressions.
