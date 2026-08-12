# Doc ↔ code discrepancies

Working list, kept while raising test coverage. Every entry is a place where the
documentation, the schema or an example says one thing and the parser or the
runtime does another.

**The point is that neither side automatically wins.** Some of these are the
documentation describing a language that was never implemented; others are the
code being thinner than what was designed and documented. Each needs a decision
before it gets fixed, and the decision belongs in the "Verdict" column.

Status: `open` = found, not decided · `decided` = we know which side is right ·
`done` = fixed on the correct side.

---

## Open

| # | Where | Doc says | Parser accepts | Recommendation |
|---|---|---|---|---|
| 1 | `connectors.md:67` — HTTP retry | `retry { count = 5 }` | only `attempts` | **doc**. Already decided: `parser_test.go` has `TestConnectorRetryBlockRejectsCount`, whose comment says the parser must reject `count` *"until the docs are aligned"*. Aligning them is the leftover. |
| 2 | `connectors.md:188` — GraphQL subscriptions | `transport = "websocket"` | `enabled`, `path`, `keep_alive_interval`, `connection_timeout` | **doc**, probably: there is one transport. Worth one look at whether a second was ever planned. |
| 3 | `connectors.md:255` — S3 | `force_path_style` | `use_path_style`, and `s3/factory.go` reads that same name | **doc**. The code is coherent end to end; only the page uses the other name. |
| 4 | `connectors.md:273` — named operations | `params = [{ name = ..., type = ... }]` | `param "<name>" { type = ... }`, a labelled block | **doc**. The block form is what the parser and the schema implement. |
| 5 | `connectors.md:312,337` — profiles | `profile "primary" { host = ... }` | `type` is **required** in a profile | **doc**, unless a profile inheriting the connector's type was the intent — that would be the friendlier language. |
| 6 | connector `retry` (design, not spelling) | — | connector-level retry takes **only** `attempts`; `error_handling.retry` takes `attempts`/`delay`/`max_delay`/`backoff` | **think first.** Retrying with no backoff hammers a failing dependency at full rate. Either the connector-level block earns `delay`/`backoff`, or it is declared thin and the docs stop showing it as resilience. |
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

**#6 is the one worth thinking about, not just fixing.** A connector that
retries with no backoff hammers a failing dependency at full rate. If the
connector-level retry is meant to be real, it wants at least `delay` and
`backoff`; if it is meant to be a thin thing and resilience belongs to
`error_handling`, the docs should say so and stop showing it.

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
| `pkg/schema` | an `environment` root block | **schema** — no parser accepts it | 2.16.0 |
