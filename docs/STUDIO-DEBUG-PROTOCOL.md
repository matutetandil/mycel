# Mycel Studio Debug Protocol

Complete specification and implementation guide for the Mycel Studio Debug Protocol. This document contains **everything** needed to build a client (IDE, CLI tool, or any WebSocket-capable application) that connects to a running Mycel service and debugs it in real time.

---

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Transport](#transport)
- [Session Lifecycle](#session-lifecycle)
  - [Setup Handshake](#setup-handshake)
  - [Event-Driven Flow Debugging (Manual Consume)](#event-driven-flow-debugging-manual-consume)
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
- [Debug Throttling and Manual Consume](#debug-throttling-and-manual-consume)
  - [Automatic Throttling (Push-Based)](#automatic-throttling-push-based)
  - [Manual Consume (Queue-Based)](#manual-consume-queue-based)
  - [Start Suspended Mode](#start-suspended-mode)
- [Implementation Details](#implementation-details)
  - [Package Structure](#package-structure)
  - [Server Implementation](#server-implementation)
  - [Session Management](#session-management)
  - [EventStream and StudioCollector](#eventstream-and-studiocollector)
  - [StudioBreakpointController](#studiobreakpointcontroller)
  - [StudioTransformHook](#studiotransformhook)
  - [TransformHook Interface](#transformhook-interface)
  - [RuntimeInspector Interface](#runtimeinspector-interface)
  - [DebugConsumer Interface](#debugconsumer-interface)
  - [Runtime Integration](#runtime-integration)
  - [Flow Handler Integration](#flow-handler-integration)
- [Zero-Cost Design](#zero-cost-design)
- [DAP Coexistence](#dap-coexistence)
- [Complete Example Session](#complete-example-session)
  - [REST Flow (Request-Response)](#rest-flow-request-response)
  - [RabbitMQ Flow (Event-Driven with Manual Consume)](#rabbitmq-flow-event-driven-with-manual-consume)
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
4. Client sends debug.setBreakpoints to configure breakpoints (repeat per flow)
5. Client sends debug.ready → Mycel starts suspended connectors, returns source capabilities
6. For event-driven flows: client sends debug.consume to pull one message at a time
7. HTTP/MQ request arrives → creates DebugThread
8. Pipeline executes → events stream to client
9. Pipeline hits breakpoint → event.stopped sent, thread pauses
10. Client inspects variables, evaluates expressions
11. Client sends debug.continue/next/stepInto → thread resumes
12. Pipeline completes → event.flowEnd sent, thread cleaned up
13. Client sends debug.detach or disconnects
```

### Setup Handshake

Before any messages are consumed, the client **must** complete a setup handshake. This eliminates the race condition where messages arrive before breakpoints are configured.

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
```

The `debug.ready` response tells the client what event-driven connectors are available and whether they support manual consume (see [debug.ready](#debugready)).

### Event-Driven Flow Debugging (Manual Consume)

For queue-based connectors (RabbitMQ, Kafka), messages are **not** consumed automatically. The client controls when each message is pulled:

```
Studio                           Mycel
  │    (after handshake)           │
  │                                │
  ├─── debug.consume ─────────────►│  Pull ONE message from "rabbit" queue
  │                                │  Message enters flow pipeline...
  │◄── event.flowStart ───────────┤
  │◄── event.stageExit ───────────┤  (sanitize, validate, etc.)
  │◄── event.stopped ─────────────┤  Breakpoint hit!
  │                                │
  ├─── debug.variables ───────────►│  Inspect data
  │◄── { input, output, ... } ────┤
  │                                │
  ├─── debug.continue ────────────►│  Resume
  │◄── event.continued ───────────┤
  │◄── event.flowEnd ─────────────┤  Message fully processed
  │◄── { ok: true } ──────────────┤  debug.consume response returns
  │                                │
  ├─── debug.consume ─────────────►│  Pull next message (repeat)
  │    ...                         │
```

This gives the IDE full control over message consumption, making event-driven flow debugging as manageable as REST debugging.

### Connection Cleanup

When the WebSocket connection closes (normal or abnormal):
- Event stream subscription is removed
- Session is deleted from the server
- `ready` state is reset (last client disconnects → `ready = false`)
- Debug throttling is disabled on all connectors
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

#### `debug.ready`

Signals that the client has finished setting breakpoints and is ready to debug. This triggers Mycel to start suspended event-driven connectors (connect to brokers, set up topology). **Must be called after all `debug.setBreakpoints` calls.**

**No params needed.**

**Result:**
```json
{
  "ok": true,
  "sources": [
    {
      "connector": "rabbit",
      "type": "rabbitmq",
      "source": "orders.q",
      "manualConsume": true
    },
    {
      "connector": "kafka_events",
      "type": "kafka",
      "source": "events.orders,events.users",
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
```

**`sources` field** lists all event-driven connectors with their capabilities:

| Field | Type | Description |
|---|---|---|
| `connector` | `string` | Connector name as defined in HCL |
| `type` | `string` | Connector type: `rabbitmq`, `kafka`, `mqtt`, `redis-pubsub`, `cdc`, `file`, `websocket` |
| `source` | `string` | Source identifier (queue name, topic list, MQTT pattern, etc.) |
| `manualConsume` | `bool` | `true` if the connector supports `debug.consume`. `false` for push-based connectors |

**`manualConsume: true`** (RabbitMQ, Kafka): No messages are consumed until the client sends `debug.consume`. The client has full control over when each message is pulled from the queue.

**`manualConsume: false`** (MQTT, Redis Pub/Sub, CDC, File watch, WebSocket): Messages arrive in real time but are throttled to one at a time via automatic debug throttling. The client cannot control when messages arrive — they are pushed by the external system.

If there are no event-driven connectors (e.g., only REST flows), `sources` will be an empty array.

---

#### `debug.consume`

Fetches and processes exactly **one message** from a queue-based connector. The request **blocks** until the message is fully processed through the entire flow pipeline (including hitting breakpoints, transforms, writes, etc.).

Only works for connectors with `manualConsume: true` in the `debug.ready` response.

**Params:**
```json
{
  "connector": "rabbit"
}
```

| Field | Type | Description |
|---|---|---|
| `connector` | `string` | The connector name to consume from (must match a `sources[].connector` value) |

**Result (on success):**
```json
{ "ok": true }
```

**Error if connector doesn't support manual consume:**
```json
{ "code": -32603, "message": "connector \"mqtt_sensors\" does not support manual consume" }
```

**Error if connector not found:**
```json
{ "code": -32603, "message": "connector \"nonexistent\" not found" }
```

**How it works per connector type:**

- **RabbitMQ**: Uses AMQP `Basic.Get` (pull one message). If the queue is empty, polls every 100ms until a message arrives or the connection is closed. After the message is processed through the flow pipeline, it is ACKed.
- **Kafka**: Uses `FetchMessage()` to pull one message from the consumer group. After processing, the offset is committed via `CommitMessages()`.

**Important**: `debug.consume` is a blocking call. While it's waiting for a message or processing one (including time spent paused at breakpoints), the WebSocket connection remains active and events (`event.stopped`, `event.stageExit`, etc.) continue flowing. The response only arrives after the message is fully processed.

**Typical IDE integration**: Show a "Consume Next Message" button. When clicked, send `debug.consume`. The button is disabled while the call is pending. Events arriving during processing update the IDE's debug panels (variables, call stack, etc.). When the response arrives, re-enable the button.

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

### ReadyResult

```json
{
  "ok": true,
  "sources": [
    { "connector": "rabbit", "type": "rabbitmq", "source": "orders.q", "manualConsume": true },
    { "connector": "mqtt", "type": "mqtt", "source": "sensors/#", "manualConsume": false }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `ok` | `bool` | Always `true` on success |
| `sources` | `SourceCapability[]` | Event-driven connectors and their capabilities |

### SourceCapability

```json
{ "connector": "rabbit", "type": "rabbitmq", "source": "orders.q", "manualConsume": true }
```

| Field | Type | Description |
|---|---|---|
| `connector` | `string` | Connector name from HCL |
| `type` | `string` | Connector type (`rabbitmq`, `kafka`, `mqtt`, `redis-pubsub`, `cdc`, `file`, `websocket`) |
| `source` | `string` | Source identifier (queue name, topic, pattern) |
| `manualConsume` | `bool` | Whether `debug.consume` is supported for this connector |

### ConsumeParams

```json
{ "connector": "rabbit" }
```

| Field | Type | Description |
|---|---|---|
| `connector` | `string` | Connector name to consume one message from |

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
  ├── Check: DebugServer != nil && DebugServer.HasClients() && !trace.IsTracing(ctx)
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
  │     └── Breakpoint = StudioBreakpointController (ALWAYS when debugger connected)
  │
  ├── Create StudioTransformHook (ALWAYS when debugger connected)
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

**Note**: Controller and hook are always attached (not gated on `HasBreakpoints`). This ensures breakpoints work even if set after the first request arrives. The `ShouldBreak()` check is dynamic — it queries the session's current breakpoint list on every call.

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
| `accept` | `StageAccept` | `accept` gate — business-level condition |
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
input → sanitize → filter → accept → dedupe → validate_input → enrich → transform → step(s) → validate_output → write/read → response
```

Not all stages are present in every flow. Stages are skipped if not configured (e.g., no `validate` block → skip `validate_input`).

---

## Debug Throttling and Manual Consume

When debugging event-driven flows, Mycel provides two mechanisms to prevent message floods from interfering with debugging:

### Automatic Throttling (Push-Based)

For **push-based** connectors where messages arrive in real time and cannot be "pulled on demand":

| Connector | Mechanism |
|---|---|
| Redis Pub/Sub | `DebugGate` serializes message processing |
| MQTT | `DebugGate` serializes all topic callbacks |
| CDC | `DebugGate` serializes database change events |
| File watch | `DebugGate` serializes file events |
| WebSocket | `DebugGate` serializes incoming client messages |

Throttling is enabled automatically when a debug client connects (`debug.attach`) and disabled when it disconnects. Messages still arrive in real time, but only one is processed at a time.

### Manual Consume (Queue-Based)

For **queue-based** connectors where messages are persistent and can be pulled on demand:

| Connector | Pull Mechanism |
|---|---|
| RabbitMQ | AMQP `Basic.Get` (one message at a time, polling if queue empty) |
| Kafka | `FetchMessage()` + `CommitMessages()` (one message, manual offset commit) |

When `debug.ready` is sent and these connectors are in suspend mode:
1. Mycel connects to the broker and sets up topology (exchanges, queues, bindings)
2. **No consumer loop is started** — no `Basic.Consume`, no consume goroutines
3. Messages stay in the queue until the IDE sends `debug.consume`
4. Each `debug.consume` pulls exactly one message and processes it through the full pipeline
5. The message is ACKed/committed only after successful processing

This gives the IDE complete control over message flow, making event-driven debugging as manageable as REST debugging.

### Start Suspended Mode

When Mycel starts with `--debug-suspend` (or `MYCEL_DEBUG_SUSPEND=true`), event-driven connectors are registered but **not started**. They wait for the full handshake:

```
1. mycel start --debug-suspend
   → REST, gRPC, GraphQL, SOAP, TCP, SSE start normally
   → RabbitMQ, Kafka, MQTT, CDC, File, WebSocket are deferred

2. Studio sends debug.attach
   → Debug throttling enabled on all event-driven connectors

3. Studio sends debug.setBreakpoints (per flow)
   → Breakpoints registered

4. Studio sends debug.ready
   → Queue-based connectors: SetManualConsume(true) + Start() → connect, topology only
   → Push-based connectors: Start() with debug throttling pre-enabled
   → Response includes sources[] with manualConsume flags

5. Studio sends debug.consume (for queue-based)
   → Pull one message → flow pipeline → breakpoints → ACK
```

If a **hot reload** occurs while a debugger is connected:
- New connector instances get debug throttling re-applied
- If `debug.ready` was already sent, suspended connectors start immediately with manual consume enabled
- The debugger does not need to re-attach or re-send `debug.ready`

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
└── debug_test.go   — 32 tests
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
    ready     atomic.Bool          // true after debug.ready handshake
    upgrader  websocket.Upgrader   // gorilla/websocket

    // Callbacks wired by Runtime
    OnClientChange func(hasClients bool)  // 0→1 or 1→0 clients
    OnReady        func()                  // debug.ready received
}

// Key methods:
NewServer(inspector, logger) *Server
Stream() *EventStream
GetSession(id) (*Session, bool)
ActiveSession() *Session              // returns first session (single-client shortcut)
HasClients() bool
IsReady() bool                        // true after debug.ready handshake
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
    // For non-transform stages: any breakpoint on this stage triggers (regardless of ruleIndex)
    // For transform stage: only stage-level breakpoints (ruleIndex < 0) trigger here
    //   (rule-level transform breakpoints are handled by StudioTransformHook.BeforeRule)
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

    // Event-driven connector capabilities (for debug.ready response)
    ListEventSources() []SourceCapability

    // Manual consume: pull one message from a queue connector (for debug.consume)
    ConsumeOne(ctx context.Context, connectorName string) error
}
```

Implemented by `Runtime` in `internal/runtime/runtime.go`:

```go
func (r *Runtime) ListEventSources() []debug.SourceCapability {
    var sources []debug.SourceCapability
    for _, name := range r.connectors.List() {
        conn, err := r.connectors.Get(name)
        if err != nil { continue }
        // Only event-driven connectors (those implementing DebugThrottler)
        if _, isEventDriven := conn.(connector.DebugThrottler); !isEventDriven {
            continue
        }
        cap := debug.SourceCapability{ Connector: name }
        if dc, ok := conn.(connector.DebugConsumer); ok {
            connType, source := dc.SourceInfo()
            cap.Type = connType
            cap.Source = source
            cap.ManualConsume = true
        } else {
            cap.Type = conn.Type()
        }
        sources = append(sources, cap)
    }
    return sources
}

func (r *Runtime) ConsumeOne(ctx context.Context, connectorName string) error {
    conn, err := r.connectors.Get(connectorName)
    if err != nil {
        return fmt.Errorf("connector %q not found: %w", connectorName, err)
    }
    dc, ok := conn.(connector.DebugConsumer)
    if !ok {
        return fmt.Errorf("connector %q does not support manual consume", connectorName)
    }
    return dc.ConsumeOne(ctx)
}
```

### DebugConsumer Interface

In `internal/connector/connector.go`, queue-based connectors implement `DebugConsumer` for manual message consumption:

```go
// DebugConsumer is implemented by queue-based connectors (RabbitMQ, Kafka)
// that support manual message consumption in debug mode.
type DebugConsumer interface {
    // SetManualConsume enables or disables manual consume mode.
    // When true, Start() connects but does not start consuming.
    SetManualConsume(enabled bool)

    // ConsumeOne fetches and processes a single message from the queue.
    // Blocks until a message is available or context is cancelled.
    ConsumeOne(ctx context.Context) error

    // SourceInfo returns the connector type and source identifier
    // (e.g., queue name for RabbitMQ, topic for Kafka) for IDE display.
    SourceInfo() (connectorType string, source string)
}
```

**RabbitMQ implementation** (`internal/connector/mq/rabbitmq/connector.go`):
- `SetManualConsume(true)` → sets internal flag
- `Start()` with flag → calls `setupTopology()` only (no `channel.Consume()`, no worker goroutines)
- `ConsumeOne(ctx)` → uses `channel.Get(queueName, false)` (AMQP `Basic.Get`), polls every 100ms if empty, processes through `handleDelivery()` which routes to the registered flow handler
- `SourceInfo()` → returns `("rabbitmq", queueName)`

**Kafka implementation** (`internal/connector/mq/kafka/connector.go`):
- `SetManualConsume(true)` → sets internal flag
- `Start()` with flag → calls `startReaderOnly()` which creates the `kafka.Reader` but starts no consume loops, `CommitInterval: 0` for manual commit
- `ConsumeOne(ctx)` → uses `reader.FetchMessage(ctx)`, processes through `handleMessage()`, then `reader.CommitMessages(ctx, msg)`
- `SourceInfo()` → returns `("kafka", topicList)`

**Push-based connectors** (MQTT, Redis Pub/Sub, CDC, File, WebSocket) do **not** implement `DebugConsumer`. They only implement `DebugThrottler` for automatic single-message throttling.

### Runtime Integration

In `runtime.go` `New()`:

```go
// Created early so flow handlers can reference it
r.debugServer = debug.NewServer(r, opts.Logger)

// Wire debug throttling: toggle single-message processing on all event-driven connectors
r.debugServer.OnClientChange = func(hasClients bool) {
    for _, name := range r.connectors.List() {
        conn, err := r.connectors.Get(name)
        if err != nil { continue }
        if throttler, ok := conn.(connector.DebugThrottler); ok {
            throttler.SetDebugMode(hasClients)
        }
    }
}

// Wire debug.ready handshake: start suspended connectors with manual consume
r.debugServer.OnReady = func() {
    if len(r.suspendedStarters) > 0 {
        for _, sc := range r.suspendedStarters {
            // Enable manual consume on queue-based connectors
            conn, _ := r.connectors.Get(sc.name)
            if dc, ok := conn.(connector.DebugConsumer); ok {
                dc.SetManualConsume(true)
            }
            sc.starter.Start(context.Background())
        }
        r.suspendedStarters = nil
    }
}
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

**Server callbacks:**

| Callback | Triggered By | Effect |
|---|---|---|
| `OnClientChange(true)` | First client sends `debug.attach` | Enables `DebugGate` on all 7 event-driven connectors, RabbitMQ sets prefetch=1 |
| `OnClientChange(false)` | Last client disconnects | Disables `DebugGate`, restores original prefetch/concurrency |
| `OnReady()` | Client sends `debug.ready` | Starts suspended connectors with `SetManualConsume(true)` for queue-based |

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

In `HandleRequest()`, debug context is injected **before** verbose flow to ensure breakpoints work:

```go
// Attach Studio debug context when a debug client is connected.
// This takes priority over verbose flow to ensure breakpoints work.
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

        // Always attach breakpoint controller when a debugger is connected.
        // Breakpoints are checked dynamically per-request, so the controller
        // must be present even if no breakpoints are set yet (they may be
        // added between requests).
        tc.Breakpoint = debug.NewStudioBreakpointController(session, debugThread, stream, debugCollector)
        ctx = trace.WithTrace(ctx, tc)

        // Always attach transform hook for per-rule debugging.
        hook := debug.NewStudioTransformHook(session, debugThread, stream, debugCollector, h.Config.Name, trace.StageTransform)
        ctx = transform.WithTransformHook(ctx, hook)

        debugCollector.BroadcastFlowStart(input)
    }
}

// Verbose flow logging only if no debug active (debug takes priority)
if h.VerboseFlow && !trace.IsTracing(ctx) && h.Logger != nil {
    // ... verbose flow setup ...
}

// ... pipeline executes ...

defer func() {
    if debugCollector != nil {
        debugCollector.BroadcastFlowEnd(result, time.Since(start), err)
    }
}()
```

**Key design decisions:**
- Breakpoint controller and transform hook are **always** injected when a debugger is connected, not only when breakpoints exist. This prevents the race condition where breakpoints are set after the flow handler was created.
- Debug context injection happens **before** verbose flow. Since both use `trace.IsTracing(ctx)`, debug takes priority.
- `debug.ready` + `debug.consume` eliminates the timing issue for event-driven connectors at the protocol level.

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

### REST Flow (Request-Response)

#### 1. Connect and Inspect

```json
→ {"jsonrpc":"2.0","id":1,"method":"debug.attach","params":{"clientName":"mycel-studio"}}
← {"jsonrpc":"2.0","id":1,"result":{"sessionId":"s1","flows":["create_user","get_users"]}}

→ {"jsonrpc":"2.0","id":2,"method":"inspect.flows"}
← {"jsonrpc":"2.0","id":2,"result":[{"name":"create_user","from":{"connector":"api","operation":"POST /users"},"to":{"connector":"postgres","operation":"users"},"hasSteps":false,"stepCount":0,"transform":{"email":"lower(input.email)","name":"input.name"},"response":null,"validate":null,"hasCache":false,"hasRetry":false},{"name":"get_users","from":{"connector":"api","operation":"GET /users"},"to":{"connector":"postgres","operation":"users"},"hasSteps":false,"stepCount":0,"transform":null,"response":null,"validate":null,"hasCache":false,"hasRetry":false}]}
```

#### 2. Signal Ready

```json
→ {"jsonrpc":"2.0","id":3,"method":"debug.ready"}
← {"jsonrpc":"2.0","id":3,"result":{"ok":true,"sources":[{"connector":"api","type":"rest","source":"POST /users, GET /users","manualConsume":false}]}}
```

> For REST flows, `manualConsume` is `false` — requests arrive externally and are processed immediately. The `sources` list tells the IDE what connectors are active and their capabilities.

#### 3. Set Breakpoints

```json
→ {"jsonrpc":"2.0","id":4,"method":"debug.setBreakpoints","params":{"flow":"create_user","breakpoints":[{"stage":"transform","ruleIndex":-1},{"stage":"transform","ruleIndex":0}]}}
← {"jsonrpc":"2.0","id":4,"result":{"breakpoints":[{"stage":"transform","ruleIndex":-1},{"stage":"transform","ruleIndex":0}]}}
```

#### 4. Trigger a Request (external)

```bash
curl -X POST http://localhost:8080/users -d '{"email":"ALICE@EXAMPLE.COM","name":"Alice"}'
```

#### 5. Receive Events

```json
← {"jsonrpc":"2.0","method":"event.flowStart","params":{"threadId":"t1a2b3c4","flowName":"create_user","input":{"email":"ALICE@EXAMPLE.COM","name":"Alice"}}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"sanitize","output":{"email":"ALICE@EXAMPLE.COM","name":"Alice"},"durationUs":50}}
← {"jsonrpc":"2.0","method":"event.stopped","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","reason":"breakpoint"}}
```

#### 6. Inspect Variables

```json
→ {"jsonrpc":"2.0","id":5,"method":"debug.variables","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","id":5,"result":{"input":{"email":"ALICE@EXAMPLE.COM","name":"Alice"},"output":{}}}
```

#### 7. Step Into Transform

```json
→ {"jsonrpc":"2.0","id":6,"method":"debug.stepInto","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","id":6,"result":{"ok":true}}
← {"jsonrpc":"2.0","method":"event.continued","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","method":"event.stopped","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","rule":{"index":0,"target":"email","expression":"lower(input.email)"},"reason":"stepInto"}}
```

#### 8. Evaluate Expression

```json
→ {"jsonrpc":"2.0","id":7,"method":"debug.evaluate","params":{"threadId":"t1a2b3c4","expression":"lower(input.email)"}}
← {"jsonrpc":"2.0","id":7,"result":{"result":"alice@example.com","type":"string"}}
```

#### 9. Continue

```json
→ {"jsonrpc":"2.0","id":8,"method":"debug.continue","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","id":8,"result":{"ok":true}}
← {"jsonrpc":"2.0","method":"event.continued","params":{"threadId":"t1a2b3c4"}}
← {"jsonrpc":"2.0","method":"event.ruleEval","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","ruleIndex":0,"target":"email","expression":"lower(input.email)","result":"alice@example.com"}}
← {"jsonrpc":"2.0","method":"event.ruleEval","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","ruleIndex":1,"target":"name","expression":"input.name","result":"Alice"}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"transform","output":{"email":"alice@example.com","name":"Alice"},"durationUs":200}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t1a2b3c4","flowName":"create_user","stage":"write","output":{"id":42,"email":"alice@example.com","name":"Alice"},"durationUs":5000}}
← {"jsonrpc":"2.0","method":"event.flowEnd","params":{"threadId":"t1a2b3c4","flowName":"create_user","output":{"id":42,"email":"alice@example.com","name":"Alice"},"durationUs":6000}}
```

#### 10. Disconnect

```json
→ {"jsonrpc":"2.0","id":9,"method":"debug.detach"}
← {"jsonrpc":"2.0","id":9,"result":{"ok":true}}
```

### RabbitMQ Flow (Event-Driven with Manual Consume)

This example shows debugging an event-driven flow where messages come from a RabbitMQ queue. The IDE has **full control** over when each message is consumed.

#### 1. Connect

```json
→ {"jsonrpc":"2.0","id":1,"method":"debug.attach","params":{"clientName":"mycel-studio"}}
← {"jsonrpc":"2.0","id":1,"result":{"sessionId":"s1","flows":["process_order","notify_user"]}}
```

#### 2. Set Breakpoints

```json
→ {"jsonrpc":"2.0","id":2,"method":"debug.setBreakpoints","params":{"flow":"process_order","breakpoints":[{"stage":"transform"}]}}
← {"jsonrpc":"2.0","id":2,"result":{"breakpoints":[{"stage":"transform","ruleIndex":-1}]}}
```

#### 3. Signal Ready (discovers manual consume capabilities)

```json
→ {"jsonrpc":"2.0","id":3,"method":"debug.ready"}
← {"jsonrpc":"2.0","id":3,"result":{"ok":true,"sources":[
  {"connector":"orders_queue","type":"rabbitmq","source":"orders","manualConsume":true},
  {"connector":"api","type":"rest","source":"POST /users","manualConsume":false}
]}}
```

> The response tells the IDE that `orders_queue` supports `manualConsume: true`. The IDE must call `debug.consume` to pull messages from this connector. REST connectors (`manualConsume: false`) process requests as they arrive — no consume call needed.

#### 4. Consume a Message (IDE pulls one message)

```json
→ {"jsonrpc":"2.0","id":4,"method":"debug.consume","params":{"connector":"orders_queue"}}
```

> **This call blocks** until the message is fully processed through the pipeline (including any breakpoint pauses). While blocked, debug events stream normally.

#### 5. Message Enters Pipeline — Events Stream

```json
← {"jsonrpc":"2.0","method":"event.flowStart","params":{"threadId":"t5x6y7z8","flowName":"process_order","input":{"orderId":"ORD-123","amount":99.99,"customer":"alice@example.com"}}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t5x6y7z8","flowName":"process_order","stage":"sanitize","output":{"orderId":"ORD-123","amount":99.99,"customer":"alice@example.com"},"durationUs":45}}
← {"jsonrpc":"2.0","method":"event.stopped","params":{"threadId":"t5x6y7z8","flowName":"process_order","stage":"transform","reason":"breakpoint"}}
```

#### 6. Inspect Variables at Breakpoint

```json
→ {"jsonrpc":"2.0","id":5,"method":"debug.variables","params":{"threadId":"t5x6y7z8"}}
← {"jsonrpc":"2.0","id":5,"result":{"input":{"orderId":"ORD-123","amount":99.99,"customer":"alice@example.com"},"output":{}}}
```

#### 7. Continue Execution

```json
→ {"jsonrpc":"2.0","id":6,"method":"debug.continue","params":{"threadId":"t5x6y7z8"}}
← {"jsonrpc":"2.0","id":6,"result":{"ok":true}}
← {"jsonrpc":"2.0","method":"event.continued","params":{"threadId":"t5x6y7z8"}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t5x6y7z8","flowName":"process_order","stage":"transform","output":{"orderId":"ORD-123","total":99.99,"email":"alice@example.com","status":"processing"},"durationUs":180}}
← {"jsonrpc":"2.0","method":"event.stageExit","params":{"threadId":"t5x6y7z8","flowName":"process_order","stage":"write","output":{"id":1,"orderId":"ORD-123","status":"processing"},"durationUs":4200}}
← {"jsonrpc":"2.0","method":"event.flowEnd","params":{"threadId":"t5x6y7z8","flowName":"process_order","output":{"id":1,"orderId":"ORD-123","status":"processing"},"durationUs":5100}}
```

#### 8. `debug.consume` Returns (message fully processed)

```json
← {"jsonrpc":"2.0","id":4,"result":{"ok":true}}
```

> The `debug.consume` response (id:4) arrives **after** the entire pipeline completes. The message is ACKed in the queue only after successful processing.

#### 9. Consume Next Message (repeat as needed)

```json
→ {"jsonrpc":"2.0","id":7,"method":"debug.consume","params":{"connector":"orders_queue"}}
← ... (events for next message) ...
← {"jsonrpc":"2.0","id":7,"result":{"ok":true}}
```

#### 10. Disconnect

```json
→ {"jsonrpc":"2.0","id":8,"method":"debug.detach"}
← {"jsonrpc":"2.0","id":8,"result":{"ok":true}}
```

> On detach, manual consume is disabled and connectors revert to automatic message consumption.

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

The test suite (`internal/debug/debug_test.go`) contains **32 tests** covering:

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
| `TestReadyReturnsCapabilities` | `debug.ready` returns source capabilities with manualConsume flags |
| `TestConsumeNotFound` | Error when consuming from nonexistent connector |
| `TestConsumeEmptyConnector` | Error when connector name is empty |

Run with:
```bash
go test ./internal/debug/ -v
```

All 32 pass. The full test suite (`go test ./...`) also passes — 65 packages, zero regressions.
