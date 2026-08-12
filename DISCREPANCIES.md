# Doc ↔ code discrepancies

Working list, kept while raising test coverage. Every entry is a place where the
documentation, the schema or an example says one thing and the parser or the
runtime does another.

**Neither side automatically wins, and the question is not "who is right".**
The question is what the person writing that configuration expects, and then
whether the docs, the code, or both, fall short of it. Sometimes the docs
describe a language nobody implemented. Sometimes the code is thinner than what
was designed. Sometimes — as with the connector retry block — the docs describe
the right idea in a vocabulary that does not exist while the code has half the
machinery and no way to reach it, and the answer is to build the missing part
and document what it actually does.

Status: `open` = found, not decided · `decided` = we know which side is right ·
`done` = fixed on the correct side.

---

## Open

| # | Where | Doc says | Parser accepts | Recommendation |
|---|---|---|---|---|
| 10 | `param` blocks in a named operation | the block accepts `min`, `max`, `min_length`, `max_length`, `pattern`, `enum`, `in`, and `type` | **only `required` and `default` do anything.** `ValidateParams` checks required, `Resolve` fills defaults; the six constraints and `in` are parsed into `ParamDef` and never read, and `type` is stored with a `// Type checking could be added here` where the check would go. Writing `param "age" { type = "number", min = 0, max = 150 }` accepts anything at all | **build it.** `internal/validate` already has MinConstraint, MaxConstraint, PatternConstraint, EnumConstraint and the length ones, now covered by tests — the constraints exist and are enforced for `type` blocks. Wiring them here is reuse, not new machinery. |
| 7 | connector `tls` | nothing documents it | `ca_cert` | **leave the name.** Renaming a working parser attribute breaks whoever already uses it. Document `ca_cert`. |
| 8 | connector `mock` | nothing documents it | `source` | same as #7: document `source`. |

### Dead-or-bug candidates (parked by decision, 2026-08-12)

Two clusters of unreachable code. Neither gets tests until we decide which it
is, because testing code that should be deleted is the most expensive of the
three options.

| Where | Size | What it looks like |
|---|---|---|
| `pkg/errors` | 19 unreachable funcs | A typed-error package with exactly one consumer in the whole repo (`internal/aspect/executor.go`). Either it was meant to be adopted everywhere and never was — in which case the *absence* of its use is the bug — or it is dead. |
| `internal/transform/functions.go` + the `ExpressionParser` half of `transformer.go` | **72 unreachable funcs** | A complete native expression engine: 23 function types (`lower`, `upper`, `pluck`, `format_date`, `coalesce`, …) with Name/Arity/Execute, reached through `NewDefaultTransformer` → `NewBaseTransformer` → `DefaultFunctionRegistry`. **Nothing outside `transformer.go` constructs any of it** — every caller in runtime, aspect and cmd uses `NewCELTransformer`. It reads as the pre-CEL engine that was never removed. |

The transform one matters for the coverage target as well as for tidiness: it
is ~127 uncovered statements in `functions.go` alone that are being counted
against a percentage while not being part of the running product.

### Notes on the open ones

**#7 and #8** may simply be the docs never having been written for those
blocks — worth checking whether anything documents them at all before renaming
anything, because renaming a parser attribute is a breaking change for whoever
already uses the working name.

---

## Resolved this session

| Where | What | Which side was wrong | Fixed in |
|---|---|---|---|
| 9 pages, ~42 lines | `output.field = ...` on the left of a transform | **docs** — HCL attribute names are single identifiers, so it never parsed | 2.17.0 |
| 42 occurrences | `input.params.*` / `input.query.*` | **docs** — a REST request arrives flattened onto `input`; this was a runtime `no such key: params` | 2.17.0 |
| 2 reference tables | `ctx.user_id`, `ctx.headers` | **docs** — `ctx` is declared in the CEL environment and never populated | 2.17.0, recorded as reserved |
| `docs/advanced/integration-patterns.md` | `connector.x = {...}`, `foreach`, `response { status, body }` | **docs** — described a language several releases out of date | 2.17.0 |
| `helm/mycel/templates/configmap.yaml` | ConfigMap keys ending in `.hcl` | **code** — the runtime has only parsed `.mycel` since 1.18.0, so the chart mounted files it ignored | 2.18.0 |
| `helm/mycel/values.yaml` | a `service` block with `port` | **code/values** — the block takes `name`, `version`, `admin_port` | 2.18.0 |
| `pkg/schema` | `saga`/`state_machine`/`validator`/`transform` declared `Open` | **schema** — did not describe what the parser accepts | 2.16.0 |
| `internal/runtime` cache key | a `cache {}` block with no explicit `key` | **code** — the default key was built by ranging over the input map, so the same request produced a different key on each call: written under one key, looked up under another, the cache never hit while still paying to store | this branch |
| `internal/aspect` conditions | `input.field`, `drop.reason`, `step.*` in an aspect `if` | **code** — three broken bindings, each a silent false: the flow input was one level deep at `input.input.field`, the drop information was built but never put in the activation so `drop.reason` always compared against the empty default, and `step` was not bound at all so referencing it was an evaluation error. All three read as "did not match" | this branch |
| connector `retry` (#1 + #6) | docs showed `count`/`interval`/`backoff = 2.0`; the parser took only `attempts`; the connector waited a hardcoded `attempt*100ms` | **both, and something was missing on each side.** The user writing that block expects exponential backoff with a base interval, which is a reasonable policy nobody could express and the code could not deliver: the gap was capped at a fifth of a second and grew linearly despite a comment claiming exponential. Implemented `delay`/`max_delay`/`backoff` with the `error_handling` vocabulary, defaults that preserve the old first wait, and docs describing what it now does | this branch |
| GraphQL `subscriptions` (#2) | docs showed `transport = "websocket"` and omitted both timeouts; the schema omitted `connection_timeout` | **docs mostly, with a gap on each side.** There is one transport — server and client both hardcode `graphql-transport-ws` — so `transport` could only ever hold its own default and was noise. Meanwhile the two attributes that decide when an idle subscription is dropped, `keep_alive_interval` and `connection_timeout`, are real, read by the factory, and were documented nowhere; `connection_timeout` was missing from the schema too, so completions never offered it | this branch |
| S3 `force_path_style` (#3) | docs used `force_path_style` | **docs.** The behaviour was never in doubt and nothing was missing: the code is coherent from parser to factory to `o.UsePathStyle`, and the name matches the AWS SDK v2 it uses. `force_path_style` is the SDK v1 / older-Terraform spelling, which is why someone would write it — so the docs say `use_path_style` and name the old spelling once, for people arriving with it | this branch |
| named operation `params` (#4) | docs showed `params = [{ name = ..., type = ..., required = ... }]` | **docs.** The parser wants one `param "<name>" { ... }` block per parameter, which is what `examples/named-operations` already uses and is strictly richer than the list form. The feature itself is wired: the resolver is built in runtime.go, defaults are applied and required parameters checked. Chasing it turned up #10 | this branch |
| connector `profile` blocks (#5) | docs put `type`/`driver` on the parent connector and omitted them from each profile | **docs, and they undersold the feature.** A profile declares what it *is*: `type` inside it is required and a parent `type` does not substitute. The reason is that profiles are heterogeneous — `examples/profiles` has one connector resolving to an HTTP API or a SQLite database depending on the selection — which the docs never mentioned while describing profiles as read/write splitting. Fixed in connectors.md, configuration.md and error-handling.md | this branch |
| database connection strings (#11) | fourteen blocks wrote `dsn = env("DATABASE_URL")`; no such attribute existed and SQL connectors took only discrete fields | **both, with an order.** Discrete fields stay primary — each is validated on its own, and a password can come from a secret while the host comes from a configmap. But a URL is not an authoring preference: every managed platform hands over one `DATABASE_URL` and HCL cannot take a string apart, so without it the only route is a wrapper script. Mycel already accepted `url` for cache and mq and `uri` for mongo, so SQL was the odd one out. Added `url` to postgres and mysql, decomposed into the fields the factories already read, explicit values winning | this branch |
| `pkg/schema` | an `environment` root block | **schema** — no parser accepts it | 2.16.0 |
