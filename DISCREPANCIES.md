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

## Unwired features — to implement

Found while reviewing unreachable code. Each is implemented and configurable and
does not run, so the configuration that asks for it is accepted and ignored.
They are listed rather than fixed in passing because each is a piece of work
with a decision or two in it.

| # | What | State | What it needs |
|---|---|---|---|
| U8 | **A cache connector as a flow destination, and the two aspect blocks that already need it** — next release | The cache connector implements Get, Set, Delete, DeletePattern, Exists and TTL, and implements none of `Caller`, `Reader` or `Writer`. This is wider than a missing destination. The `cache {}` block on an **around aspect** reads through `connector.Reader` and writes through `connector.Writer`, and the `invalidate {}` block on an **after aspect** deletes through `Call` — so all three type assertions fail against every cache connector and both documented blocks do nothing at all. Measured: an around aspect with `cache { storage = "cache", ttl = "5m" }` over three identical requests ran the flow three times and stored nothing, with no error and no warning. Redis is therefore reachable today only through a flow's own `cache {}` block, which is cache-aside over something else; never as a destination, and not from the aspects that advertise it. So a service whose job *is* the cache cannot be written: `from tcp → to redis(get/set/delete)` has nowhere to land | Implement `Call` on the cache connector, mapping the operation to the methods that already exist, and `Reader`/`Writer` so the aspect blocks work — one change fixes the destination and both aspects. It is the same gap that was fixed for HTTP in 2.11.0, where `isCallOperation → Caller.Call` was dispatched to and only exec, file, s3, grpc and graphql implemented it. Two things stay open beyond it and are design rather than wiring: a memory tier in front of a Redis one, with the cross-invalidation that a pub/sub currently does by hand, and a namespace chosen per request rather than fixed in the configuration — both wanted by the Mercury cache service this comes from |
| U4 | **A connector enforcing through the root auth block** — next release, with U8 | `auth.*` now carries what a connector learned, and what a connector can learn is a JWT, an API key or basic credentials on its own. The root `auth {}` block — users, providers, sessions, MFA, revocation — still takes no part in an incoming request: `Manager.ValidateToken` is called by nothing on that path, so a `provider` block validates nothing that arrives, and `examples/dynamic-api-key` does not work. `auth.Middleware`, with its path rules and roles, remains the one piece with no configuration behind it | Let a connector say it authenticates through the service's auth — `auth { type = "mycel" }` or the same by default when a root block exists — so that sessions, revocation and MFA state count for a request. Then `auth.Middleware` is either the shape that takes or it goes |
| U9 | **An `accept` rejection on a synchronous source** | `accept` refuses a request and the caller gets `200` with a serialised internal struct: `{"Filtered":true,"Policy":"ack","MessageID":"","MaxRequeue":0,...}`. The block was designed for message-driven flows, where the policy names an ack or a requeue, and nothing translates that for a source that has a caller waiting. It matters more now that authorisation is written as `accept { when = "'admin' in auth.roles" }` — a refused caller should be told, not handed the internals | Decide what a synchronous source answers when a flow declines: a status with a body of its own, `403` when the condition read the identity, or something the block names. Whatever it is, not the struct |

## Open

None. Every entry has been resolved on the side the reader would expect it on.

Both directions of the schema/parser boundary are held by tests now, which is
what kept producing new entries: `TestConnectorSchemasMatchTheParser` writes
each attribute a schema declares into a connector block and parses it, and
`TestTheParserAcceptsNothingUndescribed` requires everything the parser accepts
to be declared somewhere. The two deliberate asymmetries, `auth_db` and
`ssl_mode`, are listed as aliases with the name they fold onto.



### Dead-or-bug candidates (parked by decision, 2026-08-12)

Two clusters of unreachable code. Neither gets tests until we decide which it
is, because testing code that should be deleted is the most expensive of the
three options.

| Where | Size | What it looks like |
|---|---|---|
| `pkg/errors` | 19 unreachable funcs | A typed-error package with exactly one consumer in the whole repo (`internal/aspect/executor.go`). Either it was meant to be adopted everywhere and never was — in which case the *absence* of its use is the bug — or it is dead. |
| ~~`internal/transform/functions.go` + the `ExpressionParser` half of `transformer.go`~~ | **gone** | The pre-CEL expression engine — 23 function types nothing outside its own file ever constructed — has been deleted. `internal/transform/native.go` now holds something else entirely, a CEL-to-Go conversion helper with nine callers. Entry kept so the next reader does not go looking for it | done |

The transform one matters for the coverage target as well as for tidiness: it
is ~127 uncovered statements in `functions.go` alone that are being counted
against a percentage while not being part of the running product.

### The push connector speaks a Firebase API that is gone

Found 2026-08-13 while covering it. `internal/connector/push` sends with
`POST {api_url}/fcm/send` and an `Authorization: key=<server_key>` header —
the legacy FCM HTTP API. Google superseded it with HTTP v1 and has
decommissioned it, so `type = "push", driver = "fcm"` cannot deliver
anything: the request comes back 404 from Google.

Not fixed here because it is a feature with a decision in it rather than a
defect to correct. HTTP v1 means:

- a different endpoint, `POST /v1/projects/{project_id}/messages:send`
- OAuth2 with a service account rather than a static server key, so the
  configuration gains credentials someone has to supply and the connector
  gains a token it has to refresh
- a different message shape: everything moves under a `message` object,
  and the platform-specific parts (`android`, `apns`, `webpush`) become
  explicit rather than the flat fields the legacy API took

The payload reading was fixed and tested in the same pass, and those tests
survive the migration: what a flow may write, and that all of it arrives.
What changes is the envelope it is put in.

The APNs side of the same connector is unaffected — it already speaks the
current HTTP/2 API.

### The shape U8 should take: a cache is a service, not a connector call

Decided 2026-08-13. Redis is a connector like any other and should be usable as
one — `to { connector.redis = "set" }` — but that alone is the small half of
the problem. What the Mercury cache service actually is, is a *tiered* cache:
memory in front of Redis, with each tier holding the same keys for a different
length of time. Wiring that by hand means a flow per tier, a promotion path
written by the author, and an invalidation fan-out they have to remember on
every write. That is the plumbing Mycel exists to absorb.

So the block declares the tiers and the runtime does the walking:

    cache "products" {
      tier "l1" { connector = "memory", ttl = "30s"  }
      tier "l2" { connector = "redis",  ttl = "6h"   }

      invalidate_via = "redis_pubsub"   # see below
    }

What the runtime owes it:

- **Read walks down and promotes up.** L1, then L2, then a miss. A hit in L2
  is written back into L1 with L1's own ttl, which is what makes the second
  request cheap and the tenth free. The tiers hold the same key, not different
  keys.
- **Write and invalidate go to every tier**, nearest last on invalidate so a
  concurrent read cannot repopulate a tier that was already cleared.
- **A ttl per tier is the whole point** — seconds in memory, hours in Redis.
  One ttl for the service would make the tiers pointless.
- **Cross-instance invalidation is the hard part and has to be in the box.**
  Every replica has its own L1, so a write on one leaves the other nine serving
  what they already have. The shared tier is the one that can carry the
  message: publish the invalidated key on a channel every replica is
  subscribed to. Mycel already has the Redis pub/sub connector for it, and
  doing this by hand is precisely what the service being replaced does today.
  A single-replica or memory-only service should not have to configure it.
- **A namespace chosen per request**, not fixed in the configuration, so one
  cache service can serve several callers without them seeing each other —
  the same key discipline the connector's `prefix` already applies, but
  evaluated per request.

The same declaration is what the two aspect blocks should use: an around
`cache { storage = "products" }` reads and writes through the tiers rather
than through one connector, and an after `invalidate { storage = "products" }`
clears all of them and publishes. That is why the aspect bug and the missing
destination are one piece of work — a cache the runtime understands, rather
than three places that each ask a connector for a method it does not have.

### Notes on the open ones

Nothing is open. Both directions of the schema/parser boundary are now checked
by tests, and the two remaining asymmetries — `auth_db` and `ssl_mode` — are
listed as deliberate aliases with the canonical name they fold onto.

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
| connector schemas vs the parser (#13) | 38 attributes across 12 connectors that a schema declared and the parser rejected | **the parser, uniformly — which only became visible once the schemas were unified.** Every one of the 38 is read by its connector, so each was a setting implemented, described, offered as a completion and impossible to write: CSV delimiters, PDF margins, queue heartbeats, TCP idle timeout, webhook's IP allow-list, SMTP's TLS mode. Most were a missing allow-list entry; four needed more — exec's `env` and the SQL `replicas` were parsed and discarded, webhook had grown a second retry vocabulary, its `multiplier` was dropped by a float64 type assertion, and exec read `workdir` while everything documented says `working_dir`. Fixing `env` also made an old choice reachable: the declared variables replaced the environment instead of adding to it, so a command given one variable lost PATH | this branch |
| the parser's allow-list vs the schemas (#14) | 19 connector attributes the parser accepted that no schema declared | **both bugs it implies, in the same list.** Four were read and undescribed, so a working setting was invisible to completions and `mycel add`; profiles — `select`, `default`, `fallback` and the `profile` block — were undescribed altogether. Twelve were read by nobody, eight of them MongoDB connection settings, so a replica set name or authentication database could be written and had no effect; those are applied now as the URI options they are. `max_pool`/`min_pool`, `address`, `origins` and `wsdl` were removed instead, each having a working spelling elsewhere or none. `introspection` was the one that mattered: a GraphQL server publishes a map of everything it can do, and asking for that to be off left it on | this branch |
| ~~the admin server's workflow endpoints~~ | `GET /workflows/{id}`, `POST .../signal/{event}` and `POST .../cancel` were mounted on the admin port with no authentication whenever a workflow engine was configured | **fixed on this branch.** They have their own listener, served only when a `workflow { api { } }` block asks for them and never without an `auth` block; the admin port is refused outright. `auth` is the connector auth block, checked by the same code — extracted from the REST connector into `rest.Authenticator` rather than written a second time. Breaking for anyone who was using them: a `workflow` block alone no longer serves them. Found alongside it: stopping the workflow engine twice panicked the process on a closed channel | done |
| `internal/connector/email` | an `Email` carries `Attachments`, the payload reader fills them in from a flow's message, and none of the three providers send them | **half a feature, recorded rather than filled in.** SMTP builds `multipart/alternative` for the text and html parts and never `multipart/mixed`; the SendGrid payload and the SES input do not mention attachments at all. Nothing offers them to a user either — no schema, no documentation — so this promises nothing today, which is why it is a note rather than a fix. It matters because the reader was repaired earlier on this branch, so a flow can now put an attachment on a message and watch it go nowhere. Implementing it is three encoders and a decision about size limits: the user's call. A test in `provider_payload_test.go` fails when SendGrid starts sending them, so this note goes away with the change that makes it wrong | open |
| `examples/mq/flows.mycel` + 3 reference pages | consumer flows reading `input.order_id` and `id_field = "input.payment_id"` for a message-queue source | **the writing, not the code — and deliberately not the code.** A delivery reaches a flow wrapped: `{body, headers, properties, routing_key, exchange}`, built unconditionally by the RabbitMQ consumer, so everything reads `input.body.*`. The production consumers already do this (`examples/dedupe`, copied from the real one), which is exactly why the envelope must not be flattened to make the examples true: that would break every working consumer. The RabbitMQ section of `source-properties.md` had it right all along; the two consumer flows in the mq example and the three `id_field` samples did not. Found by the first integration test that read a field off a message | this branch |
