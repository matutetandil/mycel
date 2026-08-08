# Mycel — Proposed Improvements

Written 2026-08-05 against **v2.12.0**. **Status re-verified 2026-08-06** against
`HEAD` (13 commits past the v2.12.0 tag, unreleased). Statuses below were checked
by running the current binary, not by reading the diff — two items that looked
open turned out not to reproduce, and one that looked partly done was finished
by a commit landed after the first pass.

Everything below comes from building and shipping a real consumer
(`mercury-consumer-gallery-assets`) end to end, plus operating five other Mycel
consumers in production (products, styles, inventory, prices, sales-consultant)
across roughly 1.5M processed messages. Each item cites the concrete incident
that motivated it, so the value is arguable from evidence rather than taste.

The runtime performs well and the production record is good — 7 ms median for a
flow doing 9 database round-trips, stable goroutine counts, zero restarts over
days, zero flow failures across ~10k messages on the prices consumer. **None of
what follows is about performance.** It is about the gap between "the config is
wrong" and "you find out."

---

## Status at a glance

| # | Item | Status |
|---|---|---|
| 1.1 | Unmatched routing key discards silently | ✅ **Done** |
| 1.2 | `dlq { enabled }` that provisions nothing | ✅ **Done** |
| 1.3a | `to.params` is inert | ✅ **Done** |
| 1.3b | Unset env var resolves to `""` | ✅ **Done** |
| 1.3c | Hyphenated connector name → silent no-op | ❔ **Does not reproduce** |
| 1.3d | `port = env(...)` → silent fallback to 3306 | ❔ **Does not reproduce** |
| 1.3e | `env()` inside a CEL transform | ⬜ Open — unverified |
| 2 | Test harness (`mycel test`) | ⬜ Open — **largest remaining gap** |
| 3.1 | Per-stage timings | ⬜ Open (per-*flow* extremes added instead) |
| 3.2 | `mycel version` | ✅ **Done** |
| 3.3 | Startup dispatch + reachability summary | ✅ **Done** |
| 4.1 | Reusable `step` / `to` blocks | ⬜ Open |
| 4.2 | SQL fragments | ⬜ Open |
| 4.3 | Batched statements | ⬜ Open |
| 5 | Documentation drift | ✅ **Done** |

**Of the 7 Priority 1 items: 4 closed** — including both that caused real
production impact — **2 do not reproduce**, and 1 is unverified. Nothing in
Priority 1 is both confirmed and open.

---

## Priority 1 — Make silent failures loud

Every production incident traceable to Mycel in this project had the same shape:
a misconfiguration producing **no error**, just an absence of behaviour.

### 1.1 Unmatched routing key should not silently discard — ✅ Done

**What happened.** A flow declared `operation = "gallery-assets"`, an invented
value matching no routing key. Every delivery hit `findHandler() == nil`, logged
at WARN, and was `Nack(false, false)`-ed — discarded by a broker with no DLX. No
error reached the flow, no metric moved. Two engineers hit this independently in
the same week (one fixed it as DSO-258 without the other knowing).

**Resolved by `5b5f1ca`.** There is now a dedicated
`internal/connector/mq/undispatched` package. It logs at **ERROR** —
`"message dropped: no flow handles this key"` — with the comment stating the
reasoning outright: *"Error rather than warning: a message nobody handles is a
misconfiguration."* The event carries the patterns that were tried and a
`Consequence` field spelling out *"nacked without requeue; discarded unless the
queue has a dead-letter exchange."* Metric `mycel_messages_undispatched_total`
exists, and it is applied across all MQ drivers rather than just rabbitmq.

Better than what was proposed here: naming the consequence in the log line means
the reader does not need to know the DLX rule to understand the impact.

### 1.2 `dlq { enabled = true }` that provisions nothing — ✅ Done

**What happened.** Every consumer declares `dlq { enabled = true }`. Mycel only
provisions the DLX when it declared the queue itself, and all these queues
pre-exist. So no DLQ was ever created and final rejections were discarded, while
the config still read as protection.

**Resolved by `24f8e18`** via `dlq { external = true }` — an explicit assertion
that ops owns the dead-letter topology. The warning now names the flag that
silences it. A better answer than the "fail startup" originally proposed here,
because it distinguishes *"ops owns this"* from *"you forgot."*

### 1.3 Config that is structurally valid but semantically dead

| Mistake | Status |
|---|---|
| `to.params` used | ✅ Detected by `InertFlowAttrs` → `"configuration has no effect"` |
| Env var referenced with no default and unset | ✅ Fatal, and names the variable |
| Connector name with a hyphen (`magento-db`) | ❔ Does not reproduce |
| DB connector `port = env("PORT")` | ❔ Does not reproduce |
| `env()` inside a CEL transform | ⬜ Open, unverified |

**On the env-var case — the original version of this document was wrong.** It
asserted this was unhandled. It is handled, and well:
`internal/parser/env_missing.go` walks the HCL body for `env("NAME")` calls with
no default whose variable is unset, and `missingEnvHint()` appends them to the
connector registration error:

```
→ Missing environment variable "MERCURY_PRODUCTS_URL", required by connector "products_ms" (base_url)
```

The trailing parenthesis is the **attribute** the variable feeds, not the
connector type. It is **fatal**, not a warning, and is wired into `mycel check`
through `connectivity.go`. `mycel validate` also lists them, as a warning rather
than a failure, since validate legitimately runs in CI without the deployment
environment. That is precisely the scenario where `MERCURY_PRODUCTS_URL` being
unset made a cache-invalidation aspect a silent no-op for weeks — it would now
fail at startup and be named at validate time.

**Two of the three "open" items do not reproduce.** Both were re-tested against
the current binary rather than re-read:

- **`port = env("PORT")`** — a MySQL connector with `port = env("MY_DB_PORT")`
  set to `13306` dials **13306**, not 3306. The database factories coerce
  through `connector.IntFromProps`, which has parsed strings since v1.19.1. A
  non-coercing `getInt` did survive in the runtime, but only for rendering the
  startup banner, so a string port displayed wrong while connecting correctly.
  Fixed anyway in `c3efb41`.
- **Hyphenated connector name** — `connector = "magento-db"` validates, starts
  and dispatches normally. The dotted form `connector.magento-db = "..."` is a
  **hard HCL parse error**, not a silent no-op. If the no-op was real, the repro
  is still needed; the most likely candidate is a hyphenated name inside a CEL
  expression (`step.magento-db.x`), which CEL parses as subtraction. That would
  be a genuine and separate bug.

**`env()` inside a CEL transform is untested either way.** `env` is registered
only as an HCL function (`pkg/hcl/functions.go`), not in the CEL environment, so
the expected failure is a CEL compile error rather than a silent crash. If an
aspect really dies without a message, the bug is the swallowed error, not
`env()`.

---

## Priority 2 — A test harness ⬜ Open

**Still the largest remaining gap.** There is no `mycel test` subcommand, and no
way to answer "given this message, does this flow do the right thing?" without
deploying.

**What that cost.** To ship the gallery-assets consumer: validate the SQL by hand
against a production replica inside a rolled-back transaction, deploy to dev,
hand-publish a message through the management API, read pod logs, query the
database to confirm the row. A decent integration test — but not repeatable, not
in CI, and it will not catch a regression in six months.

**Proposal.**

```hcl
test "asset_upsert writes the gallery row" {
  flow    = "asset_upsert"
  message = file("fixtures/gallery-asset-update.json")

  expect_sql {
    connector = "magento_db"
    matches   = "INSERT INTO gallery_asset"
    times     = 1
  }
  expect_output { universal_asset_id = "5663f04d-..." }
  expect_disposition = "ack"
}
```

Minimum viable: connectors run in record mode; assert which statements/requests
were issued with which bound parameters, plus the final disposition (ack /
requeue / reject / filtered). Even without a database this would have caught the
routing-key bug, since the flow would never have been reached.

Second tier: fixtures for filters. `asset_delete` has still never executed its
DELETE in any environment, because no delete message has ever been published. A
harness is the only realistic way to cover that path.

---

## Priority 3 — Observability

### 3.1 Per-stage timings ⬜ Open

`mycel_flow_duration_seconds` covers the whole flow. When a message takes 7 ms
across 9 database round-trips, there is no way to see which stage dominates
without `--verbose-flow` and a restart — not an option in production.

`a39e455` added per-**flow** timing extremes and throughput
(`mycel_flow_duration_fastest_seconds`, `_slowest_seconds`, `_average_seconds`,
`mycel_flow_messages_per_second`), and `26af874` added `mycel_flow_drops_total`.
Genuinely useful — it removes the need to derive throughput by hand from log
timestamps — but it is a different axis. Per-**stage** granularity is still
missing.

Worth noting for anyone reading these gauges: they cover *successful* executions
only. `26af874` had to exclude drops after finding that a flow declining 8 of 9
messages reported a fastest of 14 µs — a filter short-circuiting, not work — and
a throughput of 9 where 1 message was processed.

### 3.2 `mycel version` ✅ Done

Registered in `c3efb41`. Previously the version appeared only in the startup
banner, which has rolled out of the log buffer on any long-running pod; finding
what a pod actually ran meant reading `/metrics` for `mycel_service_info`.

### 3.3 Startup dispatch + reachability summary ✅ Done

Two halves, both now closed.

**Connectivity.** `6707c18` made `mycel check` actually connect — it previously
created the runtime and reported success without opening a socket, so a database
on an unroutable address passed. `1444338` stopped it failing on connectors that
only listen, which had made it fail on essentially every config serving HTTP.

**Dispatch.** `7dd104c` — landed after the first pass over this document, which
is why it was recorded as missing — states at startup which messages each flow
will actually receive, and warns when every flow on a connector is narrowed, so
a delivery matching none has nothing to catch it:

```
INF dispatch: flow only accepts matching messages connector=rabbit flow=item_create
    operation=all.in.magento.q meaning="only deliveries whose key matches
    \"all.in.magento.q\" reach this flow"
WRN dispatch: messages matching no pattern will be DROPPED connector=rabbit
    patterns="\"all.in.magento.q\""
```

Run against this project's own consumers, products and collections raise the
warning and gallery-assets, prices and inventory do not — the exact distinction
that separates the flows carrying the §1.1 risk from the ones that do not. So
yes: this would have surfaced the routing-key bug at boot.

It reads configuration only and runs *before* connectors are dialled, so the
dispatch shape is visible even when the broker is down and startup is about to
fail. It is driver-agnostic — which connectors treat `operation` as a
subscription pattern comes from their own `SourceSchema`, so Kafka, Redis, MQTT,
CDC and file watch are covered without a per-driver list.

**One piece of the original ask is still open:** warning about filters that can
*provably* never match. What exists warns about a dispatch shape that can drop
messages, not about a CEL expression proven unsatisfiable.

---

## Priority 4 — Authoring ergonomics ⬜ All open

### 4.1 Reusable `step` and `to` blocks

Confirmed against the `reusableKinds` registry (`internal/parser/reusable.go:49`),
which holds exactly ten kinds:

```
dedupe  lock  semaphore  sequence_guard  coordinate
transaction  error_handling  accept  response  retry
```

`step` and `to` are not among them.

In gallery-assets, `asset_create` and `asset_update` needed the identical ~60-line
resolve step and ~40-line upsert. Unable to share them, the two flows were merged
into one `asset_upsert` with a compound filter. That happened to be defensible —
the two Magento transactions were byte-identical — but the decision was driven by
a tooling limitation rather than by the domain. **A missing feature is shaping
architecture**, which is the strongest argument for closing it.

### 4.2 SQL fragments

The gallery-assets URL derivation is a ~20-line correlated subquery repeated
**five times**, once per product slot, differing only in the bound parameter. It
is the least maintainable thing in that codebase and no language feature exists
to factor it out — `each` handles iteration on the write side, but there is no
equivalent for building a projection.

### 4.3 Batched statements

The prices flow issues 9 separate round-trips per message. At ~0,8 ms each that
is essentially all of the 7 ms. Batching would cut per-message latency close to
an order of magnitude. Not urgent — the consumer runs at under 1% of capacity
because the publisher is the bottleneck — but it is free headroom.

---

## Priority 5 — Documentation drift ✅ Done

`docs/reference/source-properties.md` previously listed `operation` as
**Required: yes** in the universal attributes table. For rabbitmq it is optional
and defaults to `"*"`. Following the docs produced exactly the silent-discard bug
in §1.1.

Now fixed and expanded into a dedicated explainer — *"Is `operation` required?"* —
with a table contrasting required-for-REST against optional-for-MQ. Generating
those tables from the connector schemas would make the class of drift impossible,
but the specific trap is closed.

---

## Landed since v2.12.0 that was not requested here

Worth recording, since it is adjacent:

- **`70a82b8`** — sync, cache and connector metrics. These had been defined,
  registered and documented from the start with no call sites anywhere, so they
  were permanently absent from `/metrics` — including a Grafana panel for cache
  hit rate that could never render. The sync metrics were also relabelled from
  `key` to `flow`: lock keys are evaluated per message, so recording them as
  declared would have grown the series set without bound.
- **`2f51e9f`** — dedupe lock metrics separated by a `purpose` label. Useful:
  gallery-assets acquires both a business lock and a dedupe lock per message, and
  they were previously indistinguishable.
- **`6707c18` / `1444338`** — `mycel check` connects for real, without false
  failures on listen-only connectors.
- **`4a0f2fb`** — every deliberate drop explains itself at debug level. Each gate
  already reported a stable reason, but only `on_drop` aspects ever saw it, so
  without one a message vanished with no error and nothing in the log:

  ```
  DBG message dropped by policy flow=only_big_orders source=api reason=filter
      decided_by="from { filter }" detail="input.total > 100" disposition=ack
  ```

  `decided_by` names the HCL block to go and edit and `detail` the expression,
  fingerprint or sequence numbers it was judging. Covers `filter`, `accept`,
  `dedupe`, `sequence_guard` and `coordinate` timeouts. The payload rides on the
  existing `MYCEL_PAYLOAD_SHOW` opt-in, since a dropped message is still customer
  data.
- **`26af874`** — drops counted separately from work. A declined message was
  recorded as `status="success"`, so a consumer filtering out most of its input
  reported full productivity and drops could not be graphed or alerted on at all.
  **This corrects existing series:** any flow with a gate will see
  `status="success"` fall and a `status="dropped"` series appear.
- **`dafb442`** — fixes a false positive introduced by the commit before it,
  which reported "no flow reads from this source" for any connector whose schema
  *allows* it to be a source. On this project's consumers that meant error-level
  noise for `db` and `magento_db` (write targets) and `rabbit_returns` (a
  publisher), at every startup, on configurations working exactly as intended.
  Whether a connector will consume is only knowable where it starts consuming,
  so the check now lives in the drivers.

---

## What is already good, and worth not regressing

- **Metrics.** Per-flow histograms, executions by status, goroutines, uptime,
  service info — scrape-ready with zero configuration. Better instrumentation
  than most hand-written services ever receive, and now materially better again.
  One caveat learned the hard way over this pass: several of these metrics were
  *defined but never recorded*, which reads identically to "this never happens".
  Worth checking a metric has a call site before building a dashboard on it.
- **The protection primitives.** Lock, sequence guard, content dedupe, retry with
  backoff, bounded requeue. Getting these right by hand is weeks of work and most
  teams ship a worse version.
- **Fan-out composition.** Multiple flows on one queue chained via
  `ChainEventDriven`, each filter selecting its own messages, sibling drops
  suppressed. Elegant, and it behaved exactly as documented under test.
- **Performance.** Nothing here is a performance complaint. 7 ms median across 9
  DB round-trips is essentially pure network time; runtime overhead is not
  measurable at this scale.

---

## Where Mycel fits, honestly

**Strong fit:** message → guard → transform → write, with standard protections.
This is the majority of integration work, and Mycel is genuinely better than
hand-written code for it.

**Poor fit:** flows with real branching or domain rules. Two independent signals
from this project: the WEBDEV-1365 availability matrix (Line Type × Order Type →
customer-facing message) was deliberately implemented in the TypeScript connector
rather than in Mycel; and the gallery-assets URL derivation, which *was* forced
into Mycel, produced the least readable and least testable code in that
repository.

The distinction worth documenting is that Mycel is a **pipeline** runtime, not a
general-purpose one. Saying so in the README would set expectations better than
discovering it at the point where a heredoc has grown to 100 lines of nested SQL.