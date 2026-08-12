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

| # | Where | Doc/schema says | Code does | Verdict |
|---|---|---|---|---|
| 1 | `docs/core-concepts/connectors.md:67` | a connector `retry` block with more than `attempts` | `parseRetryBlock` accepts **only** `attempts` | — |
| 2 | `docs/core-concepts/connectors.md:188` | a `subscriptions` attribute the parser rejects | rejected at parse time | — |
| 3 | `docs/core-concepts/connectors.md:255` | S3 `force_path_style` | the accepted name is `use_path_style` (see `examples/integration/file-to-rabbit`) | — |
| 4 | `docs/core-concepts/connectors.md:273` | an `operation` block attribute the parser rejects | rejected at parse time | — |
| 5 | `docs/core-concepts/connectors.md:312,337` | `profile` blocks | a required argument is missing from the examples | — |
| 6 | connector `retry` | flow-level `error_handling.retry` takes `attempts`/`delay`/`max_delay`/`backoff` | the **connector-level** one takes only `attempts` | Is the thin one a gap, or deliberate? |
| 7 | connector `tls` | — | accepts `ca_cert`, not `ca_file` | which name is intended? |
| 8 | connector `mock` | — | accepts `source`, not `dir` | which name is intended? |

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
| `pkg/schema` | an `environment` root block | **schema** — no parser accepts it | 2.16.0 |
