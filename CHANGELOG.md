# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [3.3.0] - 2026-08-28

### Added

- **`encoding` on a flow's `cache {}` block, so a namespace can be shared with a service that is not Mycel.** Entries were `json.Marshal` on the way out and `json.Unmarshal` on the way in with nothing to say otherwise, which is fine while Mycel owns the namespace and stops being fine during a migration — exactly when a cache is most likely to be shared, because the service being replaced is still up and still reading and writing the same keys. What happened then was not incompatibility but mutual destruction: Mycel read an entry it could not decode, treated that as a miss, did the work, and wrote plain JSON over the key; the other service failed on its next read and wrote its own format back. They took turns destroying each other's entries and the only visible symptom was a cache that never seemed to hit. The codecs are applied left to right on the way out and reversed on the way in — `["json", "base64", "gzip"]` reads and writes what a service storing `gzip(base64(JSON.stringify(v)))` does. Absent means `["json"]`, byte for byte what every cache did before. A named `cache "..."` block can carry it too, so flows sharing a namespace share its format; a flow declaring its own wins. The chain is checked when the configuration is read, not on the first write. Interop is pinned by a golden produced by a real Node process, so it is checked on every run without Node present, plus a live check when there is one.

- **`keys_from` and `patterns_from` on `after { invalidate }`, for a key set whose size the configuration cannot know.** `keys` and `patterns` are one key out per template in: the values in a key vary with the message, the number of keys does not. So a flow that has to drop N entries — every store view of a product, every rewrite path it has had, every variant of a parent — could not be written at all, and aiming a template at a list did not fan out but rendered Go syntax into the key. A wildcard is not a substitute when the members diverge: one broad enough to catch them all also deletes unrelated entries, and one narrow enough to be safe misses exactly the ones that matter — over-invalidating and under-invalidating at once. The new attributes take a CEL expression yielding a list of strings, evaluated against `input.*`, `output.*` and `step.*`, because the list almost always comes from a query the flow just ran; the result is unioned with the static list and deduplicated. Reaching `step.*` there is new plumbing: the `after` block runs once the handler that gathered the steps has returned, and the write handlers take their context by value, so what a step found could not travel back out of them. They are separate attributes rather than a second accepted shape of `keys` because the two cannot be told apart — HCL refuses a bare `step.paths.map(r, ...)` outright, so the expression has to be quoted, and a quoted CEL expression and a quoted key template are the same thing to the parser.

### Fixed

- **A cache entry that cannot be decoded is no longer counted as a hit, and no longer passes in silence.** `RecordCacheHit` fired before the decode was attempted, so an entry Mycel could not read was reported as a hit while the flow did the work — the hit rate was overstated in exactly the case where something was wrong. And the error was returned to a call site that dropped it, so from outside, "the key was not there" and "the key was there and I could not read it" were the same event; the second is the one worth knowing about, since it means a corrupt entry or a key written by something that encodes differently. It is now its own series, `mycel_cache_decode_errors_total` — neither a hit nor a miss the cache could fix by being warmer — and a warn line naming the flow, the cache and the key. It is what makes the interop problem above visible while it is happening rather than after.

- **An update no longer takes the record's id away from everything downstream.** The id addresses the row, so it is a filter rather than a column to set — and it was being deleted from the *input* to keep it out of the payload, which also took it away from the transform, from a step's `params`, and from the `${input.id}` in an `after { invalidate }`. That last one is the pattern the caching guide shows for exactly this verb, and it silently built `product:` instead, leaving the entry the write had just made stale alive while reporting success; the example in this repository invalidates a static key, which is what working around it looks like. A step naming it failed outright with `no such key: id`, and on a read the identical reference resolved, so the two behaved differently for no reason an author could see. The id is kept out of the payload instead — and only where the payload *is* the request, so a flow that states what to write can now name it.

- **A cache key template resolving to a list says so.** `${input.paths}` aimed at a list rendered Go's own syntax into the key — `url-rewrite-[a b c]` — then deleted that and reported success. The same shape as the query-string `%v` fixed in 3.2.2, except here it now warns and names the attribute that does the job.

- **`invalidate_on` on a named `cache` block is declared in the schema.** The parser accepted it and the schema did not describe it, so completions never offered it and generated documentation never mentioned it. The parity test only reads schema → parser, which is why an attribute the schema omits stayed invisible to it.

## [3.2.2] - 2026-08-27

### Fixed

- **A write to an HTTP destination no longer puts the whole message on the request line.** `Write` appended its filters to the URL for every method, including the ones that carry a body — and for a flow writing to HTTP the filters hold the inbound message, so the message went out twice: once as the body that mattered, and once as a query string nobody read. It stayed invisible while messages were small. The write succeeded, the receiver took the body, and the only symptom was a request line that grew with the message until it passed the front-end proxy's limit and came back `414 Request-URI Too Large` — from the proxy, naming infrastructure rather than the client, on an endpoint that accepted a large *body* on the same request without complaint. Reported at ~22 KB after months of ~2 KB messages working. `Call` had always split body from query correctly; `Write` uses the same rule now, and both read it from one place so they cannot drift apart again. (Thanks to @federivo for the report, which included the diagnosis.)

- **The integration stack no longer starts Mycel against a MySQL that is not listening.** The healthcheck pinged `-h localhost`, which the MySQL client reads as "use the unix socket" — and during initialization the image runs a temporary server on that socket to apply the seed scripts, so the ping was answered while nothing was listening on TCP. Measured at **5.4 seconds** of false-healthy against a `5s` probe interval: the container could report ready on its first probe inside that window, compose would start Mycel, and Mycel would exit on `connection refused` dialling 3306 — correctly, since a connector that cannot reach what it was configured for is a startup failure. It failed one CI run in four or so and passed on re-run, which is what a timing race looks like. The check now uses the transport everything else uses.

- **A structured value in a query string is JSON rather than Go syntax.** Query values were rendered with `%v`, so a nested map arrived as `map[a:map[b:c]]` — not a format any receiver can decode, which made it not merely redundant but meaningless. Structured values are JSON now; a `nil` is left out instead of being sent as the four characters `<nil>`; and the parameters are sorted, so the same filters always produce the same URL rather than a different one per request, which defeated any cache keyed on it and made two identical requests look different in a log.

## [3.2.1] - 2026-08-27

### Fixed

- **An apostrophe in a SQL comment no longer unbinds every parameter after it.** The binder that rewrites `:name` placeholders into what a driver accepts knew about string literals and nothing else, so a comment was read as if it were code: `-- the item's parent` opened a literal that never closed, and every placeholder past it reached the driver as literal text with no argument bound. The statement failed with `missing named argument "sku"` — naming the parameter, never the comment — and `mycel validate` does not execute SQL, so it passed a query that could not run. It went wrong the other way too, and that one is quieter: `-- ratio:sku` had its colon bound as a placeholder, so a comment consumed one of the statement's arguments and everything after it shifted by one. The scanner now knows the lexical structure that can hide a colon or a quote — line and block comments, string literals, quoted identifiers — and it is one scanner rather than three near-identical copies, one per driver, differing only in whether they wrote `?` or `$1`. Each dialect's own rules come with it: MySQL's `#` comments, its backslash escapes and its requirement that `--` be followed by whitespace; Postgres's nested block comments; SQLite's three ways of quoting an identifier. The same reading now answers "which placeholders does this statement carry", which is used to publish GraphQL arguments — a colon in a comment used to become an argument nothing could ever fill.

- **A flow behaving as written no longer logs as though it were misconfigured.** Deciding not to emit a coordinate signal — because `signal.when` said no, or because the message was dropped before it reached `to` — was reported once by the code that knows the reason and then again by the emitter as `coordinate.signal.emit evaluated to empty key`, which reads as a fault in an expression that is doing its job. The two are now told apart: a deliberate decision is silent at the emitter, and an emit expression that evaluates cleanly to nothing still warns, from the code that can name which expression it was.

## [3.2.0] - 2026-08-27

### Added

- **`compare_when` on the `dedupe` block — dedupe that stops trusting its own cache once the record it describes is gone.** A stored fingerprint says the content was written once; it does not say it is still written. Nothing in dedupe observes the downstream record leaving by a path the flow never sees — an admin deleting it by hand, a restore from an older backup, a data fix — and there is usually no delete flow to clear the fingerprint when it happens. The re-send meant to repair the damage then matches a fingerprint describing something that no longer exists and is acked in milliseconds having written nothing. The optional predicate gates the comparison, and only the comparison: false means the stored fingerprint is not consulted and the message cannot be dropped, while a successful write still commits the new one, so the next message can be. Evaluated against `input.*` and `output.*`, the same scope as the projection, which is what puts a `step` result within reach of it. Absent means always compare, so nothing changes for a flow that does not set it. It fails open — a predicate that cannot be evaluated, or that does not return a boolean, warns and processes the message, the same direction the cache-error path already takes.

  The check has to go here and not in `fingerprint {}`, which is where it naturally gets tried first. A projection field is symmetric: it fires when the record appears as well as when it disappears. And the fingerprint committed after a write is the one computed *before* it, so on a create the stored reading of "does this exist" is `0` by definition while every later message computes `1` — real duplicates stop being suppressed, and the deletion the field was added to catch still matches and still gets dropped. Both directions land backwards. `examples/dedupe` now carries a flow using the gate, the reasoning is in the attribute's own doc comment, and a test runs the wrong version to show what it does.

### Fixed

- **A dropped message no longer emits its coordinate signal.** A signal asserts that the flow applied its effect, and a drop short-circuits before `to`. From the coordinator's side the two were indistinguishable: `dedupe` and `sequence_guard` both return their filtered result with a nil error, and the emit fires whenever the inner call returns no error, so a suppressed message told everything waiting on it to proceed. In the case that found this, a style dropped as a duplicate emitted `parent_ready` anyway; its twelve child items then ran against a parent that did not exist, ten writing nothing and two failing on a foreign key. It could not be worked around in configuration either — the transform has already run by the time `signal.when` is evaluated, so the output it sees on a drop is the same one it sees on a write. A `filter` or `accept` rejection returns before the sync wrappers and was never affected.

- **A step naming a read verb where a table goes no longer looks like a missing table.** The parity test over the examples skips the write verbs by name and read `operation = "query"` as a table called `query`.

## [3.1.0] - 2026-08-26

### Breaking

Three things that used to be quiet now speak, and each can change what a
running service does. Read these before upgrading.

- **A `schema_registry` block on a Kafka connector is refused.** It parsed, built a client, logged "schema registry enabled" — and nothing was ever serialised through it: the client's three methods have no call sites, and messages are written and read by the ordinary JSON codec whatever the block says. So a connector configured for Avro put JSON on the topic, and every consumer expecting the registry's wire format read something else. The documentation described subject naming strategies, three formats and per-topic schemas, none of which existed.

- **A second factor Mycel cannot provide is now refused at startup.** `mfa { methods = ["sms"] }` was accepted, counted towards `min_factors` and never dispatched on; `mfa { sms { } }`, `mfa { email { } }` and `mfa { push { } }` parsed into fields nothing reads. Enrolment offers TOTP and WebAuthn whatever any of that says — so a service could be configured for SMS two-factor, start cleanly, and have no second factor at all. A configuration naming one of them now fails to start and says which two are provided.

- **A dropped message answers differently.** Where a gate — `filter`, `accept`, `dedupe`, `sequence_guard`, a `coordinate` timeout — turned a request away, the HTTP response was Mycel's internal struct: `{"Filtered":true,"Policy":"ack","MessageID":"","MaxRequeue":0,"Reason":"accept","Detail":"…"}`. It is now `{"status":"dropped","reason":"accept"}`. Anything parsing `Filtered` has to be updated. Queue consumers are unaffected: they read the value inside the process, not over HTTP.

- **A `response` block on a flow whose destination is a transaction now runs.** It was ignored, so such a flow replied with the write's own row counts. If the block references a field the transaction result does not carry, the request now fails with a 500 where it previously answered — a transaction exposes `output.affected` and `output.captured.<name>`, not the row it wrote. `validate { output }` starts being enforced on these flows for the same reason.

- **`hash_sha256` returns a different value.** It was a 64-bit djb2 hash under that name; it is SHA-256 now. Anything that stored or compared its output — a dedupe fingerprint, an idempotency key — recomputes once after the upgrade, so expect one round of cache misses.

### Security

- **The image scans clean.** It carried twenty findings, two of them HIGH — `CVE-2026-14456` against `libcrypto3` and `libssl3` — and the patch was one fetch away the whole time: `alpine:3.22` bakes in openssl 3.5.7-r0 while the repository it points at has 3.5.8-r0. A base image is built once and then sits there, and nothing in this Dockerfile brought the packages it already had up to date. `apk upgrade` before installing, and the count is zero. The base also moves from alpine 3.22 to 3.24 — which changes nothing a scanner reports, both come out clean, but a newer branch has a longer runway of patches ahead of it, and once a branch stops receiving them there is nothing left to upgrade to. The same twenty findings are in the published 3.0.0 image and clear on the next build.

### Added

- **Examples are held to the same standard as tests.** Three parity tests read the connector registry and the flow schema and name the parts no example uses — an example is how a feature is seen, and because the harness runs the commands in every README, how it is exercised end to end. On their first run: `async`, `idempotency`, `mq/kafka` and `pdf` had no example at all, and five of the twelve block kinds that can be named and reused were shown by nothing. All are closed.

- **`examples/async-jobs`** — a report that answers `202` with a job id rather than holding the connection open, and an order whose retry carries an `Idempotency-Key` and does not write twice.

- **`examples/kafka`** — the round trip: posted over HTTP, published to a topic, consumed back out of it and written to a database, by one connector with both a producer and a consumer block. Kafka's connectors were written out in another example's README and no configuration in the repository declared one.

- **`examples/pdf`** — an invoice from a database row and an HTML template, both ways the connector produces one: `generate` hands the bytes to the caller, `save` writes the file.

- **`examples/kafka-sasl`** — credentials a broker actually checks, on both halves of the connector. The test stack's Kafka grew a second listener that authenticates, so the `sasl` block is presented to something that verifies it rather than merely built into a mechanism: the right credentials get in, the wrong password does not, and neither does presenting nothing.

- **Every block a connector takes now has an example that runs.** The allow-list the coverage test keeps is empty. It held six — `tls`, `sasl`, `schema_registry`, `cluster`, `sentinel`, `ssh` — each with a note saying the test stack had nothing to point them at. Five were a container away: a TLS listener on the mock server, a SASL listener on the broker, a Sentinel, a three-node cluster, and an SSH server that will run a command. Every one of them, once there was something to run against, turned up a bug in the code it exercised. The sixth serialised nothing and was taken out of the language.

- **`examples/tls`** — how Mycel decides whether to trust the service it is calling: the same HTTPS endpoint verified against a certificate authority you name, against the machine's trust store, and not at all. Nothing in the test stack spoke TLS, so the `tls` block — which five connectors have — could not be exercised anywhere; the mock server now serves the same handlers over HTTPS with a certificate it signs itself and hands out at `/ca.pem`.

- **`examples/transforms`** — a contact arriving in whatever shape a form or a partner sent it and leaving in one shape. Fifteen of the thirty-five CEL functions appeared in no example at all; these are the ones a first transform reaches for.

- **`examples/pdf`, `examples/kafka`, `examples/async-jobs`** and the seven blocks `examples/reusable-blocks` was missing — see above.

- **The GraphQL selection helpers are shown working.** `requested_fields`, `requested_top_fields` and `field_requested` let a flow read the query's field selection rather than be optimised by it; `explainProduct` in `examples/graphql-optimization` reports what it was asked for.

- **`examples/redis-cluster` declared none of what it described.** Its three connectors were all plain `url` connections whatever their names said — a cluster is named by its nodes and Sentinel is asked which server is master, and neither is one address. The README had it right; the configuration beside it did not.

- **`examples/auth` shows what it configures.** Its security blocks were two of six, its hooks none of eight, and the tables it needs were a block of SQL in the README for the reader to paste — so nothing kept them in step with the configuration beside them. It now writes the column mapping, impossible travel with a geoip source, device binding, progressive delay, IP rules, per-endpoint rate limits, account linking, an endpoint override and three hooks with the flows they call, and its tables live in `migrations/` like every other example's.

- **The `auth` block is described.** Six of its fourteen children were a name and a doc string with nothing in them — `jwt`, `social`, `sso`, `provider`, `account_linking`, `endpoints` — so completions inside them offered nothing and `mycel export` had nothing to export. All six are transcribed from the structs the parser decodes, along with `base_url` and the four `security` children that were missing (`brute_force`, `replay_protection`, `ip_rules`, `rate_limit`).

- **`driver` is declared by the five connectors built from it.** A GraphQL connector is a server or a client depending on that attribute, and so is a gRPC one and a TCP one; an exec connector runs locally or over SSH by it; a CDC connector cannot be built without it. None said so in its schema, so `mycel add` did not generate it and the editor did not complete it. CDC's is now required, which moves the failure from connect time to `mycel validate`.

- **`examples/reusable-blocks` shows all twelve kinds**, including a flow that declares no policy of its own: the key it locks on, the ceiling it waits under, the order it observes, the sequence it refuses to go backwards on and the statements it writes are every one of them a reference.

### Fixed

- **A flow whose destination is a transaction never got to say what it answers.** The transactional write returned straight out of the dispatch, past everything that happens to an answer on its way out — so a `response` block was parsed, offered completions and ignored, and the flow replied with the write's own row counts. `validate { output }` was skipped for the same reason. **This can turn a silent no-op into a 500**: a response block referencing a field the transaction result does not carry now fails instead of being discarded. A transaction exposes `output.affected` and `output.captured.<name>`.

- **A dropped message answered an HTTP caller with Mycel's internal struct**: `{"Filtered":true,"Policy":"ack","MessageID":"","MaxRequeue":0,…}` — Go field names over the wire, fields that mean nothing to an HTTP client, and a `Detail` carrying the very expression that rejected them. It is now `{"status":"dropped","reason":"accept"}`; the requeue counts a queue consumer needs and the detail meant for the log stay inside.

- **`mycel migrate` could not create a database whose directory did not exist** — "unable to open database file: out of memory", SQLite's words for it. The data directory is gitignored, so that is the state of every fresh clone, and `mycel migrate` is the first command most example READMEs tell you to run. The connector created the directory on the way up; migrate opened the bare path. Both now build the address the same way, so migrate gets the busy timeout and foreign keys too.

- **`exec { driver = "ssh" }` could not work in the official image.** The connector runs the `ssh` client rather than speaking the protocol itself, and the image had none — so a flow using it answered `exec: "ssh": executable file not found in $PATH`, a 500 per request from a service that had started cleanly and reported the connector ready. The image now carries the client (which adds no vulnerabilities to it — the scan is identical with and without), and a service whose image lacks one refuses to start with a line naming what to install.

- **Writing a `sentinel` or `cluster` block on a cache connector now chooses that mode.** Which client got built was decided by a `mode` attribute that neither the cache page nor the redis-cluster example mentioned, so a connector written the way they showed — a `sentinel` block and nothing else — was built as a standalone one and refused with "redis standalone mode requires 'url' or 'host'", which sends you looking for the wrong thing. `mode` remains as an explicit override.

- **A Kafka consumer with SASL and no TLS never presented its credentials.** The mechanism was attached to the reader's dialer only inside the TLS branch, so against a SASL_PLAINTEXT listener — how an internal broker is usually reached — the group coordinator lookup answered EOF and the consumer read nothing for as long as the service ran, logging "Unable to establish connection to consumer group coordinator". The producer beside it had it right, so the same configuration published happily and consumed nothing.

- **`federation { enabled = false }` did nothing.** The attribute was parsed into the config and never read: the GraphQL server enabled federation unconditionally, so a server told not to federate still published its whole schema through `_service { sdl }` — which is the one reason anybody writes that setting.

- **An HTTP connector whose TLS could not be built started anyway.** The build error was discarded and the connector fell back to the default transport, so a mistyped `ca_cert` path meant verifying against the system roots instead of the CA that was named, and a client certificate that would not load meant connecting without one. Both look like working TLS from outside. It now refuses at startup.

- **The schema offered values the code rejects.** `track_by` and `key_by` were listed as accepting `both` where the runtime wants `ip+user`, and `match_by` was missing `phone` — so a completion suggested a setting that would be refused. A test in `internal/auth` compares the two: the values a schema attribute offers must be the values the struct comment beside its hcl tag says are understood.

- **`coalesce` was not the alias it is documented as.** `default(input.missing, "x")` is rewritten with a `has()` guard so it survives a field that is not there — the case the function exists for — and `coalesce`, the same function under the other name, was not: it failed with "no such key".

- **The `response` block dropped structured values.** Its rules were converted with a shallow `val.Value()`, which for a list or a map hands back CEL's own wrappers — they JSON-encode as `{"Adapter":{}}`. A flow shaping its output with `response { expensive = "input.items.filter(x, x.price > 50)" }` answered 200 with the data replaced by an empty struct. Every other rule loop already unwrapped; this one was the exception, which is why `transform` blocks were unaffected.

- **`hash_sha256` was not SHA-256.** It returned a 64-bit djb2 hash, hex encoded, under a comment saying to use `crypto/sha256` in production — while the reference documents it as "SHA-256 hash (hex encoded)" and the transforms guide shows it hashing a password. It is now SHA-256. **This changes the value the function returns**, so anything that stored or compared its output — a dedupe fingerprint, an idempotency key — recomputes once after upgrade.

- **A request body that does not parse is now a 400 instead of being treated as empty.** The decode error was dropped, so corrupt JSON was indistinguishable from no JSON: `POST /echo` with `{"broken":` answered 200 and echoed nothing, and the same body on a flow with a transform came back as a 500 blaming a missing key. An empty body still reaches the flow, as before.

- **A webhook delivery that is not the JSON it claims is now refused.** A truncated body — the shape a connection cut in flight produces — got `{"received": true}` back with a nil payload. Providers retry on a non-2xx and only on a non-2xx, so answering 200 discarded the event permanently.

- **Input the sanitizer turns away answers 400, not 500.** An oversized field came back as `500 input sanitization failed`, which reads as Mycel breaking — and 5xx is the retryable class, so a client posting a payload that can never be accepted kept re-sending it. Rejections are now marked (`sanitize.ErrRejected`) rather than recognised by searching the error text.

- **SQLite failed most writes under any concurrency.** The connector opened with the driver defaults: no busy timeout, so a write that found the database locked gave up immediately with `SQLITE_BUSY` instead of waiting, and the rollback journal, so readers and writers excluded each other. With ten concurrent writers, 195 of 200 writes were lost; under a 10-VU load test, 63% of requests failed. `busy_timeout`, `journal_mode(WAL)` and `foreign_keys` are now set on the DSN. SQLite is what the quick start and 36 of the examples use.

- **The Helm chart's default install could not pass its own probes.** Liveness and readiness hit `/health/live` and `/health/ready` on the app port, which only exists if a connector listens on it — and the chart's default configuration declares no connector. The pod never became ready and liveness restarted it, so `helm install mycel` with the defaults did not start. Health, readiness and metrics are served by the admin server whatever the connectors are doing, so `service.adminPort` (9090) is now a named container port and is what the probes target, what the Service publishes and what the ServiceMonitor scrapes — `/metrics` was being scraped off the app port for the same reason.

- **Foreign keys were enforced on one connection out of the pool.** `PRAGMA foreign_keys = ON` ran once after opening, and SQLite applies a pragma to the connection that ran it — so every connection opened afterwards had them off, and the same violating write was accepted or rejected depending on where it landed.

### Changed

- **`hash_sha256` is documented as a fingerprint, not a password hash.** The transforms guide showed it hashing a password on registration; one unsalted pass of SHA-256 is what an attacker with the table wants. Passwords belong to the auth system, which uses Argon2id.

- **The SQLite section says what the connector does.** WAL mode, the busy timeout, the two sidecar files a WAL database keeps next to it, and that an in-memory database is per connection — the pool opens several, so what one request writes the next will not find.

- **The benchmark suite is published** (`benchmark/`), minus the Linode token, Terraform state and past results. The README's performance section linked to files that only existed on the machine that wrote them.

- **The benchmark checks the answer before timing it.** Every k6 check in the suite was `status === 200`, which counts a 200 carrying an empty body as a success; the suite would have reported the `{"Adapter":{}}` bug above as 8,000 successful requests per second. `benchmark/scripts/preflight.sh` now asserts the bodies and `run.sh` refuses to measure a target that fails it. The targets also deployed `.hcl` files, which the runtime has not read since v1.18 — so they came up with no flows at all.

- **A benchmark result now says which build produced it.** The image is a Terraform variable instead of `:latest`, the targets record what they run, and each run writes `provenance.txt` next to Mycel's own flow counters.

## [3.0.0] - 2026-08-21

### Added

- **`constants` blocks.** A value declared once and referred to by name from anywhere in the configuration — a list of SKUs a queue consumer skips, a page size four queries share, a region read from the environment:

  ```hcl
  constants {
    skus_to_skip = ["SKU-1", "SKU-2"]
    page_size    = 500
    region       = env("REGION", "us")
  }
  ```

  `constants.page_size` reads the same in a `query`, which HCL evaluates when the configuration is read, and in a `filter`, which CEL evaluates per message. Which of the two you are writing for is not something you have to know: a constant that resolved in one and not the other would be worse than having none, because the failure is per-expression and nothing announces it.

  They hold literals — strings, numbers, lists, maps, `env()` calls — read once, when the configuration is. A value worked out from a message is what a transform is for. Declaring the same name twice is refused, naming both files, rather than letting the order files are walked in decide.

  `mycel add constants --value page_size=500` writes one, and `mycel validate` lists what it found.

  The name is `constants` rather than `const` because `const` and `var` are both reserved identifiers in CEL: an expression naming either does not compile, whatever the environment declares.

### Breaking

**The Go module path is now `github.com/matutetandil/mycel/v3`.** Go requires
the major version in the path from v2 onwards, so a v3 tag on a `/v2` module is
invisible to the module proxy — which is exactly how `go install` served
1.22.0 for months before 2.13.0 caught it. Install and update with:

```bash
go install github.com/matutetandil/mycel/v3/cmd/mycel@latest
```

Nothing else changes for it: the binary, the Docker images, the Linux packages,
the Homebrew formula and the Helm chart are unaffected, and Mycel is a runtime
rather than a library, so no configuration and no `.mycel` file names the path.

The rest of this section is one shape repeated. Every one of these is a setting
that was written, documented, and did nothing.
Honouring it is the fix — and honouring it is also what changes behaviour, so
each is listed here with what to do about it.

**Configurations that no longer start.** Each is refused with a message naming
what to write instead:

- A `push` connector with `server_key`. That is the FCM API Google retired in
  June 2024, so it had already stopped delivering; use `service_account_json`.
- `security { impossible_travel { enabled = true } }` with no `geoip` block, or
  with both a `database` and an `api`.
- A GraphQL `schema` block with neither a `path` nor `auto_generate`, or with
  both.
- `auth { hooks }` with `on_error = "fail"` on an `after_` hook, a hook naming a
  flow nothing declares, an `on_new_device` / `on_detect` / `on_max_reached` /
  `grant_type` this does not implement, or an `mfa` policy demanding more
  factors than there are `methods` to enrol them with.

**Services that start and behave differently.** Nothing to change unless the old
behaviour was what you wanted:

- `mfa { required = "true" }` now requires it. Accounts that never enrolled keep
  their sign-in and are refused everywhere else until they do — so a service
  that has been running with this set has users who will be asked to enrol.
  `grace_period` measures from when an account was created, which for existing
  accounts is in the past: set one long enough to give people notice, or leave
  `required` as `optional` until you are ready.
- `on_max_reached = "deny"` now refuses the new sign-in. It used to revoke the
  oldest session, which is the opposite of what the word says.
- `sessions { allow_list = false }` / `allow_revoke = false` now stop those
  endpoints being served at all — a client asking for one gets a 404.
- `password { history }` refuses a reuse, `max_age` expires a password (on a
  database only when the users `fields` block names `password_changed_at`), and
  `breach_check` makes an outbound request to api.pwnedpasswords.com on
  registration, change and reset. A service with no egress should leave it off.
- `security { device_binding }` and `impossible_travel` act. The `strict` and
  `standard` presets switch both on, so a service using a preset gains
  behaviour it was already asking for.

### Added

- **`as_list()`.** A lookup that matched one row hands back the row and one that matched several hands back the list — which is what makes `step.customer.tier` read naturally, and what makes a collection unusable: a template iterating an invoice with one line walks the fields of that line instead, so the answer changes shape with the data. `as_list` settles it: a list stays a list, anything else becomes a list of one, nothing becomes the empty list.

- **Impossible travel.** `max_speed_kmh`, `on_detect` and the `geoip` block were read by nothing, and the `strict` preset turns the feature on — so the strictest setting available and the weakest had the same effect. Two sign-ins are compared now by the straight line over the ground divided by the hours between them: any journey somebody could really make is longer than a straight line, so that speed is the slowest they could have been going, and calling it impossible is a claim that holds. An address is placed by a MaxMind City database on disk (pure Go, no CGO; the file is MaxMind's and is not shipped) or by any HTTP service with `{ip}` in its URL, whose answer is read leniently because every provider names latitude differently. Lookups are cached for an hour, a service that is down is not cached at all, and three things never happen because each is worse than a missed detection: an address nothing can place is not held against anybody, a local address is not looked up, and a geolocation service having a bad day does not stop people signing in. The guide's example named `alert_only`, which does not exist and never parsed.

- **Device binding.** `max_devices`, `trust_duration`, `on_new_device` and `fingerprint` were read by nothing — and the `strict` and `standard` presets turn device binding on, so a service that asked for no preset and one that asked for the strictest had exactly the same defence, which is none. An account's devices are remembered now, identified by what the request already carries: the browser string by default, optionally the network an address belongs to (not the address — a phone changes that between one street and the next) or an identifier the client sends. A device the account has not used runs the `on_suspicious_activity` hook with `auth.reason = "new_device"`, and `on_new_device` decides whether that is all, whether a second factor is required, or whether the sign-in is refused. Challenging an account with no second factor lets it through and says so, since there is nothing to challenge with and locking somebody out of a new laptop is the worse failure — as is refusing every sign-in because a proxy stopped forwarding the browser string. The guide's example named `fields` and `screen_resolution`, neither of which exists: the attribute is `fingerprint` and a server cannot see a screen.

- **Password reset.** `password_forgot` and `password_reset` were configured `Enabled: true` by default and no handler was ever registered for either, so the most-used account flow after signing in answered 404 while the configuration said it was on — and the guide said nothing about it at all. Both endpoints exist now. Asking for a reset answers the same way whether or not the address has an account, since answering differently turns it into a way to find out who has one. The token is handed to the `on_password_reset` hook, so the flow that knows how to send an email sends it — auth does not gain an opinion about delivery — and a service with nothing bound to that hook is told, rather than leaving somebody waiting. A token is good once, stored hashed, and lives for `reset_token_ttl` (an hour by default). Completing a reset ends every session the account had, and the password policy including `history` still applies. Tokens go in Redis when auth storage is Redis, because a link issued by one replica is unknown to the next.

- **Auth hooks run flows.** The `hooks` block was listed in the parser's block schema and never read into the configuration: `Config.Hooks` was nil however much was written in it, and nothing would have looked at it anyway — so a service asking to be told about a suspicious sign-in was told nothing, silently, which is the worst way for a security feature to be absent. A hook now names a flow, the way an aspect does, and the event arrives under `auth`: `before_login`, `after_login`, `after_register`, `on_failed_login`, `on_suspicious_activity`, `before_password_change`, `after_password_change`. A `condition` in CEL narrows when it runs. `on_error = "fail"` lets a `before_` hook refuse the thing it is attached to, and writing it on an `after_` hook is refused at startup rather than pretending it could undo one. A hook naming a flow nothing declares is refused by `mycel validate`.

### Fixed

- **An aspect's `cache` block did nothing at all.** It asked the connector to be a `Reader` and a `Writer`; a cache connector is neither — it has `Get`, `Set` and `Delete` — so the type assertions found nothing and the block read nothing, stored nothing and invalidated nothing, quietly, while looking exactly like the flow-level cache that does work. An aspect declared to spare an expensive call made every one of them. Both use the same interface now.

- **Using a TCP client after it was closed took the process down.** `Close` drains the connection pool and closes its channel, and a receive on a closed channel hands back a nil connection straight away — which was then used. A flow still in flight during a hot reload or a shutdown panicked the whole service with a nil dereference; returning a connection to the closed pool panicked with "send on closed channel". A closed client refuses now, saying so.

- **A step could not address a REST API by path.** `GET /customers/:id` was concatenated as written and every parameter went to the query string or the body, so fetching /customers/42 could not be expressed — which is why the documentation had invented `"GET /customers/${step.order.customer_id}"` in eight places: HCL interpolation of a CEL variable, which does not exist when the configuration is read, so the attribute could not be evaluated and the step ended up with no operation at all. Path parameters are filled from `params` now, in both spellings, and the one that names a segment is spent there rather than repeated in the query string.

- **An FTP or SFTP connector refused the runtime's own words for a read and a write.** A flow that does not name an operation gets `SELECT` on a read and `INSERT` on a write, because that is where the defaults come from — so fetching a file or uploading one from an ordinary flow was answered "unknown read operation: SELECT", which names nothing anybody wrote.

- **A semaphore written the documented way was refused.** The reference page's example writes `limit = 10` and says in the next sentence that `limit` and `max_permits` are the same setting under two names. Only `max_permits` was accepted, so the example did not parse. The claim is true now.

- **A Redis Pub/Sub connector ignored its `url`.** Every other Redis connector takes one and this driver read `host` and `port` only, so a connector written the documented way connected to localhost:6379 whatever the URL said, and said nothing about it.

- **A server that could not take its port reported itself ready.** `ListenAndServe` was called inside the goroutine, so a port already in use was an error logged from a background thread while startup carried on: the banner said "listening on :3000", the service said Ready, the health endpoint said healthy, and nothing was listening — a deployment that looked fine and answered nothing. The REST, WebSocket, SSE, SOAP and GraphQL servers now take their port before reporting success. (gRPC, TCP, the admin server and the workflow API already did.)

- **A second `service` block replaced the first entirely.** A project that put `service { admin_port = … }` in one file and the name, version and `workflow { }` in another ended up with a service that had no name, no version and no workflow engine, and the workflow endpoints were simply not there. Every `.mycel` file is merged and the file names are for the reader's benefit — so splitting that block is the obvious thing to do with it. It is folded field by field now.

- **A read flow could not render its answer through a connector that only writes.** Serving a generated document — the PDF connector's whole purpose — was refused with "destination connector does not support required operation", or, with steps, answered with the gathered JSON. A destination that can only be written to is a renderer, and a read flow now hands it the answer.

- **A step's `params` written as a block was ignored.** `enrich` declares it as a block and a step read it as an attribute, so the two siblings took opposite syntax and the wrong one was swept away silently: a step written the way the enrich block beside it is written ran its query with the parameter unbound. Both forms are read now.

- **An uploaded file was not where the documentation said.** The REST page describes `input.files.<field>`; files were only ever put flat on the input, so every transform written from the page failed with "no such key: files".

- **A flow with no destination ignored its transform and its enrichments.** The dispatch said "return transformed input" and returned the input, so a gateway — which is what a flow without a `to` usually is — echoed the request back, headers and all, instead of answering with what it built.

- **An enrichment flattened a list of one into an object.** It preferred the read path for a connector that can also be called, and that path turns a response into rows: a GraphQL field holding one element came back as the element and the same field holding two came back as the list, so a gateway forwarding it answered a different shape depending on how many rows existed upstream. An enrichment is a call, and is made as one where the connector allows it.

- **A GraphQL mutation returning `Boolean` always said `false`.** `deleteUser(id: ID!): Boolean!` is served by a flow whose result is `{"affected": 1}`, and nothing turned that into a boolean — so a row was deleted and the caller was told it was not. It now answers whether the write happened.

- **A GraphQL mutation could not return the record it created** when the flow assigned its own key. The read-back used the driver's last insert id, which for a table keyed by anything but an autoincrementing integer is the row's position, so it found nothing and GraphQL answered "Cannot return null for non-nullable field User.email" for a record that had just been written.

- **A sqlite connector with no `database` opened a file nobody named.** An empty value became `./data/mycel.db`, so a connector whose attribute was misspelled — `path` instead of `database`, which the integration-patterns guide showed and two tests in this repository copied — opened a database holding none of your tables, and every request answered "no such table" for as long as the service ran. The schema had always said the attribute is required and `mycel migrate` had always refused without it; only startup invented something.

- **A read flow with steps silently ignored its destination.** It answers out of its steps, so a `to` block was neither read nor written and nothing said so — the use-cases guide had a recipe built on the opposite belief that answered "no such key" for every field it meant to fill. The runtime now names those flows at startup and points at `enrich`, which reads first and adds to what came back.

- **`??` produced CEL that does not compile whenever a comma shared its expression.** Neither rewriter looked inside brackets, so `input.missing ?? join(input.tags, ",")` became `coalesce(input.missing, join(input.tags) : join(input.tags), ",")` — a syntax error at startup pointing at text nobody wrote. `join`, `replace`, `substring`, `pick` and `default` all take two arguments, so the shape is common.

- **`default()` did not work for the case it exists for.** CEL evaluates a function's arguments before calling it, so `default(input.description, '')` on a request carrying no description failed with "no such key: description" before `default` was reached — the documented way to give an optional field a value, failing on the optional field. It is guarded with `has()` now, the same way `??` already was.

- **A write with an empty payload became `INSERT INTO items () VALUES ()`**, and the answer was the driver's opinion of that — `SQL logic error: near ")"` — for a request that simply carried no fields. It is refused by name now, saying whether the request was empty or the transform produced nothing.

- **A write answered with the row's position in the table rather than the id the flow assigned.** A flow generating its own key — `id = "uuid()"`, which is the first thing the quick start teaches — answered `{"affected":1,"id":1}` for a record whose id is a uuid, so a caller that created something and fetched it by the id it was given looked up a record that does not exist.

- **A read flow's `transform` reached nothing.** On a GET it was applied neither to the request nor to the answer: it parsed, the editor offered it, the documentation names it as what `to` sees, and it did nothing. A flow's transform feeds whatever the flow has left to say — writing, the row; reading with a query of its own, that query's named parameters; reading a table by name, the answer. The middle case was sent unbound, which Postgres reports as a syntax error at `":"` and SQLite as a missing argument, neither naming the flow or the transform.

- **An `enrich` block on a read flow never called anything.** Enrichment ran only on the write paths, so the most natural place for one — read the product, add the price — fetched nothing and the fields it was to supply were absent. The enrich example, which is entirely read flows, could not do what it is about.

- **Writing the auth `endpoints` block turned every endpoint off.** It was parsed into an otherwise empty configuration, and an endpoint left nil is never routed — so the one thing the block is usually written for, moving the prefix, took login, register, refresh, me and the other twelve off the service, which then started and reported the auth system as initialised.

- **A step whose lookup found nothing left an empty list**, so every later `step.user.name` indexed a list with a string, and that is what the caller was told: `unsupported index type 'string' in list`, naming neither the step nor the empty result. Nothing found is now nothing, and a step that declares a `default` gets it.

- **A cache key could not use the interpolation the feature was built for.** `key = "user:${input.id}"` — what the guide shows, what `interpolateKey` exists to replace, and what an aspect's cache key already accepted — was refused inside a flow with "Variables may not be used here": HCL saw the `${...}` first and tried to resolve `input` as a variable it does not have. Two blocks called cache behaved differently. Keys, `invalidate_on`, and an `invalidate` block's `keys` and `patterns` are read as the templates they are now.

- **With mocking on, a connector that serves never started.** The wrapper offers the ordinary connector surface and nothing else, so the runtime's check for something it can start stopped matching — and the banner still said "listening", because that line is printed from the configuration. A service with mocks enabled came up, printed its routes and refused every request. Connectors that serve are left unwrapped now: mocking replaces what a connector answers when asked, and one that serves is not asked.

- **A `response` block could not see what the flow's steps gathered.** A flow that gathers with steps shapes its answer out of them, and only `input` and `output` were in scope, so a response naming a step was refused with "no such attribute(s): step".

- **A write carried no identifiers**, so a connector that files a record by something in the message — Elasticsearch takes the document id that way — could only ever use an id of its own invention.

- **The GraphQL field-selection optimisation asked for columns that were not columns.** The rewritten query was built from every top-level field, including the ones carrying a selection — `orders { id total user { name } }` asks for a user, not a column called user — so the database refused the query the optimisation had just improved: "no such column: user". Only fields with nothing selected inside them name a column now.

- **`id` was a field type nothing agreed on.** The GraphQL converter has mapped it since it was written, publishing it as `ID`; the schema's list of field types never named it and validation refused it as unknown. So a type using it built a schema and could not be validated against.

- **The graphql-optimization example's third feature described batching Mycel does not do.** A field of an object type cannot have a flow — only `Query`, `Mutation` and `Subscription` can — so there is no per-row resolver, no N+1 and nothing to batch, which is why the dataloader package was removed. The example declared two nested entities nothing could fill, and its README compared query counts before and after a batching that never happened.

- **An inferred GraphQL argument was always published as a String.** The name was all that was read, so `user(id: 1)` against an integer column was refused with "Expected type String, found 1". Where a flow says what it returns, a field of that type with the same name now says what the argument is.

- **A GraphQL field published no arguments when its flow named them in the destination's query.** `query = "... WHERE sku = :sku"` names the parameter there and nowhere else, and only a step's params were read — so `product(sku: "ABC-123")` was answered "Unknown argument sku". Whether a mutation takes a typed input object is still decided by what its steps gave, so a mutation naming its columns as placeholders does not lose the input object it declares.

- **`INSERT ... RETURNING` ran on SQLite and its rows were discarded.** The question "does this statement return rows" looked only at the first word, while the branch it guards exists for RETURNING clauses — Postgres asked it properly, so the same statement behaved differently on the two drivers. It is how a mutation answers with the row it made.

- **The read-replicas example declared no replicas.** It is named for the feature and its comments say a SELECT goes to a replica round-robin; neither connector had `use_replicas` or a `replicas` block, so every read went to the primary. It runs against a real Postgres and MySQL now, with replicas declared. The CDC example runs too — its README has no commands because it is a source, so the row is written to the database it watches and the event it should have become is looked for, which turned up an `events` table whose columns matched only one of its three flows. Its replication slot is named from the environment as well: a fixed name meant two copies of the example could not watch the same database.

- **The configuration reference says it documents every block with all its attributes, and did not.** The whole `transaction` block was absent — a destination whose statements all commit or none do, in the language since 2.4 — along with `envelope`, a semaphore's `lease`, everything a named `operation` can carry beyond a SQL query, all nine attributes of the `param` block inside one, and the workflow API's auth block, which is required to wake or cancel a workflow. Three tests now hold the page to that sentence.

- **A database destination that names neither a table nor a query is refused.** A `to` block accepts whatever it is given — the parser sweeps what it does not know into a bag the connector reads by name — so `targt = "users"` passed `mycel validate`, appeared in the banner with an empty destination, and answered the first request with a SQL syntax error. Four examples in this repository named neither: three wrote raw SQL under `operation` and one a table name, and all four produced malformed SQL at the first request.

- **The push reference documented `device_token` for a connector that reads `token`**, so a notification written from it had no addressee — and listed one payload field of the eleven the connector reads.

- **Four connector settings were documented under names nothing accepts.** The tcp page listed `codec` for a connector that reads `protocol` (and `raw` among its wire formats, which is not one), and `address` for a client — which the parser refuses outright, while the page's own examples use host and port. The cache pool's `min_connections` is `min_idle`, and the SSE cors block's `allowed_origins` is `origins`. Every attribute a connector's page lists is now checked against the schema, the connector's source and the parser.

- **The S3 connector could not read or write from a step.** Its page lists both first, and `Call` — which is what a step reaches — handled neither: they existed only through the Reader and Writer interfaces, so they worked as a flow's source or destination and answered "unknown operation" from a step, while the file connector answered both.

- **The cache page documented three operations a flow cannot ask for.** `get`, `set` and `delete` were listed as a connector's operations, and a flow naming a cache as its destination is answered "destination connector does not support required operation". The page's own example does not use them either — a cache is reached through the `cache {}` block, and the page now says so.

- **Refresh token rotation did not invalidate the old token.** Rotation exists so that a refresh token is good once; the spent one was written to the denylist, and the denylist was read only when `security { replay_protection }` was separately switched on. So `jwt { rotation = true }` alone issued a new token and went on honouring the one it replaced, indefinitely. The refresh path consults the list whenever rotation is on.

- **A Mongo flow's `query_filter` filtered nothing.** On a read nothing consulted it, so a flow asking for the active users answered with every user — verified against a real server with one active document and one inactive, both of which came back. On a write it was handed to the driver as written, so `query_filter = { order_id = "input.order_id" }`, the form the reference shows, matched documents whose field held that text. Its values are resolved against the message now: `input.x` is evaluated, `:id` is the path parameter of that name, an operator is an operator and a constant is a constant.

- **A destination's `params` reached no connector.** They are documented as CEL expressions and mapped to `connector.Data.Params`, and nothing ever put them there. The S3 connector asks for the bytes in exactly that place, so a flow sending its message to a bucket was refused for want of content — which is also why the S3 connector now writes the payload when no content parameter is given, as the file connector does.

- **A fan-out that shapes each destination took the service down.** A flow with a transform on each `to` and none of its own left the evaluator unbuilt, and the first message hit a nil dereference — the shape of an upload writing bytes to one place and metadata to another. Its target was also resolved against the payload, which the per-destination transform has already replaced, so it wrote to a key called `input.filename`.

- **A step could not write.** Which of a connector's abilities a step used was decided by which interface it happened to satisfy first, and a reader was asked for before a writer — so for a database, which satisfies both, the branch that writes was unreachable. A step naming `INSERT` was dispatched as a read: nothing was stored, no error was raised, and the id a later step asked it for was never there. The saga executor already chose by the operation; that list is now in one place and both read it.

- **And a step sent the expression rather than its value.** The guide writes `body = { items = "step.cart.items" }`, referring to an earlier step, and that reached the service as those characters — a step writing to a database stored the words `input.name` in the column. `params` were evaluated and `body` was not; both are now, by the same code.

- **Every table an example names is now created by something the example ships**, checked by a test that reads each one with the real parser. Following a README only reaches the examples that need nothing but Mycel; most want a broker or a database server, and the same thing was wrong with all of them. It found nineteen more, including four that had been half-fixed: the aspects example creates products and its flows also serve users.

- **The examples are now started and driven by a test.** It follows each self-contained example's README the way a reader would — applying its migrations, running its commands in order — and asserts that the service starts, the routes exist and nothing falls over. Two commands are listed as not runnable in that setting, each with the reason, and each README says the same to a reader. This is what found most of the example bugs above, and it is what stops them coming back.

- **A flow could not write to a file, or to S3.** Both connectors declared `Read` and `Write` with shapes of their own — a pointer where the interface takes a value, a map where it returns a result — so neither satisfied `connector.Reader` or `connector.Writer`, and a flow naming one as a destination was answered "destination connector does not support required operation". The documentation shows exactly such a flow, and the files example had its upload and download commented out with a note blaming the parser.

- **`permissions = "0644"` made every file unreadable.** A file mode is octal, the way chmod takes it and the way the documentation writes it; it was read as a decimal number and handed to the operating system as one, so files were created as mode `0o1204` — `--w----r--`. The default in code was a Go octal literal and so was right, which is why only configurations that set it were affected.

- **A push connector delivered to nobody and reported it as sent.** The documentation addresses a room or a user the way everything else in Mycel refers to a field of the message — `target = "input.user_id"` — and nothing evaluated it, so the message went to a client of that literal name. Verified against a running service: a client that connected as `input.user_id` received the notification meant for user 42. SSE had a second layer of it, where `send_to_room` read the destination's target and its sibling `send_to_user` read only filters, which a `to` block does not set — so the documented form was refused outright.

- **A scheduled flow crashed the service at startup.** A flow triggered by the clock has no `from` block — which is what the documentation describes for `when`, and how the scheduled example is written — and the runtime read through it in three places, each found only after fixing the one before. Every one took the process down before it listened, so the example demonstrating scheduled jobs could not be started at all, and `mycel validate` reported the configuration as fine. The accessors on a `from` block answer for a flow that has none now, which fixes the class rather than the three call sites. A flow with neither a source nor a schedule is refused by name: nothing would ever run it.

- **A flow declaring `format = "xml"` answered in JSON.** The runtime put the format on the context it hands the flow, and the connector reads the request and writes the answer against the transport's own context, which that wrapper cannot reach — so the branch honouring it was never taken from either side. Fixing it showed the XML encoder had never been asked to encode anything real: it knew the two shapes its own decoder produces, and a database read answers `[]map[string]interface{}`, which matched neither, so a list of rows went out as `<root>[map[id:1 name:Widget]]</root>` with an XML content type. A list also wrote one root per row, which no parser will read.

- **`mycel migrate` picked a database when there were several.** The flag's help says the connector is auto-detected when there is only one; with more it took the first one declared, which is an accident of file order, so the tables could land in a database nobody meant to touch. It refuses and names them now, and reads `migrations/<connector>/` when that directory exists — which is what a configuration with two databases needs, and a flat directory cannot give.

- **Thirty of the thirty-six examples using SQLite shipped no way to create their tables**, and the database files are ignored by git, so they started, printed their routes and answered 500 to every request. They have migrations now. Among what that turned up: `examples/basic` — the one the documentation leads with — failed at every step of its own walkthrough, and each of its expected responses was wrong; the `validators` example applied none of the nine validators it declares, because the type that was meant to use them listed its fields plainly and the usage was in a comment; the headline aspect of the `aspects` example had never run, since `string(input)` has no CEL overload and it failed silently on every write; and `cache`, `rate-limit` and `state-machine` kept their data in `":memory:"`, where each connection in the pool gets its own, so nothing written by one request was there for the next.

- **Three stages of the pipeline ran without announcing themselves.** `dedupe`, `enrich` and `step` were named in the trace package, given virtual lines by the debug adapter and offered by an editor as places to stop — and nothing in the runtime ever recorded them. A breakpoint on one waited for an event that had no source, and `--verbose-flow` on a flow with steps did not show the steps running. All three are recorded now, wrapped at the point they decide. `response` was offered as a breakpoint and was not a stage at all; it is one now, around the shaping of the answer.

- **A breakpoint on `accept` could not be set at all**, and one at the destination of a reading flow never fired. The debug adapter's stage-to-line list had no entry for accept, which resolves to line zero — no line, and so no breakpoint. And an editor named the destination stage "write" for every flow while the runtime records "read" for a flow that reads. The stage order was wrong in both lists besides: `validate_output` before the write it validates, `dedupe` before the validation it follows. There is one ordered list now, measured against a running service rather than taken from the diagram, and the adapter and the editor both read it.

- **A flow serving HEAD or OPTIONS answered 500.** Both are offered for a flow's `operation`, the router registers them like any other method, and the banner prints the route — but the dispatch switch named GET and QUERY, so everything else fell through to the write path, where the default branch set the operation to INSERT. A `HEAD /items` flow was an attempted insert that failed for want of columns. Both are safe methods, so both read; the switch asks `IsRead` rather than repeating the list, which is how the two lists came apart in the first place. A test holds the runtime to the list an editor offers.

- **And with CORS configured, a flow serving OPTIONS was never reached.** Every OPTIONS request was answered as a preflight before the flow was asked. A preflight is an OPTIONS carrying `Access-Control-Request-Method`, which a browser always sends and nothing else does; anything else now goes to the flow. The permissive development branch also left HEAD out of the methods it advertises.

- **The editor checked four kinds of reference and passed over the rest in silence.** A switch with no default, written when there were four kinds; the schema now marks sixteen, ten of them the reusable blocks the documentation calls the recommended style. So a misspelt `use = "lock.per_order"` drew nothing at all while an undefined connector one line above drew a squiggle, and `mycel validate` refused both by name. The reusable kinds were not even named in the editor's package, which is how the gap stayed invisible. Reference checking is a table keyed by reference kind now, held to the schema by a test that names the attribute it found; lookups go through an index of every named top-level block, so a new kind of named block needs no second list edited to be resolvable.

- **And the reading that made an aspect's key work carried its quotes.** The source text of `"products:${input.id}"` includes the quote characters, so every key an aspect produced had a stray pair in it — consistently, so it worked, which is why nobody noticed: written and read under the same wrong name. A list read that way was worse: the whole expression came back as one string holding the brackets and every element, so an invalidation of two keys invalidated neither.

### Changed

- **`mycel validate` reports everything wrong in one run, and the runtime checks the same list.** The checks grew one at a time, each returning as soon as it found something, so a configuration wrong in five ways reported the first kind, was fixed, and reported the next on the following run — the experience each check avoids inside itself, recreated between them. They now run together and the summary says how many of each kind. More to the point, both `mycel validate` and startup read one list, so the command whose whole job is to tell you beforehand cannot come to disagree with what actually happens: a configuration that passes validate and then refuses to start is worse than either outcome alone.

### Fixed

- **Ten of the thirty-nine CEL functions the editor offered did not exist.** The completion list was written by hand and nothing checked it. Four were CEL's own string methods written as though they were calls — `starts_with(s, prefix)` rather than `"s".startsWith(prefix)` — two were base64 helpers that are `base64.encode`, two were JSON helpers that were never implemented, one was an md5 that is not there, and `min`/`max` are `min_val`/`max_val` over a list. A completion is a promise: accepting one produced a configuration that parses and fails when the flow runs, which is the worst moment to learn the editor was guessing. The list is corrected and extended — `format_date`, `substring`, `merge`, `pick`, `omit`, `flatten`, `reverse` and the GraphQL field helpers were missing from it — and every entry now carries a call that the engine compiles, so it cannot drift again.

- **An unset `env()` was only reported inside a connector, and never when validation failed.** The warning was written for connectors because one that cannot start reports a generic "requires X" and the variable behind it was the missing piece — but every block reads `env()` the same way, so `auth { jwt { secret = env("JWT_SECRET") } }` with the variable unset said nothing and produced a service refusing to start over something naming neither the variable nor the file. Every block is walked now. And the warning is printed **before** the errors rather than after a successful run only: `connector "db": needs "url" or "user"` on a file that says `user = env("DB_USER")` reads as a configuration mistake until you know the variable is empty, and until now that was exactly the case where the explanation was withheld.

- **`mycel plugin update` deleted the lock file before doing anything that could fail.** Resolving versions afresh means removing it, but it is also the only record of what a deployment is pinned to — and an update that fails writes no new one, since loading stops at the first plugin it cannot fetch and saves nothing. So a failed update left no lock file at all, and the next start floated every plugin to whatever resolved that day. The previous one is kept aside and put back, with a line saying what runs has not changed.

- **Every connector accepted every connector's settings, and quietly ignored the ones it does not read.** The parser keeps one attribute list for all twenty-odd connectors, so a `pool_size` on a database whose pool is a block, a `url` on a REST server that only listens, and a `path` on SQLite, which reads `database` — each parsed, each was stored, and none was ever looked at: the service started with the default the setting was written to replace. All three were in this repository's own examples, and the third meant a transactional-write example writing to a different file than the one it names. A connector given a setting it does not read now says so at startup. It is said rather than refused, because a schema missing a real setting would otherwise turn a working service into one that will not start — and the gRPC schema was missing two, `timeout` and the `keep_alive` block, both of which the factory reads and neither of which completions knew about.

- **A service running in production with certificate verification turned off said nothing about it.** `insecure_skip_verify` does not relax checking, it removes it: the connection stays encrypted and stops being authenticated, so anything that can answer for the address can read it. It is the setting people reach for to get past a self-signed certificate in development, and the one they forget to take out. The startup warnings — which exist to say what is fine on a laptop and expensive in production — covered SQLite and a missing auth block and not this. They cover it now, in production and in staging, naming the connector and what it costs rather than only calling it insecure.

- **A step's `on_error` took any word and treated most of them as `fail`.** Three are implemented — `fail`, `skip`, `default` — and anything else fell through to failing, so `on_error = "ignore"`, which is what somebody writing from memory reaches for, meant the opposite of what it says and took the whole flow down. `default` with nothing to default to did the same: a setting that reads as handled and behaves as unhandled. Both are refused at startup now, naming the flow, the step and the words that work.

- **A federated type with a key and nothing to resolve it said so at debug level.** A type carrying `_key` is one this subgraph tells the gateway it can resolve by reference; without a resolver the gateway routes those lookups here and gets nothing back, which reads as a null in the composed graph rather than as an error anywhere. It is a warning at startup now, naming the type and what to write — a flow with `entity = "<Type>"`, or one that returns it.

- **`bool(1)` was documented and CEL has no such overload.** The CEL reference is what somebody consults for what is available inside an expression, and nothing checked it. Every expression on those pages compiles now, and where the page states what one returns — that reference is written one expression per line with the answer in a trailing comment — it is evaluated and compared against that, so a function that stops taking what the page says it takes is caught rather than discovered by whoever wrote a flow from it. The conversion takes a string: `bool("true")`.

- **The facts in `docs/llms.txt` are counted from the code now.** That file is what an assistant reads before answering questions about Mycel, and its opening section is a list written to stop one inventing syntax — so a fact that stops being true there is repeated confidently, to somebody who has no way to check it, in the exact voice of documentation. Every countable claim is checked against the parser: how many blocks and attributes a flow can hold, how many inline blocks can be named and reused, how many connector types can be a source. So are the claims about syntax, each with a document the parser accepts or refuses accordingly — `output.` on the left, a constraint in braces inside parentheses, a CEL string literal or macro left unquoted, and that a flow needs nothing but `from`.

- **The README and the fifty-two example READMEs were as unchecked as the guide, and six of their blocks did not parse.** The README somebody lands on first still showed `from { connector.api = "..." }`, the shape that stopped parsing in 2.1. The s3 example documented `use_ssl` for a connector that decides encryption from the scheme in its endpoint; the workflows example a `connector` attribute for a block that names its storage; the gRPC one a `retry_backoff`; the two validator examples the `validate` constraint again. All three roots — the guide, the README, every example's README — go through the same test now, which refuses to pass if the walk stops reaching one of them.

- **Nothing checked that the configuration examples in the documentation parse, and a third of them did not.** The documentation is where somebody copies from, so a block that does not parse is a person following the instructions and getting an error. Of 551 blocks, 134 are snippets never meant to stand alone and **49 of the rest were refused by the parser** — an attribute renamed and never updated on the page, a CEL expression written without quotes, a constraint that does not exist. The TCP page documented `codec` for a connector that reads `protocol`, and both its examples were wrong in three ways each; the cache page documented `min_connections` for a pool that takes `min_idle`; the SSE page `allowed_origins` for a block that takes `origins`. All of them are fixed — the semaphore's `max_permits` documented as `limit` in four places, a CDC connector documented with a `connection_string` and a `tables` list it does not take, Elasticsearch with `addresses` instead of `url`, a SOAP server with a `path`, a `wsdl_path` and a `version` that do not exist, GraphQL subscriptions with a `transport` and a `keepalive`, `validate` where the constraint is `validator`. Slack and Discord documented `webhook` for connectors that read `webhook_url`; a push connector `provider` and `credentials_file` for one that takes a `driver` and a service account; a webhook connector a `headers` map, where an outgoing call is signed instead; a step `params = [input.id]`, where params are named; two pages `to { response }`, which is a block of its own; an aspect `condition`, which is `if`; a `security` block three names it does not have; a named operation a `params` list, where each parameter is a block; a lock a `storage = "connector.redis"`, which became an inline block in 2.0; a memory cache `max_size`, which is `max_items`; a database `pool_size`, where the pool is a block; and `from { connector.api = "POST /payments" }`, the shape that stopped parsing in 2.1. What is left is five blocks that do not parse on purpose — the troubleshooting guide leads with the symptom, and the input-and-output page shows the spelling HCL rejects next to the one it takes — and anything that joins them fails the test on the spot.

- **The schema named a constraint the parser refuses.** A field's custom validator is `validator`; the schema said `validate`, so anything completing from it — an editor, `mycel add` — offered a word that does not parse. The parser's own "the ones there are" list left out `validator`, `required` and `description` as well, so somebody who reached that error was told a list that did not contain the thing they were reaching for.

- **A type a flow validates against, and a flow an aspect invokes, were both unchecked.** A misspelt `validate { input = "usr" }` is a 500 on the first request through the door rather than a refusal at deploy. A misspelt flow name in an aspect's action is worse: an `after` aspect whose action fails is logged at warning level and the flow carries on, so an audit aspect pointing at a flow that does not exist writes nothing, for ever, while producing a line per message that nobody reads. Both are checked at startup now, which closes the last two of the seven namespaces this configuration can point into by name.

- **A type naming a validator that does not exist left the field unvalidated.** The reference is looked up when the flow runs and a name that is not in the registry is skipped, so a typo did not fail — it turned the rule off. That is the worst place in the language for a name to go unchecked, since a validator exists precisely to refuse input that should not get in. And the spelling the validators example documents, `validator = "validator.email"`, was one of those names: the registry keys validators by their bare name, so the prefixed form resolved to nothing. Both spellings resolve now, and a name nothing declares is refused at startup — unless the configuration declares plugins, since what a plugin provides is read when the plugin loads rather than when the configuration is parsed, and a check that refuses a working service is worse than one that misses a typo.

- **Two steps with the same name both ran, and the second overwrote the first.** A step's result is stored under its name, an enrichment's likewise, and a saga step's compensation is found by its name — and nothing checked that those names were unique inside a flow, though the top-level ones were. So a copy-pasted step produced a service that answered, was quietly wrong (`step.detail` meant whichever came last), and paid for a query whose result was discarded on every request. Repeated names are refused at startup now, naming the flow, the name and what it costs.

- **A connector named anywhere but `from` or `to` was checked by nobody, and the twenty-odd places behaved three different ways.** A `dedupe` pointing at a connector nobody declared was refused at parse time. An `idempotency` block pointing at one started happily and failed on the first request that carried a key — in production rather than at deploy. A `cache` block pointing at one started happily and cached nothing, for ever, without a word: the lookup fails, nil comes back, and every caller reads nil as "no cache configured". Every reference is checked at startup now — steps, enrichments, fallbacks, each destination of a fan-out, an aspect's action, the named caches — and so is whether the connector can do the job it was named for, since a cache that caches into a database is as quiet as one that caches nowhere. The error names where it was written and offers the declared name closest to it. **The steps example in this repository referenced eight connectors it never declared, and validated.**

- **The one reusable block whose reference could not be written was `cache`.** Ten blocks were made nameable and referenceable and the documentation calls that the recommended way to write them, but `cache` kept `storage` as a required argument — so `cache { use = "cache.short" }` was refused with "the argument storage is required", which names neither `use` nor the block it points at. The evidence it had never worked: the cache example in this repository declares three named caches, says in its own header "define once, use everywhere with `cache { use = ... }`", and references none of them. The block now requires a storage **or** a reference, and the example uses the caches it declares.

- **A named cache's `prefix` was not prepended to keys the flow named itself.** It applied only when the flow wrote no key of its own, so the moment two flows referenced one named cache and each named a key, they shared its keyspace — which is the one thing a prefix exists to prevent. Its own comment, the guide and the word all said "prepended to all cache keys". This was invisible until now, because a flow could not reference a named cache at all.

- **A boolean set from the environment crashed the binary.** `env()` always returns a string, so `mfa { enabled = env("MFA_ON") }` handed a string to a boolean — and cty's `True()` panics on anything that is not one. Six attributes across four blocks went down that way, including `auth.mfa.enabled` and the service rate limiter's. Integers already had a coercion for exactly this reason; booleans now read the several ways a person writes one — `true`, `yes`, `on`, `1` and their opposites — and refuse a word that is neither.

- **A single name where a list was wanted was accepted and thrown away.** `invalidate_on = "update_user"` instead of `["update_user"]` is the mistake somebody makes once per language they learn, and these attributes were read behind a bare `if val.Type().IsListType()` whose else branch did nothing: the line parsed, the value vanished, and the cache invalidated nothing, the rule required no roles. The service started and did less than it was told to, silently. Only the aspect's `on` had the single-value branch, which is why that one worked and its seven neighbours did not; they all share one reader now.

- **A number in almost any text attribute crashed the binary.** `cache { ttl = 300 }` — a number where a duration string was wanted, and what somebody means by five minutes — took `mycel validate` down with `panic: not a string`, naming neither the attribute nor the block. It was not one attribute: a test that walks the schema and puts a number in every text attribute found **88 of them across eleven root blocks**. They are read through the same helper the auth parser already used, which reads a number or a boolean as the text a person meant and refuses what cannot be read at all.

- **A duration that could not be read was silently discarded.** Every one was parsed at the point of use with the error thrown away, so `ttl = "5 minutes"` meant *no TTL* — the entry lived for whatever the connector defaults to — and a lock timeout that did not parse meant the default timeout. Durations are checked at startup now, naming the flow and the attribute.

- **And `ttl = "30d"` was one of them.** The runtime read cache, async, idempotency and retry durations with the standard library's parser, which has no day or week unit, while this configuration language has both — `flow.ParseDuration` has supported them all along and `dedupe` validated against it. So a TTL written in days, which is how the examples in this repository write them, silently meant no TTL. All of them use one parser now, and the examples that had been quietly doing nothing for months do what they say.

- **An `each` inside a transaction wrote nothing and reported success.** Any value that was not a slice made the loop a no-op: a transaction writing a parent row and its children wrote the parent, wrote no children, committed, and the flow acknowledged its message. The commonest way to get there is an array that arrived as a string, which is the same shape that catches people writing CEL against queue payloads, and nothing anywhere said so. A value that is present and is not a list is now an error naming the expression and what it found, so the transaction rolls back rather than committing half of an aggregate.

- **And the opposite, in the same loop: a field that was simply absent failed the whole transaction.** The expression was evaluated bare, CEL raised "no such key", and the write was rolled back — so an order with no discounts did not write the order. Absent is nothing to loop over now, which is what the code's own comment had always claimed.

- **A health check raced the server it was checking.** The REST connector's `Health` read `started` from the health manager's goroutine while `Start` wrote it under the mutex, so the read was unsynchronised — a real race, caught by the detector on a loaded CI runner rather than by anybody watching. It is atomic now, so a health check cannot block on a mutex held by something slower than it is.

- **`grant_type` on an HTTP connector's `auth` block chose nothing.** Which OAuth2 grant ran was decided by the auth type alone, so a connector written for a grant this does not implement — `password`, say — was accepted in silence and ran a refresh-token flow instead, against a token endpoint that would refuse it. It selects the grant now, and a value that is neither `client_credentials` nor `refresh_token` is named at startup.

- **A GraphQL server with no schema at all started happily.** `schema { auto_generate }` was stored and read by nothing: where the schema came from was decided entirely by whether a `path` was given. So the two ways of saying there is no schema — no path, and `auto_generate = false` — produced a service that was up and answered every query with an empty schema. Both that and the contradiction, a path together with `auto_generate = true`, are refused at startup now.

- **A comment said not to write settings that work.** `parseRetryBlock` claimed only `attempts` was honoured at runtime and the wait was a fixed exponential backoff; the HTTP connector reads `delay`, `max_delay` and `backoff`.

- **Push notifications to Firebase have been failing since June 2024.** The connector spoke the legacy FCM API — `POST /fcm/send` with a server key — which Google retired then, and the two settings for the API that replaced it, `project_id` and `service_account_json`, were read by nothing: the fields existed, the documentation listed them, and the code kept posting to an endpoint that is gone. It speaks HTTP v1 now, authenticating with the service account from the Firebase console: a JWT signed with its key, exchanged for an access token that is held until it is nearly expired rather than fetched per notification. What a message carries is translated, since this API moved priority, time to live and the collapse key under `android` and takes only string values in `data`. A message to several devices becomes one request each, which is what the API allows and means a token whose app was uninstalled fails on its own and comes back in `failed_tokens`. A connector configured with `server_key` is refused at startup, naming what to write instead, rather than starting healthy and failing every push.

- **`mfa { required }` required nothing.** It was read by a status response and nowhere else: sign-in asked for a second factor only when the account had enrolled one voluntarily, so a service demanding MFA let somebody who never set one up sign in with a password for ever — while the status endpoint told them it was required. `require_for`, `require_multiple`, `min_factors` and `grace_period` were equally inert. An account that owes the policy a factor now keeps its sign-in — enrolling needs a token — and is refused everywhere else with `403 mfa_enrolment_required` until it has one, the same shape the password expiry uses. The sign-in response says what is wanted, what is held and when the grace period ends. A policy demanding more factors than there are methods to enrol them with is refused at startup, since no account could ever satisfy it.

- **`password { breach_check }` checked nothing.** A service that asked for this accepted passwords sitting in every list anybody uses — and it is the most effective password rule there is, well ahead of demanding a capital letter, which mostly produces Password1. Candidates are checked against Have I Been Pwned without the password leaving the process: it is hashed, five characters of that hash are sent, and the comparison happens here, so the service learns five characters it cannot turn back into anything. A list that cannot be reached lets the password through and says so, because the alternative is a service where nobody can register because somebody else's website is down.

- **The guide's OIDC example never parsed.** `claims { ... }` is an attribute, not a block, so a configuration copied from the enterprise SSO section was refused with "Blocks of type claims are not expected here".

- **Three settings in the `sessions` block decided nothing.** `allow_list` and `allow_revoke`, documented as enabling session listing and revocation, were read by nothing: the endpoints were governed only by the `endpoints` block, so a service that wrote them false served both anyway and had no way to know. Both are honoured now, and stop the route being mounted at all rather than serving it and refusing. Because a bare bool is false when it is absent and these have always been on, the parser fills them in before decoding — the block being present is not a decision about a setting nobody mentioned, the same rule `mfa.enabled` follows. `track`, which names what to record about a sign-in, was never consulted either, so a deployment that listed only the browser string was storing addresses regardless; naming none still records both, naming some records only those.

- **`password { max_age }` and `warn_before` expired nothing and warned nobody.** Both were read by nothing, so a service requiring a password to be changed every ninety days never asked anybody to change one. A password past its age is refused now wherever a token is validated, which is what makes it a policy rather than a note — but signing in still works, because the endpoint that fixes it needs a token, and so does signing out; everything else answers `403 password_expired`, a code a client can act on instead of the one word every refusal shares. `warn_before` puts `password_expires_at` in the sign-in response that far ahead. The age is recorded for you in memory; on a database it is a column named in the users `fields` block, opt-in like `roles` so an existing table need not grow one — and a service that configures `max_age` without it is told at startup that nothing will expire, rather than left with a policy that looks configured. A password whose age was never recorded is never treated as expired: locking out every existing account the moment somebody writes `max_age` is not what writing it means.

- **`password { history = N }` let an account cycle between two passwords for ever.** Two stores for the history have existed since the auth system was written, one for PostgreSQL and one for MySQL, each with its own tests — and nothing ever built one, because the manager had no field to hold it. So the setting that exists to stop somebody required to rotate their password from alternating between the same two did not stop it. Reuse is refused now, the password in use counting as the most recent of the N, and the stored hashes are checked with the hasher rather than compared as text, since every hash carries its own salt and comparing text would let every reuse through while looking right. A database-backed deployment keeps the history in `password_history`, the table the guide has always published; anything else keeps it in the process alongside the accounts. A history that cannot be read is reported and the change goes ahead, because somebody acting on a leak needs it to.

- **`extend_on_activity` decided nothing, so every session was a sliding one.** Each validated request refreshes the last-active time the idle sweep reads, and nothing consulted the setting, so a session in constant use never reached its idle timeout — a deployment asking for a fixed window, sign in again after thirty minutes however busy you were, got a sliding one instead. It is honoured now by writing the fixed end into the session's own expiry, so every store enforces it including Redis, where a session is a key with a lifetime and no sweep runs at all, and the last-active time stays truthful for the session listing. Like the two above it defaults on, because a boolean nobody wrote is false and false is the change in behaviour.

- **`on_max_reached = "deny"` revoked a session instead of refusing one.** The guide published `deny` and `revoke_all` for years and the code only ever knew `reject_new`; everything else fell through to revoking the oldest session, so a service that meant to turn the newest sign-in away silently signed somebody out of an existing one instead. `deny` is read as refusing now, and a value this does not understand is refused at startup, naming the two that work, rather than quietly meaning revoke_oldest. The `sessions` block is also described in the schema now, so completions and `mycel add` know its eight settings.

- **A GraphQL subscription was dropped after sixty seconds of quiet.** A subscription is idle by nature — waiting for something to happen is what it is for — and nothing kept the socket busy: the server answered a client's ping and never sent one of its own, so `keep_alive_interval`, whose documented purpose is the ping period on an idle socket, did nothing. The read deadline was a hardcoded minute refreshed only by incoming messages, so a subscriber that had nothing to say was disconnected by this server, and so was one that was actively receiving events, because those are writes and the deadline watches reads. The server now pings on the configured interval and uses `connection_timeout` as the deadline, widened when it would leave no room for an answer. Verified by removing the ping loop and watching a quiet subscription die.

- **A Kafka connector with `producer.topic` set failed every publish.** The topic reached both the writer and the message, and kafka-go refuses that outright — "Topic must not be specified for both Writer and Message" — so configuring the setting the documentation lists as **required** broke publishing entirely, with an error about the library's own rules. The integration configuration carried a comment working around it, and a unit test asserted the writer had a topic, which pinned it. The topic goes on the message now, falling back to `producer.topic` when the flow names none, so both forms work.

- **`producer.linger_ms` was ignored, and every single-message publish waited a second.** The value was read into a field nothing used, so the library's own default applied. Measured against a real broker: one publish took **1.003s** with `linger_ms = 5`. A flow publishing one message per request paid that on every request.

- **`consumer.auto_commit = false` did nothing, so a message whose flow failed was skipped.** Offsets were committed on a timer regardless — the interval was set unconditionally while the comment beside it said "if auto-commit is enabled" — and the read loop used the call that advances the offset by itself. So the setting somebody reaches for precisely to avoid losing a message did not prevent losing it: the offset moved past a message the flow had failed on, and it was never seen again. With `false` the offset is now committed only after the flow succeeds, and a restart resumes from there. Verified against a real broker in both directions.

- **`publisher.confirms` did nothing.** A publish returned as soon as the bytes reached the socket, so a broker that died a moment later took the message with it — and a flow that acknowledges its own source message after publishing downstream was relying on the opposite. The setting was parsed into a field nothing read, and the function that waited for a confirmation had no caller and enabled confirm mode per publish, which is a channel-level mode. Confirm mode is now set once when the channel is opened, and on every new channel so a reconnect does not quietly drop back to unconfirmed publishing; a publish waits for the confirmation belonging to it, so concurrent publishes do not read each other's. A broker that never answers is given thirty seconds unless the flow's own timeout is shorter. Verified against a real broker, in both directions: with confirm mode disabled the test says so and fails.

### Changed

- **BREAKING: the workflow endpoints have their own port and require authentication.** `GET /workflows/{id}`, `POST /workflows/{id}/signal/{event}` and `POST /workflows/{id}/cancel` were mounted on the admin server as soon as a `workflow` block was configured. That port carries health and metrics — read-only, unauthenticated, and reachable by anything on the network the process is on — while these three are not read-only: signalling wakes a paused workflow with data the caller chooses, and the documentation's own example is a loan approval. They are now served only when the configuration asks for them, on a port of their own, and never without something to check callers against:

  ```hcl
  service {
    workflow {
      storage = "db"

      api {
        port = 9091                 # default 9091; the admin port is refused

        auth {                      # required — the same block a connector takes
          type = "api_key"
          keys = [env("WORKFLOW_API_KEY")]
        }
      }
    }
  }
  ```

  `auth` is the connector auth block, so `jwt`, `api_key` and `basic` all work and are checked by the same code a REST connector uses — extracted from it rather than written again. An `api` block with no `auth`, an `auth` with nothing to check against, or a port equal to the admin port are each refused by `mycel validate`. A service that has a `workflow` block and no `api` block now serves no workflow endpoints at all: this is the breaking part.

### Added

- **A test that fails when a setting is carried from the configuration and read by nothing.** This is the shape that has produced more bugs in this repository than any other: an attribute is accepted by the parser, put into a config struct by the factory, and read by no one, so the file says the thing is on and the runtime never asks. It has been found by hand five times in this release alone — `sender_id` and `sms_type` on SNS, `ca_cert` on MQTT, `known_hosts` and `password` on ssh, the API key `validate` block. Every connector's config structs are now walked for fields that are written somewhere and read nowhere; anything that stays takes an entry with a written reason, and an entry that starts being read fails too, so the list cannot go stale. Verified in both directions — a planted unread field is named, and removing it goes green.

- **A dead-letter record and a custom error body were never built.** `fallback { transform }` decides what a replayable record looks like and `error_response { body }` decides what a caller is told; both passed the failure to `Transform` inside a map called `input`, and `Transform` takes that map *as* the input — so `input.id` read the wrapper, `error.message` did not resolve at all, and the transform failed. The failure was then discarded, leaving the unshaped message in the queue and the default body on the wire. `error` is a declared CEL variable that no transform bound; it is bound now, and a shape that still cannot be built is reported instead of passing in silence.

- **A destination's `when` could not read the message.** Every condition in a flow is written against `input` — a filter, an accept gate, a step, an aspect — but a destination's had the fields copied to the top level where CEL cannot see them, with only `output` bound. `when = "input.total > 1000"` therefore failed the write with "no such key" instead of deciding it, and a flow whose every destination failed reports an error, so one mis-bound condition could fail the whole thing. Both are bound now.
- **Two destinations sharing a connector reported as one.** An order and its lines going to the same database is the ordinary shape of a multi-destination write; both writes happened, but the report is keyed by connector name so one entry overwrote the other — and a failure on the first could sit under a success from the second. A connector used once keeps its bare name; repeats are told apart by their target.
- **A Slack message lost its blocks.** The payload reader took `text` and `channel`, so a flow that built a layout posted a bare line — or nothing, when the text was empty because the blocks were the message. `blocks` and `thread_ts` are also what the 2.5.0 batching checks to decide a message must go on its own, and since neither could be set from a payload that decision never had anything to look at. Threads, attachments, the icon and the unfurl settings are read now too, and Discord's interactive components with them.

- **A Discord card was dropped and the message went out empty.** The payload was read in the send path and took only `content`, so an alert built as an embed — the title, the colour, the fields — arrived as a message with nothing in it, which Discord refuses. `allowed_mentions` was dropped with it: an alert quoting text somebody else wrote can contain `@everyone`, and that field is what stops it pinging a whole server. The name and avatar a message appears under, the thread it starts and text-to-speech are read now too.

- **Most of what a flow writes on an email was dropped.** The connector read a handful of fields from the payload and ignored the rest: `attachments` — so a flow that generated a PDF and attached it sent the invoice email without the invoice — along with `cc`, `bcc`, `reply_to`, the sender's display name, custom headers, tags and the tracking flags. A copy that silently does not go is found a month later by whoever was waiting for it. `text` and `text_body` were both accepted while only `html_body` was, so writing the obvious pair sent a message with no HTML in it. Addresses are now accepted however a flow writes them — one, a list, or records carrying a name.
- **A push notification's data payload was dropped.** It was read with a `map[string]string` type assertion, which a flow's payload never satisfies, so the notification arrived saying the right thing and carrying nothing for the app to act on — tapping it landed nowhere. The device list, priority, lifetime and collapse key were not read either, so a notification could only reach one device and a new notice stacked instead of replacing the last.

- **Passkeys can be registered and used.** Writing a `webauthn` block asks for them, and everything behind it existed — the service that runs both ceremonies, the manager's registration methods, the store — with nothing a browser could call. The sign-in half was missing entirely: the service could begin and finish a login and the manager exposed neither, so a passkey could be registered and never used. Signing in goes through the same session path a password sign-in uses and is audited with the method; the authenticator's signature counter is now stored, which is what lets a cloned key be noticed. Sign-in options are asked for by address and an address with no passkey answers exactly as one with no account, so the endpoint cannot be used to ask which addresses are registered. Registration and removal act on the account in the token, removal asks for the password, the list carries no key material, and an account with no passkeys lists none rather than failing.

- **The endpoints a second factor is set up through.** The `endpoints` block declares `mfa_setup`, `mfa_verify`, `mfa_disable` and `mfa_recovery` with defaults, and none of the four was mounted — the manager could already enrol, confirm, disable and check a code, so a service with MFA on had everything except a way for a browser to reach it, and `mfa_setup` named a path that answered 404. Enrolment remains a two-step ceremony: the secret is handed over, and the account is protected only once a code proves the app holds it. Each acts on the account in the token rather than a user id from the body, turning it off asks for the password, and recovery codes come back from the confirming call because they are shown once. Signing in with a recovery code goes through the ordinary sign-in, so the brute-force counters and the audit record stay in one place.

- **Mocking says when a connector named for it has none.** A connector with mocks answers from them and never reaches the real one; without any, its calls fall through — right when mocking is on for a whole service and only some connectors have mocks, and a mistake when that connector was asked for by name. A directory called `database` for a connector called `db` left every call going to the real database while the service reported mocking as on. Naming one and finding no mocks is now a warning that says where the directory was expected.

- **`introspection` on a GraphQL server does something.** It was accepted by the parser and read by nothing, so asking for introspection to be off left it on — and a schema is a map of everything the service can do. It now defaults off in production and on elsewhere, following the same reasoning as the playground, and can be set either way. String literals are stripped before the check, so a query searching for the text `__schema` is not mistaken for an introspection query.
- **Each connector schema is defined once.** There were two hand-written copies of all 33 of them — the ones under `internal/connector` that the runtime registers, and a second set in `pkg/connectors` that external tooling links against — and nothing compared them. They had drifted for three releases: 5 of 26 connector types differed across 14 attributes, every one present in the runtime and absent from the copy the outside world reads, including `create_if_missing` from 2.0.0 and Slack's `batch` block from 2.5.0. The definitions now live in `pkg/connectors`, one file per connector, and the runtime registers from there — the direction chosen by measurement, since that package depends on nothing beyond `pkg/schema` while delegating the other way would have pulled the 123 modules the runtime needs into any program that just wants completions.
- **Connector schemas are checked against the parser.** Nothing ever did, which is how the gRPC connector came to advertise TLS attributes the parser refused. Each attribute a schema declares is now written into a connector block and parsed. The test carries an explicit list of the 38 attributes that do not parse today — a separate problem, in the parser's hand-written allow-list — so that drift cannot grow, and it fails both when a new one appears and when a listed one starts parsing.
- **Named operations are in the schema.** The connector schema declared no `operation` block at all, so a feature that was parsed, documented and exampled was invisible to completions, `mycel add` and anything else built on the schema. Both directions are now tested: every attribute the schema offers is rendered and parsed, and every attribute the parser accepts is checked for in the schema.

### Fixed

- **A conditional breakpoint on a single transform rule ignored its condition.** `{ "stage": "transform", "ruleIndex": 12, "condition": "input.email != \"\"" }` — the example in the debug protocol documentation — stopped on rule 12 for every message, because every path through the check returned `true` while the code that evaluates a condition sat in the same file, used by stage-level breakpoints. In a transform of forty rules that is the difference between one pause and forty, and it is exactly when somebody is chasing one bad record that it matters.

- **`passive = false` on an FTP connector was accepted and could not be honoured.** The library underneath has no active mode — it always asks the server for a port and connects out to it, which is what works from behind NAT — so somebody who asked for active transfers got passive ones and no word about it. If they asked, it was because they believed they needed the other one. It is refused at start-up now, and the documentation says why.

- **API keys checked against a connector were never checked against anything.** The `validate { connector = "db", query = "..." }` block was parsed into two fields that nothing read, and the function that builds a validator from them had no caller — so a service configured to check keys against a table, which is how a key is revoked without a deployment, fell back to the static list and refused every key it was given. The runtime now resolves the named connector once every connector exists and hands the server a reader. The builder also wrote the key into the statement with a string replace; the key is whatever the caller sent in a header, so `' OR '1'='1` would have matched every row and authenticated the request as whoever came back first. It is passed as a parameter now.

- **`ssh.known_hosts` was read from the configuration and overridden by the line beside it.** The exec connector ran ssh with `StrictHostKeyChecking=no` hardcoded, so the host key was never checked — including for a deployment that had configured a known-hosts file, which is the one setting whose purpose is to stop somebody standing in the middle of that connection. What runs on the far end is whatever the flow was going to run. Naming a file now turns checking on and points ssh at it; naming none keeps the previous behaviour, because turning it on for everybody would stop every deployment that has no such file, but the service says so at start-up instead of assuming it.

- **`ssh.password` was accepted and could never work.** Commands run with `BatchMode`, which exists so nothing stops to prompt, and ssh has no way to be handed a password on the command line — so a connector configured with a user and a password authenticated with nothing and failed later in ssh's own words about a closed connection. It is refused at start-up now, naming the setting that does work.

- **A message that failed while the connection was down took the process with it.** Retrying a message means republishing it, and the republish went straight to the channel — which is gone whenever the connection dropped, and a dropped connection is precisely the situation a handler is most likely to have failed in. Publishing through a channel that is not there is not an error; it is a nil dereference, and the acknowledgement methods beside it had always checked while this one never did. The message now goes back to the queue, which is what a retry was asking for anyway: the broker redelivers it once a consumer is connected again.

- **`protocol = "msgpack"` on a TCP connector sent JSON.** The codec was a placeholder that called `json.Marshal`, while the connector reference, the connector index and the runtime's own list of what it speaks all offered MessagePack. Anything that actually implements the protocol — a NestJS microservice, a device, somebody's Python service — could not read a word of it. Two Mycel services configured for it did understand each other, because both were sending JSON, which is what hid it: that is not compatibility, it is two ends sharing one bug. It is MessagePack now, and the tests read the bytes off a socket with a MessagePack library that knows nothing about Mycel. Adds one dependency, `github.com/vmihailenco/msgpack/v5`, pure Go.

- **A generated PDF could be written outside its output directory.** The filename comes from the payload — an invoice number, an order reference, whatever the flow computed, often from data that came in from outside — and it was joined straight onto the configured output directory, so `filename = "../../etc/cron.d/mycel"` wrote there. A connector whose job is writing files from data would write them anywhere the process could. The path is now confined to the output directory: a subdirectory still works, because filing invoices under a customer or a month is the ordinary use, and anything that leaves is refused. Verified by watching the test fail against the previous code.

- **An email no recipient accepted was reported as sent.** Each address the server turns down is recorded per recipient and the send carries on for the rest, which is right — but every address refused is a different thing: there is nobody left to send to. The code carried on to DATA anyway and relied on the server objecting; some do, and some accept the message and drop it, and the flow was told it had been sent either way. It now fails, naming the addresses and what the server said about each, which is what a flow that took those addresses from data needs in order to correct them.

- **`send_to_user` on a WebSocket connector delivered to nobody.** A connection's user was read by the send, and by the input a flow receives, and never written by anything — no code path set it, so the operation matched no client, sent nothing, and answered that it had written one row. A client now says who it is when it opens the socket (`/ws?user_id=alice`), which is all a browser can send on an upgrade; the rest of the query string reaches the flow alongside the message, so a flow can tell one tenant's connection from another's. The documentation says the user may be named "in the payload or in filters" and only the filters were read, so the form its own example shows also sent to nobody — both work now.

- **`ca_cert` on an MQTT connector's `tls` block was read and never used.** An MQTT broker inside a company has a certificate signed by that company's own authority, and naming it is how the broker gets verified. The setting was parsed, stored on the configuration and then dropped, so the only way to connect was `insecure_skip_verify` — which does not fix verification, it turns it off. Same shape as the gRPC CA certificate fixed earlier in this release.

- **Four metrics were declared, registered, exposed and never recorded.** `mycel_scheduled_flows` and `mycel_schedule_executed_total` mean a scheduled flow leaves a trace — it has no caller waiting and no message to acknowledge, so how many are scheduled and how the runs went is all there is — and both read zero for ever. `mycel_cache_size` is in the documented list of metrics and nothing ever set it; it is now reported from the periodic sweep that already updates uptime and goroutines, for caches that can answer without a round trip. A failing scheduled flow was also printed to stdout with `fmt.Printf` rather than logged, so it did not reach a log pipeline that reads JSON. A test now fails when any recorder in the metrics package has no call site in the runtime — this is the third time this class of bug has been found here, and the previous two were found by somebody watching a graph that stayed flat.

- **`mycel_coordinate_preflight_hit_total` filed a flow name under a label called `connector`.** The documentation says the label is `flow`, and the one call site passes a flow, so a query grouped by `flow` found nothing and one grouped by `connector` answered with flows.

- **A path listed as `public` was still refused when `required_headers` was set.** The headers were checked before the public list, so `/health` — the documented example of a public path — answered 400 to a caller that did not send them. A load balancer's health probe sends no headers of its own, so the instance read as unhealthy and was taken out of rotation, which is a long way from a header list in an auth block. A public path is now exempt from all of it. `required_headers` and `response_headers` are also declared in the connector schema now: the parser accepted both and the schema described neither, so neither was offered as a completion.

- **A `min` or `max` check read a number of the wrong width as zero.** The type check admits every integer and float width Go has; the comparison behind `min` and `max` knew four of them and returned zero for the rest — and the two most conspicuously missing are what database drivers produce. A quantity of 50 arriving as an `int32`, which is what pgx returns for a Postgres `int4` column, failed `min = 1` with "value must be at least 1"; a quantity of 5000 passed `max = 100` for the same reason. Every width is read now, and a value that is not a number at all is left to the type check to report once instead of being compared as zero and failing twice.

- **`format = "uuid"` accepted any thirty-six characters, and `format = "date"` any two dashes in the right places.** A truncated identifier or a line of text passed as an identifier, and `9999-99-99` and `0000-00-00` passed as dates — reaching a database as a date it refuses, a long way from the field that was wrong. Both are now checked as what they claim to be, and `datetime` is parsed as RFC 3339.

- **A lock, semaphore or coordinate key written as CEL could resolve to the same key for every message.** These keys exist so that work on one order is serialised without serialising work on all of them, and they are evaluated before the flow body runs — earlier than anything that creates a CEL evaluator. When there was none, the code gave up and used the expression itself as the key: on a flow with no filter, accept gate or dedupe, which is an ordinary consumer, `key = "'order:' + input.order_id"` became the literal string for every message. A per-order lock became one global lock, and a coordinate key never matched the signal it was waiting for. The same gap made `sequence_guard` read every message as carrying no sequence, and `coordinate.signal.when` never fire. All four now create the evaluator they need, the way dedupe already did.

- **A field in a `type` block written as a value panicked the process.** `age = 18` reads like a default, and a type block does not hold defaults — it says a field is a number, not which number. Parsing it reached `cty.Value.AsString()` on a number and the process died on "not a string", which takes down `mycel validate` and, during a hot reload, the running service. It now names the field and says what to write instead. Same shape as the `transform { count = 5 }` panic fixed in 2.17.0.

- **`route_by_latency` on a Sentinel cache panicked the process at start-up.** Spreading reads over the replicas needs the client that knows about all of them; the plain failover client talks to whichever server Sentinel says is master, and asking it to route by latency or at random does not return an error — it panics inside the driver. So a documented setting in a `sentinel` block took the service down with a stack trace instead of starting. The right client is now used when either routing setting is on.

- **The database schemas described requirements the drivers do not have.** `database` was marked required on Postgres and MySQL, and a connector giving only a `url` is complete — the URL is taken apart before anything is checked, so a name written inside one ends up in the same place. The claim cost nothing at run time, because nothing enforced a connector's required attributes, but `mycel add connector db --type database --driver postgres` wrote out `database = env("DATABASE") // TODO` and listed `url` under "Optional", which is backwards for every managed platform — they hand over one connection string. SQLite's `database` was marked required too, and it defaults to `./data/mycel.db`. Both are now described as they behave, with a new `RequiredOneOf` on a schema block saying "written one way or the other", and `mycel validate` reports a connector that answers in neither way — which used to validate clean and fail at start-up with "postgres connector requires database name". MongoDB keeps its plain requirement: its URI genuinely does not carry the database name. An attribute written as `env("DATABASE_URL")` with the variable unset still passes, because the start-up error that names the missing variable is a better answer than anything validation could give.

- **A connector could be generated, validated, and then refuse to start for want of a driver.** `connector "db" { type = "database" }` parsed, was listed by `mycel validate` as a database connector, and failed at start-up with `no factory found for connector type=database driver=`. `mycel add connector db --type database` generated exactly that file, so the command that exists to produce a correct starting point produced one that could not run. The cause was three deep: the `database` and `mq` schemas never declared `driver` at all; `Merge` kept the base block's bare `driver` over each connector's own — the opposite of what its comment said — so the driver lists that *were* declared (cache, email, push, sms, oauth) were discarded, and `driver = "postgress"` validated clean; and nothing required a driver anywhere. All three are fixed: schemas declare their drivers, a connector's own description now wins the merge, `mycel validate` reports a connector with no driver or an unrecognised one, and `mycel add` refuses with the flag that fixes it.

- **`profiled` was the only user-facing connector type with no schema.** Documented, exampled, and in the parser's list of built-in types, but unknown to completions, `mycel add` (which answered "unknown connector type") and validation. It has one now, including the two rules the parser enforces that are not "this attribute is required": there must be a `profile` block, and one of `select` or `default` must name which profile to use. The second is expressed with a new `RequiredOneOf` on a schema block, so the generated file parses instead of failing at the first `mycel validate`. A new round-trip test generates a connector of **every** registered type and parses it — connectors were the one thing `mycel add` produces that the round-trip test did not cover.

- **`sender_id` and `sms_type` did nothing on the SNS driver.** Both were parsed, documented, and then dropped where the message is built — the code carried a note saying they could be set in the AWS console instead. The two SMS types are not interchangeable: promotional traffic is the first thing carriers throttle and, in several countries, a one-time code sent as promotional is not delivered at all, so a service configured as `Transactional` was quietly sending as whatever the account defaulted to. They are now sent as SNS message attributes, and a connector that configures neither still sends none, so the account's own settings continue to apply.

- **Reconnecting to a database stream skipped every change written while it was down.** The replication slot exists so that nothing is lost when a consumer drops: PostgreSQL keeps the WAL a slot has not confirmed, and streaming is meant to resume from there. On reconnect the slot was found to exist already and the start position was then read from `IdentifySystem` — the server's *current* write position, past everything written during the gap. So a CDC consumer that lost its connection for a minute came back and carried on from the present, silently, having dropped exactly the changes the slot was holding for it. The slot is now asked where it left off (`confirmed_flush_lsn`, falling back to `restart_lsn`), which also means that failing to create a slot for an unrelated reason — no permission, wrong plugin — is reported instead of quietly starting at the head. Resumption is at-least-once: a change handled but not yet confirmed when the connection dropped arrives again, which is now stated in the CDC documentation along with what an abandoned slot costs in disk.

- **Stopping the workflow engine twice panicked the process.** `Stop` closed its ticker channel with no guard, and shutdown reaches it twice — a cancelled context and an explicit `Shutdown` — so a service stopping cleanly could end on "close of closed channel" instead of an exit code.

- **A federation v2 subgraph schema file did not load.** Every such file opens by declaring which version of the specification it speaks — `extend schema @link(url: "https://specs.apollo.dev/federation/v2.0", import: [...])` — and the AST parser behind the schema reader only understands `extend type`. Pointing a `graphql` connector at a real subgraph schema failed at the second line with a syntax error on text copied from the specification, and the service did not start. The same preamble is what Mycel generates for its own `_service` reply, so it was emitting SDL it could not read back. Two more things in the same file were refused or lost: `repeatable`, the keyword federation's own `@key` and `@tag` are declared with, and a schema block naming roots of its own (`schema { query: RootQuery }`), which left the root with the right name and no fields while the fields sat under the type's own name — a schema file that looks complete and exposes nothing.
- **`min_val`, `max_val` and `sort_by` ignored numbers typed differently.** JSON has one number type and CEL has three, so a single field arrives as an integer from one record and a double from the next — a price of 30 beside a price of 2.5 — and the comparison behind all three functions required both sides to be the same type, so every such pair compared equal. Nothing failed: `min_val([10, 2.5, 30, 1.5])` returned 10, and `sort_by` over totals of 30, 2.5 and 10 returned them in the order they were given. Numbers now compare as numbers whichever way each side is typed, while two integers are still compared as integers so identifiers beyond the range a float holds exactly are not flattened together.

- **A webhook allow-list could be bypassed with a header.** The list of addresses permitted to deliver an inbound webhook was decided on `X-Forwarded-For`, which is written by whoever sends the request — so `curl -H "X-Forwarded-For: 203.0.113.9"` got anyone past a list of the provider's addresses, and the refusal that never happened is invisible. A forwarding header is now believed only from a peer named in the new `trusted_proxies`; otherwise the decision is made on the peer address, which the caller cannot write. Behind a named proxy the address is taken from the nearest hop outwards, skipping the proxies we know, so a caller cannot prepend its own.

- **JWKS token validation refused every token, on gRPC as well as REST.** Both connectors carried the same defect — a key built as an anonymous struct rather than a public key, so nothing could verify with it — and both are now built by one shared package. The curve is taken from the key rather than assumed, a key that cannot be used is refused by name, and a padded base64 value is accepted since some providers pad and the specification says not to.

- **The REST connector's key set is no longer cached for ever.** `parseJWK` returned an anonymous struct holding the modulus and exponent rather than a key any signature library can use, so a REST connector pointed at Auth0, Cognito or Keycloak rejected every authenticated request with "key is of invalid type". Both the RSA and EC paths now build real keys, and the curve is read from the key rather than assumed, so one naming an unknown curve is refused by name instead of reported as a bad signature. The key set is also no longer cached for ever: an identifier that is not in what we hold prompts one refresh, so a provider's key rotation no longer refuses every request until the service is restarted — while a token naming a key nobody publishes does not turn each request into a fetch.

- **A port from the environment was ignored, and the banner said otherwise.** `port = env("PORT")` returns a string, and `Config.GetInt` refused strings while `IntFromProps` — added for exactly this case — accepts them. Five factories (rest, websocket, cdc, sse, soap) read their port through the first, got 0 and fell back to their default, while the startup banner read it through the second. Measured: with `API_PORT=18777` the banner announced `listening on :18777` and the service answered on 3000 — in a container, a published port that does not match and a readiness probe that never passes. Both readers now agree, and `GetBool` gains the same coercion, since a flag set in an environment is text and reading it as false turns a setting that was switched on into one that is silently off.

- **Only half of a `validate` block was enforced.** It names a type for what goes in and a type for what comes out; the output half was written, complete, and called by nothing, so a flow declaring an output contract had it checked nowhere — a transform that drops a field reached the caller as a record with a hole in it. The asymmetry is what made it dangerous rather than merely missing: watching the input half refuse a bad request is reason to believe the other half works too. It is now checked at the point symmetric to the input one, each record of a list included, while an answer with nothing to hold against a type — a count, a string, nothing at all — is left alone.

- **The `auth { audit { ... } }` block wrote nothing.** It was parsed into the configuration and read by nothing: the stores that write the records existed and were constructed only by their own tests, so a service with an audit block kept no record of any sign-in, failure or password change, silently. The block now builds its store, and the manager reports sign-ins with the address and agent they came from, failed attempts with a reason, lockouts, refused second factors, registrations, sign-outs and password changes — including one refused for not knowing the current password. The reason recorded for a wrong password is the same as for an address with no account, so the table does not become a list of which addresses have accounts, and a record that cannot be written is logged and swallowed rather than failing the sign-in.

- **A subscription filter was stored and never applied.** A `filter` on a subscription destination decides which subscribers an event is for. It travelled from the flow's `to` block through `RegisterSubscriptionWithFilter` and `SetSubscriptionFilter`, was stored on the schema builder, and was read by nothing — so every subscriber received every event on the topic, including the ones a filter was written to keep apart. It is now evaluated per subscriber per event against the published data (`input`) and the parameters that subscriber sent at `connection_init` (`auth`), which is where a token lives on a websocket. The expression is compiled when the subscription starts so a broken one is reported then, and an event that cannot be judged is not delivered.

- **A GraphQL schema file whose types refer to each other built or failed depending on the run.** The converter created every object type empty and then replaced it with a filled copy, so a field naming another type resolved to whatever was in the map at that moment — the empty placeholder, if that type had not been reached yet, which was then discarded while the field still pointed at it. Map iteration order is random, so the same file and the same binary produced a working service or a startup failure (`X fields must be an object with field names as keys`) from one run to the next. Fields are now resolved on demand, which also lets a type refer to itself.
- **A column answered under only one spelling.** The rename from `snake_case` to `camelCase` replaced the key rather than adding to it, so a field declared the way the database spells it — `created_at: String`, legal GraphQL and what someone mirroring their columns writes — resolved to null. Not an error, just an empty field. Both spellings are now offered, and a row that already carries both keeps them apart.

- **A wrong-typed attribute panicked the parser.** Reading an attribute with cty's `AsString` when it is not a string panics rather than failing, so `type = 5` in a connector, a number in any of a named operation's thirteen string attributes, or a non-string in a profile's `transform` stopped the process with a Go stack trace — before it had said which file, block or attribute, and reading as a crash rather than as a configuration mistake. All of them now name the attribute and what it was given, and a profile's transform takes the same values a flow's does.

- **A constraint the service would not apply was accepted.** A `type` block exists to refuse what does not fit, and a constraint whose name was not recognised was silently dropped: `string({ max_lenght = 5 })` parsed, validated, and left the field accepting anything while the configuration said otherwise. The same silence covered a constraint given the wrong kind of value, such as `format = 5`. Both are now refused, naming the field and either the constraint names that exist or the value that was not the kind it takes.

- **A GitHub account was stored under a mis-rendered identifier.** GitHub's user id is a JSON number where every other provider's is a string, so it decoded into a float64 and rendered in exponent form: the account was recorded under `1.2345678e+07` rather than `12345678`. Stable enough to log the same person back in, and wrong for anything comparing it with the identifier GitHub reports. A social sign-in that can neither join an existing account nor create one — `match_by = "none"` against an address already registered — now says so instead of surfacing "user already exists" from three layers down.

- **`mycel migrate` had never run.** It read a `dsn` property and a `path` property that no database connector has, so the address it opened was always empty, and it mapped the configured driver to `pgx` and `sqlite3` while the connectors register `postgres` and `sqlite`. Every invocation ended on the first line with `sql: unknown driver`. Two more waited behind it: the tracking table was created with SQLite's `AUTOINCREMENT` and retried with PostgreSQL's `SERIAL` only if the error text mentioned the word, so MySQL got neither; and the insert recording an applied migration used `$1` on every driver, which on MySQL and SQLite is not a parameter — it would have failed after the schema change was already made, leaving it applied and unrecorded for the next run to apply again. The addressing now lives beside `ParseURL` in the database package instead of in a second copy under `cmd`, accepts a whole `url`, and the command pings before starting so an address that goes nowhere is reported before a migration is half run.

- **A failed hot reload took the service down.** The reload dismantled what was running before it knew the replacement worked — the old connectors closed and the flow registry emptied, then the new ones built — so anything that failed after that point left the process alive and serving nothing at all. Measured: one flow registered before the reload, zero after, with the failure reported only to the log. Since a reload is triggered by writing a file, a typo in a driver name took a running service down. What was called rollback restored the configuration pointer alone. The new configuration is now built beside the old one and swapped in only once it stands up; every failure path restores the running state and closes whatever the abandoned attempt opened. A reload also makes the same flow-schema check startup makes, so it cannot install a configuration that would have been refused at boot.

- **A user's roles were lost by a database-backed store.** Roles decide authorization — the middleware matches them against a path rule, the JWT carries them downstream, a flow reads them as `auth.roles` — and neither SQL user store wrote or read them. A service holding its users in memory had roles; the same service pointed at a database got every user back with an empty list, so every role rule refused and every claim was absent, with nothing reported. The column is opt-in, since selecting one nobody created would stop a working service from reading its own users: writing `fields { roles = "roles" }` turns it on and changes nothing without it. Roles are stored as a JSON array, and read as either that or the comma-separated list an existing table is as likely to hold. The `users` block is now described in the schema as well — it was named there and detailed nowhere.

- **An OAuth2 token was never refreshed when the provider stated no lifetime.** The two grant paths disagreed: the client credentials grant assumes an hour when `expires_in` is absent, while the refresh path left the expiry unset — and an unset expiry reads as "not expired" for ever. Against a provider that omits the field, the service worked until the token quietly expired and then answered every request with somebody else's 401 until it was restarted. Both paths now assume an hour.

- **Two aspects sharing one rate limit, and unrelated circuits tripping each other.** A rate limiter was keyed on the rate and burst alone, so two separately named aspects that happen to allow the same rate shared one budget — a caller allowed ten requests a second by each got ten between them, an allowance halved by an unrelated aspect elsewhere in the configuration. A circuit breaker is keyed on its name, which is what lets several flows calling one dependency trip together, but the name is optional and every breaker written without one shared the empty key, so a failing dependency opened the circuit on flows that had never called it. Each now belongs to the aspect that declared it; naming a breaker still shares it deliberately.

- **A refused request was sent to every fallback profile.** A connector's `fallback` list answers one question — this backend is not available, is there another? — but the check behind it treated every error as worth retrying, with a TODO naming the cases it should refuse. A request a backend understood and rejected was passed on to each remaining profile in turn, which for a write repeats a side effect that already failed and turns one 4xx into as many requests as there are backends. Errors that report themselves permanent — the same ones the retry budget stops on and an MQ consumer drops rather than redelivers — no longer fall through, and neither does a cancelled request.

- **Named operations never reached the connectors.** A connector may declare its operations once and have flows refer to them by name, which the documentation covers and an example ships. The resolver that turns `get_user` into `GET /users/:id` was written and only the startup banner ever called it — everything that runs read the flow configuration directly, so the name arrived at the connector verbatim. On a REST source that meant registering a route literally called `get_user`, which panics the HTTP mux while the service is still starting: the shipped `examples/named-operations` validated and then crashed the binary. Names are now folded into their inline form before flows are registered, which fixes sources, destinations and steps together, and a database operation is routed to the parameter matching what it declares — a raw query into `query`, a table into `target` — since a query left under `target` is used as a table name. An operation that cannot be formatted fails at startup naming the flow, the operation and the connector.
- **A flow serving `/health` panicked the service.** Registering the same pattern twice on an `http.ServeMux` is fatal, and the built-in health and metrics endpoints were registered unconditionally. They now come last and yield to a path a flow claimed, which is the reading that matches the configuration: an explicit flow is a deliberate choice.
- **`param` blocks were parsed and never enforced.** A parameter contract — `param "limit" { type = "number", default = 100, max = 500 }` — had `required` checked by a function nothing called, `default` computed into a value nothing read, and `min`, `max`, `min_length`, `max_length`, `pattern` and `enum` stored on the definition and never looked at, under a comment marking where the check would go. The block documented protection that was not there. Defaults are now applied and constraints enforced before the flow runs, using the same constraint implementations the `type` block uses; every problem in a request is reported at once, as a `400`. The declared type converts rather than rejects — path and query parameters arrive as strings, so `type = "number"` would otherwise reject every request that uses it.

- **38 connector attributes were described everywhere and impossible to write.** The parity test added with the schema unification found them across 12 connectors, and every one turned out to be read by its connector: the CSV reader's delimiter and comment character, the PDF page size, font and margins, the queue heartbeat and reconnect delay, TCP's idle timeout, webhook's HTTPS requirement and IP allow-list, cache pool sizing, CORS credentials, the OIDC provider name, SMTP's TLS mode. They were settings that were implemented, declared by the schema, offered as completions and rejected by the parser, whose hand-written allow-list had fallen behind. Four needed more than an allow-list entry: exec's `env` block and the SQL read `replicas` were parsed and then discarded, so neither reached the connector — replicas are now written one block each and collected into the list the factories read; the webhook connector had grown a second vocabulary for retry, `max_attempts` and `initial_delay`, for settings the shared block already had, now folded onto `attempts` and `delay` with both spellings accepted and writing both an error; its `multiplier` was read through a `float64` type assertion, so the obvious `multiplier = 2.0` arrived as an int and was dropped; and exec read `workdir` while the documentation and examples use `working_dir`, so the former never parsed, the latter was never read, and the working directory silently did not apply.
- **A command's `env` block wiped its environment.** Making the block reachable exposed the choice behind it: the declared variables replaced the environment rather than being added to it, so a command given one variable lost `PATH`, `HOME` and everything else. They are added now, with a declared variable still overriding an inherited one of the same name.

- **The parser accepted 19 connector attributes nothing described, and twelve that nothing read.** Running the parity check the other way found them. Four were real settings invisible to tooling — the gRPC client's `target`, `insecure` and `wait_for_ready`, plus exec's `args` and TCP's `retry_delay` — and connector profiles were undescribed entirely, so `select`, `default`, `fallback` and the `profile` block, which is how one connector resolves to different backends, appeared in no completion. Of the twelve dead words, eight were MongoDB connection settings: `auth_source`, `auth_db`, `replica_set`, `srv`, `read_concern` and `direct` are now applied as the URI options they are, to a URI built from parts, while `max_pool` and `min_pool` were removed because the `pool` block already carries them. `address`, `origins` and `wsdl` were removed too, each having a working spelling elsewhere or no meaning at all.

- **The `tls` block had three vocabularies and the parser accepted one.** `http` read `ca_cert`, `client_cert`, `client_key` and `insecure_skip_verify`; `grpc` read `enabled`, `cert_file`, `key_file`, `ca_file`, `server_name` and `skip_verify`; `tcp`, `mq` and `mqtt` read `cert`, `key`, `ca_cert` and `insecure_skip_verify`. Only http's four were accepted, which meant `cert` and `key` were rejected everywhere — so mutual TLS could not be configured on `tcp`, `mq`, `mqtt` or `grpc` at all — and `enabled` was rejected while `mq` and `mqtt` build TLS only when it is true, so those two could not be given TLS by any spelling. The canonical names are the ones three of the five connectors already read, plus `server_name` where it applies; every older spelling is still accepted and folded onto its canonical name, so no working configuration breaks. Writing two spellings of one setting is now an error rather than a silent discard, and writing the block enables TLS, following the rule the `mfa` block already set.
- **Connector schemas were never checked against the parser.** The schema parity test covers root blocks, which is how the gRPC connector came to advertise six TLS attributes the parser refused: completions offered names that could not be written. Both schema registries — the runtime's own and the copy in `pkg/connectors` that external tooling links against — are now checked for the `tls` block.

### Security

- **A gRPC server whose TLS could not be built started in plaintext.** `buildServerOptions` discarded the result of `credentials.NewServerTLSFromFile` with an `if err == nil`, so when the certificate failed to load the listener came up unencrypted while the configuration said otherwise. It failed routinely rather than rarely: the parser accepted none of the attribute names that connector reads, so the certificate paths were always empty. The error is now returned and the server refuses to start, and TLS enabled with no certificate at all is reported as such.

### Documentation

- **The `tls` block is documented once, for every connector that speaks it.** It appeared in five pages in three different vocabularies, and two of the names shown for MQTT — `ca` and `insecure` — were never accepted by anything, so that block was a parse error in both pages that carried it. The new [TLS](docs/core-concepts/connectors.md#tls) section covers the attribute list, why `cert` and `key` are one setting seen from two sides, that a connector which cannot load its certificates does not start, and the older names with what each is now. Every documented block containing `tls` was run through the parser.

## [2.18.0] - 2026-08-11

### Fixed

- **The Helm chart mounted configuration the runtime ignores.** The ConfigMap wrote `service.hcl`, `connectors.hcl`, `flows.hcl` and `types.hcl`, and auto-discovery globbed `config/**.hcl` — but the runtime has only parsed `.mycel` since 1.18.0. Deploying with the chart mounted those files at `/etc/mycel` and started a service with an empty configuration, silently: no error, no warning, zero connectors and zero flows. Keys are now written as `.mycel`, and files dropped in `config/` are matched on both extensions and renamed, so a chart directory left over from before the rename keeps working.
- **The chart's own default configuration did not parse.** `mycel.config.service` shipped a `service` block with a `port` attribute, which the parser rejects — the block takes `name`, `version` and `admin_port`, and a listening port belongs to the connector that listens. The extension bug had been hiding it, since the file was never read in the first place.

### Added

- **The Helm chart is signed.** Every release now signs the chart and both container images with cosign, keyless: the workflow's OIDC token is exchanged for a short-lived Fulcio certificate, so there is no private key to store or rotate. Signing is by digest, which covers every tag pointing at the same manifest. This is also what Artifact Hub looks for to mark the chart as signed.
- **`values.schema.json` for the Helm chart.** Helm validates values against it on `install`, `upgrade`, `template` and `lint`, so a typo or a wrong type fails with a precise message — `additional properties 'replicasCount' not allowed` — instead of rendering broken manifests. Enums are pinned where the chart only accepts a fixed set (`logLevel`, `env`, `pullPolicy`, `service.type`, `pathType`), ports are range-checked, durations and paths are pattern-checked, and the keys of `mycel.config.extra` must end in `.mycel` for the same reason the ConfigMap fix exists. Artifact Hub renders it as a browsable reference.

## [2.17.1] - 2026-08-11

### Security

- **Every vulnerability the image reported is fixed.** Publishing the chart on Artifact Hub turned on its Trivy scan, and 2.17.0 came back with 2 critical, 14 high, 13 medium and 5 low — all of them with a fix already released. The two critical ones were in the PostgreSQL driver. Bumped: `jackc/pgx` 5.8.0 → 5.9.2, `golang.org/x/crypto` 0.51.0 → 0.53.0 (nine highs), `golang.org/x/text` 0.37.0 → 0.39.0, `google.golang.org/grpc` 1.81.1 → 1.82.1, `xuri/excelize` 2.10.1 → 2.11.0, `mongo-driver` 1.17.6 → 1.17.7, `aws-sdk-go-v2` and `service/s3`, `filippo.io/edwards25519` 1.1.0 → 1.1.1, and `cel-go` 0.26.1 → 0.29.0. The runtime base moved from `alpine:3.19`, which is where the musl findings came from, to `alpine:3.22`. The rebuilt image scans clean at every severity.
- The `cel-go` bump moves the engine every transform runs on, so it was checked beyond the unit tests: the same configuration was run against binaries built either side of the bump, exercising macros, `??`, string and aggregate functions, a chained `output` reference, `has()` and the CEL extensions. The results are identical.

## [2.17.0] - 2026-08-11

### Added

- **`llms.txt` at the documentation root.** An index written for assistants rather than browsers: what Mycel is, the handful of facts that stop a model from inventing syntax — expressions are quoted CEL, `input` is shaped by the source, output fields are the left-hand side, the parser is the authority — and a described link to every page worth fetching. Served at <https://matutetandil.github.io/mycel/llms.txt>.
- **The Helm chart is publishable on Artifact Hub.** The release pushes an `artifacthub-repo.yml` to the chart's OCI repository under the reserved `artifacthub.io` tag, and `Chart.yaml` now carries the category, image, link and maintainer annotations Artifact Hub renders. The image tag in the annotation is rewritten from the git tag alongside `version` and `appVersion`, so it cannot go stale.

### Fixed

- **Transform fields were evaluated in random order.** A field may reference one computed above it through `output` — `tax = "output.subtotal * 0.21"` — which the documentation shows and the examples use. But the mappings were held in a map and the rule list was built by ranging over it, so every message picked a fresh order and a backward reference resolved, or silently didn't, at random: the same config and the same payload produced the intended value on twelve of fifteen requests and a missing field on the other three. The parser now records the order the fields were written in and every site that turns mappings into rules honours it — `transform` (inline and named), `response` (inline and named), per-destination `transform`, `fallback` transform and `error_response` body.
- **A non-string mapping value panicked the binary.** `transform { count = 5 }` — or a `response` or `error_response` body holding a number, a boolean or an object — crashed with a Go stack trace out of cty's `AsString`, which every mapping value went through once HCL had evaluated it. Bare numbers and booleans are now kept as the constants they obviously are; anything else names the field and says to quote the expression.
- **An unquoted expression could lose half of itself.** `upper(input.name)` was stored as `input.name` and `output.total > 1000` as `output.total`, because the parser rebuilt the expression from its single variable reference rather than its text. Nothing errored — what remained was still valid CEL — so the transform wrote the un-uppercased name and the conditional write fired on a truthy number. Affects every unquoted expression the parser accepts: transform and response mappings, `to.when`, `dedupe.key` and fingerprints, `cache.key`, transaction `exec.when` and aspect `on` entries. Quoted expressions, the documented form, were never affected.

### Documentation

- **`input` and `output` are explained.** Every flow uses them and nothing introduced them: the first appearance in the learning path was a bare `input.name` in the quick start. The new [Input and Output](docs/core-concepts/input-and-output.md) page covers where `input` comes from and how its shape differs per source, why the field name on the left of a transform line is never written as `output.name`, what `output` means in each block that can read it, the order fields are evaluated in, and why expressions are quoted.
- **`output.field = ...` was shown as valid in nine pages.** HCL attribute names are single identifiers, so those forty-odd lines are not a Mycel-level mistake — the file does not parse at all. Rewritten to the bare field name, and the unquoted right-hand sides in the same snippets were quoted.
- **`input.params.*` and `input.query.*` do not exist.** A REST request arrives flattened onto `input`: path parameters, query parameters and body fields all sit at the top level. The forty-two occurrences across the documentation were a hard `no such key: params` at runtime, and seven of them were in example configurations that validate but fail on the first request.
- **The integration patterns page was never migrated.** Its second half described a language that has not existed for several releases: `connector.rabbit = { ... }` in place of `connector` + `operation` (twenty-seven times, a hard parse error), a `foreach` block and a `response { status, body }` block that were never implemented, queue and DLQ settings written inside the flow rather than on the connector, and `rate_limit`, `circuit_breaker` and `semaphore` attributes that no parser accepts. Rewritten against the runnable configurations in `examples/integration/`, with every block checked by the parser. The first half also read an MQ payload as `input.field` rather than `input.body.field`.
- **`ctx` is documented as reserved rather than usable.** It is declared in the expression environment and older examples show `ctx.user_id`, but nothing has ever filled it — it always evaluates as an empty map. Request headers are read from `input.headers`.

## [2.16.0] - 2026-08-10

### Added

- **`mycel add saga`, `add state-machine`, `add validator` and `add transform`.** The four blocks that were left out of the scaffolding in 2.14.0, now that their schemas describe them. As with the other generators, the shape, the allowed values and the comments come from `pkg/schema` — nothing is a template, so nothing can drift from the parser.
- **The saga, state machine, validator and transform blocks are described in the schema.** They were declared `Open`, meaning "any attribute goes", so completions offered nothing inside them and a generator had nothing to read. Each is now transcribed from its parser, which remains the authority. `transform` keeps `Open` — its attributes are CEL mappings named by the author — but gains the `enrich` child block.
- **`mycel validate` reports sagas, state machines, validators and transforms.** It listed connectors, flows and types only, so a project whose saga had just been added got no acknowledgement that the file was read.

- **Every root block is now checked against the parser.** The parity test covers all fifteen, not the four that were rewritten, so a schema that stops matching its parser fails the build.

### Fixed

- **The schema advertised an `environment` block that does not exist.** No such block type is accepted anywhere, so a document containing one fails to parse — but completions offered it, and a test asserted it was there. Per-environment configuration is connector profiles and `env()`, selected with `--env`.
- **`auth`, `security`, `service.rate_limit`, `functions` and the aspect's `rate_limit` and `circuit_breaker` were declared `Open`,** meaning any attribute is valid. Their parsers accept a fixed set and reject the rest, so nothing inside them was ever checked or completed. Each is now described; `auth`'s fourteen nested blocks are named, with `storage` and `audit` described in full.
- **A named `transform` holding an `enrich` block never parsed.** The runtime reads those enrichments when a flow references the transform, and the documentation shows one, but `hcl.Body.JustAttributes` refuses a body containing any block — including the `enrich` blocks the parser had just consumed itself. Found by the new schema parity test on its first run.
- **A saga with no `from` block was skipped at registration in silence.** Nothing else triggers a saga, so it loaded, validated and never ran. It now says so by name at startup, and `mycel add saga` requires `--from` rather than generating one.

### Changed

- **`mycel add validator` requires the rule for the type chosen** (`--pattern`, `--expr` or `--wasm`). The parser rejects an empty one by name, so generating a placeholder there produced a file that could not be parsed.

## [2.15.0] - 2026-08-10

### Added

- **Linux packages and prebuilt binaries.** Every release now attaches `.deb`, `.rpm`, `.apk` and `tar.gz` archives for linux and darwin on amd64 and arm64, with checksums. Until now the only artifact was the Helm chart, so installing without Docker or a Go toolchain was not possible.

  The packages carry more than the binary — a systemd unit, an unprivileged `mycel` user, `/etc/mycel` and `/var/lib/mycel` — so a server install is a service rather than a loose executable:

  ```bash
  sudo dpkg -i mycel_2.15.0_linux_amd64.deb
  sudo systemctl enable --now mycel
  ```

  Verified by installing into Debian 12 and Rocky Linux 9 containers: the binary runs, the `mycel` user exists, the unit lands in the right place per packager, and a scaffolded project validates.

  There is no apt or yum repository, so `apt install` and `apt upgrade` still do not apply — that needs hosting, repository metadata and a GPG signing key, and is worth doing only if the demand appears.

- **`install.sh`** for `curl -fsSL … | sh`. Detects platform and architecture, resolves the latest release, and installs to `/usr/local/bin`, reaching for `sudo` only when that directory is not already writable. `MYCEL_VERSION` pins a version and `MYCEL_INSTALL_DIR` relocates it.

- **Homebrew is documented in the installation guide**, which it was not when the tap was published.

- **Homebrew install.** `brew install matutetandil/tap/mycel`, from a new [tap](https://github.com/matutetandil/homebrew-tap). The formula builds from the source tarball GitHub generates for each tag, so it needs no release artifact that did not already exist.

  No code signing or notarization is involved. Homebrew requires both for **casks**, and makes them mandatory for the official cask tap from September 2026, but a command-line **formula** is exempt — and a Go binary is ad-hoc signed, which cannot be notarized at all. Verified on the installed binary: `Signature=adhoc`, `TeamIdentifier=not set`, and it runs without Gatekeeper intervening.

  The release workflow updates the formula's version and checksum when a tag is published, so the tap cannot drift behind. It runs after the release is complete, so a failure there leaves the image, chart and GitHub release published and means only that the tap needs attention.

## [2.14.0] - 2026-08-08

### Added

- **`mycel init`** scaffolds a project in the recommended layout — `config.mycel`, `connectors/`, `flows/`, plus `.gitignore` and `.env.example`. The generated service runs as-is: start it and `GET /status` answers. It refuses to overwrite, and writes nothing at all if any file would clash.

- **`mycel add connector | flow | aspect | type`** puts each new declaration in its own file. Anything supplied as a flag is written out, so a caller who knows what they want gets a finished file rather than one to edit:

  ```bash
  mycel add connector orders_db --type database --driver postgres
  mycel add flow order_created --from rabbit --operation "orders.created" --to orders_db --target orders
  mycel add aspect audit --on "create_*" --when after --action-connector audit_db
  mycel add type user --fields "id:number,email:string:email"
  ```

  Skeletons are generated from the connector's or block's **own schema**, not from templates, so what they emit cannot drift from what the runtime accepts. Every reference is checked before anything is written — a connector or flow that does not exist, a name already taken, a `--when` or field type the schema rejects, and an `--on` pattern matching no flow, tested with the same matcher the aspect registry dispatches with.

  Mycel does not require this layout and never will: it reads every `.mycel` file under the config directory and merges them, so a single file behaves identically. The commands make the maintainable shape the path of least resistance rather than a suggestion.

- **[Project Structure](docs/getting-started/project-structure.md) documentation.** The loading rule — every `.mycel` file, recursive, merged — existed only as an architectural aside, never as an answer to "how do I lay this out?". Covers layouts by project size and the consequences that are not obvious: names are global rather than per file, directory names mean nothing to the runtime, and the few paths that are load-bearing anyway.

- **`mycel validate` gives readability advice**: a file past eight declarations, and a file declaring exactly one thing but named after something else. Advice, never a failure, and never shown by `mycel start` — where a declaration lives changes nothing at runtime, and a startup that warns about style is one whose real warnings get ignored. Calibrated against the production services it was written for: one advisory across 81 of their files.

### Changed

- **Type fields are described in the schema.** `TypeSchema` was open with nothing else declared; field types, string formats and constraints are now schema-level facts, which is what let `mycel add type` generate a correct field and what the IDE needs to complete one.

- **Example files are named after what they declare**, and their READMEs updated. Twenty renames; filenames carry no meaning for the runtime, so no example behaves differently.

- **Pull requests now build the Docker image and run the integration suite.** Unit tests already ran on every pull request, against the merge result; two things did not.

  The runtime image was built for the first time *at tag time*, so a Dockerfile that no longer matched the source only failed once a tag existed — the expensive moment to find out. v2.10.0 shipped a `go.mod` requiring Go 1.25 against a `golang:1.24` base and the release had to be re-tagged, which meant disabling a tag-protection ruleset to do it. CI now builds it, and the integration mock server that broke in the same release, on every pull request: `linux/amd64` only and pushed nowhere, in a job that runs alongside the others. Measured at 98 seconds cold and 2 seconds cached.

  The integration suite — 146 assertions against twelve real services — was `workflow_dispatch` only, so it ran when someone remembered. Nothing else exercises the connectors against real brokers, so a regression there was found after merging, if at all. It now runs on pull requests too.

### Fixed

- **The type constraint syntax in the documentation never parsed.** Every example used `email = string { format = "email" }`; HCL reads that as an argument followed by a block, and a type body accepts only attributes. Constraints are call arguments — `string({ format = "email" })` — and that form works end to end, verified against a running service rejecting a bad address with 400. The feature was real; only the documented syntax was wrong, which is why it survived. 80 occurrences across 11 files; every `type` block in the docs now parses, against 7 of 11 before.

- **39 one-line blocks in the documentation could not parse.** A single-line block accepts exactly one argument, and these carried two or more — seventeen of them the auth endpoints table, where every entry was written that way. None were in `.mycel` files, which is why the examples kept validating.

- **`environments/` overlays do not exist.** The environments page described a directory of per-environment files overriding a base config, selected by `MYCEL_ENV`. There is no such mechanism, and following it produced a config that would not parse — the documented example redeclared a connector, which is a duplicate name. Replaced with what works: `env()` for values and connector profiles for connectors that differ in shape.

- **`mycel validate` now checks aspects the way startup does.** An aspect whose action named neither a connector nor a flow passed validate and then failed to start.

- **`on_drop` was missing from the aspect schema.** The runtime has supported it since 1.21; the schema listed four `when` values, so the IDE offered four and a config using the fifth looked unrecognised. A test now fails if the two diverge again.

- **The startup banner described response and fan-out flows as `(echo)`** — including the first flow `mycel init` generates.

- **`go install` installed v1.22.0 instead of the current release.** Go's semantic import versioning requires a module released at major version 2 or above to carry a `/vN` suffix in its path. Mycel has been on v2 since May with an unsuffixed module path, so every v2 tag was invisible to the toolchain and `go install github.com/matutetandil/mycel/cmd/mycel@latest` — the command in the README, the quick start, the installation guide and the debugging guide — silently handed people a release from April, fourteen versions behind.

  The module path is now `github.com/matutetandil/mycel/v2`, and the documented command is:

  ```bash
  go install github.com/matutetandil/mycel/v2/cmd/mycel@latest
  ```

  This only affects `go install` and anyone importing Mycel as a library; Docker, Helm and the released binaries were never involved. It cannot repair the existing tags — v2.13.0 and earlier carry the old `go.mod` forever — so the corrected path starts resolving at the next release.

- **`mycel version` reported `commit: dev` in every build, released images included.** Nothing set the version or commit through ldflags, so the commit was a placeholder and the version was whatever was hardcoded in the source — a guess for a `go install` binary, which reports what the source claimed rather than what was installed. Both now come from the build metadata Go already embeds: the module version for `go install`, and the VCS revision with a `-dirty` flag for a build from a checkout. `mycel --version` agreed with neither and now matches.

### Documentation

- **How to update.** There is no separate update command and nothing self-updates; re-running the install command is the update. Documented alongside pinning a version, and the Docker/Helm equivalent.

## [2.13.0] - 2026-08-06

Every item here has the same shape: a misconfiguration that produced no error, just an absence of behaviour. Nothing in this release changes what a correct configuration does.

### Added

- **`mycel version`.** Documented in the CLI reference, the README and the installation guide, but never actually registered — only cobra's `--version` flag existed. It prints the version, build commit, Go toolchain and platform. The same information is in the startup banner, which is no help on a pod that has been running long enough for that line to roll out of the log buffer.

  ```
  mycel 2.12.0 (commit: 64be3eb, go1.25.0, linux/amd64)
  ```

- **`mycel_messages_undispatched_total{connector,queue,routing_key}`.** Counts messages that reached a consumer and matched no flow handler. Previously nothing recorded these: the queue showed deliveries with no acks, and no metric moved.

- **`mycel validate` reports unset `env()` references.** Startup already names the missing variable when the empty value leaves a required attribute unset (2.11.1), but validate passed clean on the same config. Unset variables are a warning, not a failure — validate legitimately runs in CI, without the deployment environment — and the output names the connector and attribute each one feeds.

- **`mycel validate` and startup report configuration that has no effect.** `params` on a `to` block is the first case: it parses, and nothing ever reads it. A write sends the transform output, or the raw input when the flow has no transform. The attribute is real on `step`, on `enrich`, and on `exec` inside a `transaction`, which is how it ends up copied onto `to`.

- **`mycel check` actually checks.** It created the runtime, printed `✓ All connectors configured correctly!` and exited zero without opening a single connection — a database on an unroutable address passed, and the documented per-connector output did not exist. It now builds, connects and health-checks every connector, concurrently and each with its own timeout (`--timeout`, default 10s), reporting all of them rather than stopping at the first failure:

  ```
    ✓ orders_db (database/postgres): connected in 12ms
    ✗ payments_api (http): no response within 10s

  Error: 1 of 2 connectors unreachable
  ```

  Exits non-zero when anything is unreachable, so it works as a deploy gate. `connection refused` (something answered and said no) is distinguished from `no response within <timeout>` (nothing answered at all), and failures to build the connector are reported the same way, including the missing environment variable behind an empty `env()`.

  Connectors that listen rather than dial — REST, GraphQL, gRPC, SOAP and TCP servers, plus SSE and WebSocket — are reported as such and never fail the check: they have no endpoint to reach, and are not started here, so their health check would only ever report "not started". They declare this through a new optional `connector.InboundOnly` interface rather than being matched by type, since server and client are already separate types.

- **Per-flow timing extremes and throughput**, over successful executions only: `mycel_flow_duration_fastest_seconds`, `mycel_flow_duration_slowest_seconds`, `mycel_flow_duration_average_seconds` and `mycel_flow_messages_per_second`.

  Most of this was already derivable from the `mycel_flow_duration_seconds` histogram — `rate(_count)` is the throughput and `rate(_sum)/rate(_count)` the average — and those queries remain the better instrument where a Prometheus server is available, being windowed rather than cumulative. Two things were not derivable: a histogram records which bucket a value fell into rather than the value, so the true fastest and slowest cannot be recovered from it, and the histogram is not split by status, so nothing from it can be narrowed to messages that actually succeeded. A flow failing in 1 ms would otherwise take the "fastest" spot and pull the average down.

  Computed at scrape time by a collector, with no background goroutine, and bounded memory per flow (throughput uses a fixed 60-slot ring, not a list of samples). The existing histogram is unchanged and still observes every execution.

- **The sync, cache and connector metrics are recorded.** They were defined, registered and documented from the start — including a Grafana panel for cache hit rate — with no call sites anywhere, so they were permanently absent from `/metrics`. Now emitted: `mycel_lock_*`, `mycel_semaphore_*`, `mycel_coordinate_*`, `mycel_cache_{hits,misses}_total`, `mycel_connector_health`, `mycel_connector_operations_total` and `mycel_connector_latency_seconds`.

  `mycel_lock_wait_seconds` is the one worth watching: time spent waiting for a lock is invisible in `mycel_flow_duration_seconds`, so a consumer that looks fast per message can still be serialized behind a hot key.

  Lock metrics carry a second label, `purpose`: `flow` for the flow's own `lock {}` block, guarding a business key, and `dedupe` for the critical section around the duplicate check. Contention means different things in each — a hot business key versus duplicate deliveries piling up — and they want different responses.

  **The sync metrics are now labelled by `flow`, not by `key`.** Lock, semaphore and signal keys are CEL expressions evaluated per message — one per order, per SKU, per customer — so recording them as declared would have grown the time series set without bound. `mycel_connector_operations_total` labels the operation coarsely (`read`, `write`, `call`) for the same reason. Since none of these metrics had ever been emitted, no existing dashboard or alert can break.

- **`mycel_flow_drops_total{flow,reason}`**, and a `dropped` status on `mycel_flow_executions_total`. A declined message was counted as a success, so a consumer filtering out most of its input reported full productivity and there was no way to graph or alert on drops at all — only to read them out of the log. `reason` matches the drop log line, so the two are read together. **This corrects existing series**: flows with a `filter`, `accept`, `dedupe`, `sequence_guard` or `coordinate` timeout will see `status="success"` fall and a `status="dropped"` series appear.

  Drops are also excluded from the timing gauges above. A drop short-circuits before the transform, so it was owning the "fastest" gauge permanently and pulling the average down — on a flow filtering 8 of 9 messages, `fastest` reported 14µs of doing nothing instead of the 1.9ms of real work.

- **Every deliberate drop explains itself at debug level.** A message Mycel declines to process is not an error, so nothing failed, nothing logged, and the result was indistinguishable from a broker that never delivered it. Each gate already reported a stable reason, but only `on_drop` aspects ever saw it.

  ```
  DBG message dropped by policy flow=only_big_orders source=api reason=filter
      decided_by="from { filter }" detail="input.total > 100" disposition=ack
  ```

  `decided_by` names the HCL block to go and edit, and `detail` the expression, fingerprint or sequence numbers that block was judging — `reason` alone says which gate said no, not why. Covers `filter`, `accept`, `dedupe`, `sequence_guard` and `coordinate` timeouts, logged from the one choke-point every source funnels through, so the line is identical whichever connector delivered the message. The payload can be included with `MYCEL_PAYLOAD_SHOW`, under the same cap as the incoming-payload log; it stays a separate opt-in because a dropped message is still customer data.

### Changed

- **A message no flow can handle is now an error, not a warning.** This applies to **RabbitMQ, Kafka and Redis**, which all had the same hole: WARN, no metric, message gone. Each states what it just did, because the outcome differs — RabbitMQ nacks without requeue (a dead-letter exchange may still catch it), Kafka commits the offset (it will not be redelivered), Redis pub/sub simply discards.

  The usual cause is that on a message queue source `operation` reads like an operation *name* but is a subscription *pattern*, so an invented value matches nothing and every delivery is dropped. The first occurrence per key is logged at ERROR with the patterns flows actually registered alongside it — the difference between the two is the diagnosis. Repeats only move the counter, so a misconfigured consumer does not drown the log.

- **Startup states, per flow, which messages it will actually receive.** On a stream source `operation` is optional, which reads as inert — it is not. Declaring it registers the flow's handler under that key and filters every delivery against it, a *second* filter after the broker's own exchange and binding. Nothing said so, at startup or in the reference.

  ```
  INF dispatch: flow only accepts matching messages connector=rabbit flow=item_create
      operation=all.in.magento.q meaning="only deliveries whose key matches \"all.in.magento.q\" reach this flow"
  WRN dispatch: messages matching no pattern will be DROPPED connector=rabbit
      patterns="\"all.in.magento.q\"" hint="on a message queue source `operation` is a
      subscription pattern, not an operation name; omit it to accept every message"
  ```

  The warning fires only when **every** flow on a connector is narrowed, since one catch-all sibling guarantees a handler for anything that arrives. It runs before connectors are dialled, so the dispatch shape is visible even when the broker is down and startup is about to fail, and it is driver-agnostic: which connectors treat `operation` as a subscription pattern comes from their own `SourceSchema`, so RabbitMQ, Kafka, Redis, MQTT, CDC, WebSocket and file watch are all covered without a per-driver list. RabbitMQ consumers additionally log an error when they start with no flow handlers at all.

- **`dlq { enabled = true }` says plainly when it is not in effect.** Mycel provisions the dead-letter exchange only when it declared the queue itself; on a pre-existing queue it provisions nothing, so a message that exhausts its retries is discarded unless the queue already carries `x-dead-letter-exchange` or a server-side policy sets one. This was already warned about, but the message opened with the part that still works and left the conclusion to the end. It now leads with the conclusion and states what to check.

  It stays a warning rather than a startup failure because Mycel genuinely cannot check: AMQP's `queue.declare-ok` returns the name, message count and consumer count and nothing else, and policies are not visible over AMQP at all — a correctly dead-lettered queue and one with no dead-lettering anywhere look identical. New `dlq { external = true }` records the answer once you have checked the broker yourself: Mycel then provisions no DLX or DLQ, sets no `x-dead-letter-exchange` argument (it does not know which exchange name ops chose), and stays quiet. Retry counting is unaffected either way, and omitting the attribute keeps the previous behaviour exactly.

- **Per-delivery handler lookup logging moved from info to debug.** It logged a line for every message in production.

## [2.12.0] - 2026-08-04

### Added

- **Flow source parameters are validated against the connector's schema.** Connectors have always declared which flow parameters they require via `ConnectorSchemaProvider.SourceSchema`, but only the IDE engine consumed that contract. A REST flow missing its `operation` parsed cleanly and registered a handler for the empty operation instead of failing. `mycel validate` and `mycel start` now check every flow's `from` block and report each missing required attribute:

  ```
  flow "create_user": from block is missing attribute "operation" required by connector "api" (rest)
  ```

  Only attributes marked required are enforced, so connectors that default a parameter are unaffected — message queues, MQTT, CDC, WebSocket and file watch all default `operation` to the catch-all `"*"` and stay legal without it. Connectors with no registered schema, such as plugin-provided ones, are skipped. Verified against all 81 example config directories with no new failures.

### Fixed

- **CLI errors no longer dump the usage text.** Every failure printed the full flag list after the error, burying the diagnostics — a config that fails to validate is not a usage mistake. Usage is suppressed from the point a command starts running; flag and argument errors are raised before that and still print it, and unknown commands keep cobra's `Run 'mycel --help' for usage.` hint. `main` also printed the error a second time after cobra had already reported it.

### Documentation

- **`operation` was documented as always required in the `from` block; it isn't.** It is required for `rest`, `graphql`, `grpc`, `soap`, `tcp` and `sse`, and optional for `mq`, `mqtt`, `cdc`, `websocket` and `file` watch, which default it to `"*"`. The flows page and the source properties reference now state which case each connector falls into, and the catch-all default is documented for the first time.
- **The connector type for message queues was documented as `queue`.** The real HCL type is `mq` — `queue` parses but fails when the runtime builds the connector.
- **New "Flow Anatomy" section opens the flows page**: the pipeline diagram, then a table of all 21 blocks and 4 attributes a flow can contain, each with what it does and a link to its full documentation. Writing it surfaced blocks the configuration reference had missed entirely — `accept` and `sequence_guard` were parseable but undocumented there, and the PDF connector had no section at all; all three added, plus a `sequence_guard` section on the flows page.
- **Navigation repairs.** The reusable blocks and resilience pages existed but were absent from the site navigation. Guides were ordered alphabetically in the nav while the documentation index ordered them as a learning path; the nav now follows the index. Top-level groups render as tabs instead of listing every page in one scroll. Every broken link and heading anchor is fixed — links leaving `docs/` now use absolute repository URLs (they resolved from the wrong depth and 404'd on the site), and heading slugs are GitHub-compatible so the same anchor works in both places. `mkdocs build --strict` completes with no warnings.

## [2.11.1] - 2026-07-27

### Changed

- **Connector startup errors now name the missing environment variable.** An `env("X")` call for an unset variable with no default silently evaluates to an empty string, so the connector factory failed with a generic message (`http connector requires base_url`) that gave no clue about the real cause. Connector blocks are now scanned at parse time for unresolved `env()` calls, and a registration failure appends them to the error:

  ```
  ✗ failed to register connector url_ms: factory failed to create connector url_ms: http connector requires base_url
        → Missing environment variable "URL_MS_BASE", required by connector "url_ms" (base_url)
  ```

  Nested blocks keep their path (`consumer.queue`), `env("X", "default")` calls are never reported, and the hint only appears when registration actually fails.

## [2.11.0] - 2026-07-04

### Added

- **HTTP QUERY method support (RFC 10008).** Flows can now declare `from { connector.api = "QUERY /search" }` — QUERY is the new IETF-standardized method (June 2026) that carries a query in the request body like POST, but with GET's safe, idempotent, cacheable read semantics. What v1 covers:
  - **REST server:** the QUERY request body is decoded (Content-Type aware, same as POST) and merged into the flow input alongside path/query parameters. Per the RFC, a QUERY with content but no `Content-Type` is rejected with `415`. The permissive dev-mode CORS fallback now advertises QUERY (browsers always preflight it — it is not a safelisted method).
  - **Runtime:** QUERY dispatches to the read path (`handleRead`), including steps/orchestration flows, response shaping, and the flow cache. The default cache key already incorporates the body fields (they're merged into the input), satisfying the RFC's requirement that request content be part of the cache key. QUERY never triggers write-side behavior (multi-destination writes, cache invalidation).
  - **HTTP client:** outbound requests with method QUERY encode and send the payload as the request body.
  - **QUERY proxying / body forwarding:** when a flow's destination targets QUERY (`to { target = "QUERY /search" }` or `operation = "QUERY"`), the whole flow input — the inbound QUERY body, path and query string params — is forwarded and encoded as the outbound request body (header metadata and internal fields excluded). A Mycel service can now sit in front of another QUERY-speaking API end-to-end; verified live between two services.
  - **Tooling:** the IDE/LSP accepts QUERY as a valid method (plus autocomplete). The OpenAPI export documents QUERY flows: since the `query` operation slot only exists from OpenAPI 3.2 on, a config containing QUERY flows is emitted as **OpenAPI 3.2.0** (with `query` operations and their request bodies); configs without QUERY keep emitting 3.0.3 for maximum tooling compatibility. The startup banner colors QUERY like the read method it is.
  - **Discovery:** responses on paths with a QUERY flow advertise the accepted media types via the RFC's `Accept-Query` response header (`application/json, application/xml`).
  - **Example:** [`examples/query-method`](examples/query-method) — body-driven search over SQLite (`QUERY /products/search` feeding raw SQL named params), verified end-to-end.

### Fixed

- **HTTP client now implements the `Caller` interface — saga, state machine, and step actions against HTTP APIs work end-to-end.** The saga executor (and the state machine engine) were designed to dispatch `action { operation = "POST /reserve", body = {...} }` through a `Call` interface that the HTTP connector never implemented — the branch was dead code, and HTTP actions fell through to the read path, which dropped the body. `Call` parses `"METHOD /path"` (bare paths default to GET), sends params as the encoded request body for body-carrying verbs (POST/PUT/PATCH/QUERY) and as query string parameters otherwise, and returns the decoded response so step results are usable in CEL (`step.<name>.<field>`). Flow `step {}` blocks against HTTP connectors also dispatch through `Call` now, gaining proper body semantics for write verbs. Verified live: a saga with a SQLite step plus two HTTP steps completes, and on a failing payment step the compensation fires with data captured from the prior HTTP response (`step.inventory.reservation_id`).
- **HTTP client: the schema-documented `"METHOD /path"` form never worked — and database-flavored operations leaked onto the wire.** The connector's own schema documents `operation = "GET /endpoint"` (and several examples use it), but that form failed with `invalid method "GET /endpoint"`; and when the verb was carried in `target` instead (`target = "POST /orders"`), the runtime's database-flavored defaults (`SELECT`/`INSERT`/`UPDATE`) clobbered it — requests actually went out with method `INSERT` and, because the body-encoding gate only matched real HTTP verbs, **an empty body**. Only the split form (`operation = "POST"` + `target = "/path"`) ever worked, which is why production configs using it never noticed. All forms are now normalized in one place (`resolveMethodPath`): a combined `"METHOD /path"` in either field wins, a bare HTTP verb in `operation` overrides the method (the split form is untouched), and a DB-flavored operation maps to its HTTP equivalent (SELECT→GET, INSERT→POST, UPDATE→PUT) only when `target` didn't carry an explicit verb. Verified live: flow `to {}` writes/reads in both forms, aspect `action {}` in both forms, and QUERY end-to-end between two Mycel services. Additionally, outbound `Read` with method QUERY now sends filters as an encoded request body (the method's purpose) instead of query string parameters.

## [2.10.0] - 2026-06-12

### Added

- **OpenTelemetry distributed tracing (opt-in).** Mycel can now emit OpenTelemetry traces over OTLP, so a request is followed end-to-end across services in Jaeger / Tempo / Grafana / any OTel backend. It's off by default and a strict no-op when unconfigured (the global tracer stays OTel's no-op, so instrumentation costs essentially nothing). Turn it on with `MYCEL_TRACING=true`, or simply by setting the standard `OTEL_EXPORTER_OTLP_ENDPOINT` — the OTLP exporter reads the rest of its configuration (endpoint, headers, TLS/insecure, timeout) from the usual `OTEL_*` environment variables, so it's wired up exactly like any other OpenTelemetry service.

  What gets traced:
  - A **root span per flow execution**, started at the single choke-point every request passes through (`FlowHandler.HandleRequest`) — so it works for any source connector (queue message, HTTP body, TCP frame, CDC event), in any environment.
  - **Inbound context propagation:** the flow joins an existing distributed trace when a W3C `traceparent` is present in the source headers — HTTP headers or message headers alike (header lookup is case-insensitive, since AMQP and HTTP carry different casing).
  - **Child spans** around connector writes (`to {}` destinations), tagged with the connector name, operation, and target.
  - **Depth inside the flow:** spans for the transform/steps stage, the `to { transaction {} }` block, and each `each` loop inside it — so a trace shows where a flow's time actually goes (e.g. which `each` loop in a large transaction is slow), not just a flat flow → write.
  - **Trace ↔ log correlation:** logs emitted with a context during a traced flow automatically carry `trace_id` / `span_id`, so you can jump from a log line to its trace (and back) in Grafana/Loki. No-op when there's no active span.
  - **Outbound context propagation** on HTTP client calls and on **RabbitMQ / Kafka** publishes (the `traceparent` is written into the AMQP headers / Kafka record headers), so the downstream service or consumer continues the same trace. Redis Pub/Sub and MQTT v3 have no message-header mechanism, so trace context cannot be carried across those hops — they are intentionally left un-propagated rather than mangling the payload.

  Spans carry `service.name` / `service.version` from the service config, plus `mycel.flow`, `mycel.source`, `mycel.connector`, and operation attributes; errored flows/writes are marked on the span. This is separate from the existing development/debug tracer (verbose flow logging + the Studio debugger), which is unchanged — the two can run at the same time. Prometheus `/metrics` is unaffected. (OTel metrics/logs export are planned follow-ups.)

  ```bash
  OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 mycel start
  ```

## [2.9.1] - 2026-06-12

### Fixed

- **RabbitMQ consumers became non-consuming zombies after an idle/network disconnect.** A consumer connector that lost its connection (CloudAMQP idle-disconnect, network blip — surfacing as `Exception (501) ... read tcp ...: i/o timeout`) never resumed consuming: the broker showed `consumers: 0`, messages piled up, and only a full process restart restored draining. The cause was in `Start()`: the consumer path returned early (`return c.startConsumer(...)`) **before** the line that launches the connection-monitor goroutine, so a consumer never watched its own close-notification channel. The reconnect logic that re-issues `basic.consume` (in `handleReconnect`) was therefore dead code for consumers — nothing ever invoked it. Publishers were unaffected because their path reaches the monitor; in a service running both (consumer + publisher against the same broker), only the publisher reconnected after a drop, which is exactly how the bug surfaced. The monitor goroutine now starts for **every** MQ connector before the consumer branch, so consumers reconnect and re-subscribe on their own after a drop. Heartbeats were already configured (10s default) and were in fact what detected the dead connection; the gap was purely the missing re-subscribe. Covered by a new integration test that drives a real consumer through a TCP proxy, drops the connection mid-flight, and asserts a message published after the drop is still consumed.

- **CDC (PostgreSQL logical replication) stopped streaming permanently after a connection drop.** The same class of bug as the RabbitMQ consumer above, found while auditing the other long-running sources: when the replication connection dropped (network blip, DB failover, idle timeout), the driver's stream loop returned an error and the listener goroutine exited without ever being restarted — the connector silently stopped emitting change events until a process restart. `pglogrepl`/`pgconn` are low-level and do not reconnect on their own. The listener now runs under a supervisor that restarts it with capped exponential backoff (1s→30s, reset after a healthy run) until the connector is shut down. PostgreSQL persists the replication slot and its confirmed-flush LSN, so a reconnect resumes from where the stream left off — events buffered in the slot's WAL during the gap are delivered, none are missed. The replication connection is now also closed when the stream loop exits, so reconnects don't leak a Postgres backend per drop. Redis Pub/Sub, Kafka, and MQTT consumers were audited and are unaffected — their client libraries reconnect and re-subscribe on their own.

## [2.9.0] - 2026-06-04

### Added

- **Incoming payload logging via `MYCEL_PAYLOAD_SHOW`.** Set `MYCEL_PAYLOAD_SHOW=true` (together with `MYCEL_LOG_LEVEL=debug`) to log the raw payload entering every flow, regardless of source connector — a queue message, an HTTP body, a TCP frame, etc. Previously the only way to see an incoming payload was `--verbose-flow`, which traces every pipeline stage and is disabled outside development mode; this logs *just* the incoming payload at the single choke-point all requests pass through (`FlowHandler.HandleRequest`), so it works for every connector and in any environment, including production. The payload is logged raw, on entry — before sanitization and validation — so the line appears even for requests that later fail validation. Off by default because payloads may carry PII or secrets.

  ```bash
  MYCEL_LOG_LEVEL=debug MYCEL_PAYLOAD_SHOW=true mycel start
  # DBG incoming payload flow=create_user source=api payload={"name":"Ada","email":"..."}
  ```

  `MYCEL_PAYLOAD_SIZE` caps the logged size (default `4k`; accepts a plain byte count or a `k`/`m` suffix, e.g. `512`, `4k`, `1m`), with truncated payloads marked `…(truncated, N bytes total)`. Both `MYCEL_PAYLOAD_SHOW=true` and debug level are required; setting `MYCEL_PAYLOAD_SHOW=true` without debug logs a one-time startup warning. The `Enabled()` guard skips JSON marshalling unless debug logging is active, so there is no hot-path cost when it isn't.

## [2.8.1] - 2026-06-03

### Fixed

- **Flow metrics were recorded but never exposed at `/metrics`.** A running service recorded flow executions (`mycel_flow_executions_total`, `mycel_flow_duration_seconds`, `mycel_flow_errors_total`) but `/metrics` exposed none of them — only the Go/process collectors plus `mycel_goroutines`, `mycel_uptime_seconds`, and `mycel_service_info` showed up. The cause was `metrics.Default()`: it lazily created a fallback registry behind a `sync.Once`, and the first recorder call fired that `Once` **after** `SetDefault` had already installed the registry the admin/REST endpoint serves — clobbering it with a throwaway `"unknown"` registry that nothing exposes. Every flow/connector recorder then wrote into the orphaned registry. The metrics that *did* appear were written directly on the served registry, which is why they were unaffected. `Default()` no longer self-initializes over an assigned registry (mutex + nil-check instead of `sync.Once`), so `SetDefault` sticks and recorders write into the registry that is actually served. Message throughput is now derivable, e.g. `rate(mycel_flow_executions_total{flow="..."}[1m])`.

## [2.8.0] - 2026-06-02

### Fixed

- **Goroutine leak in redis-backed `coordinate`.** A flow with a redis `coordinate` block leaked ~2-3 goroutines and a pooled Redis connection on **every message processed** — goroutines (and RSS) climbed linearly under load toward an eventual OOM, flat only when idle. `Manager.GetCoordinator` returned a fresh coordinator on every call, and `ExecuteWithCoordinate` calls it once per message (even when the preflight skips the wait); each coordinator `PSubscribe`s and spawns a listener goroutine that was never closed. The coordinator is now cached and shared (it is a long-lived Pub/Sub hub by design), with an idempotent `stop()` that releases the subscription on shutdown. Lock heartbeats and the other redis primitives were not affected.

### Added

- **Optional `pprof` profiling on the admin server.** Set `MYCEL_PPROF` to a truthy value to mount the Go `net/http/pprof` endpoints under `/debug/pprof/` on the admin server (`:9090`). Off by default (pprof exposes runtime internals and the profile/trace endpoints are CPU-heavy), but safe to enable in any environment — including production — because the admin port is internal (reach it with `kubectl port-forward`). For diagnosing goroutine leaks, heap growth, and CPU hotspots on a live process.

  ```bash
  MYCEL_PPROF=true mycel start
  # then, after port-forwarding the admin port:
  go tool pprof http://localhost:9090/debug/pprof/goroutine
  ```

## [2.7.0] - 2026-06-01

### Added

- **External HTTP identity providers.** The `provider` block under `auth` is now wired end-to-end: it validates an incoming credential (an API key or opaque bearer token) against an external HTTP endpoint at request time, instead of against local JWTs or a static list. Local JWT validation runs first; providers are tried only when the credential isn't a valid JWT, in declaration order, and the first whose `success` expression is truthy wins.

  ```hcl
  auth {
    provider "api_keys" {
      type     = "http"
      validate = env("KEYS_VALIDATE_URL")        # URL; {token} is templated in
      request  = { Authorization = "Bearer {token}" }
      response {
        success = "status == 200 && body.active == true"  # CEL over status + body
        user_id = "body.user_id"
        email   = "body.email"
        roles   = "body.roles"                            # list<string> or CSV
      }
    }
  }
  ```

  The full response `body` is exposed to flows as `auth.claims.*`, and `user_id` as `auth.user_id`. A provider timeout or transport error is treated as a rejection (not a `5xx` from your service). CEL expressions are compiled at startup, so an unsupported `type`, a missing `validate`/`success`, or an invalid expression fails fast. Response caching and `sync_to` are not implemented yet (`sync_to` logs a warning when set). See the [`dynamic-api-key` example](examples/dynamic-api-key) and the [auth guide](docs/guides/auth.md#external-identity-providers).

### Fixed

- **MFA could never be turned on via config.** `parseAuthMFABlock` left `enabled` out of its schema, so `mfa { enabled = true }` was a parse error; and no preset set the top-level `MFAConfig.Enabled`, so the `NewManager` gate was effectively always false and the MFA service never initialized. The `mfa` block now parses `enabled` (and defaults it to `true` when the block is present; explicit `enabled = false` still disables it), and the strict/standard/relaxed presets enable MFA.

## [2.6.0] - 2026-05-29

### Added

- **Reusable inline blocks.** Every inline block that used to be copy-pasted flow after flow can now be declared **once** at the top level with a name and referenced from any number of flows via `use = "<kind>.<name>"`, with optional attribute-level overrides. This extends the mechanism `transform` and `cache` already had to ten more kinds:

  | Category | Kinds |
  |---|---|
  | Flat | `dedupe`, `retry`, `lock`, `semaphore`, `sequence_guard` |
  | Sub-block | `coordinate`, `transaction`, `error_handling` |
  | Mapping | `accept`, `response` |

  ```hcl
  dedupe "standard" {
    cache = "fingerprints"
    key   = "'item:' + input.id"
    ttl   = "30d"
    fingerprint { id = "output.id"  price = "output.price" }
  }

  flow "ingest_products" {
    # ...
    dedupe { use = "dedupe.standard" }
  }

  flow "ingest_orders" {
    # ...
    dedupe {
      use = "dedupe.standard"
      key = "'order:' + input.id"   # override just the key
      ttl = "7d"                    # cache + fingerprint inherited
    }
  }
  ```

  Merge rules: scalars override when set; map fields (dedupe `fingerprint`, `response` mappings) merge key by key; sub-blocks (lock `storage`, error_handling `retry`, …) are replaced wholesale. A named `error_handling` may itself reference a named `retry` (resolved outer-first). References are validated at config load — a `use` pointing at a non-existent name fails `mycel validate` with the list of available names, never at runtime.

  `error_response`, `on_timeout`, and `on_error` are intentionally **not** independently nameable: they live inside `error_handling`, which holds a single one of each, so reusing the whole named `error_handling` already covers them.

  Strictly **additive and backward-compatible**: every existing inline block (written without `use`) behaves exactly as before. Names live in a per-kind namespace. See [`examples/reusable-blocks/`](examples/reusable-blocks/) and [`docs/core-concepts/reusable-blocks.md`](docs/core-concepts/reusable-blocks.md).

### Internal

- Reusable-block plumbing is table-driven: a single `reusableKinds` registry in `internal/parser/reusable.go` drives the top-level parse dispatch, name-uniqueness validation, and reference resolution, so each kind contributes only its parse/merge functions. References are folded into self-contained blocks by a new `ResolveReferences` pass at parse time; the runtime is unchanged.

## [2.5.0] - 2026-05-28

### Changed

- **Slack connector batches messages by default to stay under Slack's per-channel rate limit.** Slack's `chat.postMessage` and Incoming Webhooks are limited to roughly 1 msg/sec per channel; above that Slack does not return an error — it silently hides messages with a "high volume of activity" banner. To fix that for everyone on upgrade, the Slack connector now **coalesces high-rate writes into a single summary message per window**, enabled by default.

  Defaults (no config needed):
  - `window = "3s"` — first message in a bucket arms a 3-second tumbling-window timer.
  - `max_size = 50` — flush early if the bucket reaches this many messages.
  - `group_by = "channel"` — one bucket per Slack channel (matching the per-channel rate limit).
  - Built-in summary: `📨 *N events:*` + a bullet line per message.

  **Single-message bypass:** when a window contains only one message it is sent **as-is** (no bullet wrapper) — low-rate notifications look identical to v2.4.0 with up to `window` seconds of added latency.

  **Tuning:**

  ```hcl
  connector "slack" {
    type        = "slack"
    webhook_url = env("SLACK_WEBHOOK_URL")

    batch {
      window   = "5s"
      max_size = 100
      group_by = "channel"           # or "global"
      summary  = <<-CEL
        "🔔 " + string(count) + " alerts in " + window + ":\n" +
        messages.map(m, "• " + m.text).join("\n")
      CEL
    }
  }
  ```

  CEL scope inside `summary`: `messages` (list of `{text, channel, username}`), `count` (int), `channel` (string), `window` (string). Default is the built-in bullet list when `summary` is omitted.

  **Opting out** restores the pre-v2.5.0 immediate-send behavior:

  ```hcl
  connector "slack" {
    type        = "slack"
    webhook_url = env("SLACK_WEBHOOK_URL")
    batch { enabled = false }
  }
  ```

  **Safety:**
  - Messages with `blocks`, `attachments`, or a `thread_ts` bypass batching automatically — collapsing them would lose structure or land the summary in the wrong thread.
  - `Close()` drains every pending bucket before exiting, so graceful shutdown loses nothing. A hard crash drops the in-memory buffer; for must-not-lose audit-style notifications, opt out.
  - A rendered summary above ~38 KB is truncated with a marker rather than silently dropped.

  **Behavior change to call out for upgraders:** existing flows that send many Slack notifications per second now coalesce them into one summary per 3-second window. This is a strict improvement when you were already hitting Slack's banner; for code that relied on every message arriving individually within 3s, set `batch { enabled = false }`.

### Added

- **Slack connector handles HTTP 429 from Slack with `Retry-After` backoff.** When `chat.postMessage` or the webhook responds with `429 Too Many Requests`, the connector now parses `Retry-After` (seconds or HTTP-date) and retries once after that delay. The wait is clamped to a 30-second cap so a malicious or buggy server cannot stall the connector indefinitely, and is interrupted by `context.Context` cancellation. This complements the new batching: batching keeps you under Slack's soft "high volume" suppression, and the 429 handler covers the rare burst that still trips the hard rate limit.

## [2.4.0] - 2026-05-26

### Added

- **Transactional, iterative, multi-statement write primitive: `to { transaction { } }`.** A flow destination can now run an ordered list of SQL statements inside a single pinned database connection wrapped in one `BEGIN`/`COMMIT` — all-or-nothing. This makes a complex aggregate write (clean previous rows, insert a parent and capture its autoincrement id, insert N children that reference it, route attributes by type) expressible declaratively, which the single-statement `to` write could not do.

  ```hcl
  to {
    connector = "db"            # must be a database connector

    transaction {
      exec {
        query  = "DELETE FROM child WHERE owner_id = :owner"
        params = { owner = "output.owner_id" }
        when   = "output.owner_id > 0"      # optional CEL gate; false = skip
      }

      exec {
        query   = "INSERT INTO parent (owner_id, name) VALUES (:owner, :name)"
        params  = { owner = "output.owner_id", name = "output.name" }
        capture = "parent_id"               # INSERT → captured.parent_id = last insert id
      }

      each "child" in "output.children" {   # iterate a list from the payload
        exec {
          query   = "INSERT INTO child (parent_id, label, position) VALUES (:pid, :label, :pos)"
          params  = {
            pid   = "captured.parent_id"     # value captured above
            label = "child.label"            # current element
            pos   = "child_index"            # 0-based each index
          }
          capture = "child_id"
        }

        each "store" in "child.stores" {     # each is nestable
          exec {
            query  = "INSERT INTO child_value (child_id, store_id, val) VALUES (:cid, :sid, :v)"
            params = { cid = "captured.child_id", sid = "store.id", v = "store.value" }
          }
        }
      }

      exec {
        query   = "SELECT option_id FROM lookup WHERE code = :c LIMIT 1"
        params  = { c = "output.code" }
        capture = "option_id"               # SELECT → first column of first row (null if 0 rows)
      }
    }
  }
  ```

  **Semantics:**
  - **Pinned connection + transaction.** All statements run on one connection inside one transaction, so `LAST_INSERT_ID()`/`last_insert_rowid()` and captured `SELECT`s are coherent across statements. Commit on success; rollback on any error (a failed statement, an unresolved `when`/param CEL, or a panic), then the error propagates to the flow's `error_handling` (retry/ack/requeue/etc.) exactly like a failed classic write.
  - **`exec`** runs one statement with `:named` params resolved from CEL. `capture` stores the last insert id for `INSERT/UPDATE/DELETE`, or the first column of the first row for `SELECT` (`null` when no rows).
  - **`each "<var>" in "<listExpr>"`** evaluates a CEL list and runs its body per element, binding the element to `<var>` and its 0-based index to `<var>_index`. A non-list or empty result runs nothing. Nestable.
  - **CEL scope:** `input`, `output` (transform result), `step`, `captured` (values captured so far), plus active `each` bindings.
  - **Driver-agnostic:** the runtime depends only on a `connector.TxRunner` interface; MySQL/MariaDB and SQLite implement it today, and the interface leaves room for Postgres `RETURNING`.
  - **Wrapped by the standard envelope:** dedupe, `after`/`on_error` aspects, and `error_handling` (retry, `on_timeout`/`on_error` dispositions) wrap the transaction just like any other `to` write.

  **Validation (`mycel validate`):** rejects `transaction` combined with `query`/`target`/`operation`/`envelope` in the same `to`; requires the `to` connector to be of type `database`; requires each `exec` to have a non-empty `query`; requires `each` to be written as `each "<var>" in "<listExpr>"`.

  **Backward compatible:** purely additive. Flows without a `transaction` block are unchanged.

## [2.3.0] - 2026-05-23

### Changed

- **Dedupe now commits the fingerprint when a failed write's disposition is `ack`.** The biphasic dedupe primitive persists the fingerprint in Phase B not only after a successful write, but also when the write failed and the flow's `error_handling` will ack it (e.g. `on_timeout { action = "ack" }`). Rationale: `ack` means "treat this as processed/terminal", so the duplicate the upstream redelivers must be filtered — not reprocessed into a concurrent second operation on the backend.

  This closes the gap left by v2.2.0's `on_timeout { action = "ack" }`: previously a timed-out (but still-processing) backend POST was acked, but because the write "failed" the fingerprint was not stored, so the redelivered duplicate slipped past Phase A and was reprocessed concurrently. Now the fingerprint is committed on the ack path.

  **Backward compatible:** a failed write with no class handler — or with `requeue`/`reject` — still skips Phase B exactly as before, so the message is reprocessed cleanly on redelivery. Only the `ack` disposition commits.

## [2.2.0] - 2026-05-23

### Added

- **Per-class error dispositions: `on_timeout` and `on_error` blocks inside `error_handling`.** Beyond the existing `retry {}`, a flow can now declare what broker disposition to apply per **class** of failure:

  ```hcl
  error_handling {
    retry { attempts = 3, delay = "2s", max_delay = "30s", backoff = "exponential" }
    on_timeout { action = "ack" }      # timeout → drop, no retry, no requeue
    on_error   { action = "requeue" }  # other transient errors → requeue
  }
  ```

  `action` is one of `ack` (acknowledge/drop), `retry` (use the `retry {}` budget), `requeue` (nack with requeue), or `reject` (nack without requeue → DLQ). `on_timeout` matches timeout / `context.DeadlineExceeded` failures; `on_error` matches transient, non-timeout, non-permanent failures. Permanent errors (HTTP 4xx) are never routed here and keep their ack-and-drop behavior.

  The motivating case: a consumer `POST`ing to a backend with a timeout. When the backend takes longer than the timeout, the HTTP client aborts but the backend keeps processing — so retrying fires a concurrent duplicate request. `on_timeout { action = "ack" }` drops the timed-out (idempotent) message instead of replaying it. See `examples/timeout-handling`.

  **Backward compatible:** flows without `on_timeout` / `on_error` behave exactly as before (a timeout stays transient → retry budget → requeue). Implemented via a `DispositionError` that MQ consumers (RabbitMQ, Kafka) honor explicitly, falling back to the existing permanent-vs-transient inference when no disposition is set.

## [2.1.1] - 2026-05-22

### Fixed

- **Connector attribute parser out of sync with schemas.** The connector parser validated attributes against a hardcoded allow-list that was missing attributes already defined in several connector schemas, so valid configs failed with `Unsupported argument`. Added the missing attributes for CDC (`slot_name`, `publication`), WebSocket (`ping_interval`, `pong_timeout`), SSE (`heartbeat_interval`, `origins`), Elasticsearch (`nodes`, `index`), and OAuth (`client_secret`, `redirect_uri`, `scopes`, `issuer_url`, `auth_url`, `token_url`, `userinfo_url`).
- **Event-driven sources wrote to databases as `SELECT` instead of `INSERT`.** A flow sourced from WebSocket, SSE, or TCP that wrote to a database kept the default `GET` method and ran a read query, silently discarding the inbound payload. WebSocket/SSE/TCP are now treated as event-driven sources, so such flows correctly write. Verified end-to-end (WebSocket chat message persisted; REST→WebSocket broadcast and REST→SSE push delivered to clients).
- **Example configurations.** Fixed 14 example projects that no longer validated against the current parser: outdated database connection attributes (`source`/`dsn` → `database`), inline type constraints that the type parser does not accept, dotted `output.` transform keys (now flat keys), the `workflow {}` block (only `storage` is supported), `lock`/`semaphore` inline storage (now a `storage {}` block), and an invalid `to { response }` (now a `response {}` block). All example configs now validate.

### Documentation

- Added the [Resilience & Failure Recovery guide](docs/guides/resilience.md): availability vs. durability, broker redelivery, synchronous vs. event-driven ingestion, and idempotency.

## [2.1.0] - 2026-05-20

### ⚠️ BREAKING — `dedupe {}` block replaced

The existing key-based dedupe primitive is replaced by a content-based, biphasic dedupe. The old block had a correctness bug (the dedup marker was stored *before* the inner exec ran, so a failed `to` followed by a broker redelivery would see the marker and silently drop the message, losing real work) and a conceptual overlap with what operators actually wanted (drop messages whose persisted projection is identical to the last one persisted, not just "have I seen this key before").

**Migration:** replace `storage` with `cache`, restrict `on_duplicate` to `ack`/`reject`/`requeue`, and declare an explicit `fingerprint {}` projection:

```hcl
# v2.0.0 and earlier
dedupe {
  storage      = "redis_cache"
  key          = "input.message_id"
  ttl          = "1h"
  on_duplicate = "skip"
}

# v2.1.0+
dedupe {
  cache        = "redis_cache"
  key          = "input.message_id"
  ttl          = "1h"
  on_duplicate = "ack"
  fingerprint {
    id = "input.message_id"
  }
}
```

The two in-tree examples in `examples/steps/flows.mycel` were migrated. The `fingerprint {}` block is required and must declare at least one entry — silent defaults would risk dropping real changes when the author forgets to enumerate a persisted field.

### Added

- **Content-based biphasic dedupe primitive** that drops no-op messages **before** they reach a slow downstream. Phase A (before `to`) computes a canonical fingerprint over the named projection, GETs the previously stored fingerprint for the key, and compares byte-for-byte; on match the message is dropped according to `on_duplicate` (`ack`/`reject`/`requeue`) without invoking `to`. Phase B (after `to` ONLY on success) stores the new fingerprint, so a failed-then-retried message will not self-discard. Both phases run under an in-process lock keyed on `dedupe.key` (via `SyncManager.ExecuteWithLock` with memory backend), so two workers cannot both pass Phase A with identical fingerprints and double-call the downstream. Cross-process serialization remains the caller's responsibility via an outer `lock {}` block.

- **Canonical fingerprint encoder** (`internal/runtime/dedupe_fingerprint.go`): map keys sorted alphabetically; array elements sorted by their own encoded bytes (treated as order-insensitive sets); each value type-tagged and length-prefixed so the string `"a,b"` cannot collide with the array `["a","b"]`; whole-number floats normalize to ints so `float64(5.0)` and `int64(5)` round-trip identically (CEL evaluates numeric literals to float64). Full bytes are stored — zero risk of hash collision; SHA-256 hashing is left as a future opt-in.

- **`flow.ParseDuration`** (`internal/flow/duration.go`) — supports day (`"30d"` → 720h) and week (`"2w"` → 336h) suffixes on top of the stdlib units. Malformed inputs error explicitly instead of silently falling back to zero (the symptom that contradicted the documented "30-day baseline" for dedupe TTLs and would have caused Redis to grow unbounded). The HCL parser validates `dedupe.ttl` at parse time so misconfigured TTLs fail the deploy rather than degrading at runtime.

### Tests

- 11 fingerprint encoder unit tests (deterministic output, key/array order independence, type-tag collision prevention, number normalization, length-prefix boundary cases, real Mercury-shape projection).
- 8 runtime integration tests covering same-message-twice-is-dropped, different-content-both-pass, write-failure-skips-commit (so retries actually re-attempt), 20-goroutine race on the same fingerprint (only one downstream call), per-key parallelism (workers with distinct keys do not serialize on each other), zero-overhead when unconfigured, `on_duplicate` policy round-trip.
- 4 parser tests including a rejected-`30days` regression guard and a canonical-`30d`-accepted lock.
- Functional end-to-end via `tests/integration/run.sh::test-dedupe.sh`: 5 POSTs through `REST → dedupe → mock-server`, the mock receives exactly 3 (POSTs 1, 3, 4 — POSTs 2 and 5 deduped). 143/0 in the full suite.

### Documentation

- `examples/dedupe/` — runnable example (RabbitMQ → dedupe → Magento HTTP, memory cache for local, redis for prod). Includes a prominent caveat about array order-insensitivity in `fingerprint {}` and how to reshape order-sensitive arrays in `transform` before dedupe sees them.

## [2.0.0] - 2026-05-18

### ⚠️ BREAKING CHANGE
- **RabbitMQ connector no longer auto-creates queues or exchanges that do not exist on the broker.** Previously, when a `queue {}` block (or `consumer.queue` shorthand) referenced a queue that did not exist, Mycel silently issued `QueueDeclare` to create it. Same for `exchange {}`. That behaviour made typos invisible — a misspelled `queue = "magento.system.itesm.in.q"` would create a brand new empty queue and the consumer would happily sit on it forever while the real queue's messages went to whoever owned the correct name. It also meant Mycel imposed its idea of topology onto shared brokers where ops or another service genuinely owns the lifecycle.

  From v2.0.0 the default is **fail-fast**: when `QueueDeclarePassive` / `ExchangeDeclarePassive` returns `NotFound`, Mycel returns a clear actionable error at startup instead of declaring. To opt back to the previous behaviour, add `create_if_missing = true` to the relevant block.

  This is the conservative completion of the passive-first refactor shipped in v1.21.4 (which already preserved existing topologies when queues did exist). v1.21.4 closed the "shared queue with different args" failure mode; v2.0.0 closes the "queue does not exist" silent-creation footgun.

  **Migration:**
  - **Production with externally-managed queues (Terraform, rabbitmqctl, etc.):** no action — this is the new safe default. Queues already exist; passive declare succeeds.
  - **Dev/local/demo environments where Mycel should create the queue:** add `create_if_missing = true`:
    ```hcl
    consumer {
      queue             = "test-queue"
      create_if_missing = true
    }
    # or in the full block form:
    queue {
      name              = "test-queue"
      durable           = true
      create_if_missing = true
    }
    # same flag on exchange{}:
    exchange {
      name              = "events"
      type              = "topic"
      create_if_missing = true
    }
    ```
  - **Error message** when missing: `queue "X" does not exist on broker HOST (vhost "Y"). Declare it externally (Terraform, rabbitmqctl, RabbitMQ Management UI) or, for ephemeral environments, set create_if_missing = true on the consumer or queue block`.

### Added
- `create_if_missing` attribute on the `consumer {}`, `queue {}`, and `exchange {}` blocks of the RabbitMQ connector. Defaults to `false`. On the `consumer {}` block it applies to the shorthand-created queue (so users using `consumer { queue = "x" }` don't have to switch to a full `queue {}` block just to opt in). On `queue {}` and `exchange {}` blocks it applies to that declaration. Wired through `internal/connector/mq/schema.go`, `factory.go`, and `rabbitmq/config.go` (`QueueConfig.CreateIfMissing`, `ExchangeConfig.CreateIfMissing`).
- New helper `Connector.declareExchange` mirrors `declareConsumerQueue`'s passive-first pattern for exchanges, with the same channel-reopen-after-NotFound dance (passive declare on a missing entity closes the channel server-side).

### Changed
- `setupTopology` now routes the exchange declare through `declareExchange` (passive-first) instead of unconditional `ExchangeDeclare`.

### Updated config fixtures
- `tests/integration/config/connectors/mq.mycel`: `create_if_missing = true` added to `test_queue` and `fanout_queue` so the docker-compose integration suite continues to declare them.
- `examples/mq/service.mycel`, `examples/graphql-federation/connectors.mycel`: pedagogical examples now show `create_if_missing = true` with a comment explaining when to use it.

### Tests
- `internal/connector/mq/factory_test.go::TestRabbitMQQueueCreateIfMissing` — table-driven coverage of all four entry points: consumer-shorthand default (false), consumer-shorthand explicit (true), `queue {}` block (true), `exchange {}` block (true). Locks the breaking-change default and the opt-in plumbing.

## [1.22.0] - 2026-05-14

### Added
- **`/metrics` now exposes the standard Go runtime and process collectors.** Mycel builds metrics on a custom `prometheus.Registry` (not the global default), which meant `go_*` and `process_*` series — memory, goroutines, GC pauses, open FDs, CPU seconds — were never registered. For MQ-only services (no REST connector) the endpoint effectively reported nothing but `mycel_uptime_seconds`, `mycel_goroutines`, and `mycel_service_info`, leaving operators unable to monitor runtime health from Prometheus at all. `NewRegistry` now registers `collectors.NewGoCollector()` and `collectors.NewProcessCollector()`.
- **Flow execution metrics are now wired into the runtime.** `mycel_flow_executions_total{flow,status}`, `mycel_flow_duration_seconds{flow}`, and `mycel_flow_errors_total{flow,error_type}` were defined in `internal/metrics` but **never recorded** — no caller invoked `RecordFlowExecution` / `RecordFlowError` anywhere in the codebase, so the series stayed empty even while flows ran and failed. `FlowHandler.HandleRequest` now records execution count, duration, and (on failure) a classified error in its deferred completion block, so the metrics populate for every flow regardless of source connector type. `error_type` is bucketed by `classifyFlowError` into a small bounded set (`timeout`, `canceled`, `validation`, `connection`, `error`) to avoid label cardinality explosion from raw error strings.

### Changed
- **`GOMAXPROCS` is now aligned to the CPU cgroup quota on startup** via `go.uber.org/automaxprocs`. Previously the Go runtime sized its scheduler to the host's core count — on an 8-core node a container limited to `300m` CPU would still spin up 8 Ps, so the kernel throttled the runtime, wasted cycles on context switches, and cut GC assists short mid-cycle. `mycel start` now calls `maxprocs.Set` right after logger init, logging the adjustment through the Mycel logger (`maxprocs: Updating GOMAXPROCS=N: ...`). Discovered while profiling the Mercury consumers in dev: bursty CPU throttling on otherwise near-idle pods.

### Tests
- `internal/metrics/metrics_test.go::TestRegistry_ExposesGoAndProcessCollectors` — scrapes the handler and asserts `go_goroutines`, `go_memstats_alloc_bytes`, `process_resident_memory_bytes`, `process_cpu_seconds_total` are present.
- `internal/runtime/flow_metrics_test.go::TestClassifyFlowError` — table-driven coverage of the error classifier (nil, wrapped `context.DeadlineExceeded`, timeout/validation/connection message matching, generic fallback).

### Notes
- Connector-level operation metrics (`RecordConnectorOperation`) remain defined but unwired — that surface spans 25+ connectors and is deferred to a follow-up.

## [1.21.4] - 2026-05-13

### Fixed
- **RabbitMQ consumer crashed at startup on shared, pre-existing queues when `dlq{}` was enabled**, with `AMQP 406 PRECONDITION_FAILED: inequivalent arg 'x-dead-letter-exchange' for queue 'X': received the value 'Y' of type 'longstr' but current is none`. Cause: `setupTopology` (`internal/connector/mq/rabbitmq/connector.go:476-499`) always issued an **active** `QueueDeclare` and, when `dlq.enabled = true`, appended `x-dead-letter-exchange` (and optionally `x-dead-letter-routing-key`) to the args. AMQP's idempotency rule rejects any redeclare whose args do not match the existing queue's args exactly, so any queue created by another publisher (Burro publishing to `magento.system.items.in.q`, a historical consumer, an ops-managed queue, etc.) blocked Mycel from booting.

  Hit in production by the Mercury consumers: the queue is owned by an upstream service, declared without DLX args, and cannot be deleted or redeclared without a coordinated outage across multiple consumers. The user-visible effect is a hard boot failure (`failed to start servers: failed to start rabbit: failed to setup topology: ...`) — the consumer never reaches its first message.

  The fix decouples Mycel's retry-counting from its DLQ-routing. Retry counting in `handleRetry` (`internal/connector/mq/rabbitmq/consumer.go:333-429`) was always implemented via republish — the consumer increments `x-retry-count` in the message header and re-publishes to the same exchange/routing-key, acking the original. That path does not require any AMQP-level DLX; it works on any queue. Only the **final** disposition at `retries >= max_retries` uses `delivery.Reject(false)`, which routes to DLX *if the queue carries the arg* and otherwise discards. For operators who only need "retry N times then drop" (Mercury's case) the DLX arg is unnecessary.

  `setupTopology` now uses a passive-first strategy: `QueueDeclarePassive` runs before any active declare. If it succeeds, the queue exists and Mycel preserves its topology untouched — no DLX args, no redeclare, no 406. A WARN is logged whenever `dlq.enabled = true` and the queue pre-existed, spelling out that retry counting still works but final rejection will discard rather than route to a DLQ unless a server-side policy is configured. If the passive declare fails with `NotFound` (404, queue doesn't exist), Mycel reopens the channel (AMQP closes it server-side on passive failure) and falls back to the existing active declare with full DLX args — greenfield deployments retain full DLQ-for-inspection behavior. Any non-NotFound passive error is treated as fatal and bubbled up.

  DLX/DLQ infrastructure setup (`setupDLQ`, which declares the DLX exchange + DLQ queue + binding) now runs **only when Mycel actually owns the main queue declaration**. Previously it ran unconditionally before `QueueDeclare`, so a 406 left behind orphan DLX infrastructure that nothing routed to.

### Docs
- `docs/guides/error-handling.md` (RabbitMQ Dead Letter Queue section): clarified that retries are implemented via republish (not RabbitMQ DLX TTL), documented the passive-first behavior on pre-existing queues, and noted the WARN log path.

### Tests
- Existing `internal/connector/mq/rabbitmq/` unit tests pass unchanged. The AMQP wire-level behavior (passive declare on an existing queue, NotFound reopen flow, args-merge on greenfield declare) is exercised by `tests/integration/scripts/test-rabbitmq.sh` under the docker-compose harness; run via `tests/integration/run.sh`.

## [1.21.3] - 2026-05-13

### Fixed
- **`dlq {}` block inside `consumer {}` was rejected by the parser**, even though the connector schema declares it, `docs/guides/error-handling.md` documents it, and the MQ factory reads it. Operators copying the documented snippet hit `connector parse error: consumer block error: block content error: ...: Unexpected "dlq" block; Blocks are not allowed here.` and either fell back to the `retry_count = N` shorthand (no control over `exchange` / `queue` / `routing_key` / `retry_delay`) or shipped without DLQ. Bug present since v1.18.0 (when `pkg/schema` made `dlq` a child block of `consumer`).

  Two layered causes:

  1. `internal/parser/connector.go` routed `consumer {}` through `parseGenericBlock`, which calls `body.JustAttributes()`. That HCL method rejects every nested block by design — so the schema said "dlq is a child block", the factory expected `consumer["dlq"]` as a map, but the parser refused to even produce that map.

  2. The obvious workaround — `PartialContent({Blocks: [{Type: "dlq"}]})` + `remain.JustAttributes()` — looks correct but **also fails** on hclsyntax v2.24.0. `JustAttributes()` inspects the body's `Blocks` slice directly and does not consult `hiddenBlocks`, so even after `PartialContent` marks `dlq` as extracted, `remain.JustAttributes()` still sees it in the underlying slice and emits the same diagnostic. This is documented in `hclsyntax/structure.go:248-263` ("we will continue processing anyway, and return the attributes we are able to find") — `JustAttributes` continues, but `HasErrors()` is true, so any caller that checks diags bails out.

  Fix: new `parseConsumerBlock` casts the body to `*hclsyntax.Body` (same pattern already used by `extractDynamicAttrs` in `internal/parser/flow.go:1487`) and iterates `Attributes` and `Blocks` directly, routing the `dlq` child to `parseGenericBlock`. Bypasses both `JustAttributes` paths entirely. The factory layer (`internal/connector/mq/factory.go::buildRabbitMQConfig`, extracted from `createRabbitMQ` to make it testable) already handled the map — it was just never reached.

  Workaround for users stuck on v1.21.2 or earlier without redeploying: `consumer { retry_count = N }` still creates `DLQConfig{Enabled: true, MaxRetries: N}` with default `Exchange` / `Queue` / `RoutingKey` / `RetryHeader = "x-retry-count"`. Anything beyond `MaxRetries` requires this fix.

### Tests
- `internal/parser/parser_test.go::TestParseConnectorRabbitMQConsumerDLQ` — the exact documented snippet (`consumer { ... dlq { enabled, max_retries, retry_delay } }`) now parses and produces `consumer["dlq"]` as `map[string]interface{}` with `enabled=true`, `max_retries=3`, `retry_delay="5s"`. Locks the parser contract.
- `internal/connector/mq/factory_test.go::TestRabbitMQConsumerDLQEndToEnd` — end-to-end: HCL → parser → `buildRabbitMQConfig` → `rabbitmq.Config.Consumer.DLQ`. Asserts the full struct (`Enabled`, `MaxRetries`, `RetryDelay` as `5*time.Second`, `Exchange`, `Queue`, `RoutingKey`, `RetryHeader`). Guards against silent regressions where the parser would accept the block but the factory would drop it on the way to the broker.
- `internal/connector/mq/factory_test.go::TestRabbitMQConsumerRetryCountShorthand` — `retry_count = 7` shorthand still creates `DLQConfig{Enabled: true, MaxRetries: 7}`. Pre-fix workaround stays valid.

### Refactor
- `internal/connector/mq/factory.go`: `createRabbitMQ` split into `createRabbitMQ` (returns `*rabbitmq.Connector`) and `buildRabbitMQConfig` (returns `*rabbitmq.Config`). No semantic change — the only reason to split was to let the factory test inspect the materialized config without an AMQP connection.

## [1.21.2] - 2026-04-30

### Fixed
- **Fan-out filter coordination — one accepter must silence the rejecting siblings**: when several flows shared the same MQ source via fan-out (multiple flows registered on the same routing key) and exactly one passed its filter, the rejecting siblings still fired their `on_reject = "requeue"` policy and `on_drop` aspects. For one Mercury delivery on `all.in.magento.q` with `operation = "update"` and three registered flows (one per operation: create / update / delete), the symptom was:
  - `item_create` filter rejects → `on_reject = "requeue"` → broker requeue
  - `item_update` filter accepts → enters `coordinate.wait` → 30s timeout → ack
  - `item_delete` filter rejects → `on_reject = "requeue"` → broker requeue
  - Net per-delivery: **6 spurious `on_drop` notifications** (2 from each cycle × 3 cycles), **3 retry cycles** of 30s each (because `aggregateFanoutResults` picked the most-aggressive policy across the 3 returns and `requeue > ack`, so the broker saw "requeue" even though the matching flow had ack'd), until the requeue tracker capped it at 3 attempts. Notification spam, log noise, 90 seconds wasted per orphan-shaped delivery.

  The `on_reject = "requeue"` configuration is intentional and correct at the consumer-protocol level — it lets *cross-service* fan-out work, where another container's Mycel (or another consumer entirely) takes the message a flow in this container doesn't own. Changing it to `ack` would break that routing pattern. The bug was specifically about *intra-container* coordination: when a sibling in the same container has the message, the local rejecters should be silent, but the cross-service path must still requeue when ALL local flows reject.

  The fix splits these two cases at the aggregator. `FilteredResultWithPolicy.Reason` already named the gate that produced the drop (`"filter"` / `"accept"` / `"coordinate_timeout"` / `"sequence_older"`); the aggregator now reads it: if exactly one branch's `Reason != "filter"` (it passed its filter and was deflected later), that branch wins outright and the rejecting siblings are silenced — no policy contribution, no on_drop firing. If both passed filter (concurrent post-filter drops) or both rejected at filter (no flow in this container owns it), fall through to most-aggressive policy as before. Cross-service requeue path is preserved.

  To suppress the rejecting siblings' on_drop aspects (which previously fired inline inside each flow's handler before the aggregator could decide), `on_drop` firing is now deferred via a `PendingOnDrop` closure attached to `FilteredResultWithPolicy`. The aggregator nils out the loser's closure; the consumer fires only the winner's via `flow.FireDropAspect`. End result for the Mercury scenario: **1 `on_drop` notification** (from the winning `coordinate_timeout`), **0 retry cycles**, broker acks the message immediately.

### Implementation notes
- `flow.FilteredResultWithPolicy.PendingOnDrop func(context.Context)` — closure attached at the gate, invoked at most once. `flow.FireDropAspect(ctx, result)` is the helper consumers call; nils the slot before firing so a stray double-call is impossible. No-op on nil / non-filtered results.
- `internal/runtime/flow_registry.go`: `dispatchOnDropAndReturn` → `prepareDropResult`. The closure replays the executor pipeline (so `before`/`around` aspects still observe filter drops the same way they did) but defers the actual firing.
- `internal/connector/fanout.go::aggregateFanoutResults`: filter-coordination branch added. `suppressDrop` helper nils losing branches' closures so even if a result reference escapes, nobody fires them by accident.
- `flow.FireDropAspect` wired into every consumer that hands a flow result to broker / response logic: RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, file watcher, WebSocket, REST, GraphQL (3 resolver paths + subscription client), gRPC server (unary + streaming), SOAP server, TCP server (NestJS protocol + standard).

### Tests
- `internal/connector/fanout_test.go` — 5 new tests: `FilterCoordination_AccepterWinsOverRejecter`, `_AccepterFirst` (order-independent), `_ThreeFlowsMercuryScenario` (exact reproduction with 3 chained flows), `_AllRejectFiresOnDropOnce` (cross-service requeue path preserved + single on_drop), `_SuccessSuppressesFilterDrop` (real success also suppresses sibling filter drops).
- `internal/runtime/on_drop_aspect_test.go::TestOnDropAspect_FiresOnFilterReject` updated to call `FireDropAspect` after `HandleRequest`, mirroring the consumer contract.

## [1.21.1] - 2026-04-30

### Fixed
- **HTTP retry sent an empty body on the second attempt** → silent data corruption when destinations had committed side effects before returning 5xx. The connector built a `bytes.NewReader(encoded)` ONCE before the retry loop. The first attempt consumed the reader; subsequent retries handed `doRequest` an exhausted reader, producing a request with no body. The destination saw 500 once (from the real error) and then a 400 "field required" from the empty retry — causing several cascading downstream effects:
  - **Lost error context**: the operator saw the 400 in `flow failed after 1 attempt`, not the actual 5xx that triggered the retry. Diagnosing took 3× as long because the surface error pointed at a body-shape problem that wasn't real.
  - **Permanent failure from transient**: the connector's `isClientError` correctly stops retrying on the 4xx that the empty-body retry produced — but that 4xx was a synthetic artifact of the bug, not a real permanent failure. Real transient 5xx errors that would have recovered with a proper retry instead converted to permanent failures.
  - **Half-baked side effects**: when a destination committed a side effect before returning 500 (e.g. the row was inserted, then a post-save plugin threw), the empty-body retry never overwrote/completed the operation. The system was left with partial state — a row created but not linked to its parent, an order accepted but never invoiced, etc. — and the retry budget was burned without ever reapplying the original payload.

  The fix moves the `bytes.NewReader(encoded)` construction inside the retry loop. The encoded payload is computed once at the top (no double encoding); each attempt gets a fresh reader from the same `[]byte`. Retries now send the exact same bytes as the first attempt.

### Tests
- `internal/connector/http/connector_test.go` adds `TestRetryPreservesBodyAcrossAttempts`: server returns 500 then 200, test asserts the connector sent the same non-empty body on both attempts. Locks the contract.

## [1.21.0] - 2026-04-30

### Fixed
- **Hot reload left flows without handlers on the new connector instances**: v1.20.1's hot reload fix called `Start()` on the new connector after `CloseAll` + `initConnectors` + `registerFlows`, but never called `registerFlowHandlers(name, conn)`. The connector spun up its consumer loop against an empty handler map; every delivery hit `handler_found=false / registered_handlers=0`, the broker saw silent ack-and-drop, and the queue drained without any work being done. Symptom for the operator: `kubectl apply` on the ConfigMap → reload completed cleanly → service stayed "healthy" → messages stopped being processed. Same shape as v1.20.1 (Start was missing) and v1.19.0 (config field dropped at boundary): the visible logs showed success, the runtime wiring was broken one layer down. The fix wires `registerFlowHandlers(name, conn)` immediately before `Start()` in the post-reload loop, mirroring the initial `startServers` path.

### Added
- **`when = "on_drop"` aspect phase**: a new aspect lifecycle hook for messages that were deflected via a documented disposition rather than succeeded or failed — coordinate `on_timeout="ack"`, sequence_guard older-than-stored, filter rejections, accept rejections. The aspect's CEL expressions see a `drop` map with:
  - `drop.reason` — `"filter"` / `"accept"` / `"coordinate_timeout"` / `"sequence_older"`
  - `drop.policy` — `"ack"` / `"reject"` / `"requeue"` (from the gate's configured on-rejection policy)
  - `drop.message_id` — present when the gate captured one (e.g. filter requeue dedup)

  Use case: notify operators on orphaned messages without writing one aspect per gate.

  ```hcl
  aspect "orphan_alert" {
    on   = ["item_update", "style_update"]
    when = "on_drop"
    action {
      connector = "slack"
      transform {
        text = "'_*WARN:*_ ' + _flow + ' dropped (' + drop.reason + ')'"
      }
    }
  }
  ```

  `before` and `around` aspects still run on the dispatch path (so cache hits / rate limits still gate deflected messages); `after` and `on_error` are explicitly skipped — the message wasn't a success or a failure. The `Reason` field on `flow.FilteredResultWithPolicy` is populated at every drop site so future gates can plug in by setting one string.

### Documentation
- The `on_drop` phase joins the `when = "before" / "after" / "around" / "on_error"` validation set in `internal/aspect/types.go`. Validation error messages now list `on_drop` as a valid value.

### Tests
- `internal/runtime/hot_reload_start_test.go` — `TestHotReloadRegistersHandlersBeforeStart` adds a fake `RouteRegistrar` connector and asserts `RegisterRoute` is invoked before `Start` during the post-reload loop.
- `internal/runtime/on_drop_aspect_test.go` — three integration tests:
  - `TestOnDropAspect_FiresOnCoordinateTimeout` — `drop.reason="coordinate_timeout"` / `drop.policy="ack"` resolve correctly.
  - `TestOnDropAspect_FiresOnFilterReject` — filter rejection dispatches `on_drop` with `drop.reason="filter"`.
  - `TestOnDropAspect_DoesNotFireOnSuccess` — regression: `on_drop` only fires on deflection, not on completion.

## [1.20.8] - 2026-04-30

### Fixed
- **`sequence_guard` rejection had no log line** → user reported as "preflight skips the entire flow body". When `coordinate.preflight` returned skip and the flow then hit `sequence_guard` with a current sequence ≤ stored, the guard correctly rejected the message — but emitted **zero logs**. The only visible signal was a suspiciously fast "request" entry (~6ms) followed by no transform / to / aspect activity. Operators reasonably blamed the most recent visible decision (preflight) for stopping the flow, but the actual gate was the sequence_guard one step inside.

  Verified live in the Mercury container: `mycel:seqguard:sku_seq:AI02LTA = 355054`. Any republished message for that SKU with `jobId ≤ 355054` was getting rejected by the guard with no log.

  - `ExecuteWithSequenceGuard` now logs at INFO with `key`, `stored`, `current`, `policy`, `action` for the rejection path; logs `sequence guard passed` (existing key, current > stored) and `sequence guard initialized` (no prior stored value) on the success paths so the disposition is always visible at the default log level.
  - `sequence guard write-back failed` is upgraded from silent `_ = err` to a WARN that includes the key and current value.

### Tests
- `internal/sync/sequence_guard_test.go` adds two tests with a captured log handler:
  - `TestExecuteWithSequenceGuard_LogsRejection` — pre-seed the store with 100, attempt 50, assert the rejection log contains `stored=100 current=50`.
  - `TestExecuteWithSequenceGuard_LogsPass` — first call logs `sequence guard initialized`; second call (higher sequence) logs `sequence guard passed`.

### Note on the user's bug report
The user reported this as "v1.20.7 regression — preflight skips entire flow body". Verified that v1.20.6 and v1.20.7 have identical behavior here: the preflight branch correctly hands control to `fn()`, the inner sequence_guard wrapper rejects the message, and the rejection — until this release — produced no log. The lock heartbeat work in v1.20.7 didn't touch this code path; the apparent regression is the change in stored sequence values (from 0 to a real jobId) once the guard had been live for a while. With these logs, future debugging is one-line.

## [1.20.7] - 2026-04-30

### Fixed
- **Lock TTL expired mid-flow → mutual exclusion silently broken**: `lock { timeout = "30s" }` set a 30-second Redis TTL on the lock key but did nothing while the flow ran. A flow that took longer than 30 seconds (preflight DB query + outbound HTTP + retries — Mercury's typical p99) let the lock auto-expire mid-execution; another worker would then acquire the same key for a different message of the same SKU, both flows processed concurrently, and the second one's `Release` failed with `lock release failed key=... error="lock is not held by this instance"`. Symptom on the user side: duplicate POSTs to the destination, occasionally with corrupted bodies (CEL evaluation context shared across the racing workers), and the silent "lock didn't actually exclude anyone" failure mode the primitive is supposed to prevent.

  - Added `Extend(ctx, key, timeout) (bool, error)` to the `Lock` interface. RedisLock already had this method (Lua script that atomically checks ownership and extends); MemoryLock now implements it for parity.
  - `ExecuteWithLock` now starts a heartbeat goroutine after `Acquire` returns. The heartbeat ticks at `timeout / 3` (clamped to ≥50ms) and calls `Extend`. As long as the goroutine keeps ticking and the lock is still owned, the Redis TTL is reset every interval — the lock effectively stays held for the entire flow duration.
  - `timeout` becomes a **deadman switch** for crashed workers (process dies → no heartbeats → key expires → another worker takes over) rather than a hard cap on flow duration. Recommended values: a few seconds; don't size `timeout` to worst-case flow duration.
  - When `Extend` returns false (caller no longer owns the lock), the heartbeat logs `lock lost during execution — TTL expired or another worker took it` at ERROR with a hint about the timeout setting. The flow continues to completion in that case (we don't have ctx-level abort yet) but the operator is alerted that mutual exclusion was breached.

### Documentation
- `docs/guides/synchronization.md`: `Lock Attributes` table now describes `timeout` as the deadman switch, with a new `Heartbeat (TTL renewal)` subsection documenting the behavior, the log-line on lost ownership, and recommended timeout sizing.

### Tests
- `internal/sync/lock_heartbeat_test.go` — 4 tests:
  - `TestExecuteWithLock_HeartbeatExtendsTTL` — worker A holds for 700ms with 200ms timeout; worker B waits ~600ms (would have acquired at 200ms without heartbeat).
  - `TestExecuteWithLock_HeartbeatExtendCalled` — Extend is called repeatedly while fn() runs and the lock stays held past the original TTL.
  - `TestMemoryLock_ExtendNotHeldReturnsFalse` — non-owner Extend returns false (parity with Redis).
  - `TestMemoryLock_ExtendMissingKeyReturnsFalse` — Extend on missing key returns false, no error.

## [1.20.6] - 2026-04-30

### Fixed
- **`coordinate.preflight` was parsed but never executed**: the parser captured the block, the `flow.PreflightConfig` struct was populated, and `docs/guides/synchronization.md` documented it as the canonical way to gate the wait against a DB existence check. But `sync.FlowCoordinateConfig` had no slot for it and `ExecuteWithCoordinate` had no preflight branch — the data was dropped on the floor at the runtime/sync boundary. Mercury's `style_update` flow used preflight as a fast-path skip for SKUs that already exist in the destination DB; with preflight unwired, every update message blocked on `coordinate.wait` for the configured timeout (5 minutes), with `on_timeout="ack"` eventually dropping the message — the worst of both worlds.
  - Added `FlowPreflightFn` closure to `sync.FlowCoordinateConfig`. The runtime builds the closure with access to the connector registry and the CEL transformer; `ExecuteWithCoordinate` runs it before the wait.
  - Implemented full `if_exists` semantics:
    - `"pass"` (default) — skip the wait when the query returns ≥ 1 row. Resource already exists; no point waiting.
    - `"fail"` — abort the flow with `ErrPreflightCheckFailed` when the query returns ≥ 1 row. Surfaces through the on_error path.
  - Transient errors during preflight (DB blip, CEL eval failure on params) fall through to the wait — best-effort gate so a single bad check doesn't drop the message.

### Added
- **INFO logs at every preflight branch** (no DEBUG required), parallel to the existing wait/signal logs:
  - `coordinate preflight running connector=... if_exists=...` — when the closure is invoked.
  - `coordinate preflight passed connector=... action=skip_wait reason=resource_exists rows=N` — query returned rows, wait will be skipped.
  - `coordinate preflight rejected connector=... action=enter_wait reason=resource_missing rows=0` — query returned no rows, wait will fire.
  - `coordinate preflight passed, skipping wait key=...` / `coordinate preflight rejected, entering wait key=...` — manager-side decision for cross-correlation with the wait key.
  - `coordinate preflight error, falling through to wait error=...` — transient error path.

### Tests
- `internal/runtime/coordinate_preflight_test.go` — 4 integration tests using a stub Reader connector:
  - `TestPreflight_SkipsWaitWhenResourceExists` — Mercury's canonical case: rows=1, if_exists=pass, wait skipped, destination called, sub-millisecond elapsed.
  - `TestPreflight_EntersWaitWhenResourceMissing` — rows=0 → wait fires → on_timeout=ack → FilteredResultWithPolicy.
  - `TestPreflight_IfExistsFailReturnsError` — rows=1 + if_exists=fail surfaces an error; destination not called.
  - `TestPreflight_NotConfiguredFallsThroughToWait` — regression: no preflight block leaves existing wait path unchanged.

## [1.20.5] - 2026-04-30

### Fixed
- **`coordinate.wait.when` was ignored**: the parser captured the attribute, the config carried it through, but no code path ever evaluated it. The wait fired unconditionally, blocking the flow for the configured `coordinate.timeout` (5 minutes in the typical setup) before failing — even when `when = "false"` was hardcoded. Mercury's `style_update` flow used `when = "size(step.check_product) == 0"` and the wait fired anyway. Now `coordinate.wait.when` is evaluated against `input` before the wait fires; false → wait is skipped entirely (fast path).
- **`coordinate.signal.when` was ignored**: same bug, mirrored on the emit side. The condition is now evaluated post-success against `input` and the captured transform output (`output`); false → no signal is emitted. Failed evaluations / non-boolean results log a WARN and fail closed.
- **Aspects fired for filter-rejection results**: filter / accept blocks return `*flow.FilteredResultWithPolicy`; the message was deflected, but `after` aspects fired anyway because the runtime saw `flowErr == nil`. The aspect executor now short-circuits its after/on_error dispatch for filter dispositions via a new `flow.FilteredDropError` sentinel — `after` no longer announces "completed" for messages that were filter-rejected, accept-rejected, or coordinate-acked.

### Added
- **`on_timeout = "ack"` for `coordinate { ... }`**: when a wait times out and there's no point retrying (the awaited signal would never arrive — typical for cross-flow synchronization where the producer flow never received a matching message), `on_timeout = "ack"` acks the broker delivery and drops the message immediately. The flow's `transform` / `to` is skipped, `after` and `on_error` aspects don't fire (it's a documented disposition, not a success or a failure), and the retry budget is **not** consumed. INFO log `coordinate wait timed out, acking delivery key=... timeout=... action=ack`. Joins the existing `fail`/`retry`/`skip`/`pass` set.
- **INFO logs at coordinate decision points** (no DEBUG required):
  - `coordinate wait blocking flow=... key=... timeout=...` when the wait fires.
  - `coordinate wait skipped flow=... reason="when=false"` when the wait is bypassed.
  - `coordinate wait timed out, acking delivery flow=... key=... timeout=... action=ack` for the new ack disposition.
  - `coordinate signal skipped flow=... reason="when=false"` when emit is bypassed.
  - `coordinate signal emitted flow=... key=... ttl=...` (existing) when the emit fires.

### Documented
- `docs/guides/synchronization.md`: bindings reference for `wait.when` (`input` only — step results are not in scope because `wait.when` is evaluated before the flow body runs; for DB-driven pre-flight checks use the `preflight` block). Plus a new `on_timeout` semantics table covering all five values and when each is appropriate.

### Tests
- `internal/runtime/coordinate_when_test.go` — six integration tests covering both the When fixes and the new ack disposition:
  - `TestCoordinateWaitWhenFalse_SkipsWaitFastPath` — flow completes in sub-second, not after the timeout.
  - `TestCoordinateWaitWhenTrue_ActuallyWaits` — regression: `when=true` still blocks for the configured timeout.
  - `TestCoordinateWaitWhenInputDriven` — realistic `input.body.kind == 'create'` style condition skips correctly.
  - `TestCoordinateSignalWhenFalse_SkipsEmit` — `signal.when=false` → no key written to the coordinator.
  - `TestCoordinateSignalWhenTrue_StillEmits` — regression: `signal.when=true` still emits.
  - `TestCoordinateOnTimeoutAck` — orphaned message is acked, `to` is not called, retry budget is not consumed (timeout is 100ms, total elapsed is < 250ms even with `attempts = 3`).

## [1.20.4] - 2026-04-30

### Fixed
- **MQ consumers redelivered permanent failures forever**: when a flow returned a permanent error (HTTP 4xx etc.) and the consumer's DLQ was not configured, `handleRetry` did `Nack(false, true)` — nack-with-requeue — and the broker re-handed the same message to the consumer immediately. With nothing acking it, the same delivery cycled at ~750ms intervals, generating dozens of duplicate POSTs and Slack alerts per published message. The consumer now distinguishes permanent failures (any error implementing the new `connector.PermanentError` interface) from transient ones: permanent → `Ack(false)` with a WARN log naming the delivery tag and reason; transient → existing `handleRetry` path. Same fix applied to the Kafka consumer (commits the offset on permanent failure to skip the message instead of looping).
- **Misleading "after N attempts" failure log**: when a permanent error broke the retry loop early, the runtime still emitted `"flow failed after 3 attempts: ..."` because the budget value was used in the format string. The actual count taken is now reported, plus a `"(permanent failure, retry skipped)"` suffix when the break was due to a permanent error. Reading the logs no longer suggests three POSTs were made when only one was.

### Added
- **`connector.PermanentError` interface**: an exported interface in `internal/connector/` that error types opt into via `IsPermanent() bool`. `*httpconn.HTTPError` and `*gqlconn.HTTPError` now implement it (4xx → permanent, 5xx → transient). `connector.IsPermanent(err)` is the helper used by the runtime retry loop and the MQ consumers, and walks `errors.As` so wrapping via `fmt.Errorf("...%w", ...)` still works through the chain.

### Tests
- 4 new unit tests in `internal/runtime/permanent_ack_test.go`:
  - `TestRetryFailureMessageReportsActualAttempts` — 4xx → "after 1 attempt" + "permanent failure, retry skipped".
  - `TestRetryFailureMessageOn5xxShowsFullBudget` — 5xx → "after 3 attempts" without the permanent suffix.
  - `TestHTTPErrorImplementsPermanent` — full 4xx/5xx matrix exercising the interface.
  - `TestPermanentDetectionUnwraps` — `errors.As` walks the wrapping chain so the runtime's `fmt.Errorf` wrapper doesn't hide the underlying HTTP error.

## [1.20.3] - 2026-04-30

### Fixed
- **Flow-level retry now skips permanent HTTP errors**: `executeWithRetry` retried on every error, including HTTP 4xx responses where the destination has already returned a verdict that the request itself is wrong. A 409 Conflict was burning three retry attempts (and producing three identical Slack alerts, three identical destination POSTs, and 3× the time-to-DLQ) before giving up. New `isPermanentError` helper detects `*httpconn.HTTPError` / `*gqlconn.HTTPError` with status 4xx and breaks out of the retry loop. 5xx still consumes the full retry budget — those can be transient backend hiccups.

- **`after` aspects no longer fire on flow failure**: contradicting `docs/guides/extending.md` ("Run after the flow succeeds"), `executeAfter` ran unconditionally even when `flowErr != nil`. The combined effect with the layering bug below was that a 409 produced both an "INFO: completed" and an "ERROR: failed" Slack message per retry attempt. `executeAfter` is now gated on success.

- **Aspects fire once per delivery, not once per retry attempt**: pre-fix the layering was `retry → aspects → flow`, so `after` / `on_error` ran inside every retry iteration. A flow that hit its 3-attempt budget emitted three `after` notifications + three `on_error` notifications. Refactored to `aspects → retry → flow`: aspects now wrap the whole retry budget and fire exactly once per delivery, with the final outcome.

- **`error` variable in CEL was typed as String, not Map**: `cel.Variable("error", cel.StringType)` (in `internal/transform/cel.go`) caused every reference to `error.message` / `error.code` / `error.type` from an `on_error` aspect's transform to fail at compile time with `type 'string' does not support field selection`. The runtime injected the correct map value, but the static type check rejected field access. Declared as `cel.MapType(cel.StringType, cel.DynType)` (matching `result`), and updated the default activations in `EvaluateExpression` / `EvaluateExpressionWithSteps` / `EvaluateExpressionWithOutput` to inject an empty map (`{message: "", code: 0, type: ""}`) instead of an empty string.

### Documented
- `docs/guides/extending.md` already documented `after` as success-only and `error` as a structured object with `.message`/`.code`/`.type`. The runtime now matches the documentation (it didn't before).

## [1.20.2] - 2026-04-29

### Fixed
- **`coordinate.signal.emit` was stored verbatim in Redis instead of evaluated as CEL**: the runtime resolved the signal key BEFORE running the flow body, with only `input` bound. References to `output.*` failed to resolve and `evaluateSyncKey` silently fell back to the literal source string — the Redis key `mycel:coord:'parent_ready:' + output.sku` was being written instead of `mycel:coord:parent_ready:AI02LT`. Every `coordinate.wait { for = ... }` consumer that should match such a signal missed and timed out.
  - `ExecuteWithCoordinate` now accepts a `SignalKeyBuilder` closure invoked AFTER `fn()` returns. The runtime captures the transform output via a per-execution `OutputSlot` (attached to context in `executeFlowCore`, populated by `applyTransforms`) and binds it to `output` when evaluating the signal expression. Echo flows that have no transform fall back to using the destination response.
  - When CEL evaluation fails or the resolved key is empty, the runtime now logs a `WARN` and skips emitting rather than writing a corrupted key. The previous silent fallback to literal source is gone.
  - New `transform.EvaluateExpressionWithOutput` evaluator is the entry point for post-success expression resolution.

- **Lock not released after flow failure** (defer `Release` ran with a cancelled context): `ExecuteWithLock` / `ExecuteWithSemaphore` / `ExecuteWithCoordinate` (signal emit) used the same parent context for cleanup. When `coordinate.wait` timed out and cancelled the sub-context, every cleanup defer that talked to Redis silently no-op'd. The lock key sat at its TTL, queued workers piled up behind it and timed out one by one with `failed to acquire lock: context canceled`.
  - All cleanup defers now use a fresh `context.Background()` with a 5-second timeout so lock / permit / signal release happens even when the parent context is being torn down.

### Added
- **INFO logs on lock acquire / release**: every lock now emits `lock acquired` and `lock released` (or `lock release failed`) at INFO with the resolved key. Makes pile-ups visible without source modifications.
- **WARN on heuristic CEL fallback**: when a sync key string looks like CEL (contains `+`, `(`, `input.`, `output.`, `step.`) but the evaluator errors out, the runtime now logs a clear warning naming the expression and the error before falling back to the literal — silent corruption is no longer possible.

### Bindings reference
- `coordinate.wait.for` is evaluated up front with `input` bound (unchanged).
- `coordinate.signal.emit` is evaluated post-success with `input` AND `output` bound. `output` is the transform output map (the user's mental model — fields written by the `transform` block), not the destination's raw response. Echo flows without a transform fall back to the destination response.

## [1.20.1] - 2026-04-29

### Fixed
- **Hot reload silently halted MQ consumers**: After a hot reload, `hotReloadSwitch` called `CloseAll()` on the existing connectors (which cancels their consumer goroutines) and `initConnectors()` on the new ones (which calls `Connect()` only). It never called `Start()` on the new event-driven connectors outside of debug mode. The connector reported "connected to RabbitMQ" but no worker was reading from the queue, so message delivery silently halted until the next process restart. Affects RabbitMQ, Kafka, MQTT, CDC, file watchers, WebSocket, SSE — every connector that implements `Starter`. The fix iterates the new registry after init/registerFlows and starts every Starter, deferring only when a debugger is connected but not yet ready (preserving the existing debug-suspend behavior). Also re-applies `HealthRegistrar` / `MetricsRegistrar` / `RateLimitRegistrar` wiring on the new instances so probes and metrics keep working post-reload.

## [1.20.0] - 2026-04-29

### Added
- **`sequence_guard` block — monotonic sequence-number deduplication per resource**: rejects messages whose sequence number is not strictly greater than the last one stored for the same key. Use case: an MQ source that may re-deliver under retry / fan-out / requeue, where an older update must not overwrite a newer one already applied. Different from `dedupe` (boolean "have I seen this key") and `idempotency` (returns cached result for same key) — this is **comparative** dedup, with a stored numeric sequence per key.

  ```hcl
  flow "style_update" {
    from { connector = "rabbit" operation = "all.in.magento.q" }

    lock {
      storage { driver = "redis" url = env("REDIS_URL", "...") }
      key = "'sku:' + input.body.payload.sku"
    }

    sequence_guard {
      storage  { driver = "redis" url = env("REDIS_URL", "...") }
      key      = "'sku:' + input.body.payload.sku"
      sequence = "input.body.payload.jobId"
      on_older = "ack"
      ttl      = "30d"
    }

    transform { /* ... */ }
    to { connector = "magento" target = "/rest/V1/products" operation = "POST" envelope = "productData" }
  }
  ```

  - Storage block accepts both `url` and `host`/`port`/`password`/`db` forms (consistent with `lock`/`coordinate`).
  - Read happens after `accept`, before `transform`. If `current <= stored`, the flow returns the configured `on_older` policy (`ack` / `reject` / `requeue`) and the destination is never touched.
  - Write-back happens only after a successful flow execution. If the destination errors, the stored sequence is **not** bumped, so the next retry can re-process.
  - Atomicity requires an outer `lock` on the same key — wraps run from outer to inner: `lock → coordinate → sequence_guard → transform → to`.

- **Sync primitives compose now**: `executeFlowCore` was refactored from else-if to chained wrappers. A flow can use `lock` + `coordinate` + `sequence_guard` together; the previous code only honored one. Existing single-primitive flows are unaffected.

- **Docs**: `docs/guides/synchronization.md` gains a "Sequence Guard" section covering composition, semantics, and edge cases.

### Backend
- New `internal/sync/sequence_guard.go` (interface), `sequence_guard_memory.go` (in-process backend with TTL reaper), `sequence_guard_redis.go` (Redis GET/SET backend keyed by `mycel:seqguard:`). Manager exposes `GetSequenceGuard` and `ExecuteWithSequenceGuard` paralleling the lock/semaphore/coordinate APIs.

### Tests
- 6 unit tests for the memory backend covering empty reads, write-then-read, TTL expiry, distinct keys, overwrites, and `on_older` parsing.
- 3 parser tests for the HCL block (full attrs, host/port form, invalid `on_older`).
- 6 runtime integration tests against a real `httptest.Server`: first message bumps store, older rejected, equal rejected, newer passes (and bumps), distinct keys are independent, destination failure does **not** bump the store.

## [1.19.7] - 2026-04-29

### Fixed
- **Fan-out: a sibling flow's filter rejection no longer masks another flow's success**: when two or more flows on the same MQ source were registered against the same routing key (e.g. one matches `operation == "create"` and another matches `operation == "update"`, both with `on_reject = "requeue"`), `ChainEventDriven` returned the FIRST handler's result regardless of what it was. If the rejecting flow's `FilteredResultWithPolicy` came first, it shadowed the sibling success — the consumer saw a filter-rejection result and issued a `Nack(requeue=true)` even though the message had already been processed successfully. The broker re-delivered up to the requeue dedup tracker's cap (default 3), producing 3 successful HTTP POSTs to the destination per message before Mycel finally acked.
- The new aggregation rules in `ChainEventDriven`:
  - At least one branch returned a real success → return that success → **delivery is acked**.
  - All branches returned `FilteredResultWithPolicy` → pick the most-aggressive policy (`requeue` > `reject` > `ack`).
  - Any branch returned an error → first error wins, retry/DLQ path takes over (unchanged).

### Added
- **INFO-level ack/nack/requeue logs in MQ consumers**: RabbitMQ and Kafka consumers now log every filter-driven decision at INFO with `routing_key`/`topic`, `delivery_tag`/`partition+offset`, `message_id`, `action` (`nack`/`ack`/`republish_*`), and (for requeues) the `attempt`/`max` counter. Acks on the success path stay at DEBUG to avoid steady-state noise. Lets operators see redelivery loops without changing log level when something is misbehaving.

## [1.19.6] - 2026-04-29

### Added
- **HTTP outbound body observability**: when `MYCEL_LOG_LEVEL=debug`, the HTTP connector logs one line per outbound `POST` / `PUT` / `PATCH` with the connector name, method, path, body size in bytes, and the payload's sorted top-level keys. Values are deliberately not logged (safe to enable when bodies may carry sensitive data). Lets users verify wrap / envelope behavior end-to-end without intercepting traffic. Silent at any level above `debug`.
- **Integration tests for envelope end-to-end**: `internal/runtime/envelope_integration_test.go` drives a full `from MQ → handleCreate → HTTP destination` flow through a real `httptest.Server` and asserts the bytes on the wire. The companion test confirms the absence of the attribute leaves the body flat. Closes the test gap in v1.19.5 where only parser-level capture was verified.

### Documentation
- `docs/connectors/rest.md`: added a "Debugging outbound requests" section describing the new DEBUG log.

### Verified
- `envelope` attribute on `to {}` works end-to-end in production: confirmed against the live `mercury-microservices-mercury-consumer-styles-1` container with real RabbitMQ messages and a transparent HTTP capture proxy. Every outbound `POST` arrived with `{"productData": {...}}` as expected. No code change was required for v1.19.5 envelope behavior — this release adds the test and observability that should have shipped with it.

## [1.19.5] - 2026-04-29

### Added
- **`envelope` attribute on `to` and `step` blocks**: wraps the outgoing payload under a single root key just before it reaches the connector. Required by Magento webapi, Spring `@RequestBody`, and SOAP-derived REST APIs that expect bodies shaped like `{ "<paramName>": { ...body... } }`. The transform block stays clean — one mapping per line — and the wrapping is a one-line opt-in. Connector-agnostic: works for HTTP, MQ, and any other writer that takes a payload map. Schema declared in both `pkg/schema/builtins.go` and the IDE engine so editors recognize the attribute.

```hcl
to {
  connector = "magento"
  target    = "/rest/V1/products"
  operation = "POST"
  envelope  = "productData"   # wraps the transform output as { "productData": {...} }
}
```

## [1.19.4] - 2026-04-29

### Documentation
- **HTTP connector TLS settings**: `tls { ca_cert, client_cert, client_key, insecure_skip_verify }` were already implemented end-to-end (parser → factory → `*tls.Config` → `http.Transport`) but undocumented in `docs/connectors/rest.md`. Added a TLS reference table, mTLS example, and dev-only `insecure_skip_verify` example.

### Added
- **WARN log when `insecure_skip_verify` is enabled**: `Connect()` on the HTTP connector now emits a single `WARN` at startup when TLS verification is disabled, including the connector name and base URL. Loud enough that an accidental production deploy is obvious in the logs. Fires exactly once (at connect time), zero noise on the safe path.

## [1.19.3] - 2026-04-29

### Fixed
- **Nested CEL values reaching JSON encoders**: Transform output flowed through CEL's shallow `result.Value()` conversion, which leaves child elements as `ref.Val`. When a transform attribute resolved to a nested map (e.g. `websites = "input.body.payload.websites"`), `json.Marshal` rejected it with `json: unsupported type: map[ref.Val]ref.Val` and the flow failed at the connector encode step. Added `transform.CELValueToNative` — a recursive walker that unwraps `traits.Mapper` to `map[string]interface{}`, `traits.Lister` to `[]interface{}`, scalars to their Go counterparts, and stringifies non-string map keys (so JSON keys are always strings). Applied to every CEL evaluation site in the transform pipeline (`Transform`, `TransformWithContext`, `Evaluate`, `EvaluateExpressionWithSteps`). Also handles already-unwrapped Go containers carrying ref.Val children, so partial-unwrap states are normalized too.

## [1.19.2] - 2026-04-29

### Added
- **`??` null-coalescing operator now works in CEL expressions**: Documentation has used `??` since v1 (`docs/reference/cel-functions.md:305`, `docs/guides/troubleshooting.md`, several integration patterns), but native CEL has no `??` operator — every transform that used it failed at compile time with `Syntax error: extraneous input '?'`. Mycel now preprocesses CEL expressions before compilation:
  - When the left-hand side is a simple dotted path (e.g. `input.body.payload.jobId`), the rewrite emits `has(input.body) && has(input.body.payload) && has(input.body.payload.jobId) ? coalesce(path, default) : default`. Missing intermediate fields fall back to the default rather than raising "no such key".
  - For other left-hand sides, the rewrite is `coalesce(lhs, rhs)` (existing behavior — catches present-but-null and present-but-empty-string).
  - Chaining is right-associative: `a ?? b ?? c` becomes `a ?? (b ?? c)`.
  - String literals containing `??` are passed through unchanged.
  - `??` inside parens, brackets and braces is processed recursively, so `f(a ?? b)` and sibling args like `concat(a ?? 'x', b ?? 'y')` work correctly.
- The rewrite is applied at both CEL compilation sites (`internal/transform/cel.go` and `internal/validator/types.go`), so transforms, aspects, validators, `when` expressions, `wait.for`, `accept.when`, and any other CEL-typed string benefits.

### Limitation
- When mixing `??` with the ternary `?:` at the same depth, parenthesize the `??` expression: `(a ?? b) ? c : d`. The rewriter does not resolve precedence against `?:` automatically. Same convention as JS/C#.

## [1.19.1] - 2026-04-28

### Fixed
- **Sync storage panic on string `port`/`db`**: `parseSyncStorageBlock` called `cty.Value.AsBigFloat()` directly on `port` and `db`, which panicked with `not a number` when the values came from `env()` (which always returns strings). The parser now accepts either a numeric literal or a numeric string and returns a typed validation error on garbage input. Affects `lock`, `semaphore`, and `coordinate`.
- **HTTP connector retry vocabulary**: `parseRetryBlock` accepted `attempts` + `backoff`, the IDE schema declared a flat `retry_count`, and `docs/connectors/rest.md` documented `retry { count, interval, backoff }` — three divergent vocabularies for the same feature. Aligned everything to `retry { attempts = N }` (matching flow-level retry). The `retry_count` shorthand still works at the connector level. The runtime currently only honors `attempts`; the schema/parser no longer declare unsupported sub-attributes that were silently ignored.
- **Panic-prone `AsBigFloat()` calls**: Added `coerceInt`/`coerceFloat` helpers in `internal/parser/types.go` that accept either numbers or numeric strings. Applied to user-configurable fields across the parser (`max_requeue`, `retry.attempts`, `error_response.status`, `semaphore.max_permits`, `coordinate.max_retries`, `coordinate.max_concurrent_waits`, `batch.chunk_size`, `service.admin_port`, `rate_limit.requests_per_second`, `rate_limit.burst`, aspect priority/thresholds, security `max_*` limits, auth integers). All paths that previously panicked on `env()`-sourced strings now return clear errors.
- **Connector factories silently ignored string ports**: All connector factories had a private `getInt(props, key, default)` helper (or inline `props[key].(int)` assertions) that only handled `int`/`int64`/`float64`. When a numeric value came from `env()` — which always returns a string — the assertion failed silently and the factory fell back to the default. Result: `port = env("DB_PORT", "3306")` looked correct in HCL but the database actually connected to whatever default was hardcoded. Introduced `connector.IntFromProps` / `IntFromPropsStrict` in `internal/connector/coerce.go` and wired it into every factory: `database/{mysql,postgres,mongodb}`, `mq` (RabbitMQ/Kafka/Redis), `cache`, `tcp`, `grpc`, `graphql`, `mqtt`, `ftp`, `file`, `webhook`, `email`, `exec` (SSH port), `http` (`retry_count` + nested `retry.attempts`). Also fixed the runtime's startup banner to use the same coercion (`getRESTPort`, TCP/RabbitMQ details).

### Documentation
- `docs/guides/synchronization.md`: `host`/`port` example updated to use `env()` and clarifies that `port`/`db` accept either numeric literals or numeric strings.
- `docs/connectors/rest.md`: `retry` block reference rewritten with the canonical `attempts` attribute.

## [1.19.0] - 2026-04-14

### BREAKING CHANGES
- **Sync primitives use inline storage config**: `lock`, `semaphore`, and `coordinate` blocks no longer reference a connector via `storage = "connector.redis"`. Instead, they define their own `storage {}` block with `driver`, `url` (or `host`/`port`/`password`/`db`). The sync manager creates its own Redis connection and no longer depends on the connector registry

### Added
- **`SyncStorageConfig`**: New inline storage configuration for sync primitives with `driver` (redis/memory), `url`, `host`, `port`, `password`, `db`
- **Cache connector individual params**: Cache connector now accepts `host`, `port`, `password`, `db` as alternatives to `url`. A connection URL is built automatically from individual params when `url` is not provided

### Fixed
- **`address` attribute in docs**: Replaced all stale `address = env("REDIS_ADDRESS")` references with `url` or `host`/`port` across documentation and examples

## [1.18.10] - 2026-04-14

### Fixed
- **IDE unknown attribute false positives**: `validateBlocks()` now receives the schema registry and uses `connectorTypeAttrsWithRegistry()` to resolve connector-specific attributes. Previously it only used the static fallback, which lacked entries for most connector types (http, file, s3, tcp, exec, soap, etc.), causing valid attributes like `base_url`, `timeout`, `retry_count` to be flagged as unknown
- **Coordinate documentation**: Fixed `synchronization.md`, `configuration.md`, `flows.md`, and `coordinate_example.mycel` to reflect the actual sub-block syntax (`wait {}`, `signal {}`, `preflight {}` with `when`/`for`/`emit` attributes). Documentation previously showed a flat attribute syntax (`signal = "name"`, `key = "..."`) that does not match the parser

## [1.18.9] - 2026-04-14

### Fixed
- **Missing connector types in schema**: Added `http` and `profiled` to `connectorTypes()` in `pkg/schema/builtins.go` — IDE was flagging these valid types as unknown
- **Missing format values in schema**: Added `tsv` to `from` block format values and `csv`/`tsv` to `to` block format values — IDE was rejecting valid format options supported by file/s3/ftp connectors
- **Version string**: Updated hardcoded version in CLI from `1.15.12` to `1.18.9`

## [1.18.0] - 2026-03-23

### BREAKING CHANGES
- **File extension changed from `.hcl` to `.mycel`**: All configuration files must use the `.mycel` extension. The parser, IDE engine, hot reload, and plugin system only scan for `.mycel` files. Existing projects must rename their files (e.g., `config.hcl` → `config.mycel`). HCL syntax is unchanged — only the file extension is different.

### Added
- **SchemaProvider architecture** (`pkg/schema/`): Single source of truth for all HCL block schemas. Core types (`Block`, `Attr`, `SchemaProvider`, `ConnectorSchemaProvider`), `Registry` for connector lookup by type+driver, `Merge` for composing schemas, `ValidateParams` for schema-driven validation with defaults
- **Self-describing connectors**: All 25+ connector types implement `ConnectorSchemaProvider` with their full schema — attributes, child blocks (pool, consumer, queue, tls, etc.), source params, and target params. Each defined in a `schema.go` file within its package
- **Schema Registry wiring**: Runtime creates a fully-populated `SchemaRegistry` with all connector schemas and passes it to the parser. `RegisterBuiltinSchemas` exported for Studio/CLI. `schema.NewRegistryWith(fn)` enables injection without circular imports
- **IDE uses pkg/schema**: `pkg/ide/` types are now aliases for `pkg/schema/` types. `rootSchema()` delegates to `schema.BuiltinRootSchemas()`. Engine accepts `WithRegistry()` for connector-type-aware intelligence
- **Unknown attribute detection**: Strict blocks (accept, validate, dedupe, service) flag unknown attributes as errors. Connector-type-specific attrs merged into the known set. Open blocks (transform, type, from, to, step) skip this check
- **All breakpoints pause BEFORE execution**: Runtime filter, accept, dedupe, and write stages use `RecordStage` to pause before evaluation. Added trace for dedupe stage
- **Breakpoint locations on logic lines**: Accept → `when` line, write → `query`/`target` line, step → `query`/`operation` line, validate → attribute line
- **Accept block**: New `accept` block in flows for business-level message gating with `on_reject` policy

## [1.17.1] - 2026-03-21

### Added
- **CEL completions**: Transform, response, filter, and accept value positions now suggest CEL variables (`input.*`, `step.<name>.*`, `enriched.<name>.*`, `output.*`, `error.*`) and 39 built-in CEL functions (`uuid`, `lower`, `now`, `has`, etc.)
- **Connector-type-aware validation**: Diagnostics warn about missing required attributes per connector type (e.g., `database` requires `driver`, `rest` requires `port`)
- **Connector-type-aware completions**: Inside a connector block, attribute suggestions adapt based on `type` value (e.g., `type = "database"` suggests `driver`, `host`, `database`)
- **Operation string validation**: REST operations like `"GETX /users"` produce warnings for unknown HTTP methods and missing leading `/`
- **Operation completions**: `operation = ""` suggests method+path templates based on connector type (REST: `GET /`, `POST /`; GraphQL: `Query.`, `Mutation.`; gRPC: service/method)
- **Rename support**: `Engine.Rename()` finds definition and all references across the project, returns `RenameEdit` list
- **Code actions**: Quick-fixes for undefined connectors ("Create connector X") and undefined types ("Create type X"), plus missing required attribute insertion
- **Workspace symbols**: `Engine.Symbols()` returns all named entities for Ctrl+P navigation; `Engine.SymbolsForFile()` for document outline
- **Transform rule ordering**: `Engine.TransformRules()` returns ordered rules with index, target, expression, stage, and position — enables per-rule breakpoint placement in Studio
- **Flow stage discovery**: `Engine.FlowStages()` returns the pipeline stages present in a flow in execution order — enables Studio to show valid breakpoint locations

## [1.17.0] - 2026-03-21

### Added
- **IDE intelligence engine** (`pkg/ide/`): Importable Go package for Mycel Studio providing real-time HCL intelligence. Permissive parser (tolerates incomplete files), project-wide index (connectors, flows, types, transforms, aspects), context-aware completions (blocks, attributes, values with project references), 3-layer diagnostics (syntax errors, schema validation, cross-reference checks), go-to-definition, and hover documentation. Thread-safe, no dependency on `internal/`. 14 tests

## [1.16.1] - 2026-03-21

### Added
- **Accept block in error handling guide**: New "Message Rejection" section in `docs/guides/error-handling.md` documenting `filter` and `accept` `on_reject` policies, with examples, comparison table, and requeue loop warning

## [1.16.0] - 2026-03-21

### Added
- **Accept block**: New `accept` block in flows — a business-level gate that runs after `filter` but before `transform`. While `filter` determines if a message belongs to this flow (structural match), `accept` determines if this flow should process it (business logic). Supports `on_reject` policy (`ack`/`reject`/`requeue`), enabling multi-consumer patterns where multiple flows listen to the same queue and each flow can requeue messages that aren't for them. New `StageAccept` trace stage for breakpoints and debugging. Pipeline order: `from → filter → accept → dedupe → validate → transform → to`
- **`accept` in debug protocol**: `inspect.flow` now returns `AcceptInfo` (when expression and on_reject policy). Studio debug protocol recognizes `accept` as a valid breakpoint stage

## [1.15.12] - 2026-03-20

### Fixed
- **Debug reconnection: workers stuck forever after disconnect**: When Studio disconnected, workers blocked on the debug gate and breakpoint pauses stayed stuck permanently, preventing any future debug sessions from working. Two fixes: (1) `Session.ResumeAll()` resumes all paused threads on disconnect so breakpoint-blocked workers can continue, (2) `DebugGate` uses a cancel channel that is closed on disable, unblocking all workers waiting in `Acquire()`

## [1.15.7] - 2026-03-20

### Fixed
- **DebugGate channel replaced while workers blocked**: `SetEnabled(true)` was called twice (once on client connect, once after connector start). The second call created a new channel while consumer workers were already blocked on the first one, causing `Allow()` tokens to go to the wrong channel. `SetEnabled` is now idempotent — calling it when already enabled keeps the existing channel

## [1.15.6] - 2026-03-20

### Fixed
- **Debug mode not applied after connector start**: When using `--debug-suspend`, `SetDebugMode(true)` was called before the connector started (no channel yet), then `Start()` created a fresh channel with the original prefetch/concurrency settings, overriding the debug configuration. Now `SetDebugMode(true)` is re-applied after `Start()` completes, ensuring prefetch=1 and the debug gate are active on the actual consumer channel. Same fix applied to the hot reload path

## [1.15.5] - 2026-03-20

### Changed
- **Studio-controlled debug gate**: Replaced manual consume (`ConsumeOne`/`SetManualConsume`/`setupTopologyForManualConsume`) with a simpler gate-based approach. The `DebugGate` now starts blocked when a debugger connects — the IDE sends `debug.consume` to allow exactly one message through. The connector's normal consumer loop handles processing, eliminating the need for temporary consumers or separate reader-only modes
- **`DebugThrottler` interface expanded**: Now includes `AllowOne()` and `SourceInfo()` methods. All 7 event-driven connectors (RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch, WebSocket) implement the full interface, making all of them controllable from the IDE via `debug.consume`
- **`debug.consume` returns immediately**: No longer blocks waiting for a message. Puts one token in the gate and returns — the message is processed asynchronously by the consumer loop, with events flowing through the event stream

### Removed
- **`DebugConsumer` interface**: Replaced by expanded `DebugThrottler`. The separate `SetManualConsume`, `ConsumeOne`, and `SourceInfo` methods on `DebugConsumer` are no longer needed
- **RabbitMQ `setupTopologyForManualConsume`**: Connector now always starts its normal consumer loop with `startConsumer`
- **Kafka `startReaderOnly`**: Connector now always starts its normal consumer with `startConsumer`
- **Goroutine in `handleConsumeAsync`**: Since `debug.consume` is now synchronous (just puts a token), the async dispatch was removed

## [1.15.4] - 2026-03-19

### Fixed
- **RabbitMQ `ConsumeOne` not receiving messages**: Replaced `Basic.Get` (polling) with `Basic.Consume` (temporary consumer). `Basic.Get` silently failed to return messages on managed RabbitMQ providers (CloudAMQP). Now uses a short-lived consumer tag that receives one message and cancels, which is the standard push-based mechanism and works reliably across all RabbitMQ deployments

## [1.15.3] - 2026-03-19

### Fixed
- **`debug.consume` deadlock**: `handleConsume` blocked the WebSocket read loop while waiting for the message to complete the full pipeline. If the pipeline hit a breakpoint, the IDE could not send `debug.continue` because the read loop was blocked — causing a deadlock. Now `ConsumeOne` runs in a separate goroutine (`handleConsumeAsync`) and the read loop stays free to process commands during message processing

## [1.15.2] - 2026-03-19

### Added
- **Manual consume for event-driven debugging** (`debug.consume`): Queue-based connectors (RabbitMQ, Kafka) now support IDE-controlled message consumption. Instead of auto-consuming after `debug.ready`, the IDE calls `debug.consume` to pull one message at a time, giving full control over when messages enter the pipeline
- **`DebugConsumer` interface** (`internal/connector/connector.go`): New interface for queue-based connectors — `SetManualConsume(enabled bool)`, `ConsumeOne(ctx) error`, `SourceInfo() (type, source)`. Implemented by RabbitMQ (`channel.Get` / Basic.Get) and Kafka (`reader.FetchMessage` + manual `CommitMessages`)
- **`debug.ready` handshake with source capabilities**: `debug.ready` now returns `ReadyResult` with `sources[]` listing all active connectors, their type, source identifier, and `manualConsume` capability flag. The IDE uses this to know which connectors require `debug.consume` calls
- **RabbitMQ manual consume**: Uses AMQP `Basic.Get` (pull-based) with 100ms polling loop instead of `Basic.Consume` (push-based). Topology (exchanges, queues, bindings) is set up without starting the consumer goroutine
- **Kafka manual consume**: Uses `reader.FetchMessage` + explicit `reader.CommitMessages` with `CommitInterval: 0` for manual offset control. Reader is created without starting consume goroutines

### Fixed
- **`ShouldBreak` for non-transform stages**: Previously only checked `HasBreakpoints()` which was true only for rule-level breakpoints. Now correctly checks stage-level breakpoints for all pipeline stages (sanitize, validate, read, write, etc.)
- **Always-inject debug instrumentation**: `BreakpointController` and `TransformHook` are now always attached when a debugger is connected, not gated on `HasBreakpoints()`. Prevents race condition where breakpoints set after request start were missed
- **VerboseFlow ordering**: `VerboseFlow` log collector is now attached before `StudioCollector` in the trace context, ensuring proper event ordering

### Changed
- **Studio Debug Protocol docs** (`docs/STUDIO-DEBUG-PROTOCOL.md`): Complete rewrite of session lifecycle (handshake diagrams), new `debug.ready`/`debug.consume` method docs, new RabbitMQ manual consume example session, updated data types, runtime integration, and test references

## [1.15.1] - 2026-03-19

### Fixed
- **Hot reload + debug suspend**: After hot reload with a debugger already connected, event-driven connectors (RabbitMQ, Kafka, etc.) were never started because `OnClientChange(true)` had already fired before the reload. Now `hotReloadSwitch()` checks `HasClients()` after recreating connectors and immediately starts suspended connectors and re-applies debug throttling
- **All notification connectors now implement `connector.Writer`**: Slack, Discord, Email (SMTP/SendGrid/SES), SMS (Twilio/SNS), Push (FCM/APNs), and Webhook connectors now implement the `connector.Writer` interface, enabling them to be used from aspects (`action { connector.slack = "..." }`)

### Improved
- **Connector connection logs**: Added useful context to connection success logs across 7 connectors — RabbitMQ (queue name), Kafka (topics, consumer group), Elasticsearch (nodes, default index), FTP/SFTP (protocol, host, port, base path), CDC (tables), File watch (patterns)

## [1.15.0] - 2026-03-18

### Changed
- **Connector Owns Config refactor**: Parser no longer hardcodes connector-specific attributes. Each connector now validates its own parameters via `SourceValidator` / `TargetValidator` interfaces. All connector-specific data flows through a `ConnectorParams` map and is accessed via getter methods (`GetOperation`, `GetTarget`, `GetQuery`, etc.)
- **Parser simplification**: Parser only declares flow-level attributes (`connector`, `when`, `parallel`, `filter`, `timeout`, `on_error`, `default`). Connector-specific attributes (`operation`, `target`, `query`, `format`, `filter`, `params`, `body`, `query_filter`, `update`) are captured dynamically into `ConnectorParams` instead of typed struct fields
- **Connector validation interfaces**: 14 connectors implement `SourceValidator` and/or `TargetValidator` interfaces to validate their own parameters at parse time. New or plugin connectors can accept any parameters without parser changes
- **Removed typed fields from config structs**: `Operation`, `Target`, `Query`, `Filter`, `Format`, `Params`, `Body`, `QueryFilter`, `Update` removed from `FromConfig`, `ToConfig`, `StepConfig`, `EnrichConfig`. All access goes through `ConnectorParams` getters

## [1.14.4] - 2026-03-18

### Added
- **Automatic debug throttling**: When a Studio debugger connects, all event-driven connectors automatically switch to single-message processing. RabbitMQ sets AMQP prefetch to 1, Kafka/Redis/MQTT/CDC/File/WebSocket use a shared semaphore gate. Original concurrency is restored when the debugger disconnects. Zero overhead when no debugger is connected
- **`DebugThrottler` interface** (`internal/connector/connector.go`): Optional interface for event-driven connectors — `SetDebugMode(enabled bool)`. Implemented by 7 connectors: RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch, WebSocket
- **`DebugGate`** (`internal/connector/debuggate.go`): Reusable token-based semaphore. `Acquire()` blocks when enabled, passes through when disabled. 4 unit tests
- **Debug server `OnClientChange` callback** (`internal/debug/server.go`): Called when clients go from 0→1 (enable) or 1→0 (disable). Runtime wires this to toggle `SetDebugMode` on all connectors
- **Start Suspended mode** (`--debug-suspend` / `MYCEL_DEBUG_SUSPEND=true`): Event-driven connectors defer `Start()` until a debugger connects via `debug.attach`. Prevents message consumption before breakpoints are set. REST/gRPC/GraphQL/SOAP/TCP/SSE start normally (needed for health checks). Dev-only, automatically disabled outside development mode
- **Source properties reference** (`docs/reference/source-properties.md`): Complete reference of `from` block properties per connector type — operation format, `input.*` variables, and examples for all 13 source connector types

## [1.14.3] - 2026-03-16

### Changed
- **PDF connector: template in config**: Template path moved from flow transform payload to connector configuration (`template` attribute). Connector-level template serves as default; flows can still override via `template` payload field for dynamic template selection. Follows the black-box principle — infrastructure details belong in connector config, not in business flows
- **Email connector: template in config**: Template path moved from flow payload (`template_file`) to connector configuration (`template` attribute). Same resolution: connector config as default, payload override for dynamic cases. Field renamed from `template_file` to `template` for consistency with PDF connector
- **Consistent template naming**: Both PDF and email connectors now use `template` as the field name (email previously used `template_file`)

## [1.14.2] - 2026-03-16

### Fixed
- **Aspect metadata pollution**: `enrichInput()` injected `_flow`, `_operation`, `_target`, `_timestamp` into input maps which reached DB connectors as column names, causing SQL errors on all flows when aspects were loaded. Metadata is now stripped before passing to the flow core
- **Cache hit type mismatch**: Cached results (from Redis or memory) deserialized as `[]interface{}` instead of `[]map[string]interface{}`, causing `resultToConnectorResult` to return empty results. Added `[]interface{}` handling with map conversion
- **MongoDB ID preservation**: `resultToConnectorResult` only handled `int64` and `int` for LastID, losing MongoDB's string hex ObjectIDs. Now preserves original ID type
- **Integration test: cache key format**: Cache flow HCL used CEL string literals (`'cached_users'`) for cache keys, but `buildCacheKey` doesn't evaluate CEL — resulting in keys with embedded quotes. Changed to plain strings
- **Integration test: aspects SQLite contention**: Aspects test `POST /aspects/init` failed with `SQLITE_BUSY` when running in parallel with SQLite test. Added retry with backoff
- **Integration test: plugin health flakiness**: Plugin test health check occasionally returned 503 during startup. Added retry loop
- **PDF connector documentation**: Added `docs/connectors/pdf.md` with full reference (configuration, operations, HTML template syntax, complete invoice example)

## [1.14.1] - 2026-03-16

### Added
- **Fan-out from source**: Multiple flows can now share the same `from` connector and operation. When a request or message arrives, all registered flows execute concurrently. For request-response connectors (REST, gRPC, TCP, WebSocket, SOAP, SSE, GraphQL), the first registered flow returns the response while additional flows run as fire-and-forget. For event-driven connectors (RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch), all flows execute in parallel and the message is acknowledged only after all complete. 13 tests covering chain helpers, input isolation, error handling, and nested chaining
- **Fan-out chain helpers** (`internal/connector/fanout.go`): `ChainRequestResponse` and `ChainEventDriven` helper functions that compose multiple handlers into a single handler. `CopyInput` ensures input isolation between concurrent handlers. Used by all 14 connector types
- **Common `HandlerFunc` type** (`internal/connector/fanout.go`): Universal handler type in the connector package, enabling type-safe chaining across all connector implementations

### Changed
- **All connector `RegisterRoute` methods**: Now detect duplicate registrations and chain handlers using fan-out instead of silently overwriting. Logs `fan-out: multiple flows registered` at Info level when chaining occurs. Affects: REST, gRPC, TCP, WebSocket, SOAP, SSE, GraphQL (server+client), RabbitMQ, Kafka, Redis Pub/Sub, MQTT, CDC, File watch (14 connectors)

## [1.14.0] - 2026-03-15

### Added
- **Mycel Studio Debug Protocol** (`internal/debug/`): WebSocket JSON-RPC 2.0 debug server for IDE integration. Mounted on admin server at `:9090/debug`. Provides full runtime introspection and live debugging of running Mycel services. 29 tests covering all 6 protocol phases
- **Session management**: `debug.attach` / `debug.detach` RPC methods. Each connected client gets an isolated session with its own breakpoints, threads, and resume channels. Multiple IDE clients can connect simultaneously
- **Runtime introspection**: `RuntimeInspector` interface exposes read-only views of a live service. RPC methods: `inspect.flows`, `inspect.flow`, `inspect.connectors`, `inspect.types`, `inspect.transforms`. Enables IDEs to build autocompletion and object trees from a running service
- **Event streaming**: `EventStream` fan-out broadcasts pipeline trace events to all connected debug clients in real time. `StudioCollector` implements `trace.Collector`. Events: `event.flowStart`, `event.flowEnd`, `event.stageEnter`, `event.stageExit`, `event.ruleEval`
- **Stage-level breakpoints**: `StudioBreakpointController` implements `trace.BreakpointController`. `debug.setBreakpoints` configures stage breakpoints per flow. `debug.continue` / `debug.next` resume execution. `debug.threads` lists in-flight requests. `debug.variables` inspects data at paused stage. `event.stopped` / `event.continued` notifications. One `DebugThread` per concurrent request
- **Per-CEL-rule breakpoints**: `TransformHook` interface (`BeforeRule` / `AfterRule`) injected via `context.Context` into CEL rule evaluation loops. `StudioTransformHook` streams `event.ruleEval` and pauses at individual rules. `debug.stepInto` steps into the next rule within a transform block
- **Watch expressions and evaluate**: `debug.evaluate` executes a CEL expression against the paused thread's activation record. Enables ad-hoc queries like `output.email` or `size(input.items)` while paused at a breakpoint
- **Conditional breakpoints**: `debug.setBreakpoints` accepts an optional `condition` field (CEL expression). Breakpoint only pauses when condition evaluates to `true`
- **TransformHook context helpers** (`internal/transform/hook.go`): `WithTransformHook(ctx, hook)` / `HookFromContext(ctx)`. Zero-cost when no hook (~10ns nil-check)

### Changed
- **Admin server always starts**: `:9090` now starts unconditionally on every `mycel start`, regardless of REST connector presence. Ensures debug protocol, health checks, and metrics are always reachable
- **`internal/transform/cel.go`**: Hook injection in `Transform`, `TransformResponse`, `TransformWithContext` rule loops. Zero overhead when no hook: single nil-check per transform invocation
- **`internal/runtime/runtime.go`**: Debug server initialization, `RuntimeInspector` methods on Runtime, admin mux registration
- **`internal/runtime/flow_registry.go`**: Debug context injection in `HandleRequest` — trace, breakpoints, and transform hooks attached when debug session is active

## [1.13.0] - 2026-03-13

### Added
- **PDF connector** (`internal/connector/pdf/`): Generate PDF documents from HTML templates using pure Go (no CGO, no external binaries). Uses `go-pdf/fpdf` for rendering and Go's `text/template` for data binding. Supports: headings (h1-h6), paragraphs, tables with headers, bold/italic, lists (ul/ol), horizontal rules, images, and basic CSS styles (text-align, font-size, color). Two operations: `generate` (returns PDF bytes for HTTP response) and `save` (writes to file)
- **Binary HTTP responses**: REST connector now detects `_binary` + `_content_type` fields in results and serves raw binary responses (PDF, images, etc.) with proper Content-Type and Content-Disposition headers
- **Response enrichment in after aspects**: After aspects can now include a `response` block with CEL expression body fields and HTTP headers. Body fields are merged into every row of the result. Headers are set as actual HTTP headers by the REST connector (or protocol equivalent for other connectors). Only valid for `after` aspects. Useful for API versioning (RFC 8594 deprecation headers), pagination metadata, CORS, and cross-cutting response decoration
- **Idempotency keys**: Flow-level `idempotency` block with `storage` (cache connector), `key` (CEL expression), and `ttl`. Prevents duplicate processing by caching results and returning them for matching keys
- **Async execution (HTTP 202 + polling)**: Flow-level `async` block with `storage` (cache connector) and `ttl`. Returns HTTP 202 with a `job_id` immediately, processes in background, auto-registers `GET /jobs/{job_id}` polling endpoint
- **Database migrations**: `mycel migrate` CLI command runs SQL migration files from `migrations/` directory in alphabetical order. `mycel migrate status` shows migration status. Tracking via `_mycel_migrations` table (SQLite + PostgreSQL compatible)
- **File upload (multipart/form-data)**: REST connector auto-detects multipart uploads, parses files (32MB max), encodes as base64 with metadata (`filename`, `content_type`, `size`, `data`). Available in transforms as `input.files`
- **HTML email templates**: Email connectors (SMTP, SendGrid, SES) support `template_file` for Go `text/template` rendering. Template receives the full payload as data context
- **Multi-tenancy via request headers**: Request headers now available as `input.headers` in flow transforms/CEL expressions. Enables tenant isolation by reading `X-Tenant-ID` or similar headers
- **Distributed rate limiting**: Rate limiter now supports Redis backend via `storage` attribute in `rate_limit` block. Uses fixed-window counter algorithm with automatic fallback to in-memory on Redis errors
- **Use case examples #15-22**: Queue consumer to database, scheduled/cron jobs, API aggregation (BFF pattern), CDC pipeline, GraphQL API over database, circuit breaker on external APIs, PDF generation from HTML template, API versioning with deprecation warnings. Total: 22 complete examples
- New dependency: `github.com/go-pdf/fpdf` v0.9.0 (pure Go, BSD license)

## [1.12.3] - 2026-03-13

### Added
- **Flow invocation from aspects**: Aspect actions can now invoke flows directly using `action { flow = "flow_name" }` instead of only writing to connectors. The `connector` and `flow` attributes are mutually exclusive. The invoked flow receives the transform output as its input. Errors in invoked flows are soft failures (warning log, main flow unaffected)
- **Internal flows**: Flows without a `from` block can now serve as reusable building blocks, invocable only from aspects. Enables flow orchestration and composition through the AOP system
- **`FlowInvoker` interface** (`internal/aspect/executor.go`): Decoupled interface for flow invocation from aspect executor. `FlowRegistry` implements it via `InvokeFlow` method
- Use case examples #11-14: flow orchestration (welcome email), error recovery flow, notification hub (route by event type), data sync to external system

## [1.12.2] - 2026-03-13

### Added
- **Structured error object in on_error aspects**: The `error` variable in `on_error` aspects is now a structured object with `error.code` (int, HTTP status code), `error.message` (string), and `error.type` (string: `http`, `flow`, `validation`, `not_found`, `timeout`, `connection`, `auth`, `unknown`). Enables routing errors to different actions based on status code or error type (e.g., `if = "error.code == 404"` or `if = "error.type == 'timeout'"`)
- **Common use cases guide** (`docs/guides/use-cases.md`): 10 complete, copy-paste ready examples covering REST+DB+Slack notifications, welcome emails, audit logging, caching with invalidation, event publishing, error alerting with routing, input validation, response enrichment, webhook relay, and rate limiting

## [1.12.1] - 2026-03-12

### Changed
- **Aspects target flow names instead of file paths**: `on` patterns in aspects now match against flow names using `filepath.Match` glob syntax (e.g., `create_*`, `*_user`). File path matching removed entirely — aspects are now decoupled from filesystem layout
- **Unique name validation per type**: Parser now enforces unique names within each configuration type (connector, flow, type, transform, aspect, validator). Duplicate names produce clear errors with file locations: `duplicate flow name "create_user": defined in flows/api.hcl and flows/users.hcl`

### Removed
- **File path matching in aspects**: All `doublestar` and path-based matching code removed from `internal/aspect/registry.go`. No backward compatibility — patterns must reference flow names
- **`FlowPath` field**: Removed from `FlowHandler` struct in `internal/runtime/flow_registry.go`

## [1.12.0] - 2026-03-11

### Added
- **Response block**: New `response` block in flows transforms data **after** receiving it from the destination connector. For echo flows (no `to` block), the response block defines the output directly. Variables: `input.*` (original request), `output.*` (destination result)
- **Echo flows**: Flows without a `to` block are now fully supported. They return the transformed input (or response block output) directly, enabling pure transformation endpoints, health checks, and stub responses
- **HTTP status code override**: `http_status_code` field in response block sets custom HTTP status codes (REST, SOAP connectors). Example: `http_status_code = "501"` returns HTTP 501
- **gRPC status code override**: `grpc_status_code` field in response block sets custom gRPC status codes with optional `error` message field
- **`ExtractStatusCode` helper** (`internal/connector/connector.go`): Shared utility for extracting protocol-specific status codes from flow results, used by REST, SOAP, and gRPC connectors
- **`TransformResponse` method** (`internal/transform/cel.go`): CEL transformer method for response blocks with `input` and `output` context variables

### Fixed
- **Nil pointer dereference in echo flows**: Fixed multiple nil dereferences when `Config.To` is nil — in `registerFlows` (runtime.go), `executeFlowCoreInternal` (flow_registry.go lines 818, 874), and flow banner printing

## [1.11.0] - 2026-03-10

### Added
- **Environment-aware defaults** (`internal/envdefaults/`): `MYCEL_ENV` now changes runtime behavior, not just the banner. Central `ForEnvironment()` function returns defaults for development, staging, and production environments
- **Environment-aware logging**: Log level and format default to the environment (debug/text in dev, info/json in staging, warn/json in production). Priority: CLI flag > env var > environment default
- **Environment-aware hot reload**: Enabled by default in development/staging, disabled in production. Explicit `--hot-reload` flag overrides
- **Environment-aware GraphQL Playground**: Enabled in development/staging, disabled in production. Explicit `playground` property overrides
- **Environment-aware health checks**: Detailed mode (latencies + error messages) in development/staging, minimal (status only) in production via `SetDetailedMode()` on health manager
- **Environment-aware rate limiting**: Disabled by default in development, enabled with sensible defaults (100 req/s, burst 200) in staging/production when no explicit config
- **Environment-aware CORS**: Permissive (all origins) in development when no CORS config, strict (no CORS headers) in production
- **Environment-aware error responses**: Verbose errors in development/staging, minimal errors (no internal details) in production for 500-level responses
- **Startup warnings**: Production/staging log warnings for SQLite usage and missing auth configuration
- **Environment label in metrics**: `mycel_service_info` gauge now includes `environment` label
- **Environment propagation**: `connector.Config.Environment` field carries the runtime environment to all connector factories
- **Flow trace system** (`internal/trace/`): `mycel trace <flow-name>` CLI command executes a single flow and shows step-by-step data pipeline trace. Stages: input → sanitize → filter → dedupe → validate → enrich → transform → steps → read/write. Zero overhead in production (nil-check). `--dry-run` simulates writes without executing. `--list` shows available flows. `MemoryCollector` for CLI, `LogCollector` for runtime verbose logging. 9 tests
- **Debugging guide** (`docs/guides/debugging.md`): Complete reference for `mycel trace`, dry-run mode, breakpoints, verbose flow logging, Docker debugging
- **Connector doc cross-references**: All 16 connector docs now link to full configuration reference in `docs/reference/configuration.md`
- **Verbose flow logging** (`--verbose-flow`): Per-request pipeline tracing via structured logs at debug level. All pipeline stages logged for every request when enabled on `mycel start`
- **Interactive breakpoints** (`--breakpoints`, `--break-at`): Step-by-step interactive debugging in `mycel trace`. Pause at every stage or specific stages (input, sanitize, validate, transform, step, read, write). Commands: next, continue, print, quit, help
- **Dry-run for all write operations**: `--dry-run` now works for UPDATE, DELETE, and multi-destination writes (previously only INSERT)
- **DAP server** (`internal/dap/`): Debug Adapter Protocol server for IDE integration (VS Code, IntelliJ, Neovim). `mycel trace --dap=4711` starts a TCP DAP server. Supports: initialize, launch, setBreakpoints, configurationDone, threads, stackTrace, scopes, variables, continue, next, disconnect. Pipeline stages mapped to virtual line numbers. 11 tests
- **Dev-only debug features**: `--verbose-flow`, `--breakpoints`, `--break-at`, and `--dap` are restricted to development mode (`MYCEL_ENV=development`). In other environments, a warning is logged and the feature is silently disabled
- **BreakpointController interface** (`internal/trace/`): Breakpoint control abstracted to interface, enabling both CLI (`Breakpoint`) and IDE (`DAPBreakpoint`) implementations
- **MQTT connector** (`internal/connector/mqtt/`): Standalone IoT messaging connector. Publish/subscribe with QoS 0/1/2, topic wildcards (`+`, `#`), TLS support, automatic reconnection with re-subscription. `paho.mqtt.golang` client. 13 unit tests
- **FTP/SFTP connector** (`internal/connector/ftp/`): Remote file transfer over FTP, FTPS, and SFTP. Directory listing (LIST), file download (GET) with auto-format detection (JSON/CSV/text), file upload (PUT), directory creation (MKDIR), file deletion (DELETE). `remoteClient` interface abstracts both protocols. Standard `connector.Reader`/`connector.Writer` interfaces. 22 unit tests
- **Redis Pub/Sub** (`internal/connector/mq/redis/`): New MQ driver (`driver = "redis"`) for fire-and-forget pub/sub. Subscribe/PSubscribe with channel and glob-pattern matching. Handler resolution: exact channel → pattern → wildcard. Uses existing `go-redis/v9` dependency. 13 unit tests
- **Integration tests for MQTT, FTP/SFTP, Redis Pub/Sub**: Docker Compose services (Mosquitto MQTT broker, atmoz/sftp server), HCL configs (connectors + flows), test scripts following existing patterns. 3 new test suites added to parallel execution
- **Connector documentation**: `docs/connectors/mqtt.md` (MQTT), `docs/connectors/ftp.md` (FTP/SFTP), Redis Pub/Sub section added to `docs/connectors/message-queues.md`
- **Example configurations**: `examples/mqtt/` (IoT gateway), `examples/ftp/` (SFTP file processor), `examples/redis-pubsub/` (event processor)

### Changed
- **FTP connector interface compliance**: `Read` and `Write` methods now return standard `*connector.Result` instead of raw maps, enabling FTP/SFTP to work through the flow_registry like all other connectors
- `metrics.NewRegistry` now accepts an `environment` parameter for the service info metric
- REST connector CORS middleware is now environment-aware (permissive in dev, strict in prod)
- REST connector `writeError` now strips internal error details in production
- GraphQL factory uses `envdefaults.ForEnvironment()` for playground default instead of hardcoded `true`
- Logger creation uses environment defaults as baseline instead of hardcoded `info`/`text`
- Rate limiter initialization checks environment defaults when no explicit config

## [1.10.0] - 2026-03-09

### Added
- **CSV/TSV enhanced I/O** (`internal/connector/file/`): Configurable CSV options — delimiter (comma/tab/semicolon/pipe), comment character, skip_rows, no_header mode, custom column names, trim_space. TSV auto-detected from `.tsv`/`.tab` extensions. UTF-8 BOM detection and stripping. Sorted header output with optional column ordering. Connector-level CSV defaults via `csv_*` properties. 10 new tests
- **Long-running workflow engine** (`internal/workflow/`): Persistent workflow execution for sagas with delay/await steps. `Engine` manages background ticker (5s) for processing delayed and expired instances. `SQLStore` with SQLite/Postgres/MySQL dialect support (UPSERT, indexes, nullable timestamps). Workflow states: running, paused, completed, failed, timeout, cancelled
- **Delay steps** in sagas: `delay = "5m"` pauses workflow execution, persists `resume_at` timestamp, background ticker automatically resumes when delay expires
- **Await/Signal steps** in sagas: `await = "payment_confirmed"` pauses workflow until external signal. Signal API resumes execution with optional data payload. Step-level timeout for await steps
- **Workflow REST API**: `GET /workflows/{id}` (status), `POST /workflows/{id}/signal/{event}` (resume), `POST /workflows/{id}/cancel` (cancel). Auto-registered when workflow engine is active
- **Workflow service config**: `workflow {}` block in `service {}` — `storage` (connector name), `table` (custom table name), `auto_create` (auto-create schema). Parser support with `WorkflowConfig` type
- **Saga timeout**: `timeout = "24h"` on saga config enforces maximum workflow duration. Background ticker marks expired instances and runs compensation
- **DBAccessor interface** (`internal/connector/connector.go`): Database connectors expose `DB() *sql.DB` for workflow engine to reuse existing connections. Implemented on PostgreSQL and MySQL connectors
- **NeedsPersistence helper**: Detects if a saga has delay/await steps requiring async execution. Simple sagas (no delay/await) continue synchronous execution unchanged — full backward compatibility

### Changed
- **Saga parser** (`internal/parser/saga.go`): Added `delay` and `await` attributes to step schema. Delay/await steps don't require an action block
- **Saga executor** (`internal/saga/executor.go`): Added `ExecuteStep` and `ExecuteAction` exported methods for workflow engine access
- **FlowHandler** (`internal/runtime/flow_registry.go`): Async sagas dispatched via workflow engine return HTTP 202 with `workflow_id`. Sync sagas unchanged
- **Runtime** (`internal/runtime/runtime.go`): Workflow engine initialization, endpoint registration, graceful shutdown integration

## [1.9.0] - 2026-03-09

### Added
- **Plugin git sources** (`internal/plugin/git.go`): Plugins can now be sourced from GitHub, GitLab, Bitbucket, or any git-cloneable URL. SSH first with automatic HTTPS fallback. Version resolution via `git ls-remote --tags`
- **Semver constraint engine** (`internal/plugin/semver.go`): Full semver parsing and constraint matching — supports `^1.0` (caret), `~1.5` (tilde), `~> 2.0` (HashiCorp), `>= 1.0, < 3.0` (range), exact versions, and `latest`
- **Plugin cache** (`internal/plugin/cache.go`): Local cache in `mycel_plugins/` directory (like `node_modules`). Plugins downloaded once, reused across restarts. `copy = true` option for local plugins (useful for Docker)
- **Plugin lock file** (`internal/plugin/lockfile.go`): `plugins.lock` JSON file for reproducible builds. Atomic writes via temp+rename. Records source, version, resolved URL, and timestamp
- **Plugin CLI** (`cmd/mycel/plugin.go`): `mycel plugin install`, `mycel plugin list`, `mycel plugin remove`, `mycel plugin update`. Auto-install on `mycel start` when plugins are declared
- **Plugin validators and sanitizers** (`internal/plugin/types.go`, `internal/plugin/loader.go`): Plugins can now provide validators and sanitizers in addition to connectors and functions. Registered automatically in the runtime
- **Validator wiring** (`internal/runtime/flow_registry.go`, `internal/parser/types.go`): Custom validators (regex/CEL/WASM) can now be referenced from type field definitions via `validator = "name"` attribute. The `ValidatorRef` on type fields is resolved at validation time against the `validator.Registry`, connecting config/plugin validators to the type validation system
- **Plugin manifest detection** (`internal/parser/parser.go`): Main parser now auto-detects plugin manifest files (`plugin {}` without label + `provides {}` block) and skips them during recursive scanning. Only `mycel_plugins/` cache directory is excluded by name — user plugin directories can be placed anywhere in the config tree
- **Plugin integration test** (`tests/integration/`): End-to-end test with local WASM plugin providing an `always_valid` validator. Type `plugin_validated` references it via `validator = "always_valid"`. Flow validates input through the plugin's WASM binary before writing to SQLite. 4 assertions (startup, log verification, validated POST)
- **Request logging** (`internal/runtime/flow_registry.go`): Every flow execution is now logged with flow name, source connector, operation, and duration. Errors are logged at WARN level with the error message. Centralized in `FlowHandler.HandleRequest` — works for all connectors (REST, GraphQL, gRPC, SOAP, TCP, MQ, WebSocket, CDC, SSE, file watcher)
- **Pretty logs with tint** (`internal/logging/logging.go`): Text format now uses `lmittmann/tint` for colored, human-readable output similar to pino-pretty. Short timestamp (`4:49PM`), colored level (`INF`/`WRN`/`ERR`), dimmed attributes. JSON format unchanged for production

### Fixed
- **Integration test runner empty arrays** (`tests/integration/run.sh`): Fixed `unbound variable` errors when running a subset of tests by using `${ARRAY[@]+"${ARRAY[@]}"}` syntax for potentially empty arrays under `set -u`

## [1.8.0] - 2026-03-09

### Added
- **Security system — secure by default** (`internal/sanitize/`, `internal/security/`): Core input sanitization pipeline that runs before every flow execution. Cannot be disabled. Protects against null bytes, invalid UTF-8, control character injection, Unicode bidi attacks, oversized inputs, and deep nesting. Configurable thresholds via `security {}` HCL block (adjust limits, not disable)
- **WASM sanitizers**: Custom sanitization rules via WebAssembly modules. Define `sanitizer` blocks in the `security {}` config with field targeting and flow pattern matching. Same WASM interface as validators/functions
- **Connector-specific security rules** (`internal/sanitize/rules/`): XML entity blocking (XXE), file path containment, shell metacharacter detection, SQL identifier validation. Applied automatically based on connector type
- **Security HCL block** (`internal/parser/security.go`): New top-level `security {}` block for threshold overrides, WASM sanitizers, and per-flow security config. Parser, types, and runtime integration
- **Security documentation** (`docs/SECURITY.md`): Complete reference covering core pipeline, connector protections, HCL configuration, WASM sanitizer interface, and vulnerability mitigations
- **Security integration tests** (`tests/integration/scripts/test-security.sh`): 29 end-to-end assertions sending malicious payloads to real endpoints (REST, GraphQL, SOAP, File). Tests null byte injection, control character injection, bidi override attacks, SQL injection safety, oversized payloads, deep nesting (JSON bomb), XXE entity expansion, and path traversal — all against live services

### Fixed
- **XXE vulnerability** (`internal/codec/xml.go`, `internal/connector/soap/envelope.go`): Blocked XML entity expansion in both the XML codec and SOAP envelope parser by setting `decoder.Entity = map[string]string{}`
- **SSH command injection** (`internal/connector/exec/connector.go`): User-provided arguments to SSH remote commands and shell-wrapped local commands are now individually quoted with `shellQuote()` to prevent shell metacharacter injection
- **File path traversal** (`internal/connector/file/connector.go`): `resolvePath()` now strips absolute paths, normalizes `../` sequences, and validates that resolved paths stay within `BasePath`

### Changed
- **WASM documentation** (`docs/WASM.md`): Complete reference for building WASM modules in 6 languages — Rust, Go (TinyGo), C, C++, AssemblyScript, and Zig. Covers the WASM interface specification (alloc/free/validate/function exports), memory flow, HCL configuration for validators/functions/plugins, module size comparison, and best practices. Fixed broken link from `examples/plugin/README.md` and added cross-references between all WASM-related examples

## [1.7.0] - 2026-03-06

### Added
- **Integration Test Suite** (`tests/integration/`): Complete end-to-end testing infrastructure with Docker Compose. 10 infrastructure services (PostgreSQL, MySQL, MongoDB, Redis, RabbitMQ, Kafka, Elasticsearch, MinIO, Mock HTTP server, Cosmo Router), 25 test suites, 86 assertions. Tests every connector type and protocol (REST, GraphQL, gRPC, SOAP, AMQP, Kafka, S3, HTTP client, notifications). Includes mock server for capturing outbound API calls, shared bash test library, master runner script, and CI workflow (manual trigger). Kafka init container ensures topic exists before Mycel starts
- **Parallel test execution** (`tests/integration/run.sh`): Tests run in 3 phases: preflight (health/metrics), parallel (22 suites concurrently), solo (rate-limit). `--sequential` flag available for one-by-one execution. Mock-dependent tests (http-client, notifications) grouped to avoid `mock_clear` conflicts. `run-group.sh` helper for CI grouped parallel execution
- **CI grouped steps** (`.github/workflows/integration.yml`): Each test group is a separate collapsible step in GitHub Actions UI (Health & Metrics, Databases, Protocols, Messaging, Storage & Cache, Integration, Rate Limit). Tests within each group run in parallel via `run-group.sh`
- **GraphQL typed DTOs**: `returns` attribute on flows generates typed GraphQL output types instead of generic JSON. Mutations auto-infer typed `<TypeName>Input` argument from the return type. GraphQL introspection tests verify `User` and `UserInput` types exist with correct fields
- **`required = false` type field attribute** (`internal/parser/types.go`): Fields in HCL type definitions can now be marked as optional with `required = false` in their constraint block (e.g., `id = number({ min = 0, required = false })`). Fields remain required by default
- **Federation SDL type generation** (`internal/connector/graphql/schema.go`): `generateSDL()` now includes HCL-generated object types and input types in the federation SDL, enabling Cosmo Router composition

### Fixed
- **gRPC connector — RegisterRoute interface mismatch** (`internal/connector/grpc/server.go`): Changed handler signature from named `HandlerFunc` type to the concrete `func(ctx context.Context, input map[string]interface{}) (interface{}, error)` required by the `RouteRegistrar` interface. gRPC flows were silently never registered before this fix
- **gRPC connector — reflection registration** (`internal/connector/grpc/server.go`): Added `registerFileDescriptor()` that registers jhump `FileDescriptor` objects with `protoregistry.GlobalFiles` and sets `Metadata` on the `grpc.ServiceDesc`. Makes `grpcurl list` show registered services
- **gRPC connector — proto response adaptation** (`internal/connector/grpc/server.go`): Added `adaptResultForProto()` that auto-wraps arrays in repeated fields for list operations (e.g., `ListUsers`) and unwraps single-element arrays for scalar operations (e.g., `GetUser`)
- **gRPC input-as-filters in flow registry** (`internal/runtime/flow_registry.go`): `SourceType == "grpc"` is now included in the SOAP/TCP branch of `handleRead`, so gRPC input parameters are used as database query filters
- **gRPC read-back on create** (`internal/runtime/flow_registry.go`): The "read back created record" logic that was previously applied only for GraphQL sources now also applies for gRPC sources, returning the full created row after an INSERT
- **to.Operation method override** (`internal/runtime/flow_registry.go`): When `to.Operation` is explicitly set (e.g., `INSERT`), the HTTP method is now derived from the operation value. Fixes gRPC and other non-REST sources where `parseOperation()` would default to GET
- **PostgreSQL INSERT RETURNING** (`internal/connector/database/postgres/connector.go`): `RETURNING *` is now appended to all INSERT queries. `isSelectQuery()` updated to recognize the `RETURNING` keyword. Returns the full created row (including auto-generated `id`, `created_at`) instead of the empty `{id: 0, affected: 1}` result
- **GraphQL Federation variable values filtering** (`internal/connector/graphql/resolver.go`): In `MapArgsToInput()`, complex types (maps, slices) from `VariableValues` that are already resolved via `Args` are now skipped. Prevents Cosmo Router from re-injecting nested `input` objects that break SQL serialization
- **Event-driven source flow routing**: MQ consumers, CDC, and file watcher flows now correctly write to the destination instead of reading. Added `SourceType` field to FlowHandler and `isEventDrivenSource()` detection for `mq`, `cdc`, and `file` connector types
- **SOAP/TCP input-as-filters**: Non-REST read operations (SOAP GetItem, TCP message handlers) now pass all input parameters as query filters, enabling proper database lookups
- **Operation override in handleCreate/handleRead**: Both `handleCreate` and `handleRead` now respect the `to.operation` attribute, allowing connectors like Elasticsearch and HTTP client to use their native operations (e.g., `index`, `get`, `POST`) instead of hardcoded `INSERT`/`SELECT`
- **Auto-generated GraphQL input types are optional** (`internal/connector/graphql/hcl_to_graphql.go`): Input type fields generated from HCL types no longer apply NonNull, following GraphQL best practice where output types guarantee non-null but input types allow partial updates

### Changed
- **Kafka consumer logging** (`internal/connector/mq/kafka/consumer.go`): Removed verbose debug-level `Logger` from kafka-go Reader (was flooding logs with internal fetch/heartbeat/rebalance messages). Changed `ErrorLogger` from `Error` to `Warn` level since most kafka-go internal errors are transient and expected
- **Kafka integration test reliability** (`tests/integration/scripts/test-kafka.sh`): Retry timeout increased from 12x2s to 20x3s for more reliable consumer group initialization
- **PostgreSQL integration test assertion** (`tests/integration/scripts/test-postgres.sh`): POST response assertion updated from `affected` field to actual row data (`Alice`) to reflect the new `RETURNING *` behavior
- **Integration test rate limit config** (`tests/integration/config/config.hcl`): Increased from 10 req/s burst=20 to 50 req/s burst=200 to support parallel test execution. Rate limit test updated to use concurrent requests

## [1.6.0] - 2026-03-05

### Added
- **SOAP Connector** (`internal/connector/soap/`): Bidirectional SOAP web service support. Client mode (call external SOAP services) and Server mode (expose SOAP endpoints) auto-detected from config. SOAP 1.1 and 1.2 supported. Envelope build/parse, SOAP fault handling, WSDL auto-generation at `/wsdl`, basic/bearer auth. 22 tests
- **Codec System** (`internal/codec/`): Multi-format encoding/decoding with `Codec` interface and global registry. JSON and XML codecs included. XML↔map conversion with attributes (`@attr`), text content (`#text`), repeated elements (slices). `DetectFromContentType()` for auto-format detection. 18 tests
- **Format Declarations**: `format` attribute on connectors, flows (`from`/`to`), and steps. Connectors set a default format for all operations. Flow-level format overrides connector default. Context-based format propagation. REST server auto-detects incoming format from Content-Type header. HTTP client auto-detects response format
- **Format Documentation** (`docs/FORMAT.md`): Complete reference for the format system, XML mapping rules, auto-detection behavior, and extensibility
- **File Watch Mode**: Polling-based directory watcher for the file connector. When `watch = true`, the connector scans `base_path` for new and modified files and triggers flow handlers automatically. Glob pattern matching in `from.operation` (e.g., `*.csv`, `reports/*.xlsx`). Handler input includes file metadata (`_path`, `_name`, `_size`, `_mod_time`, `_event`) merged with parsed file content. Works on all filesystems including NFS, Docker volumes, and network mounts. 7 tests

## [1.5.1] - 2026-03-05

### Added
- **Configurable API URLs for notification connectors**: All notification connectors (Slack, Discord, Twilio SMS, FCM, APNs) now support an `api_url` property to override the default API base URL. Useful for proxies, enterprise endpoints, or testing

## [1.5.0] - 2026-03-05

### Added
- **MQ Filter Rejection Policy (`on_reject`)**: Configurable behavior for messages that don't match a `from.filter` in MQ flows. Three policies: `ack` (default, discard), `reject` (send to DLQ), `requeue` (return to queue with dedup tracking). Supports both string and block filter syntax
  - `FilterConfig` struct with `condition`, `on_reject`, `id_field`, `max_requeue`
  - `RequeueTracker` for in-memory dedup with TTL cleanup (prevents infinite requeue loops)
  - RabbitMQ: `Nack(false, false)` for reject, `Nack(false, true)` for requeue
  - Kafka: republish to `<topic>.dlq` for reject, republish to same topic for requeue
  - Lazy writer initialization for consumer-only Kafka connectors
  - Full backwards compatibility with string filter syntax

## [1.4.3] - 2026-03-04

### Added
- **RabbitMQ URL Support**: The `url` field now works as documented — if set, it takes precedence over `host`/`port`/`username`/`password`/`vhost`. Previously, the factory never read the `url` property
- **RabbitMQ Consumer Shorthands**: `consumer.queue` creates a queue declaration if no explicit `queue {}` block is set. `consumer.workers` is an alias for `concurrency`. `consumer.retry_count` creates a DLQ config with `max_retries`
- **RabbitMQ DLQ Documentation**: Full Dead Letter Queue configuration options now documented (exchange, queue, routing_key, max_retries, retry_delay, retry_header)

### Fixed
- **MQ Documentation Consistency**: Rewrote `docs/connectors/message-queues.md` to match implementation. Fixed Kafka `offset` → `auto_offset_reset` (correct field name), fixed default from `latest` to `earliest`, added all missing Kafka fields (auto_commit, min_bytes, max_bytes, max_wait_time, concurrency, retries, batch_size, linger_ms, client_id), added Kafka Schema Registry documentation, added RabbitMQ TLS/Queue/Exchange/DLQ sections, added Required column to all option tables

## [1.4.0] - 2026-03-04

### Fixed
- **Version Display**: Runtime version was hardcoded to `0.1.0` — now propagated from the CLI binary. Banner, health endpoints, and metrics all report the correct Mycel version
- **Runtime Metrics**: `mycel_uptime_seconds` and `mycel_goroutines` Prometheus metrics are now updated every 15 seconds via a background goroutine (previously the methods existed but were never called)

### Added
- **Service Version in Health**: All health endpoints (`/health`, `/health/live`, `/health/ready`) now include `service_version` from `config.hcl` in their JSON response
- **Mycel Version in Metrics**: `mycel_service_info` metric now includes a `mycel_version` label alongside `service` and `version`, making it easy to identify which Mycel release is running

## [1.3.0] - 2026-03-04

### Added
- **.env File Support**: Mycel now automatically loads a `.env` file on startup (`start`, `validate`, `check` commands). Looks for `<config-dir>/.env` first, falls back to `./.env`. Existing environment variables are never overridden. Silent when no `.env` file is found (normal for production/Docker)
- **Deployment Guide** (`docs/DEPLOYMENT.md`): New documentation covering Docker, Docker Compose, Kubernetes deployment, environment variable reference, and `.env` file usage

## [1.2.0] - 2026-03-04

### Added
- **Standalone Admin Server**: Health checks (`/health`, `/health/live`, `/health/ready`) and metrics (`/metrics`) are now always available, even without a REST connector. When no REST connector is configured, Mycel automatically starts a lightweight admin server on port 9090 (configurable via `admin_port` in the `service` block). This ensures Kubernetes probes and monitoring work for queue workers, CDC pipelines, and any service without HTTP endpoints.

## [1.1.0] - 2026-03-04

### Added
- **Error Handling Guide** (`docs/ERROR_HANDLING.md`): Comprehensive guide covering all error handling layers — retry, fallback/DLQ, circuit breaker, rate limiting, on-error aspects, connector profiles, health checks
- **On-Error Aspects** (`when = "on_error"`): New aspect timing that executes only when a flow fails. Provides `error.message` in transform expressions for logging errors, sending alerts, or notifying external systems
- **Custom Error Responses** (`error_response` block): Define custom HTTP status codes, headers, and response bodies for flow errors using CEL expressions

## [1.0.0] - 2026-03-03

### Added - Excel Support
- **Native Excel (.xlsx) read/write** in the file connector via `excelize/v2`
  - Auto-detect format from `.xlsx`/`.xls` extensions
  - First row treated as column headers (same convention as CSV)
  - Sheet selection via `params = { sheet = "SheetName" }` (defaults to first sheet)
  - Empty rows automatically skipped on read
  - Sorted column headers for deterministic write output

### Fixed
- **SSE data race**: Synchronized initial header flush with `client.mu` to prevent race with concurrent `sendEvent` writes
- **README features table**: Separated GraphQL Subscriptions into its own row (was incorrectly listed inside the Federation row)
- **CONCEPTS.md**: Updated subscriptions cross-reference to link to connector docs and example

### Changed - Documentation
- **README Quick Start**: Added context for creating a project directory and clearer step descriptions
- **README Installation**: Added Docker Hub as alternative registry (`mdenda/mycel`)
- **Filesystem connector docs**: Complete rewrite with all 8 operations, format output examples, and real-world usage patterns

### Changed - Helm Chart v0.2.0: Directory-Based Configuration
- **`config/` directory**: Chart auto-discovers all `.hcl` files under `helm/mycel/config/` using `.Files.Glob` — copy your project files in and deploy, no flags needed
- **`existingConfigMap`**: New `mycel.config.existingConfigMap` value to reference a pre-existing ConfigMap instead of creating one
- **Inline fallback**: Inline values in `values.yaml` (service, connectors, flows, types) still work when `config/` is empty
- **ConfigMap guard**: Chart skips ConfigMap creation when `existingConfigMap` is set
- **Deployment template**: Volume uses `existingConfigMap` when provided, falls back to generated ConfigMap
- **Chart version**: Bumped to 0.2.0

### Added - Phase 12.1: Saga Pattern
- **Saga pattern** for declarative distributed transactions with automatic compensation
  - New top-level `saga` HCL block with `step`, `action`, `compensate`, `on_complete`, `on_failure`
  - Saga executor: runs steps in order, compensates in reverse on failure
  - Step results available as `step.<name>.*` in subsequent actions and compensations
  - CEL expression resolution in `data`, `body`, `where`, `set` fields
  - `on_error = "skip"` for non-critical steps
  - Saga handler registers sagas as flow handlers in the runtime
- **New package**: `internal/saga/` (config types + executor)
- **New parser**: `internal/parser/saga.go` — `parseSagaBlock`, `parseSagaStepBlock`, `parseSagaActionBlock`
- **Tests**: 10 saga executor tests (all steps succeed, compensation, compensation failure, skip on error, multiple reverse compensations, etc.)
- **Parser tests**: `TestParseSaga` — validates full HCL parsing
- **New example**: `examples/saga/` — order creation with 3-step saga

### Added - Phase 12.2: State Machine
- **State machine** for entity lifecycle management with declarative states and transitions
  - New top-level `state_machine` HCL block with `state`, `on` (transitions), `guard`, `action`, `final`
  - State machine engine: validates transitions, evaluates CEL guards, executes actions, persists state
  - State stored in entity's `status` column — no separate state table needed
  - Guards (CEL expressions) prevent invalid transitions
  - Transition actions execute connector operations during state changes
  - Final states block further transitions
- **New `state_transition` block in flows** — triggers state machine transitions from REST/queue/etc.
  - CEL expressions for `id`, `event`, `data` fields
- **New package**: `internal/statemachine/` (config types + engine)
- **New parser**: `internal/parser/statemachine.go` — `parseStateMachineBlock`, `parseStateBlock`, `parseTransitionBlock`
- **Tests**: 10 state machine engine tests (valid transitions, initial state, invalid events, final states, guards, actions, multi-step, action failure)
- **Parser tests**: `TestParseStateMachine`, `TestParseFlowWithStateTransition`
- **New example**: `examples/state-machine/` — order status lifecycle
- **Docs**: CONCEPTS.md updated with Sagas and State Machines sections

### Added - Connector Documentation Catalog
- **New `docs/connectors/` directory** with individual documentation for every connector type
  - Catalog README with categorized tables linking to all 16 connector docs
  - 16 connector docs: REST, Database, GraphQL, gRPC, Message Queues, TCP, WebSocket, SSE, CDC, Elasticsearch, Cache, Filesystem, S3, Exec, Notifications, OAuth, Profile
  - Each doc follows a consistent template: description, configuration, operations table, example flow
- **Refactored CONCEPTS.md**: Removed 5 inline connector sections (WebSocket, CDC, SSE, Elasticsearch, OAuth) — now linked from the catalog
- **Updated README.md**: Connector concept links point to `docs/connectors/`, added Connector Catalog to Documentation section

### Added - Phase 11.1: Elasticsearch Connector
- **Elasticsearch connector** for full-text search and analytics via REST API
  - Read operations: `search` (query DSL), `get` (by ID), `count`, `aggregate`
  - Write operations: `index` (create/replace), `update` (partial), `delete`, `bulk`
  - Multi-node cluster support with round-robin load balancing
  - Basic auth support (`username`/`password`)
  - Filter→bool/must term conversion, pagination (`size`/`from`), sorting (`sort`)
  - Field selection via `_source` includes
  - Implements `Connector`, `Reader`, `Writer` interfaces
- **New connector type**: `elasticsearch` with `nodes`, `username`, `password`, `index`, `timeout` configuration
- **Tests**: 25 tests covering factory, search, get, count, aggregate, index, update, delete, bulk, auth, round-robin, health, errors
- **New example**: `examples/elasticsearch/` — search, CRUD, count

### Added - Phase 11.2: External OAuth Connector
- **OAuth connector** for declarative social login flows
  - Operations: `authorize` (generate state + auth URL), `callback` (exchange code for user info), `userinfo` (fetch profile), `refresh` (refresh token)
  - Built-in CSRF protection with state management and 10-minute expiry
  - Drivers: `google`, `github`, `apple`, `oidc` (with discovery), `custom` (manual URLs)
  - Reuses existing `internal/auth` OAuth2Service and provider implementations
  - Implements `Connector`, `Reader` interfaces
- **New connector type**: `oauth` with `driver`, `client_id`, `client_secret`, `redirect_uri`, `scopes` configuration
- **Tests**: 21 tests covering factory, authorize, callback, state validation, expired state, exchange errors, userinfo, refresh, custom driver
- **New example**: `examples/oauth/` — Google and GitHub social login

### Added - Phase 11.3: Batch Processing
- **Batch processing** for chunked data operations (migrations, ETL, reindexing)
  - New `batch` block in flows: `source`, `query`, `params`, `chunk_size`, `on_error`, `transform`, `to`
  - Reads from source connector in pages using LIMIT/OFFSET pagination
  - Optional per-item transform with CEL expressions
  - Error handling: `"stop"` (halt on first error) or `"continue"` (skip and report)
  - Returns `BatchResult` with `processed`, `failed`, `chunks`, `errors` stats
- **Parser**: `parseBatchBlock()` for HCL batch block parsing
- **Runtime**: `executeBatch()` method on FlowHandler
- **Tests**: 12 tests covering basic batch, chunking, transforms, on_error modes, empty source, params, stats, error cases
- **New example**: `examples/batch/` — user migration, product reindexing, order export

### Added - Phase 10.1: WebSocket Connector
- **Standalone WebSocket connector** for bidirectional real-time communication
  - Source operations: `message`, `connect`, `disconnect` — receive client events as flow triggers
  - Target operations: `broadcast` (all clients), `send_to_room` (room members), `send_to_user` (specific user)
  - Room management via JSON protocol (`join_room`, `leave_room`)
  - Configurable keepalive: `ping_interval` and `pong_timeout`
  - Thread-safe client tracking and room membership
  - Implements `Connector`, `Writer`, `Starter`, and `RouteRegistrar` interfaces
- **New connector type**: `websocket` with `port`, `host`, `path`, `ping_interval`, `pong_timeout` configuration
- **Tests**: 12 tests covering connect, message handling, broadcast, rooms, disconnect cleanup, factory, error cases
- **New example**: `examples/websocket/` — chat, broadcast, room notifications

### Added - Phase 10.2: CDC (Change Data Capture)
- **PostgreSQL CDC connector** for real-time database change streaming via logical replication
  - Source operations: `INSERT:table`, `UPDATE:table`, `DELETE:table` with wildcard support (`*`)
  - Target operations: none (source-only connector)
  - Uses `pgoutput` plugin (built into PostgreSQL 10+) via `jackc/pglogrepl`
  - Automatic publication and replication slot creation
  - Column type decoding: int, float, bool, timestamp, text from pgoutput format
  - Implements `Connector`, `Starter`, and `RouteRegistrar` interfaces
- **New connector type**: `cdc` with `driver`, `host`, `port`, `database`, `user`, `password`, `slot_name`, `publication` configuration
- **Tests**: 15 tests covering factory, dispatch, wildcards, event format, health, operation parsing
- **New example**: `examples/cdc/` — user creation, order status changes, session cleanup, product monitoring
- **New dependencies**: `jackc/pglogrepl`, `jackc/pgx/v5` (pure Go, no CGO)

### Added - Phase 10.3: SSE (Server-Sent Events)
- **SSE connector** for unidirectional server-to-client push over standard HTTP
  - Source operations: none (target-only connector)
  - Target operations: `broadcast` (all clients), `send_to_room` (room members), `send_to_user` (specific user)
  - Room and user targeting via query params (`?room=`, `?rooms=`, `?user_id=`)
  - Configurable heartbeat: `heartbeat_interval` sends periodic keepalive comments
  - CORS support: `cors { allowed_origins = [...] }` for cross-origin clients
  - Implements `Connector`, `Writer`, `Starter`, and `RouteRegistrar` interfaces
- **New connector type**: `sse` with `port`, `host`, `path`, `heartbeat_interval`, `cors` configuration
- **Tests**: 18 tests covering factory, connect, broadcast, rooms, disconnect cleanup, heartbeat, event format, health, CORS, user targeting
- **New example**: `examples/sse/` — live feed, room updates, per-user notifications

### Added - GraphQL Subscription Client
- **Client-side GraphQL subscriptions** — Mycel can subscribe to external GraphQL servers
  - `ClientConnector` implements `Starter` and `RouteRegistrar` interfaces
  - `from { connector = "ext_gql", operation = "Subscription.fieldName" }` syntax
  - WebSocket client using graphql-ws protocol (connection_init → subscribe → next)
  - Automatic reconnection with exponential backoff on disconnect
  - Auth headers forwarded to WebSocket handshake
  - Custom subscription path via `subscriptions { path = "/ws" }` config
  - HTTP→WS / HTTPS→WSS URL scheme conversion
- **New `ClientConfig` field**: `Subscriptions *SubscriptionsConfig`
- **Tests**: 6 new tests covering registration, WebSocket lifecycle, reconnect, URL building, factory

### Changed - Federation Auto-enabled
- **Federation v2 is now always enabled** on every GraphQL server connector
  - `_service { sdl }` always exposed — gateways discover and compose automatically
  - `_entities` available when types have `_key` attributes
  - `federation` block is now optional (only needed to override version)
  - Zero-config federation: just add `_key` to your types and entity resolver flows

### Added - Phase 9: GraphQL Federation Complete
- **Subscription type support** in GraphQL schema
  - `SchemaBuilder` supports `Subscription` fields alongside Query/Mutation
  - Channel-based resolvers backed by PubSub (publish/subscribe)
  - SDL generation includes `type Subscription { ... }` block
  - WebSocket delivery via graphql-ws protocol
- **Flow-triggered subscriptions** — publish from any flow to a subscription topic
  - `to { operation = "Subscription.fieldName" }` syntax
  - Runtime detects `Subscription.*` prefix and publishes instead of writing
  - Steps and transforms are applied before publishing
  - Works with any source connector (REST, Queue, TCP, etc.)
- **Per-user subscription filtering**
  - `PubSub.SubscribeWithFilter()` for per-subscriber message filtering
  - WebSocket `connection_init` params available in subscription context
  - `to { filter = "input.user_id == context.auth.user_id" }` syntax
- **Automatic entity resolution** from HCL types
  - Types with `_key` auto-register Federation entity resolvers
  - Explicit entity resolver flows with `entity = "TypeName"` attribute
  - Runtime matches types to query flows by return type
  - Compatible with Apollo Router and Cosmo Router
- **New flow attribute**: `entity` — marks a flow as a Federation entity resolver
- **New to block attribute**: `filter` — CEL expression for subscription filtering
- **New interfaces**: `SubscriptionPublisher`, `SubscriptionRegistrar`, `EntityRegistrar`
- **New example**: `examples/graphql-federation/` — complete federation subgraph
- **Spec**: `docs/PHASE-9-GRAPHQL-FEDERATION.md`

### Added - Named Operations for Connectors
- **Named operations** for better encapsulation and reusability
  - Connectors define their operations with metadata
  - Flows reference operations by name
  - Improves maintainability and enables mycel-studio introspection
- **Operation block syntax** in connector definitions
  ```hcl
  connector "api" {
    type = "rest"
    port = 8080

    operation "list_users" {
      method      = "GET"
      path        = "/users"
      description = "List all users"

      param "limit" {
        type    = "number"
        default = 100
      }
    }
  }
  ```
- **Parameter definitions** with type, required, default, and validation
  - `type`: string, number, boolean, array, object
  - `required`: mark parameters as mandatory
  - `default`: provide default values
  - `description`: documentation for the parameter
  - Validation constraints: min, max, min_length, max_length, pattern, enum
- **OperationResolver** for operation resolution
  - Automatic resolution based on connector type
  - Parameter validation and default value application
- **Supported operation attributes per connector type**
  - REST: method, path
  - Database: query, table
  - GraphQL: operation_type, field
  - gRPC: service, rpc
  - MQ: exchange, routing_key, queue
  - TCP: protocol, action
  - File/S3: path_pattern
  - Cache: key_pattern, ttl
  - Exec: command, args
- **New example**: `examples/named-operations/`

### Added - Phase 8: GraphQL Query Optimization
- **Automatic query optimization** - Zero configuration required, same HCL produces optimized execution
- **Field Analyzer** (`internal/graphql/analyzer/`)
  - Extract requested fields from GraphQL AST
  - `FieldTree` hierarchical data structure for field tracking
  - `RequestedFields` with `Has(path)`, `Get(path)`, `List()`, `ListFlat()`, `SubFields(path)`, `IsEmpty()`
  - Supports nested fields and arguments extraction
- **Result Pruner** (`internal/graphql/pruner/`)
  - Remove unrequested fields from response data (safety net)
  - `Prune(data, requested)` - prune using RequestedFields
  - `PruneWithPaths(data, paths)` - prune using path list
  - Handles nested objects and arrays recursively
- **Request Context Integration**
  - `__requested_fields` available in input (flat list of all field paths)
  - `__requested_top_fields` available in input (top-level fields only)
  - Automatic injection via `CreateSmartResolver`
- **CEL Functions** for field-based conditional logic
  - `has_field(input, path)` - check if field was requested
  - `field_requested(input, path)` - alias for has_field
  - `requested_fields(input)` - get all requested field paths
  - `requested_top_fields(input)` - get top-level fields only
- **Database Optimizer** (`internal/graphql/optimizer/`)
  - `SQLOptimizer` rewrites `SELECT *` to only fetch requested columns
  - `OptimizeQueryWithFields(query, fields)` - optimize any query
  - `CamelToSnake(field)` - convert GraphQL camelCase to SQL snake_case
  - `FieldsFromInput(input)`, `TopFieldsFromInput(input)` - extract fields from input
  - Integrated into runtime `handleRead` for automatic optimization
- **Step Optimizer** (`internal/graphql/optimizer/step_optimizer.go`)
  - `StepOptimizer` analyzes dependencies between steps and fields
  - `AnalyzeDependencies()` - determine which steps are needed
  - `GetSkippableSteps()` - identify steps that can be skipped
  - `GenerateStepConditions()` - auto-generate when conditions
  - `OptimizeFlowSteps(steps, transformExprs)` - return optimized steps
  - Detects step-to-step dependencies for correct execution order
- **DataLoader** (`internal/dataloader/`) - N+1 query prevention
  - Generic `Loader[K, V]` wrapper around graph-gophers/dataloader v7
  - `LoaderConfig` with BatchSize, Wait, and Cache options
  - `LoaderCollection` for request-scoped loader management
  - `WithLoaders(ctx, collection)` / `GetLoaders(ctx)` context integration
  - `GetOrCreateFromContext[K, V]` for easy loader retrieval
  - `SQLBatchLoader` helper for creating batch functions from SQL queries
  - `SQLManyBatchLoader` helper for one-to-many relationships (e.g., user → orders)
  - `LoaderKey(table, operation)` for generating unique loader keys
  - Automatic batching with configurable wait time (default: 1ms)
  - Optional caching per request (default: enabled)
- **Runtime Integration**
  - Step Optimizer integrated into `executeSteps` - skips unused steps automatically
  - `analyzeNeededSteps()` checks `__requested_top_fields` and uses optimizer
  - DataLoader middleware added to GraphQL server - creates LoaderCollection per request
  - GraphQL requests now have DataLoader context available for batching
- **Dependencies added**:
  - `github.com/graph-gophers/dataloader/v7` - DataLoader implementation
- **Transparent optimization** - Example:
  ```hcl
  # User writes:
  flow "get_users" {
    from { connector = "api", operation = "Query.users" }
    to   { connector = "postgres", target = "users" }
  }

  # Client requests: query { users { id, name } }
  # Mycel automatically executes: SELECT id, name FROM users (not SELECT *)
  ```
- **New example**: `examples/graphql-optimization/`
  - Demonstrates Field Selection, Step Skipping, and DataLoader
  - Complete setup with SQLite database and sample data
  - README with test queries and optimization explanations

### Fixed - HCL Type Circular References
- **Bug**: HCL types that reference each other (e.g., `Order { user = User }`) caused schema build error:
  "User fields must be an object with field names as keys or a function which return such an object"
- **Cause**: First pass created empty placeholder types, second pass created new objects but other types still referenced the empty placeholders
- **Fix**: Use `graphql.FieldsThunk` (lazy loading) for all HCL type conversions, allowing circular references to resolve correctly
- **File**: `internal/connector/graphql/hcl_to_graphql.go`

### Improved - GraphQL Arguments Inference
- **Before**: All GraphQL queries used generic `input: JSON` argument (e.g., `product(input: {id: "1"})`)
- **After**: Arguments are automatically inferred from step params (e.g., `product(id: "1")`)
- **How it works**:
  - Looks for `input.*` references in step params (e.g., `params = { id = "input.id" }`)
  - Creates typed `String` arguments for each discovered field
  - Falls back to `input: JSON` for flows without steps
- **Schema example**:
  ```graphql
  # Before (all flows)
  product(input: JSON): Product

  # After (flows with steps)
  product(id: String): Product

  # After (flows without steps - unchanged)
  users(input: JSON): [User]
  ```
- **Files**:
  - `internal/connector/graphql/schema.go` - Added `ArgDef`, `RegisterHandlerWithArgs`, `buildArgs`, `mapArgType`
  - `internal/connector/graphql/server.go` - Added `RegisterRouteWithArgs`
  - `internal/runtime/runtime.go` - Added `RouteRegistrarWithArgs`, `inferArgsFromFlow`, `extractInputArgs`

### Fixed - Step Param Evaluation in Flows
- **Bug**: Step params like `{ id = "input.id" }` were not being evaluated - the literal string "input.id" was passed instead of the actual value
- **Cause**: `executeSteps` didn't initialize the CEL Transformer, and `EvaluateExpression` didn't support the `step` variable
- **Fix**:
  - Initialize CEL Transformer at start of `executeSteps` if nil
  - Added `EvaluateExpressionWithSteps(ctx, input, steps, expr)` function that includes `step` in CEL activation
  - Updated step param evaluation to use new function
- **Additional fix**: Skipped steps now always set `stepResults[step.Name] = nil` so CEL expressions like `step.X != null` work correctly
- **Files**:
  - `internal/transform/cel.go` - Added `EvaluateExpressionWithSteps`
  - `internal/runtime/flow_registry.go` - Initialize Transformer, use new function, set nil for skipped steps

### Added - Phase 8: GraphQL Federation Complete
- **GraphQL Subscriptions with WebSocket transport**
  - Full `graphql-transport-ws` protocol support
  - WebSocket handler with connection management
  - PubSub mechanism for subscription events
  - Keep-alive ping/pong handling
  - Configurable via HCL `subscriptions` block
  - Path customization (default: `/subscriptions`)
  - Integration with GraphQL server connector
- **Federation directives support in HCL types**
  - Type-level federation directives:
    - `_key = "id"` or `_key = ["id", "email name"]` for @key directive
    - `_shareable = true` for @shareable directive
    - `_inaccessible = true` for @inaccessible directive
    - `_implements = ["Node", "Entity"]` for interface implementations
    - `_description = "..."` for type documentation
  - Field-level federation directives:
    - `external = true` for @external directive
    - `provides = "field1 field2"` for @provides directive
    - `requires = "otherField"` for @requires directive
    - `shareable = true` for @shareable directive
    - `inaccessible = true` for @inaccessible directive
    - `override = "subgraph-name"` for @override directive
    - `description = "..."` for field documentation
  - SDL generation with federation directives
  - Parser support for underscore-prefixed type directives
- **New HCL syntax for federated types**
  ```hcl
  type "User" {
    _key         = "id"
    _shareable   = true
    _description = "A user entity"

    id    = string
    email = string { external = true }
    name  = string { requires = "email" }
  }
  ```

### Added - Phase 5 Complete: Aspects Runtime (AOP)
- **Aspect-Oriented Programming (AOP)** for cross-cutting concerns
  - Pattern-based matching with flow name glob patterns (`create_*`, `update_*`, `*`)
  - Before/After/Around execution points
  - Priority ordering for multiple matching aspects
  - Conditional execution with `if` CEL expressions
- **Cache aspects** for transparent caching
  - Automatic cache lookup before flow execution
  - Cache storage after successful flow completion
  - Template-based cache keys with `${input.id}` interpolation
- **Cache invalidation aspects**
  - Invalidate specific keys after mutations
  - Pattern-based invalidation with wildcards
  - Template interpolation in keys and patterns
- **Rate limiting aspects**
  - Integrated with ratelimit package
  - Per-key rate limiting with CEL key expressions
  - Configurable RPS and burst limits
- **Circuit breaker aspects**
  - Integrated with circuitbreaker package
  - Automatic circuit state management
  - Configurable failure/success thresholds and timeout
- **Action aspects** for side effects
  - Execute connector operations (audit logs, notifications)
  - Transform expressions for building action data
  - Access to flow result in after aspects
- **Parser improvements**
  - Fixed template expression parsing in cache keys (`${input.id}`)
  - Fixed array parsing with template expressions in invalidate keys/patterns

### Added - Phase 7 Flow Orchestration: Step Blocks
- **Multi-step flow execution** with intermediate connector calls
  - Steps execute in order before the transform
  - Results available as `step.<name>.*` in subsequent steps and transforms
  - Support for database queries, HTTP operations, and all connector types
- **Conditional step execution** with `when` clause
  - CEL expressions to conditionally execute steps
  - Access to `input.*` and previous `step.*` results in conditions
- **Error handling per step** with `on_error` attribute
  - `fail`: Fail the entire flow if step fails (default)
  - `skip`: Skip the step and continue with nil result
  - `default`: Use a default value if step fails
- **Step timeout configuration** with `timeout` attribute
- **Request filtering** with `filter` in `from` block
  - CEL expression evaluated before any processing
  - Returns `FilteredResult` when filter evaluates to false
  - Example: `filter = "input.total >= 1000"` for high-value orders only
- **New CEL transformer methods**
  - `EvaluateCondition`: Evaluate boolean CEL expressions
  - `TransformWithSteps`: Transform with step results available
  - `TransformWithContext`: Unified transform with enriched and step data
- **Array helper functions** for data manipulation in transforms
  - `first(list)`, `last(list)`: Get first/last element
  - `unique(list)`: Remove duplicates
  - `reverse(list)`: Reverse list order
  - `flatten(list)`: Flatten nested lists
  - `pluck(list, key)`: Extract field from list of maps
  - `sum(list)`, `avg(list)`: Aggregate numeric values
  - `min_val(list)`, `max_val(list)`: Find min/max values
  - `sort_by(list, key)`: Sort list of maps by a key
- **Map helper functions** for response composition
  - `merge(map1, map2, ...)`: Combine multiple maps (later values override earlier)
  - `omit(map, key1, ...)`: Remove specified keys from a map
  - `pick(map, key1, ...)`: Select only specified keys from a map
  - Supports 2-4 maps for merge, 1-4 keys for omit/pick
  - Ideal for API Gateway aggregation and data sanitization
- **Multi-destination fan-out** for writing to multiple destinations
  - Multiple `to` blocks in a single flow
  - Parallel execution by default (configurable per destination)
  - Conditional writes with `when` CEL expressions
  - Per-destination transforms with access to `output.*`
  - Use cases: event broadcasting, data replication, audit logging
- **Message deduplication** with `dedupe` block
  - Prevent duplicate message processing using cache-based deduplication
  - Configurable key expression (CEL) for unique message identification
  - TTL-based expiration for dedup keys
  - Behavior on duplicate: `skip` (silent) or `fail` (return error)
  - Fail-open design: continues processing if cache is unavailable
  - Use cases: idempotent APIs, message queue exactly-once processing, payment idempotency
- **Flow-level error handling** with `error_handling` block
  - `retry`: Automatic retries with configurable backoff (constant, linear, exponential)
  - `fallback`: Send failed messages to DLQ (Dead Letter Queue)
  - `include_error`: Include error details in fallback message
  - `max_delay`: Cap delay for exponential backoff
- **New example** (`examples/steps/`)
  - Basic multi-step flow (user/product lookup → order creation)
  - Conditional steps (optional pricing/inventory)
  - Chained steps (step results used in subsequent steps)
  - Error handling strategies
  - Request filtering examples
  - Array transforms with aggregation functions
  - Retry with exponential backoff and DLQ fallback
  - Response composition with merge/omit/pick functions
  - API Gateway aggregation pattern
  - Multi-destination fan-out examples
  - Message deduplication examples (order processing, idempotent payments)

### Added - Event-Driven Integration Examples
- **RabbitMQ → REST** (`examples/integration/rabbit-to-rest/`)
  - Consume messages and call external REST APIs
  - Includes DLQ, circuit breaker, and retry configuration
  - Order processing and CRM sync examples
- **RabbitMQ → GraphQL** (`examples/integration/rabbit-to-graphql/`)
  - Consume messages and call GraphQL APIs
  - Inventory updates and user sync examples
  - Bulk operations and query-before-mutation patterns
- **RabbitMQ → Exec** (`examples/integration/rabbit-to-exec/`)
  - Consume messages and execute processes/scripts
  - PDF generation, image processing, video transcoding
  - Semaphore-based concurrency control examples
- **REST → RabbitMQ** (`examples/integration/rest-to-rabbit/`)
  - API Gateway pattern: receive HTTP, queue for processing
  - Webhook receiver with dynamic routing
  - Bulk event ingestion and request-reply patterns
- **File → RabbitMQ** (`examples/integration/file-to-rabbit/`)
  - Scheduled file imports (cron-based)
  - Drop folder watching with polling
  - S3 file processing and log streaming
  - CSV, JSON, XML file processing examples
- **Integration Patterns Documentation** (`docs/INTEGRATION-PATTERNS.md`)
  - Event-driven architecture patterns section
  - Best practices: DLQ, semaphores, locks, circuit breakers
  - Complete order processing pipeline example

### Added - Phase 4.2 Runtime Integration
- **SyncManager**: Unified manager for sync primitives (Lock, Semaphore, Coordinator)
  - Memory and Redis backends for all primitives
  - Automatic resource cleanup on shutdown
  - Stats collection for monitoring
- **Flow execution with sync primitives**
  - Locks: Execute flows with distributed mutex protection
  - Semaphores: Limit concurrent flow executions
  - Coordinator: Signal/wait pattern for flow dependencies
  - CEL expression evaluation for dynamic sync keys
- **Scheduler integration**: Cron-based flow triggers fully integrated
  - Flows with `when` attribute automatically scheduled
  - Support for cron expressions, intervals, and shortcuts

### Added - Enterprise Connector Examples
- **Dynamic API Key Validation** (`examples/dynamic-api-key/`)
  - Validates API keys against database instead of static config
  - Supports user association, expiration, and metadata
  - Auth context available in flows via `auth.user_id` and `auth.claims`
- **gRPC Load Balancing** (`examples/grpc-loadbalancing/`)
  - Client-side load balancing with `round_robin` and `pick_first` policies
  - DNS-based service discovery support
  - Client-side health checking
- **Redis Cluster/Sentinel** (`examples/redis-cluster/`)
  - Redis Cluster mode for horizontal sharding
  - Redis Sentinel mode for automatic failover
  - Connection pooling and timeout configuration
- **Database Read Replicas** (`examples/read-replicas/`)
  - Automatic read/write routing for PostgreSQL and MySQL
  - Load balancing strategies: round_robin, random, least_conn
  - Replication lag handling with max_lag configuration
- **Validators README** (`examples/validators/README.md`)
  - Documentation for regex and CEL validators
  - Usage examples and best practices

### Added - Documentation Improvements
- **Getting Started Guide** (`docs/GETTING_STARTED.md`)
  - Step-by-step tutorial from zero to running service
  - Examples for service, connectors, flows, and types
  - Verification commands with expected outputs
  - Next steps with links to advanced features
- **Troubleshooting Guide** (`docs/TROUBLESHOOTING.md`)
  - Quick diagnosis commands
  - Startup issues (port in use, parse errors)
  - Database issues (connection, auth, missing db)
  - Flow issues (not triggered, transform errors)
  - Message queue issues
  - Performance issues
  - Docker/Kubernetes issues
- **Observability Guide** (`docs/OBSERVABILITY.md`)
  - Complete Prometheus metrics reference
  - Health check endpoints documentation
  - Kubernetes probe configuration
  - Grafana dashboard examples
  - Common PromQL queries
  - Alerting rules examples
- **Example Verification Sections**
  - Added "Verify It Works" sections to 13 examples
  - Each includes expected logs, curl commands, common issues
  - Examples: basic, enrich, tcp, graphql, cache, profiles,
    grpc, auth, notifications, mocks, files, s3, mongodb
- **CLI Help Messages**
  - Added Quick Start section to root command
  - Added comprehensive examples to all commands
  - Added environment variables documentation
  - Added common issues to check command

### Added - Helm Chart for Kubernetes
- **Helm Chart** (`helm/mycel/`)
  - Complete Kubernetes deployment configuration
  - Deployment with configurable replicas and rolling updates
  - Service (ClusterIP, NodePort, LoadBalancer)
  - Ingress with TLS support
  - ConfigMap for HCL configuration files
  - Secret for sensitive environment variables
  - ServiceAccount with configurable annotations
  - HorizontalPodAutoscaler for auto-scaling
  - PodDisruptionBudget for high availability
  - ServiceMonitor for Prometheus Operator
  - Health checks (liveness/readiness probes)
  - Security context (non-root, read-only filesystem)
  - Resource limits and requests
  - Comprehensive documentation and examples
- **GitHub Actions for Helm Release**
  - Automatic chart versioning from git tags
  - Push to GitHub Container Registry (GHCR)
  - Chart attached to GitHub releases
  - Install via: `helm install mycel oci://ghcr.io/matutetandil/charts/mycel`

### Added - Notification Connectors (Phase 6)
- **Webhooks** (`internal/connector/webhook/`)
  - Inbound webhook receiver with HTTP handler
  - Outbound webhook sender with retry and exponential backoff
  - Signature verification (HMAC-SHA256, HMAC-SHA1)
  - Support for Stripe and GitHub signature formats
  - Timestamp validation for replay protection
  - IP allowlist for inbound webhooks
- **Email** (`internal/connector/email/`)
  - SMTP connector with connection pooling and STARTTLS/TLS
  - SendGrid API connector with template support
  - AWS SES connector with v2 SDK
  - Support for attachments, CC/BCC, reply-to
  - HTML and plain text content
- **Slack** (`internal/connector/slack/`)
  - Webhook-based messaging
  - Bot API support with OAuth tokens
  - Rich message formatting with blocks
  - Attachments and interactive elements
- **Discord** (`internal/connector/discord/`)
  - Webhook-based messaging
  - Bot API support
  - Embeds with fields, images, thumbnails
  - Interactive components
- **SMS** (`internal/connector/sms/`)
  - Twilio connector with full API support
  - AWS SNS connector for SMS delivery
- **Push Notifications** (`internal/connector/push/`)
  - Firebase Cloud Messaging (FCM) with legacy API
  - Apple Push Notification service (APNs) with HTTP/2

### Added - SSO and Social Login (Phase 5.1d)
- **OAuth2/OIDC Base** (`internal/auth/sso_oauth.go`)
  - OAuth2Service for authorization code flow
  - OIDCService with discovery document support
  - State generation and token exchange
  - User info fetching and token refresh
  - ID token parsing for OIDC claims
- **Social Providers** (`internal/auth/sso_providers.go`)
  - `GoogleProvider` with offline access and refresh tokens
  - `GitHubProvider` with email fetching from emails API
  - `AppleProvider` with Sign in with Apple (ES256 client secret)
  - `OIDCProvider` for enterprise SSO (Okta, Azure AD, Auth0)
  - Configurable scopes and claim mappings per provider
- **Account Linking** (`internal/auth/sso_linking.go`)
  - `LinkedAccountStore` interface with memory implementation
  - `AccountLinkingService` for user/account association
  - Match strategies: email, none
  - On-match actions: link, prompt, reject
  - Prevention of unlinking only authentication method
  - Duplicate provider account prevention
- **SSO Orchestration** (`internal/auth/sso.go`)
  - `SSOService` coordinating all SSO flows
  - Provider initialization and OIDC discovery
  - State management with automatic expiration
  - Background cleanup for expired states
  - Unified callback handling with account linking
- **Tests** (`internal/auth/sso_test.go`)
  - OAuth2 flow tests with mock HTTP servers
  - OIDC discovery and ID token parsing
  - Account linking scenarios (new user, existing user, reuse)
  - Provider auth URL generation tests
  - State management and cleanup tests

### Added - Multi-Factor Authentication (Phase 5.1c)
- **MFA Service** (`internal/auth/mfa.go`)
  - Complete MFA orchestration service
  - Support for multiple MFA methods (TOTP, WebAuthn, Recovery)
  - `MFAStatus` for user MFA state
  - `MFASetup` for setup ceremony data
  - `MFAUserData` for persistent storage
  - `MFAStore` interface with memory implementation
- **TOTP Implementation** (`internal/auth/mfa_totp.go`)
  - RFC 6238 compliant TOTP generation
  - Support for SHA1, SHA256, SHA512 algorithms
  - Configurable digits (6/8) and period (30s default)
  - Clock skew tolerance
  - QR code generation for authenticator apps
  - Provisioning URI (otpauth://) generation
- **Recovery Codes**
  - Configurable count and length
  - Secure hashing with Argon2id
  - One-time use with automatic consumption
  - Regeneration support
- **WebAuthn/Passkeys** (`internal/auth/mfa_webauthn.go`)
  - Registration and login ceremonies
  - Support for platform and cross-platform authenticators
  - Attestation preferences (none, indirect, direct)
  - User verification options
  - Multiple credentials per user
- **Manager Integration** (`internal/auth/manager.go`)
  - `WithMFAStore` option for MFA store injection
  - MFA verification in Login flow (TOTP and recovery codes)
  - MFA management methods:
    - `GetMFAStatus`: Check user's MFA status
    - `BeginTOTPSetup`/`ConfirmTOTPSetup`: TOTP enrollment flow
    - `DisableMFA`: Disable all MFA methods (requires password)
    - `RegenerateRecoveryCodes`: Generate new recovery codes
  - WebAuthn methods:
    - `BeginWebAuthnRegistration`/`FinishWebAuthnRegistration`
    - `GetWebAuthnCredentials`/`RemoveWebAuthnCredential`
- **UserStore Interface Extensions**
  - `UpdateMFAEnabled` method for all implementations
  - Memory, PostgreSQL, and MySQL support
- **Dependencies**
  - `github.com/boombuler/barcode` - QR code generation
  - `github.com/go-webauthn/webauthn` - WebAuthn protocol
- **Tests** (`internal/auth/mfa_test.go`)
  - Full TOTP setup and validation flow
  - Recovery code lifecycle
  - MFA store operations

### Added - MySQL Storage Support
- **MySQL Storage** (`internal/auth/storage_mysql.go`)
  - `MySQLUserStore` for user CRUD operations
  - Configurable table and column names via HCL
  - `MySQLPasswordHistoryStore` for password history
  - `MySQLAuditStore` for audit logging
  - `MySQLSessionStore` for session management
  - `MySQLTokenStore` for token blacklist
  - MySQL-specific syntax (? placeholders, ON DUPLICATE KEY)
  - Tests in `storage_mysql_test.go`

### Added - Authentication System Security Features (Phase 5.1b)
- **Redis Storage** (`internal/auth/storage_redis.go`)
  - `RedisSessionStore` for session storage with TTL
  - `RedisTokenStore` for refresh tokens and blacklist
  - `RedisBruteForceStore` with progressive delay support
  - `RedisReplayProtectionStore` for one-time token usage
  - All stores implement the base interfaces
- **PostgreSQL Storage** (`internal/auth/storage_postgres.go`)
  - `PostgresUserStore` for user CRUD operations
  - Configurable table and column names via HCL
  - `PostgresPasswordHistoryStore` for password history
  - `PostgresAuditStore` for audit logging
  - Event filtering support
- **Brute Force Service** (`internal/auth/bruteforce.go`)
  - `BruteForceService` for coordinated protection
  - `CheckAccess()` returns lockout status and progressive delay
  - `RecordFailedAttempt()` with automatic lockout
  - `RecordSuccess()` clears attempts
  - `GetStats()` for monitoring
  - Progressive delay: exponential backoff with max cap
- **Session Cleanup Service** (`internal/auth/cleanup.go`)
  - `CleanupService` with configurable interval
  - Automatic cleanup of expired sessions
  - Idle session timeout support
  - Token blacklist cleanup
  - Graceful start/stop with context support
  - `MemorySessionStoreWithIdle` for idle timeout
- **Per-Endpoint Rate Limiting** (`internal/auth/ratelimit.go`)
  - `RateLimiter` for global rate limiting
  - `PerKeyRateLimiter` for per-IP/user rate limiting
  - `RateLimitConfig` with per-endpoint configuration
  - Default stricter limits for sensitive endpoints (login: 5/min, register: 10/min)
  - `RateLimitMiddleware` for HTTP handler integration
  - Key extraction by IP, user, or combined
- **Extended BruteForceStore Interface**
  - Added `GetAttempts()` alias for compatibility
  - Added `GetDelay()` for progressive delay retrieval
  - Added `SetDelay()` for progressive delay storage
- **Tests**
  - `bruteforce_test.go` - Brute force protection tests
  - `cleanup_test.go` - Cleanup service lifecycle tests
  - `ratelimit_test.go` - Rate limiting tests

### Added - Authentication System Core (Phase 5.1a)
- **Auth Package** (`internal/auth/`)
  - Complete enterprise-grade authentication system
  - Declarative configuration via HCL `auth {}` block
- **Types and Config** (`internal/auth/types.go`)
  - Full configuration structs for all auth features
  - User, Session, TokenPair, Claims types
  - LoginRequest, RegisterRequest, RefreshRequest
  - Common auth errors (ErrInvalidCredentials, ErrUserNotFound, etc.)
- **Presets** (`internal/auth/presets.go`)
  - `strict`: Maximum security (MFA required, 15m tokens, strong passwords)
  - `standard`: Balanced (MFA optional, 1h tokens, moderate passwords)
  - `relaxed`: Minimal (no MFA, 24h tokens, basic passwords)
  - `development`: For dev (no security, 7d tokens, no requirements)
  - `MergeWithPreset()` for combining user config with defaults
  - `ParseDuration()` supporting day suffix (e.g., "7d", "90d")
- **Password Hashing** (`internal/auth/password.go`)
  - Argon2id hashing (memory-hard, GPU-resistant)
  - PHC string format for hash storage
  - `PasswordHasher` with configurable parameters
  - `PasswordValidator` for policy enforcement
  - Complexity requirements (upper, lower, number, special)
  - Strength scoring (0-100)
  - `GenerateRandomPassword()` utility
- **JWT Tokens** (`internal/auth/jwt.go`)
  - Support for HS256, RS256, ES256 and variants
  - Access and refresh token generation
  - Token validation with issuer/audience verification
  - Custom claims support
  - `TokenManager` for all JWT operations
- **Storage Interfaces** (`internal/auth/storage.go`)
  - `UserStore` interface for user CRUD
  - `SessionStore` interface for session management
  - `TokenStore` interface for blacklist/replay protection
  - `BruteForceStore` interface for failed attempt tracking
  - In-memory implementations for all stores (development/testing)
- **Auth Manager** (`internal/auth/manager.go`)
  - Central coordination of all auth components
  - `Register()`, `Login()`, `Logout()`, `LogoutAll()`
  - `ValidateToken()`, `RefreshToken()`
  - `ChangePassword()`, `GetSessions()`, `RevokeSession()`
  - Brute force protection with configurable lockout
  - Session limits with oldest-first revocation
- **HTTP Handlers** (`internal/auth/handlers.go`)
  - REST endpoints for all auth operations
  - Automatic endpoint registration on HTTP mux
  - Configurable paths and methods
  - Proper error responses with codes
- **Middleware** (`internal/auth/middleware.go`)
  - `Middleware` for protecting routes
  - Path exclusion support
  - Role and permission-based authorization
  - `RequireAuth()`, `OptionalAuth()` helpers
  - `RequireRoles()`, `RequirePermissions()` helpers
  - Context extraction: `GetUser()`, `GetClaims()`
- **HCL Parser** (`internal/parser/auth.go`)
  - Full parsing of `auth {}` block
  - Support for all nested blocks (jwt, password, mfa, security, etc.)
  - WebAuthn configuration with biometrics/passkeys
  - Social login and SSO configuration
  - External provider configuration
- **Runtime Integration** (`internal/runtime/runtime.go`)
  - `authManager` and `authHandler` fields
  - Automatic initialization when auth config present
  - `AuthManager()` and `AuthHandler()` getters
- **Tests** (`internal/auth/auth_test.go`)
  - Password hashing and verification
  - Password validation and strength
  - Token generation and validation
  - Full auth flow (register, login, refresh, logout)
  - Memory store operations
  - Preset configuration
- **Example**: `examples/auth/`
  - Complete auth service configuration
  - Database schema for PostgreSQL
  - API documentation with curl examples

### Added - Plugin System (Phase 5e)
- **Plugin Types** (`internal/plugin/types.go`)
  - `PluginDeclaration` for plugin references in config
  - `PluginManifest` for plugin metadata
  - `ConnectorProvide` for connector definitions
  - `ConfigField` for connector configuration schema
  - `LoadedPlugin` for runtime plugin state
- **Plugin Loader** (`internal/plugin/loader.go`)
  - Load plugins from local directories
  - Parse `plugin.hcl` manifest files
  - Resolve plugin paths (local, git planned, registry planned)
- **WASM Connector** (`internal/plugin/connector.go`)
  - `WASMConnector` implementing `connector.Connector`, `Reader`, `Writer`
  - JSON-based communication with WASM modules
  - Support for `init()`, `read()`, `write()`, `call()`, `health()`, `close()`
- **Plugin Registry** (`internal/plugin/registry.go`)
  - Manage loaded plugins
  - Create connector instances from plugins
  - Track connector types provided by plugins
- **Plugin Factory** (`internal/plugin/factory.go`)
  - Factory for creating plugin connectors
  - Support for `type = "plugin"` or direct plugin type names
- **Parser support** for `plugin` blocks
  - `source` attribute for plugin location
  - `version` attribute for version constraints (git/registry)
- **Runtime integration**
  - Plugin registry initialization at startup
  - Plugin connector factory registration
  - Plugin functions integration with CEL
- **Example**: `examples/plugin/`
  - Example plugin structure and manifest
  - Documentation for building WASM connectors

### Added - WASM Functions (Phase 5d)
- **Custom Functions** (`internal/functions/`)
  - WASM functions that extend CEL transform expressions
  - `functions "name" { wasm = "...", exports = [...] }` blocks
  - Registry for managing function modules
  - Support for 0-5 function arguments
- **CEL Integration** (`internal/transform/wasm_functions.go`)
  - `CreateWASMFunctionOptions()` for CEL environment setup
  - `NewCELTransformerWithOptions()` for custom function support
  - Automatic JSON serialization for function calls
- **Parser support** for `functions` blocks
  - `wasm` attribute for .wasm file path
  - `exports` array for function names
- **Example**: `examples/wasm-functions/`
  - Pricing functions (calculate_price, apply_discount, tax_for_country)
  - Complete Rust example with checkout flow

### Added - WASM Runtime and Validators (Phase 5)
- **WASM Runtime** (`internal/wasm/`)
  - Pure Go runtime using wazero (no CGO)
  - Module loading from .wasm files
  - Memory management with alloc/free helpers
  - JSON-based function I/O
  - Hot reload support for WASM modules
- **WASM Validators** (`internal/validator/wasm.go`)
  - `WASMValidator` type for compiled validators
  - Shared runtime with module caching
  - CallValidate helper for validation functions
- **Example**: `examples/wasm-validator/`
  - Complete Rust example for building validators
  - Documentation for WASM interface specification

### Added - Custom Validators (Phase 5)
- **Custom Validators** (`internal/validator/`)
  - Regex validators for pattern matching (email, phone, UUID, etc.)
  - CEL validators for expression-based validation (age checks, enums, password strength)
  - WASM validators for complex custom logic
  - Validator registry for managing validators
  - Factory function for creating validators from config
- **Parser support** for `validator` blocks
  - `type = "regex"` with `pattern` attribute
  - `type = "cel"` with `expr` attribute
  - `type = "wasm"` with `wasm` and `entrypoint` attributes
  - Custom `message` for validation errors
- **Integration with type system**
  - `ValidatorRef` field in FieldSchema
  - `CustomValidatorConstraint` for using validators as constraints
- **Example**: `examples/validators/`
  - Regex validators: email, phone_ar, uuid, slug, username
  - CEL validators: adult_age, positive_number, valid_status, strong_password

### Fixed - Parser & Example Files
- **Parser support for MQ connectors** (`internal/parser/connector.go`)
  - Added `username` attribute (alias for `user`)
  - Added `vhost` attribute for RabbitMQ virtual host
  - Added `exchange` block for MQ exchange configuration
- **MQ example files** (`examples/mq/`)
  - Fixed CORS syntax: `allowed_origins` → `origins`, `allowed_methods` → `methods`
  - Fixed types.hcl syntax: changed from `field {}` blocks to simple `field = type` attributes
  - Removed invalid `operation` attribute from `to` blocks in flows
- **AsyncAPI CLI flags** (`cmd/mycel/main.go`)
  - Added `-o/--output` and `-f/--format` flags to AsyncAPI export command

### Added - Documentation Export (Phase 5)
- **Mock System** (`internal/mock/`)
  - JSON-based mock files for connector responses
  - Conditional responses with CEL expressions
  - CLI flags: `--mock=connector` and `--no-mock=connector`
  - `mocks {}` block in service configuration
  - Connector wrapping for seamless mock injection
  - Example: `examples/mocks/`
- **OpenAPI Export** (`internal/export/openapi/`)
  - Generate OpenAPI 3.0.3 specification from Mycel configuration
  - REST endpoints from flows with path parameters
  - Request/response schemas from types
  - Server information from connectors
  - CLI command: `mycel export openapi`
  - Flags: `-o/--output`, `-f/--format` (yaml/json), `--base-url`
- **AsyncAPI Export** (`internal/export/asyncapi/`)
  - Generate AsyncAPI 2.6.0 specification from Mycel configuration
  - Message channels from MQ flows (RabbitMQ, Kafka)
  - Subscribe/Publish operations
  - Message schemas from types
  - Server information with protocol bindings
  - CLI command: `mycel export asyncapi`
  - Flags: `-o/--output`, `-f/--format` (yaml/json)
- **HCL Syntax for Mocks**:
  ```hcl
  service {
    name = "my-service"

    mocks {
      enabled = true
      path    = "./mocks"
    }
  }
  ```
- **Mock File Format**:
  ```json
  {
    "responses": [
      {"when": "input.id == 1", "data": {"id": 1, "name": "John"}},
      {"default": true, "data": []}
    ]
  }
  ```

### Added - Synchronization Primitives (Phase 4.2)
- **Lock (Mutex)** - Distributed mutex for exclusive access by key
  - Memory and Redis implementations
  - `lock {}` block in flows with key, timeout, wait, retry options
  - Lua script for safe release (only owner can release)
- **Semaphore** - Limit concurrent access to resources
  - Memory and Redis implementations (sorted sets + Lua)
  - `semaphore {}` block with max_permits, lease, timeout
  - Automatic lease expiration for crash protection
- **Coordinate** - Signal/Wait pattern for dependency coordination
  - `wait {}` - Wait for a signal with conditional expression
  - `signal {}` - Emit signal when condition is met
  - `preflight {}` - Check database before waiting
  - `on_timeout` options: fail, retry, skip, pass
  - Redis Pub/Sub hub for efficient waiting
- **Flow Triggers** - Cron and interval scheduling
  - `when` attribute: "always", cron expressions, "@every X"
  - Shortcuts: @hourly, @daily, @weekly, @monthly
  - Uses robfig/cron/v3 library
- **MQ Headers Access**
  - `input.body`, `input.headers`, `input.properties` for RabbitMQ
  - `input.body`, `input.headers`, `input.key`, `input.topic` for Kafka
- **Prometheus Metrics** for sync primitives
  - Lock acquired/released/timeout counters
  - Semaphore acquired/released/available gauges
  - Coordinate signal/wait/timeout metrics
  - Scheduler execution counters
- **Parser Support** for lock, semaphore, coordinate, when blocks
- **Full specification**: [docs/PHASE-4.2-SYNC.md](docs/PHASE-4.2-SYNC.md)

### Added - Connector Profiles (Phase 4.3)
- **Connector Profiles** - Multiple backend implementations for the same logical connector
  - `type = "profiled"`: New connector type for profile-based routing
  - `select` attribute: CEL expression to determine active profile (e.g., `env('PRICE_SOURCE')`)
  - `default` attribute: Fallback profile when select evaluates to empty
  - `fallback` attribute: Ordered list of profiles to try on failure
- **ProfiledConnector** (`internal/connector/profile/`)
  - Wrapper implementing Connector interface
  - Routes operations to the active profile
  - Automatic fallback on retriable errors (connection timeout, 5xx)
  - Statistics tracking per profile (requests, errors, fallbacks)
- **Per-profile transforms** to normalize data from different backends
  - Each profile can have its own transform block
  - CEL expressions applied after reading from backend
  - Normalizes data before passing to flow (consistent interface)
- **Prometheus Metrics** for profile observability
  - `mycel_connector_profile_active` - Currently active profile (gauge)
  - `mycel_connector_profile_requests_total` - Requests per profile (counter)
  - `mycel_connector_profile_errors_total` - Errors per profile (counter)
  - `mycel_connector_profile_fallback_total` - Fallback events (counter)
  - `mycel_connector_profile_latency_seconds` - Latency per profile (histogram)
- **Parser Support** (`internal/parser/connector.go`)
  - `select`, `default`, `fallback` attributes
  - `profile "name" {}` blocks with label for name
  - `transform {}` blocks within profiles
- **Factory Integration** (`internal/connector/profile/factory.go`)
  - ProfileFactory creates ProfiledConnector instances
  - Uses Registry to create underlying connectors for each profile
- **Example** (`examples/profiles/`)
  - Pricing service with Magento, ERP, and Legacy backends
  - Profile selection via PRICE_SOURCE environment variable
- **Use cases**:
  - Same API, different data sources (Magento vs ERP vs Legacy)
  - Multi-region deployments
  - Read replicas vs primary database
  - Gradual migration between systems
- **HCL Syntax**:
  ```hcl
  connector "pricing" {
    type = "profiled"

    select   = "env('PRICE_SOURCE')"
    default  = "magento"
    fallback = ["erp", "legacy"]

    profile "magento" {
      type     = "http"
      driver   = "client"
      base_url = "http://magento/api"

      transform {
        product_id = "input.entity_id"
        price      = "double(input.price)"
        source     = "'magento'"
      }
    }

    profile "erp" {
      type     = "database"
      driver   = "postgres"
      host     = env("ERP_DB_HOST")

      transform {
        product_id = "string(input.id)"
        price      = "input.precio"
        source     = "'erp'"
      }
    }
  }
  ```
- **Full specification**: [docs/PHASE-4.3-PROFILES.md](docs/PHASE-4.3-PROFILES.md)

### Added - Runtime Configuration (Phase 4.1)
- **Environment Variables** for runtime configuration
  - `MYCEL_ENV`: Select environment (development, staging, production)
  - `MYCEL_LOG_LEVEL`: Set log level (debug, info, warn, error)
  - `MYCEL_LOG_FORMAT`: Set log format (text, json)
  - Flags override environment variables (priority: flag > env var > default)
- **Logging Package** (`internal/logging/`)
  - Centralized logging configuration
  - JSON logging support for production environments
  - Level filtering with standard slog integration
  - Comprehensive test coverage
- **CLI Improvements**
  - New `--log-level` flag: debug, info, warn, error
  - New `--log-format` flag: text, json
  - Deprecated `--verbose` flag (use `--log-level=debug` instead)
- **Docker Configuration Updates**
  - Standard config path: `/etc/mycel` (instead of `/config`)
  - Production defaults: `MYCEL_LOG_FORMAT=json`
  - Updated docker-compose.yml with documented env vars
- **Documentation Updates**
  - README updated with environment variables table
  - ROADMAP marked Phase 4.1 as complete

### Added - Cache Connector (Phase 3.3)
- **Cache Connector** (`internal/connector/cache/`)
  - In-memory and Redis caching for flow responses
  - Automatic cache lookup before flow execution (cache-aside pattern)
  - Cache storage after successful GET operations
  - Cache invalidation after write operations (POST/PUT/DELETE)
- **Memory Cache Driver** (`internal/connector/cache/memory/`)
  - LRU eviction policy with configurable max items
  - TTL-based expiration with background cleanup
  - Pattern-based key deletion with wildcard support (`*`)
  - Thread-safe operations with RWMutex
- **Redis Cache Driver** (`internal/connector/cache/redis/`)
  - Connection pooling with configurable settings
  - TTL support via Redis native expiration
  - Pattern deletion using SCAN (safe for large datasets)
  - Key prefix support for namespace isolation
- **Named Cache Definitions**
  - Reusable cache configurations (`cache "name" { ... }`)
  - Reference in flows with `cache { use = "name" }`
  - Shared TTL and prefix settings
- **Cache Invalidation**
  - `after { invalidate { ... } }` block for post-write invalidation
  - Specific key invalidation: `keys = ["products:${input.id}"]`
  - Pattern invalidation: `patterns = ["products:*", "lists:*"]`
  - Variable interpolation in keys and patterns
- **Cache Key Interpolation**
  - Path parameters: `${input.id}`
  - Query parameters: `${input.query.page}`
  - Request body: `${input.data.field}`
  - Result data: `${result.id}` (in invalidation)
- **Cache Example** (`examples/cache/`)
  - Memory cache with product and user caching
  - Inline and named cache configurations
  - Cache invalidation patterns
- **Dependencies**:
  - `github.com/hashicorp/golang-lru/v2` - LRU cache implementation
  - `github.com/redis/go-redis/v9` - Redis client
- **HCL Syntax**:
  ```hcl
  # Memory Cache Connector
  connector "cache" {
    type   = "cache"
    driver = "memory"
    max_items   = 10000
    eviction    = "lru"
    default_ttl = "5m"
  }

  # Redis Cache Connector
  connector "redis_cache" {
    type   = "cache"
    driver = "redis"
    url    = "redis://localhost:6379"
    prefix = "myapp"
    pool {
      max_connections = 10
      min_idle       = 2
    }
  }

  # Named Cache Definition
  cache "products" {
    storage = "cache"
    ttl     = "10m"
    prefix  = "products"
  }

  # Flow with Inline Cache
  flow "get_product" {
    from { connector = "api", operation = "GET /products/:id" }
    to   { connector = "db", target = "products" }
    cache {
      storage = "cache"
      ttl     = "5m"
      key     = "products:${input.id}"
    }
  }

  # Flow with Named Cache
  flow "get_user" {
    from { connector = "api", operation = "GET /users/:id" }
    to   { connector = "db", target = "users" }
    cache {
      use = "products"
      key = "user:${input.id}"
    }
  }

  # Flow with Cache Invalidation
  flow "update_product" {
    from { connector = "api", operation = "PUT /products/:id" }
    to   { connector = "db", target = "products" }
    after {
      invalidate {
        storage  = "cache"
        keys     = ["products:${input.id}"]
        patterns = ["lists:products:*"]
      }
    }
  }
  ```

### Added - MySQL and MongoDB Connectors (Phase 3.2)
- **MySQL Connector** (`internal/connector/database/mysql/`)
  - Full CRUD operations (SELECT, INSERT, UPDATE, DELETE)
  - Connection pooling configurable (max_open, max_idle, max_lifetime)
  - Named parameter support (`:param` syntax converted to `?` placeholders)
  - DSN auto-generation from HCL config
  - SSL/TLS support
  - **HCL Syntax**:
    ```hcl
    connector "mysql_db" {
      type     = "database"
      driver   = "mysql"
      host     = env("MYSQL_HOST")
      port     = 3306
      database = "myapp"
      user     = env("MYSQL_USER")
      password = env("MYSQL_PASSWORD")
      charset  = "utf8mb4"

      pool {
        max          = 100
        min          = 10
        max_lifetime = 300
      }
    }
    ```
- **MongoDB Connector** (`internal/connector/database/mongodb/`)
  - Full NoSQL CRUD operations
  - Operations: INSERT_ONE/MANY, UPDATE_ONE/MANY, DELETE_ONE/MANY, REPLACE_ONE
  - Automatic ObjectID handling (string ↔ ObjectID conversion)
  - BSON to Map conversion with timestamp handling
  - MongoDB operators support (`$set`, `$gte`, `$lt`, `$in`, etc.)
  - Connection pooling configurable
  - **HCL Syntax**:
    ```hcl
    connector "mongo_db" {
      type     = "database"
      driver   = "mongodb"
      uri      = env("MONGO_URI")
      database = "myapp"

      pool {
        max             = 200
        min             = 10
        connect_timeout = 30
      }
    }
    ```
- **NoSQL Query Support**
  - New `RawQuery` field in `connector.Query` for NoSQL filters
  - New `Update` field in `connector.Data` for MongoDB update operations
  - New `query_filter` and `update` attributes in HCL flows
  - Parser function `ctyValueToMap` for HCL → Go map conversion
  - **HCL Syntax for MongoDB queries**:
    ```hcl
    flow "get_active_users" {
      from { connector = "api", operation = "GET /users/active" }
      to {
        connector    = "mongo_db"
        target       = "users"
        query_filter = { status = "active", age = { "$gte" = 18 } }
      }
    }

    flow "update_user_status" {
      from { connector = "api", operation = "PUT /users/:id/status" }
      to {
        connector    = "mongo_db"
        target       = "users"
        query_filter = { "_id" = ":id" }
        update       = { "$set" = { status = "input.status" } }
      }
    }
    ```
- **Dependencies**:
  - `github.com/go-sql-driver/mysql` - MySQL driver
  - `go.mongodb.org/mongo-driver` - MongoDB driver

### Added - Integration Patterns Documentation
- **New guide:** `docs/integration-patterns.md` with complete, copy-paste ready examples for:
  - GraphQL API → Database (CRUD)
  - REST → GraphQL passthrough
  - GraphQL → REST passthrough
  - RabbitMQ → Database (message processing)
  - REST/GraphQL → RabbitMQ (async processing)
  - Raw SQL queries (JOINs, subqueries, aggregations)
- Quick reference for connector types and flow structure
- Common CEL functions reference

### Added - Raw SQL Query Support
- **Custom SQL queries** for complex database operations (JOINs, subqueries, multi-table operations)
  - Named parameter substitution with `:param` syntax
  - Automatic conversion to database-specific placeholders (`?` for SQLite, `$1, $2` for PostgreSQL)
  - Support for SELECT, INSERT, UPDATE, DELETE with raw SQL
  - Handles RETURNING clauses for INSERT/UPDATE operations
- **Updated connector interfaces** (`internal/connector/connector.go`)
  - Added `RawSQL` field to `Query` struct
  - Added `RawSQL` field to `Data` struct
- **SQLite connector** (`internal/connector/database/sqlite/connector.go`)
  - `parseNamedParams()` function for parameter substitution
  - String literal handling to avoid replacing `:param` inside strings
- **PostgreSQL connector** (`internal/connector/database/postgres/connector.go`)
  - Same features as SQLite but with PostgreSQL-style `$N` placeholders
- **REST connector improvements** (`internal/connector/rest/connector.go`)
  - Dynamic path parameter extraction for any route (not just `:id`)
  - New `extractParamNames()` function for parsing route definitions
- **HCL Syntax**:
  ```hcl
  # Using heredoc syntax for multi-line SQL
  flow "get_order_with_user" {
    from {
      connector = "api"
      operation = "GET /orders/:id"
    }
    to {
      connector = "sqlite"
      query = <<-SQL
        SELECT o.*, u.name as user_name, u.email as user_email
        FROM orders o
        JOIN users u ON u.id = o.user_id
        WHERE o.id = :id
      SQL
    }
  }

  # Using inline SQL with named parameters
  flow "get_orders_by_user" {
    from {
      connector = "api"
      operation = "GET /orders-by-user/:user_id"
    }
    to {
      connector = "sqlite"
      query = "SELECT * FROM orders WHERE user_id = :user_id AND status = :status"
    }
  }
  ```
- **Integration tests** (`internal/runtime/runtime_test.go`)
  - `TestIntegration_RawSQL` with 3 test cases:
    - JOIN query with path parameter
    - Multiple named parameters
    - Raw SQL INSERT

### Added - GraphQL Dual-Approach Schema Generation
- **Schema-first mode**: Define types in SDL file (`.graphql`), Mycel auto-connects flows
  - Full SDL parser with AST using `graphql-go/language/parser`
  - Automatic type conversion from SDL to graphql-go types
  - Smart resolver that auto-unwraps single-element arrays for non-list types
  - Support for custom scalars: DateTime, Date, Time, JSON
  - Input types, enums, and interfaces support
- **HCL-first mode**: Define types in HCL, Mycel generates GraphQL schema
  - TypeSchema to GraphQL converter (`hcl_to_graphql.go`)
  - New `returns` attribute in flows to specify return type
  - Automatic schema generation from HCL types
  - Type mapping: `id` → `ID`, `string` → `String`, `number` → `Int/Float`, `boolean` → `Boolean`
- **New files**:
  - `internal/connector/graphql/sdl_parser.go` - Complete SDL parser
  - `internal/connector/graphql/sdl_to_graphql.go` - SDL → graphql-go converter
  - `internal/connector/graphql/hcl_to_graphql.go` - HCL → GraphQL converter
  - `internal/connector/graphql/scalar_types.go` - Custom scalar types
- **Comprehensive integration tests** (`internal/runtime/runtime_test.go`)
  - Schema-first CRUD tests: 14 test cases
  - HCL-first CRUD tests: 13 test cases
  - Tests cover: Query, Mutation, UpdateUser, DeleteUser, Introspection, Playground
  - GraphQL Variables tests for both modes
  - Error handling tests (invalid queries, missing required fields, empty queries)
  - All tests use SQLite as backend
- **Column mapping (snake_case → camelCase)** for GraphQL responses
  - `snakeToCamel()` function in `resolver.go`
  - Automatic conversion: `external_id` → `externalId`, `created_at` → `createdAt`
  - Recursive conversion for nested objects
- **HCL Syntax for returns**:
  ```hcl
  flow "get_users" {
    from { connector = "gql", operation = "Query.users" }
    to   { connector = "db", target = "users" }
    returns = "User[]"  # Specifies GraphQL return type
  }

  flow "get_user" {
    from { connector = "gql", operation = "Query.user" }
    to   { connector = "db", target = "users" }
    returns = "User"  # Single object, auto-unwrap enabled
  }
  ```

### Added - GraphQL Connector (Phase 3)
- **GraphQL Server Connector** (`internal/connector/graphql/`)
  - Expose GraphQL API endpoints with playground UI
  - Dynamic schema building from registered handlers
  - SDL file loading support for schema-first approach
  - **Features**:
    - Query and Mutation support
    - GraphQL Playground UI at `/playground`
    - CORS configuration
    - JSON scalar type for flexible arguments
    - Health check endpoint at `/health`
  - **Operation format**: `Query.fieldName` or `Mutation.fieldName`
- **GraphQL Client Connector**
  - Call external GraphQL APIs
  - **Authentication types**:
    - Bearer token
    - API Key (custom header)
    - Basic auth
    - OAuth2 client credentials
  - Retry with exponential backoff
  - Timeout configuration
  - Custom headers support
  - Use as enrichment source via `Call()`
- **GraphQL Example** (`examples/graphql/`)
  - Server with CRUD operations
  - Schema file example
- **HCL Syntax**:
  ```hcl
  # GraphQL Server
  connector "graphql_api" {
    type   = "graphql"
    driver = "server"

    port       = 4000
    endpoint   = "/graphql"
    playground = true

    cors {
      origins = ["*"]
      methods = ["GET", "POST", "OPTIONS"]
    }
  }

  # GraphQL Client
  connector "external_api" {
    type     = "graphql"
    driver   = "client"
    endpoint = "https://api.example.com/graphql"

    auth {
      type  = "bearer"
      token = env("API_TOKEN")
    }

    timeout     = "30s"
    retry_count = 3
  }
  ```

### Added - Exec Connector (Phase 3.2)
- **Exec Connector** (`internal/connector/exec/`)
  - Execute external commands locally or on remote servers
  - **Local driver**: Shell command execution on the local machine
    - Direct command execution with arguments
    - Shell wrapper support (`bash -c`, etc.) for pipes and shell features
    - Environment variables injection
    - Working directory configuration
    - Timeout handling with context cancellation
  - **SSH driver**: Remote command execution via SSH
    - Key-based authentication (recommended)
    - Password authentication (supported but not recommended)
    - Custom SSH port configuration
    - Known hosts verification
  - **Input formats**:
    - `args`: Pass input as command-line arguments (`--key=value`)
    - `stdin` / `json`: Send JSON-encoded input via stdin
  - **Output formats**:
    - `text`: Raw output as single string `{"output": "..."}`
    - `json`: Parse output as JSON object/array
    - `lines`: Split output by newlines with line numbers
  - **Use cases**:
    - Execute local scripts and CLI tools
    - Remote server monitoring and management
    - Data enrichment via external APIs (curl, etc.)
    - Process data through external programs (jq, awk, etc.)
    - Integration with existing shell scripts
- **Exec Example** (`examples/exec/`)
  - Local command execution examples
  - Shell command with pipes
  - JSON output parsing
  - Data enrichment using exec connector
- **HCL Syntax**:
  ```hcl
  # Local execution
  connector "my_script" {
    type   = "exec"
    driver = "local"

    command       = "echo"
    args          = ["hello", "world"]
    timeout       = "10s"
    output_format = "text"
  }

  # SSH remote execution
  connector "remote_server" {
    type   = "exec"
    driver = "ssh"

    command = "uptime"
    ssh {
      host     = "server.example.com"
      user     = "admin"
      key_file = "/path/to/key"
    }
  }
  ```

### Added - Enrich System (Data Enrichment)
- **Enrich blocks** for fetching data from external services during transformation
  - Flow-level enrich: Specific to a single flow
  - Transform-level enrich: Reusable across multiple flows (inside named transforms)
  - Multiple enrichments per flow/transform
- **`enriched.*` namespace** available in CEL expressions
  - Access enriched data: `enriched.pricing.price`, `enriched.inventory.stock`
  - Combine with input: `input.quantity * enriched.pricing.unit_price`
- **CEL transformer enhancements** (`internal/transform/cel.go`)
  - `EvaluateExpression()`: Evaluate single expressions with input and enriched data
  - `TransformWithEnriched()`: Full transformation with enriched context
- **Connector support for enrichment**
  - Database connectors: Uses `Read()` for data lookup
  - TCP/HTTP connectors: Uses `Call()` interface for RPC-style calls
- **Enrich Example** (`examples/enrich/`)
  - Flow-level enrichment with pricing service
  - Multiple enrichments (pricing + inventory)
  - Reusable transforms with built-in enrichment
- **HCL Syntax**:
  ```hcl
  # Flow-level enrich
  flow "get_product" {
    enrich "pricing" {
      connector = "pricing_service"
      operation = "getPrice"
      params { product_id = "input.id" }
    }
    transform {
      price = "enriched.pricing.price"
    }
  }

  # Transform-level enrich (reusable)
  transform "with_pricing" {
    enrich "pricing" { ... }
    price = "enriched.pricing.price"
  }
  ```

### Added (Phase 3.1)
- **Message Queue Connector** (`internal/connector/mq/`)
  - **RabbitMQ Support**: Full producer and consumer implementation
    - Connection management with automatic reconnection
    - Queue and exchange declaration with binding support
    - Topic pattern matching (`*` matches one word, `#` matches zero or more)
    - Manual acknowledgment for reliable message processing
    - Concurrent consumers with configurable prefetch (QoS)
    - Publisher confirms for guaranteed delivery
  - **Kafka Support** (`internal/connector/mq/kafka/`): Full producer and consumer implementation
    - Consumer groups with auto-commit or manual offset management
    - Multiple topic subscription
    - SASL authentication (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)
    - TLS support
    - Compression (gzip, snappy, lz4, zstd)
    - Configurable acks (none, one, all) for delivery guarantees
    - Batch publishing with configurable batch size and linger time
    - Concurrent consumers
  - **Message types** (`internal/connector/mq/types/`)
    - Generic Message struct with headers, routing key, exchange
    - DeliveryMode (transient/persistent)
    - AckMode (auto/manual/none)
  - **Exchange types** (RabbitMQ): direct, fanout, topic, headers
  - **Consumer features**:
    - Routing key pattern matching for topic exchanges (RabbitMQ)
    - Consumer groups (Kafka)
    - Prefetch/QoS configuration
    - Concurrent worker goroutines
    - Graceful shutdown with message draining
  - **Publisher/Producer features**:
    - Exchange and routing key configuration (RabbitMQ)
    - Topic and partition key configuration (Kafka)
    - Persistent message delivery
    - Publisher confirms support (RabbitMQ)
    - Batch publishing
- **MQ Example** (`examples/mq/`)
  - RabbitMQ consumer and publisher configuration
  - Order processing with pub/sub pattern
  - Topic routing examples

### Added (Phase 2.5)
- **TCP Connector** (`internal/connector/tcp/`)
  - **TCP Server**: Listen for incoming TCP connections
    - Length-prefixed message framing (4-byte big-endian header)
    - Message routing by `type` field in JSON
    - Configurable max connections, read/write timeouts
    - TLS support (optional)
    - Graceful shutdown with connection draining
  - **TCP Client**: Connect to remote TCP servers
    - Connection pooling with configurable size
    - Automatic retry with configurable count and delay
    - Request-Response and Fire-and-forget patterns
    - TLS support with custom CA certificates
  - **Protocol codecs**: JSON, msgpack, raw, **nestjs**
  - **Wire protocols**:
    - Mycel: `[4-byte length][payload]`
    - NestJS: `{length}#{json}` (compatible with @nestjs/microservices TCP transport)
  - **NestJS Protocol Support** (`internal/connector/tcp/nestjs.go`)
    - Full compatibility with NestJS TCP microservices
    - Wire format: `{length}#{json}` where json is `{"pattern":"...", "data":{...}, "id":"..."}`
    - Handles NestJS patterns (string or `{cmd: "..."}` objects)
    - Automatic conversion between Mycel and NestJS message formats
    - Support for NestJS response format with `response`, `err`, and `isDisposed` fields
- **TCP Example** (`examples/tcp/`)
  - Complete example with TCP server + SQLite
  - Python and netcat testing scripts

### Added (Phase 2)
- **HTTP Client connector** (`internal/connector/http/`)
  - Call external REST APIs from flows
  - Authentication support: Bearer, OAuth2 (with refresh tokens), API Key, Basic
  - Configurable timeout and retry settings
  - Custom headers support
- **PostgreSQL connector** (`internal/connector/database/postgres/`)
  - Full CRUD operations with parameterized queries
  - Connection pooling configuration
  - SSL mode support
- **Transform system powered by CEL** (`internal/transform/`)
  - Google's Common Expression Language (CEL) for powerful, safe transformations
  - Full expression support: operators (`+`, `-`, `*`, `/`, `%`, `==`, `!=`, `<`, `>`, `&&`, `||`)
  - Ternary expressions: `age >= 18 ? "adult" : "minor"`
  - List operations: `filter()`, `map()`, `exists()`, `all()`, `size()`, `in`
  - Custom Mycel functions: `uuid()`, `now()`, `now_unix()`, `lower()`, `upper()`, `trim()`, `replace()`, `substring()`, `len()`, `default()`, `coalesce()`, `split()`, `join()`, `hash_sha256()`, `format_date()`
  - **CEL Standard Extensions enabled:**
    - `ext.Strings()`: charAt, indexOf, lastIndexOf, join, quote, replace, split, substring, trim, upperAscii, lowerAscii, reverse
    - `ext.Encoders()`: base64.encode, base64.decode
    - `ext.Math()`: math.abs, math.ceil, math.floor, math.round, math.sign, math.greatest, math.least, math.isNaN, math.isInf
    - `ext.Lists()`: lists.range, slice, flatten
    - `ext.Sets()`: sets.contains, sets.equivalent, sets.intersects
  - Expression validation at startup (early error detection)
  - Program caching for optimal runtime performance
  - Named/reusable transforms in separate HCL files
  - Inline transforms in flow definitions
- **Transformations documentation** (`docs/transformations.md`)
  - Complete CEL reference guide with examples
  - All available functions documented
  - Real-world transformation examples
- **Type validation on flows**
  - Input and output validation with type schemas
  - Built-in constraints: min, max, min_length, max_length, format, pattern, enum
  - Format validators: email, url, uuid, date, datetime
- **Environment support** - Enhanced HCL functions:
  - `env("VAR_NAME", "default")` - Environment variable with optional default
  - `file("./path/to/secret")` - Read file contents
  - `base64encode()` / `base64decode()` - Base64 encoding/decoding
  - `abspath()` - Convert relative paths to absolute
  - `coalesce()` - Return first non-empty value

### Added (Phase 1.5)
- **ASCII art banner** with colored terminal output
  - New `internal/banner/` package for styled console output
  - ANSI color support with automatic detection (respects NO_COLOR env var)
  - Color-coded HTTP methods (GET=green, POST=yellow, DELETE=magenta)
  - Clean startup display with service info, connectors, and flows

### Fixed
- **GET with path parameters** now correctly filters results
  - Operations like `GET /users/:id` automatically extract path params as query filters
  - `extractPathParams()` helper function added to flow registry

### Added (Phase 1)
- **`mycel start` command is now functional!**
  - Full runtime orchestration: parse config → init connectors → register flows → start HTTP server
  - Graceful shutdown with SIGINT/SIGTERM handling
- **REST connector** (`internal/connector/rest/`)
  - HTTP server with configurable port and CORS
  - Automatic route registration from flow configurations
  - JSON request/response handling
- **SQLite connector** (`internal/connector/database/sqlite/`)
  - Full CRUD operations (SELECT, INSERT, UPDATE, DELETE)
  - Pure Go driver (no CGO required) via `modernc.org/sqlite`
  - Connection pooling and health checks
- **Runtime engine** (`internal/runtime/`)
  - Configuration-driven service orchestration
  - Flow registry with automatic handler building
  - Connector lifecycle management
- Working example in `examples/basic/` with SQLite database
- `mycel validate` command to check configuration validity
- `mycel check` command to verify connector configuration

### Changed
- **BREAKING:** Updated flow block syntax for HCL compatibility
  - `from` block now uses `connector` and `operation` attributes
  - `to` block now uses `connector`, `target`, and optional `filter` attributes
  - Old syntax: `from { connector.api = "GET /users" }`
  - New syntax: `from { connector = "api", operation = "GET /users" }`

### Fixed
- Fixed `TestParseFlow` and `TestParseDirectory` parser tests
- Updated example files to use valid HCL syntax
- Fixed connector driver parsing in HCL parser

### Added (Initial)
- Initial project setup
- Project specification and design documents (CLAUDE.md)
- CLI scaffolding with cobra (start, validate, check commands)
- HCL parser for connectors, flows, types, and service blocks
- Connector interfaces (Reader, Writer, ReadWriter, Registry, Factory)
- Flow executor with pipeline pattern and stages
- Validation system with TypeValidator and built-in constraints
- Transform system with FunctionRegistry
- Custom HCL functions: `env()`, `coalesce()`

---

## Version History

_No releases yet. Development starting from Fase 1 - Core._
