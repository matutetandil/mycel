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

| 13 | connector schemas vs the parser's allow-list | the schemas declare 38 attributes across 12 connectors that the parser rejects outright: `cache.pool.max_connections`/`min_idle`, `database.replicas.*`, `email.template`/`tls`, `exec.workdir`/`env{}`, the five `file.csv_*`, `ftp.tls`, `graphql.cors.allow_credentials`, `mq.heartbeat`/`reconnect_delay`, `oauth.name`, seven `pdf.*`, `tcp.idle_timeout`, and six `webhook.*` | **completions offer them and a file using them does not parse.** This is the v2.1.1 gotcha again — the parser keeps a hand-written allow-list of connector attributes and it has fallen behind the schemas | **decide each one, the same way as the others.** For each: does the connector actually read the property? Then the parser should accept it and the feature is currently unreachable, like TLS was. Does nothing read it? Then the schema is over-claiming and should stop offering it. The parity test in `internal/parser` holds the list, so it cannot grow, and fails if an entry starts parsing so it cannot go stale. |

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

**#13** came out of closing #12: unifying the schemas made it possible to check
them against the parser for the first time, and the check found this.

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
| connector-level `mock` block (#8) | `mock { enabled, source }` inside a connector | **code, by deletion.** The parser stored it in `Properties["mock"]` and nothing ever read it, no schema declared it, no example used it. The real feature is the root-level `mocks {}` block, which runtime.go reads and which offers more per connector (`latency`, `fail_rate`, `enabled`). Removed from the parser, so what was a silent no-op is now a parse error that names the block | this branch |
| `pkg/schema` | an `environment` root block | **schema** — no parser accepts it | 2.16.0 |
| connector `tls` (#7) | three connectors read three different attribute sets; the parser accepted only http's; the gRPC schema advertised six names nothing accepted; a gRPC server whose certificate failed to load started in plaintext | **everything, in different ways, and the user's expectation named all of them.** Someone writing `tls {}` expects one vocabulary that works on every connector and a server that refuses to start rather than quietly downgrade. Measured against that: `cert` and `key` were rejected everywhere, so mutual TLS was unconfigurable on tcp, mq, mqtt and grpc; `enabled` was rejected while mq and mqtt require it, so those two could not be given TLS at all; the docs showed three vocabularies plus two MQTT names (`ca`, `insecure`) nothing ever accepted. Unified on the names three of five connectors already read, older spellings folded on so nothing breaks, the swallowed error turned into a refusal to start, and both schema registries now tested against the parser | this branch |
| named operations (#10) | `param` blocks declared `min`, `max`, `min_length`, `max_length`, `pattern`, `enum` and `type`, and only `required` and `default` were even meant to work | **the code, and much further down than the entry said.** Chasing the ignored constraints found that `ValidateParams` is called by nobody, that `Resolve`'s defaults are discarded by its only caller, and finally that the resolver itself runs only for the startup banner — so a flow referring to an operation by name passed the name to the connector verbatim, and the shipped example validated and then panicked the HTTP mux at startup. Someone writing that block expects the operation to run and the contract to hold, so both were built: names resolve before flows are registered, defaults are applied and constraints enforced as a 400, and the declared type converts because query parameters are always strings. The schema, which declared no `operation` block at all, now describes it and is tested against the parser both ways | this branch |
| connector schemas (#12) | two hand-written copies of all 33 connector schemas, one under `internal/connector` and one in `pkg/connectors` | **the duplication itself, and the recommendation I had logged was wrong.** It said to make the public copy delegate to the internal packages; measuring first showed that would take `pkg/connectors` from nothing to the 123 modules `internal/runtime` pulls, so any program wanting completions would compile every driver. Inverted instead: the definitions live in the public, dependency-free package, one file per connector, and the runtime registers from there. 1,463 lines deleted. The validators stay with their connectors — behaviour belongs there; the schema is a description of a public language | this branch |
