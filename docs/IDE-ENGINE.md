# Mycel IDE Intelligence Engine

> **Package:** `github.com/matutetandil/mycel/pkg/ide`
> **For:** Mycel Studio (Go + Wails) and any IDE integration
> **Dependencies:** `hashicorp/hcl/v2`, `zclconf/go-cty` (no `internal/` imports)

## What This Package Does

`pkg/ide` is a standalone Go library that provides real-time HCL intelligence for Mycel configurations. It parses all `.mycel` files in a project directory, builds an in-memory index of all entities (connectors, flows, types, transforms, aspects, etc.), and answers IDE queries: completions, diagnostics, hover documentation, and go-to-definition.

Studio imports this package directly — no separate LSP process, no subcommand, no JSON-RPC. Just Go function calls.

## Schema Architecture

The schema system has three layers:

```
pkg/schema/                     ← Canonical types and built-in block schemas
    ↑              ↑
    |              |
internal/connector/*/schema.go  ← Each connector describes itself (ConnectorSchemaProvider)
    ↑
    |
internal/runtime/               ← Registers all connectors into a schema.Registry
    ↑
    |
pkg/ide/                        ← Consumes the registry for completions, diagnostics, etc.
```

**Single source of truth:** Each connector defines its own schema (attributes, child blocks, source/target params). The IDE engine and parser consume the same schemas. Adding a new connector = one `schema.go` file, one registration line.

**`pkg/schema/`** contains:
- `schema.go` — Core types: `Block`, `Attr`, `SchemaProvider`, `ConnectorSchemaProvider`
- `builtins.go` — Schemas for all non-connector blocks (flow, aspect, service, etc.)
- `registry.go` — `Registry` maps connector type+driver to schema providers
- `validate.go` — `ValidateParams()` validates params with defaults
- `defaults.go` — `DefaultRegistry()`, `NewRegistryWith(fn)`

## Quick Integration

### Without connector schemas (basic, offline)

```go
import "github.com/matutetandil/mycel/pkg/ide"

// Uses built-in schemas only (flow, aspect, service blocks)
// Connector child blocks (pool, consumer, etc.) use static fallback
engine := ide.NewEngine("/path/to/mycel-project")
diags := engine.FullReindex()
```

### With full connector schemas (recommended for Studio)

```go
import (
    "github.com/matutetandil/mycel/pkg/connectors"
    "github.com/matutetandil/mycel/pkg/ide"
)

// Registry with all 26 connector schemas — no internal/ imports needed
reg := connectors.FullRegistry()
engine := ide.NewEngine("/path/to/mycel-project", ide.WithRegistry(reg))
diags := engine.FullReindex()
```

With the registry, the engine knows every attribute of every connector type — pool settings for database, consumer/queue/exchange for RabbitMQ, TLS for gRPC, etc. Without it, connector child blocks use a static fallback.

**All packages are in `pkg/`** — Studio can import them without `internal/` restrictions:
- `pkg/ide` — IDE engine
- `pkg/schema` — Core types and built-in block schemas
- `pkg/connectors` — All 26 connector schemas + `FullRegistry()`

### Common API calls

```go
// On project open
engine := ide.NewEngine("/path/to/mycel-project")
diags := engine.FullReindex() // parse all .mycel files
// → show diags in Problems panel

// On file edit (content = unsaved buffer)
diags := engine.UpdateFile(path, content)
// → update Problems panel for this file

// On file delete
diags := engine.RemoveFile(path)
// → update Problems panel

// When user triggers autocomplete (Ctrl+Space or typing)
items := engine.Complete(path, line, col)
// → show items in autocomplete dropdown

// When user hovers over a token
result := engine.Hover(path, line, col)
// → show result.Content in tooltip

// When user Ctrl+clicks or "Go to Definition"
loc := engine.Definition(path, line, col)
// → open loc.File at loc.Range.Start
```

## Public API

### Engine

```go
type Engine struct { /* unexported fields */ }

func NewEngine(rootDir string) *Engine
```

All methods are **thread-safe** (internal `sync.RWMutex`). Studio can call `UpdateFile` from a file watcher goroutine and `Complete` from the UI thread simultaneously.

### Lifecycle Methods

| Method | When to call | Returns |
|--------|-------------|---------|
| `FullReindex() []*Diagnostic` | On project open | All diagnostics across all files |
| `UpdateFile(path string, content []byte) []*Diagnostic` | On file save or edit (pass unsaved buffer content) | Diagnostics for the updated file |
| `RemoveFile(path string) []*Diagnostic` | On file delete | Cross-reference diagnostics affected by removal |

### Query Methods

| Method | When to call | Returns |
|--------|-------------|---------|
| `Complete(path string, line, col int) []CompletionItem` | User triggers autocomplete | Context-aware suggestions |
| `Diagnose(path string) []*Diagnostic` | On demand for a single file | Parse + schema + cross-ref diagnostics |
| `DiagnoseAll() []*Diagnostic` | On demand for entire project | All diagnostics |
| `Hover(path string, line, col int) *HoverResult` | User hovers over a token | Documentation or entity info |
| `Definition(path string, line, col int) *Location` | User Ctrl+clicks a reference | Source location of the referenced entity |

### Inspection

| Method | Purpose |
|--------|---------|
| `GetIndex() *ProjectIndex` | Access the project index for custom queries (testing, visualization) |

## Data Types

### Diagnostic

```go
type Diagnostic struct {
    Severity Severity `json:"severity"` // SeverityError=1, SeverityWarning=2, SeverityInfo=3
    Message  string   `json:"message"`
    File     string   `json:"file"`
    Range    Range    `json:"range"`
}
```

**Four diagnostic layers:**

1. **Parse errors** — HCL syntax errors (unclosed braces, invalid tokens). These come from `hashicorp/hcl/v2` directly.
2. **Schema validation** — Unknown block types, missing required attributes, invalid enum values, **unknown attributes** (typos), and connector-type-specific required attributes. Uses the static schema in `schema.go` merged with `connectorTypeAttrs()`.
3. **Cross-reference validation** — Undefined connectors/types/transforms/flows, duplicate names. Uses the project-wide index. Works across all block types including aspect `action { connector = "..." }` and `action { flow = "..." }`.
4. **Operation validation** — REST operations like `"GETX /users"` produce warnings for unknown HTTP methods and missing leading `/`.

**Example messages:**
- `"unknown block type 'flw'"` (typo in block type)
- `"unknown attribute 'on_regehgrhe' in accept block"` (typo in attribute name)
- `"missing required attribute 'type' in connector block"` (forgot `type = "rest"`)
- `"database connector requires attribute driver"` (connector-type-specific required attr)
- `"invalid value 'banana' for accept.on_reject (valid: [ack reject requeue])"` (invalid enum)
- `"undefined connector 'nonexistent'"` (reference to missing connector)
- `"duplicate flow name 'test' (also defined in other.mycel)"` (name collision)
- `"unknown HTTP method 'GETX'"` (invalid REST operation)

**Open vs strict blocks:**

Some blocks have a fixed set of valid attributes (strict) while others accept any attribute (open):

| Block | Mode | Why |
|-------|------|-----|
| `connector` | Strict (base + type-specific) | Known attrs per connector type |
| `accept`, `validate`, `dedupe`, `service` | Strict | Fixed schema |
| `transform`, `response` | Open (dynamic) | Attributes are CEL field mappings |
| `type` | Open (dynamic) | Attributes are user-defined schema fields |
| `from`, `to`, `step` | Open | Accept connector-specific params beyond base attrs |

Strict blocks flag unknown attributes as errors. Open blocks allow any attribute name.

### CompletionItem

```go
type CompletionItem struct {
    Label      string         `json:"label"`
    Kind       CompletionKind `json:"kind"`       // CompletionBlock=1, CompletionAttribute=2, CompletionValue=3
    Detail     string         `json:"detail"`
    Doc        string         `json:"doc"`
    InsertText string         `json:"insertText"` // What gets inserted (may include snippet template)
}
```

### HoverResult

```go
type HoverResult struct {
    Content string `json:"content"` // Markdown-like documentation text
    Range   Range  `json:"range"`
}
```

### Location (go-to-definition)

```go
type Location struct {
    File  string `json:"file"`
    Range Range  `json:"range"`
}
```

### Position and Range

```go
type Position struct {
    Line   int `json:"line"`   // 1-based
    Col    int `json:"col"`    // 1-based
    Offset int `json:"offset"` // 0-based byte offset
}

type Range struct {
    Start Position `json:"start"`
    End   Position `json:"end"`
}
```

**Important:** Lines and columns are **1-based** (matching HCL and most editor conventions). Offsets are **0-based** byte offsets.

## How Completions Work

The engine detects the cursor context (what block the cursor is in, whether it's in an attribute name or value position) and returns context-appropriate suggestions.

### Context Detection

Given `(file, line, col)`, the engine:
1. Walks the parsed block tree to find the deepest block containing the cursor
2. Checks if the cursor is on an attribute (name or value position)
3. Builds a "block path" like `["flow", "from"]` or `["connector"]`

### Five Completion Modes

**1. Root level** — Cursor is not inside any block:
- Returns all 17 valid root block types: `connector`, `flow`, `type`, `transform`, `aspect`, `service`, `validator`, `saga`, `state_machine`, `functions`, `plugin`, `auth`, `security`, `mocks`, `cache`, `environment`
- InsertText includes snippet: `flow "name" {\n  \n}`

**2. Inside a block** — Cursor is inside a block body (not in a value):
- Returns valid child blocks not yet present (except multi-allowed: `step`, `enrich`, `to`)
- Returns valid attributes not yet defined
- Required attributes marked with `(required)` in detail
- InsertText: `connector = ""` (strings), `parallel = true` (bools)
- **Connector-type-aware**: Inside a `connector` block with `type = "database"`, suggests database-specific attrs (`driver`, `host`, `database`, `user`, `password`) in addition to base attrs

**3. Attribute value (enum/reference)** — Cursor is after `=`:
- **Enum values**: For attributes with a fixed set of valid values (e.g., `on_reject` → `"ack"`, `"reject"`, `"requeue"`)
- **Project references**: For attributes that reference other entities:
  - `connector = ` → names from `ProjectIndex.Connectors` (with type/driver detail)
  - `validate { input = ` → names from `ProjectIndex.Types`
  - `transform { use = ` → names from `ProjectIndex.Transforms`
  - `aspect { action { flow = ` → names from `ProjectIndex.Flows`
  - `cache = ` → names from `ProjectIndex.Caches`

**4. CEL expression value** — Cursor is after `=` in a transform, response, filter, accept when, or condition attribute:
- **Variables**: `input`, `output` (response only), `step.<name>` (if flow has steps), `enriched.<name>` (if flow has enrichments), `error` (in on_error aspects)
- **Functions**: 39 built-in CEL functions (`uuid()`, `now()`, `lower()`, `upper()`, `has()`, `len()`, `contains()`, `first()`, `last()`, `sum()`, `avg()`, etc.)

**5. Operation value** — Cursor is after `=` on an `operation` attribute:
- Suggests templates based on the referenced connector's type:
  - **REST**: `GET /`, `POST /`, `PUT /`, `PATCH /`, `DELETE /`
  - **GraphQL**: `Query.`, `Mutation.`, `Subscription.`
  - **gRPC**: `ServiceName/MethodName`

### Example

User is editing `flows.mycel` with cursor at `|`:

```hcl
flow "get_users" {
  from {
    connector = "|"
  }
}
```

The engine returns:
```json
[
  {"label": "api", "kind": 3, "detail": "rest", "insertText": "\"api\""},
  {"label": "db", "kind": 3, "detail": "database/postgres", "insertText": "\"db\""}
]
```

## How Go-to-Definition Works

When the user Ctrl+clicks on a reference value (e.g., `connector = "api"`), the engine:

1. Detects cursor is in an attribute value
2. Looks up the attribute's schema to find its `RefKind`
3. Resolves the reference against the project index
4. Returns the `Location` of the target entity's block definition

**Supported references:**

| Attribute context | RefKind | Resolves to |
|-------------------|---------|-------------|
| `from/to/step { connector = "X" }` | `RefConnector` | `connector "X" { }` block |
| `validate { input = "type.X" }` | `RefType` | `type "X" { }` block |
| `transform { use = "X" }` | `RefTransform` | `transform "X" { }` block |
| `aspect { action { flow = "X" } }` | `RefFlow` | `flow "X" { }` block |
| `cache = "cache.X"` | `RefCache` | `cache "X" { }` block |

## How Hover Works

Three hover contexts:

1. **On attribute name** (e.g., hovering on `on_reject`):
   → Shows the attribute's documentation from the schema
   → If the attribute has enum values, appends "Valid values: ack, reject, requeue"

2. **On reference value** (e.g., hovering on `"api"` in `connector = "api"`):
   → Shows the referenced entity's info: name, type, driver, source file

3. **On block type keyword** (e.g., hovering on `accept`):
   → Shows the block's documentation from the schema

## Schema Registry

The schema is a static Go data structure in `schema.go` that describes every valid block, attribute, and value in the Mycel HCL configuration language.

### Block Types

| Block | Labels | Doc |
|-------|--------|-----|
| `connector` | 1 | Bidirectional adapter for databases, APIs, queues, and other services |
| `flow` | 1 | Data flow from source to destination |
| `type` | 1 | Schema definition for input/output validation |
| `transform` | 1 | Reusable named transformation (CEL expressions) |
| `aspect` | 1 | Cross-cutting concern applied via flow name pattern matching (AOP) |
| `service` | 0 | Global service configuration |
| `validator` | 1 | Custom validation rule (regex, CEL, or WASM) |
| `saga` | 1 | Distributed transaction with automatic compensation |
| `state_machine` | 1 | Entity lifecycle with guards, actions, and final states |
| `functions` | 1 | Custom CEL functions |
| `plugin` | 1 | WASM plugin for extending Mycel |
| `auth` | 0 | Authentication configuration |
| `security` | 0 | Security and sanitization rules |
| `mocks` | 0 | Mock data for testing |
| `cache` | 1 | Named cache definition |
| `environment` | 1 | Environment-specific variables |

### Connector Types (24)

`rest`, `database`, `mq`, `graphql`, `grpc`, `file`, `s3`, `cache`, `tcp`, `exec`, `soap`, `mqtt`, `ftp`, `cdc`, `websocket`, `sse`, `elasticsearch`, `oauth`, `email`, `slack`, `discord`, `sms`, `push`, `webhook`, `pdf`

### Connector Drivers (12)

`sqlite`, `postgres`, `mysql`, `mongodb`, `rabbitmq`, `kafka`, `redis`, `memory`, `json`, `msgpack`, `nestjs`

### Flow Children (18 block types)

`from`, `to`, `accept`, `step`, `transform`, `response`, `validate`, `enrich`, `lock`, `semaphore`, `coordinate`, `cache`, `require`, `after`, `error_handling`, `dedupe`, `idempotency`, `async`, `batch`, `state_transition`

### Extending the Schema

When adding a new block type or attribute to Mycel, update `schema.go`:

1. Add the block to `rootSchema()` or as a child of an existing block
2. Define attributes with `AttrSchema` (name, doc, type, required, enum values, ref kind)
3. The engine will automatically provide completions, diagnostics, and hover docs

Example — adding a new `websocket` attribute to the `from` block:
```go
// In fromSchema(), add to Attrs:
{Name: "reconnect", Doc: "Auto-reconnect on disconnect", Type: AttrBool},
```

## Project Index

The `ProjectIndex` is rebuilt incrementally. When a file is updated:

1. The file is re-parsed (permissive parser, ~1ms)
2. Its `FileIndex` replaces the old one
3. All lookup tables are rebuilt from scratch (O(files × blocks), typically <1ms)

The rebuild scans every block in every file and populates the entity maps (`Connectors`, `Flows`, `Types`, etc.) by checking `block.Type` and extracting `type` and `driver` attributes for connectors.

### Entity Resolution

```go
// Get a connector by name
entity := engine.GetIndex().Connectors["api"]
// entity.Kind = "connector"
// entity.Name = "api"
// entity.ConnType = "rest"
// entity.Driver = ""
// entity.File = "connectors/rest.mycel"
// entity.Range = {Start: {Line: 1, Col: 1}, End: {Line: 4, Col: 2}}
```

## Studio Integration Guide

### Recommended Architecture

```
Mycel Studio (Wails app)
├── Frontend (HTML/JS/CSS)
│   ├── Editor component (Monaco or CodeMirror)
│   │   ├── On edit → call Go binding: UpdateFile(path, content)
│   │   ├── On Ctrl+Space → call Go binding: Complete(path, line, col)
│   │   ├── On hover → call Go binding: Hover(path, line, col)
│   │   ├── On Ctrl+click → call Go binding: Definition(path, line, col)
│   │   ├── On F2 (rename) → call Go binding: Rename(path, line, col, newName)
│   │   └── Gutter breakpoint dots → driven by GetBreakpoints(file)
│   ├── Problems panel → render diagnostics
│   ├── Symbols panel → Ctrl+P navigation via GetSymbols()
│   └── File explorer → open/close files
│
├── Go Backend (Wails bindings)
│   ├── engine *ide.Engine (singleton)
│   ├── watcher (fsnotify) → on .mycel change: engine.UpdateFile()
│   └── Wails-exposed methods:
│       ├── OpenProject(dir string) → engine = ide.NewEngine(dir); engine.FullReindex()
│       ├── GetCompletions(path, line, col) → engine.Complete(...)
│       ├── GetHover(path, line, col) → engine.Hover(...)
│       ├── GetDefinition(path, line, col) → engine.Definition(...)
│       ├── GetDiagnostics(path) → engine.Diagnose(...)
│       ├── OnFileChanged(path, content) → engine.UpdateFile(...)
│       ├── GetSymbols() → engine.Symbols()
│       ├── GetFileSymbols(path) → engine.SymbolsForFile(path)
│       ├── Rename(path, line, col, newName) → engine.Rename(...)
│       ├── GetCodeActions(path, line, col) → engine.CodeActions(...)
│       ├── GetBreakpoints(file) → engine.AllBreakpoints()[file]
│       ├── GetFlowBreakpoints(flowName) → engine.FlowBreakpoints(flowName)
│       └── RemoveBlock(path, type, name) → engine.RemoveBlock(...)
│
└── Debug Client (separate, WebSocket to :9090)
    └── Runtime debugging — uses breakpoint locations from IDE engine
```

### File Watcher Pattern

```go
import (
    "github.com/fsnotify/fsnotify"
    "github.com/matutetandil/mycel/pkg/ide"
)

type App struct {
    engine  *ide.Engine
    watcher *fsnotify.Watcher
}

func (a *App) OpenProject(dir string) []*ide.Diagnostic {
    a.engine = ide.NewEngine(dir)
    diags := a.engine.FullReindex()

    // Start watching for file changes
    a.watcher, _ = fsnotify.NewWatcher()
    go func() {
        for event := range a.watcher.Events {
            if !strings.HasSuffix(event.Name, ".mycel") {
                continue
            }
            switch {
            case event.Op&fsnotify.Write != 0:
                content, _ := os.ReadFile(event.Name)
                diags := a.engine.UpdateFile(event.Name, content)
                // Emit diags to frontend via Wails event
                runtime.EventsEmit(a.ctx, "diagnostics", event.Name, diags)
            case event.Op&fsnotify.Remove != 0:
                diags := a.engine.RemoveFile(event.Name)
                runtime.EventsEmit(a.ctx, "diagnostics", event.Name, diags)
            }
        }
    }()
    a.watcher.Add(dir)
    // Watch subdirectories too...

    return diags
}
```

### Unsaved Buffer Support

The engine works with unsaved content. When the user is typing (before save), pass the buffer content directly:

```go
func (a *App) OnBufferChange(path string, content string) []*ide.Diagnostic {
    return a.engine.UpdateFile(path, []byte(content))
}
```

This re-parses the (possibly incomplete) content and returns diagnostics in real-time.

### Breakpoint Integration

The IDE engine provides the exact lines where breakpoints can be set. Studio uses this to show gutter breakpoint indicators — the user can only click on lines that are valid breakpoint positions.

```go
// On project open or file change: get all breakpointable lines per file
func (a *App) GetBreakpointLines(file string) []ide.BreakpointLocation {
    allBps := a.engine.AllBreakpoints()
    return allBps[file]
}
```

Each `BreakpointLocation` contains:

```go
type BreakpointLocation struct {
    File      string // Source file path
    Line      int    // 1-based line number (for gutter dot placement)
    Flow      string // Flow name (for debug.setBreakpoints)
    Stage     string // Pipeline stage (input, filter, accept, transform, write, etc.)
    RuleIndex int    // -1 for stage-level, 0+ for per-rule breakpoints
    Label     string // Human-readable label for the UI tooltip
}
```

**Example output** for a flow file:

```
Line  3: input                                                    (stage: input, ruleIndex: -1)
Line  6: filter                                                   (stage: filter, ruleIndex: -1)
Line 10: accept: input.body.payload.type == 'A1'                  (stage: accept, ruleIndex: -1)
Line 14: transform                                                (stage: transform, ruleIndex: -1)
Line 15: transform: number = input.body.payload.associateNumber   (stage: transform, ruleIndex: 0)
Line 16: transform: name = input.body.payload.name                (stage: transform, ruleIndex: 1)
Line 17: transform: emails = input.body.payload.emails.join(',')  (stage: transform, ruleIndex: 2)
Line 23: write: query                                             (stage: write, ruleIndex: -1)
```

**Key design principle:** Breakpoints are placed on the **logic line** (the expression, query, or key), not on block openings. This matches what a developer expects — you break where the action happens, like in any programming language debugger.

**All breakpoints pause BEFORE execution** — the runtime uses `RecordStage` (wraps the function call) for every stage, so the debugger always pauses before the expression is evaluated or the operation is executed.

**How Studio uses this:**

1. **Gutter dots**: Show a faded breakpoint circle on lines returned by `AllBreakpoints()[file]`
2. **Click to toggle**: When user clicks a gutter dot, toggle a breakpoint for that `{flow, stage, ruleIndex}`
3. **Send to debug server**: When debugging, map the toggled breakpoints to `debug.setBreakpoints` calls using the `Flow`, `Stage`, and `RuleIndex` fields:

```go
// When user toggles a breakpoint on line 15 of flows.mycel:
bp := breakpointLocations[15] // {Flow: "upsert_sales_conultant", Stage: "transform", RuleIndex: 0}

// Send to debug server via WebSocket:
debug.setBreakpoints({
    flow: bp.Flow,
    breakpoints: [{stage: bp.Stage, ruleIndex: bp.RuleIndex}]
})
```

**Coverage:** The engine returns breakpoint locations for all stages that exist in a flow. Each breakpoint points to the most meaningful line (the logic, not the block opening):

| Stage | When present | Location (logic line) |
|-------|-------------|----------|
| `input` | Always | Flow block opening line |
| `filter` | `from.filter` attr or `filter {}` block | The `filter` expression line |
| `accept` | `accept {}` block | The `when` expression line |
| `dedupe` | `dedupe {}` block | The `key` expression line |
| `validate_input` | `validate { input = "..." }` | The `input` attribute line |
| `enrich` | `enrich "name" {}` block(s) | Each enrich block line |
| `step` | `step "name" {}` block(s) | The `query` or `operation` line |
| `transform` (stage) | `transform {}` block | Transform block opening line |
| `transform` (per-rule) | Each CEL mapping inside transform | Each attribute line |
| `validate_output` | `validate { output = "..." }` | The `output` attribute line |
| `write` | `to {}` block(s) | The `query` or `target` line |
| `response` (stage) | `response {}` block | Response block opening line |
| `response` (per-rule) | Each CEL mapping inside response | Each attribute line |

**Per-flow vs all-project:**

```go
// Get breakpoints for a specific flow
bps := engine.FlowBreakpoints("create_user")

// Get breakpoints for ALL flows, grouped by file
allBps := engine.AllBreakpoints()
for file, bps := range allBps {
    fmt.Printf("%s: %d breakpoint locations\n", file, len(bps))
}
```

## Parser Details

### Permissive Mode

Unlike the runtime parser (`internal/parser/`), the IDE parser:
- **Never evaluates expressions** — `env("X")` is not resolved, CEL expressions are stored as raw strings
- **Never fails on semantic errors** — unknown attributes are collected as diagnostics, not fatal errors
- **Works with incomplete files** — a file with just `flow "test" {` (unclosed brace) still produces a partial block tree
- **Does not need environment variables** — works offline without any runtime context

### What Gets Extracted

For each attribute, the parser extracts:
- **Literal strings**: `type = "rest"` → `ValueRaw = "rest"`
- **Literal numbers**: `port = 3000` → `ValueRaw = "3000"`
- **Literal bools**: `auto_ack = false` → `ValueRaw = "false"`
- **Simple templates**: `host = "localhost"` → `ValueRaw = "localhost"`
- **Complex expressions**: `host = env("DB_HOST")` → `ValueRaw = ""` (not evaluated)

This means the engine can resolve static references (`connector = "api"`) but not dynamic ones (`connector = env("CONNECTOR")`).

## File Structure

```
pkg/schema/                          # Canonical schema definitions (source of truth)
├── schema.go                        # Core types: Block, Attr, SchemaProvider, ConnectorSchemaProvider
├── builtins.go                      # Built-in block schemas (flow, aspect, service, etc.)
├── registry.go                      # Registry: maps connector type+driver to schema providers
├── validate.go                      # ValidateParams: schema-driven validation with defaults
├── defaults.go                      # DefaultRegistry(), NewRegistryWith(fn)
└── schema_test.go                   # 8 tests

pkg/ide/                             # IDE intelligence engine
├── ide.go                           # Engine: public API, WithRegistry option, thread-safe
├── index.go                         # ProjectIndex: data structures, lookup tables, rebuild
├── parse.go                         # Permissive HCL parser (sorted attrs by source position)
├── schema.go                        # Type aliases delegating to pkg/schema
├── complete.go                      # Completions: root, block, values, CEL, operations, connector-aware
├── diagnose.go                      # Diagnostics: parse, schema, cross-refs, unknown attrs, operations
├── position.go                      # Types (Position, Range, Diagnostic, etc.), cursor context
├── cel.go                           # CEL functions/variables, context detection
├── connector_schema.go              # Connector-type-aware validation (registry + static fallback)
├── operation.go                     # Operation string validation and completion
├── rename.go                        # Rename: definition + all references
├── codeaction.go                    # Quick-fix code actions
├── symbols.go                       # Workspace and file symbols
├── transform_rules.go               # Transform rules, flow stages, breakpoint locations
├── ide_test.go                      # 14 core tests
└── enhancements_test.go             # 21 enhancement tests

pkg/connectors/connectors.go             # All 26 connector schemas + FullRegistry()

internal/connector/*/schema.go           # Connector schemas (also in pkg/connectors for external access)
internal/runtime/schema_registration.go  # Runtime-internal registration (uses internal/ imports)
```

## Additional APIs (v1.17.1+)

### Rename

```go
func (e *Engine) Rename(path string, line, col int, newName string) []RenameEdit
```

Renames an entity (connector, flow, type, transform) and returns all edits needed across the project — both the definition and every reference. Works when cursor is on a block label or a reference value.

### Code Actions

```go
func (e *Engine) CodeActions(path string, line, col int) []CodeAction
```

Returns quick-fix suggestions for the diagnostic at the cursor position:
- **Create connector** — When referencing an undefined connector
- **Create type** — When referencing an undefined type
- **Add attribute** — When a required attribute is missing

### Workspace Symbols

```go
func (e *Engine) Symbols() []Symbol              // All entities across the project
func (e *Engine) SymbolsForFile(path string) []Symbol  // Entities in a specific file
```

Returns named entities for Ctrl+P workspace navigation and document outline. Each symbol has name, kind, detail (connector type/driver), file, and range.

### Transform Rules

```go
func (e *Engine) TransformRules(flowName string) []TransformRule
```

Returns the ordered transform and response rules for a flow. Each rule has index, target field name, CEL expression, stage ("transform" or "response"), and source position. This enables per-rule breakpoint placement in Studio.

### Flow Stages

```go
func (e *Engine) FlowStages(flowName string) []string
```

Returns the pipeline stages present in a flow in execution order (e.g., `["input", "sanitize", "filter", "accept", "transform", "write"]`). Only includes stages that are actually configured. Used for breakpoint placement and pipeline visualization.

### Flow Breakpoints

```go
func (e *Engine) FlowBreakpoints(flowName string) []BreakpointLocation
func (e *Engine) AllBreakpoints() map[string][]BreakpointLocation
```

`FlowBreakpoints` returns every valid breakpoint position for a single flow — with exact file, line, stage, ruleIndex, and human-readable label. `AllBreakpoints` does the same for every flow in the project, grouped by file path.

Studio uses these to:
1. Show gutter breakpoint dots on valid lines only
2. Map user clicks to `{flow, stage, ruleIndex}` tuples for the debug protocol
3. Display tooltips with the breakpoint label

See [Breakpoint Integration](#breakpoint-integration) above for full usage details and examples.

### CEL Completions

Inside transform, response, filter, accept, and condition values, the engine automatically suggests:
- **Variables**: `input`, `output` (response only), `step.<name>` (if flow has steps), `enriched.<name>` (if flow has enrichments), `error` (in on_error aspects)
- **Functions**: 39 built-in CEL functions (`uuid()`, `now()`, `lower()`, `upper()`, `has()`, `len()`, `contains()`, `first()`, `last()`, `sum()`, `avg()`, etc.)

### Connector-Type-Aware Intelligence

When the engine has a registry (`connectors.FullRegistry()`), completions inside connector blocks adapt based on the `type` and `driver` values. Both **attributes** and **child blocks** are suggested from the connector's schema.

Example: inside a `type = "mq"` `driver = "rabbitmq"` connector, completions include:
- **Attributes**: `url`, `port`, `username`, `password`, `vhost`, `heartbeat`, `connection_name`, ...
- **Child blocks**: `tls {}`, `queue {}`, `exchange {}`, `consumer {}`, `publisher {}`

Example: inside a `type = "slack"` connector, completions include:
- `webhook_url`, `token`, `api_url`, `channel`, `username`, `icon_emoji`, `icon_url`, `timeout`

All 26 connector types are fully covered via the schema registry. Without a registry, the engine falls back to a static subset.

Diagnostics also use the registry — if a database connector is missing `driver`, or a rest connector is missing `port`, a warning is produced.

### Operation Completions

When editing `operation = ""` in a from/to block, the engine suggests templates based on the referenced connector's type:
- **REST**: `GET /`, `POST /`, `PUT /`, `PATCH /`, `DELETE /`
- **GraphQL**: `Query.`, `Mutation.`, `Subscription.`
- **gRPC**: `ServiceName/MethodName`

### RemoveBlock

```go
func (e *Engine) RemoveBlock(path, blockType, name string) *TextEdit
```

Returns a `TextEdit` that removes a named block from a file. Used when Studio's canvas deletes a component that shares a file with other blocks.

```go
// User deletes the "api" connector from connectors.mycel (which also has "db")
edit := engine.RemoveBlock("connectors.mycel", "connector", "api")
// edit.Range = lines 1-5, edit.NewText = "" → Studio removes those lines
```

The edit covers the block's full range including the trailing newline. Studio applies the edit to the file content and calls `engine.UpdateFile()` to refresh diagnostics.
