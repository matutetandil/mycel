# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **A federation v2 subgraph schema file did not load.** Every such file opens by declaring which version of the specification it speaks — `extend schema @link(url: "https://specs.apollo.dev/federation/v2.0", import: [...])` — and the AST parser behind the schema reader only understands `extend type`. Pointing a `graphql` connector at a real subgraph schema failed at the second line with a syntax error on text copied from the specification, and the service did not start. The same preamble is what Mycel generates for its own `_service` reply, so it was emitting SDL it could not read back. Two more things in the same file were refused or lost: `repeatable`, the keyword federation's own `@key` and `@tag` are declared with, and a schema block naming roots of its own (`schema { query: RootQuery }`), which left the root with the right name and no fields while the fields sat under the type's own name — a schema file that looks complete and exposes nothing.
- **`min_val`, `max_val` and `sort_by` ignored numbers typed differently.** JSON has one number type and CEL has three, so a single field arrives as an integer from one record and a double from the next — a price of 30 beside a price of 2.5 — and the comparison behind all three functions required both sides to be the same type, so every such pair compared equal. Nothing failed: `min_val([10, 2.5, 30, 1.5])` returned 10, and `sort_by` over totals of 30, 2.5 and 10 returned them in the order they were given. Numbers now compare as numbers whichever way each side is typed, while two integers are still compared as integers so identifiers beyond the range a float holds exactly are not flattened together.
### Added

- **A Discord card was dropped and the message went out empty.** The payload was read in the send path and took only `content`, so an alert built as an embed — the title, the colour, the fields — arrived as a message with nothing in it, which Discord refuses. `allowed_mentions` was dropped with it: an alert quoting text somebody else wrote can contain `@everyone`, and that field is what stops it pinging a whole server. The name and avatar a message appears under, the thread it starts and text-to-speech are read now too.

- **Most of what a flow writes on an email was dropped.** The connector read a handful of fields from the payload and ignored the rest: `attachments` — so a flow that generated a PDF and attached it sent the invoice email without the invoice — along with `cc`, `bcc`, `reply_to`, the sender's display name, custom headers, tags and the tracking flags. A copy that silently does not go is found a month later by whoever was waiting for it. `text` and `text_body` were both accepted while only `html_body` was, so writing the obvious pair sent a message with no HTML in it. Addresses are now accepted however a flow writes them — one, a list, or records carrying a name.
- **A push notification's data payload was dropped.** It was read with a `map[string]string` type assertion, which a flow's payload never satisfies, so the notification arrived saying the right thing and carrying nothing for the app to act on — tapping it landed nowhere. The device list, priority, lifetime and collapse key were not read either, so a notification could only reach one device and a new notice stacked instead of replacing the last.

- **Passkeys can be registered and used.** Writing a `webauthn` block asks for them, and everything behind it existed — the service that runs both ceremonies, the manager's registration methods, the store — with nothing a browser could call. The sign-in half was missing entirely: the service could begin and finish a login and the manager exposed neither, so a passkey could be registered and never used. Signing in goes through the same session path a password sign-in uses and is audited with the method; the authenticator's signature counter is now stored, which is what lets a cloned key be noticed. Sign-in options are asked for by address and an address with no passkey answers exactly as one with no account, so the endpoint cannot be used to ask which addresses are registered. Registration and removal act on the account in the token, removal asks for the password, the list carries no key material, and an account with no passkeys lists none rather than failing.

- **The endpoints a second factor is set up through.** The `endpoints` block declares `mfa_setup`, `mfa_verify`, `mfa_disable` and `mfa_recovery` with defaults, and none of the four was mounted — the manager could already enrol, confirm, disable and check a code, so a service with MFA on had everything except a way for a browser to reach it, and `mfa_setup` named a path that answered 404. Enrolment remains a two-step ceremony: the secret is handed over, and the account is protected only once a code proves the app holds it. Each acts on the account in the token rather than a user id from the body, turning it off asks for the password, and recovery codes come back from the confirming call because they are shown once. Signing in with a recovery code goes through the ordinary sign-in, so the brute-force counters and the audit record stay in one place.

- **Mocking says when a connector named for it has none.** A connector with mocks answers from them and never reaches the real one; without any, its calls fall through — right when mocking is on for a whole service and only some connectors have mocks, and a mistake when that connector was asked for by name. A directory called `database` for a connector called `db` left every call going to the real database while the service reported mocking as on. Naming one and finding no mocks is now a warning that says where the directory was expected.

### Fixed

- **A webhook allow-list could be bypassed with a header.** The list of addresses permitted to deliver an inbound webhook was decided on `X-Forwarded-For`, which is written by whoever sends the request — so `curl -H "X-Forwarded-For: 203.0.113.9"` got anyone past a list of the provider's addresses, and the refusal that never happened is invisible. A forwarding header is now believed only from a peer named in the new `trusted_proxies`; otherwise the decision is made on the peer address, which the caller cannot write. Behind a named proxy the address is taken from the nearest hop outwards, skipping the proxies we know, so a caller cannot prepend its own.

- **JWKS token validation refused every token.** `parseJWK` returned an anonymous struct holding the modulus and exponent rather than a key any signature library can use, so a REST connector pointed at Auth0, Cognito or Keycloak rejected every authenticated request with "key is of invalid type". Both the RSA and EC paths now build real keys, and the curve is read from the key rather than assumed, so one naming an unknown curve is refused by name instead of reported as a bad signature. The key set is also no longer cached for ever: an identifier that is not in what we hold prompts one refresh, so a provider's key rotation no longer refuses every request until the service is restarted — while a token naming a key nobody publishes does not turn each request into a fetch.

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

### Fixed

- **38 connector attributes were described everywhere and impossible to write.** The parity test added with the schema unification found them across 12 connectors, and every one turned out to be read by its connector: the CSV reader's delimiter and comment character, the PDF page size, font and margins, the queue heartbeat and reconnect delay, TCP's idle timeout, webhook's HTTPS requirement and IP allow-list, cache pool sizing, CORS credentials, the OIDC provider name, SMTP's TLS mode. They were settings that were implemented, declared by the schema, offered as completions and rejected by the parser, whose hand-written allow-list had fallen behind. Four needed more than an allow-list entry: exec's `env` block and the SQL read `replicas` were parsed and then discarded, so neither reached the connector — replicas are now written one block each and collected into the list the factories read; the webhook connector had grown a second vocabulary for retry, `max_attempts` and `initial_delay`, for settings the shared block already had, now folded onto `attempts` and `delay` with both spellings accepted and writing both an error; its `multiplier` was read through a `float64` type assertion, so the obvious `multiplier = 2.0` arrived as an int and was dropped; and exec read `workdir` while the documentation and examples use `working_dir`, so the former never parsed, the latter was never read, and the working directory silently did not apply.
- **A command's `env` block wiped its environment.** Making the block reachable exposed the choice behind it: the declared variables replaced the environment rather than being added to it, so a command given one variable lost `PATH`, `HOME` and everything else. They are added now, with a declared variable still overriding an inherited one of the same name.

- **The parser accepted 19 connector attributes nothing described, and twelve that nothing read.** Running the parity check the other way found them. Four were real settings invisible to tooling — the gRPC client's `target`, `insecure` and `wait_for_ready`, plus exec's `args` and TCP's `retry_delay` — and connector profiles were undescribed entirely, so `select`, `default`, `fallback` and the `profile` block, which is how one connector resolves to different backends, appeared in no completion. Of the twelve dead words, eight were MongoDB connection settings: `auth_source`, `auth_db`, `replica_set`, `srv`, `read_concern` and `direct` are now applied as the URI options they are, to a URI built from parts, while `max_pool` and `min_pool` were removed because the `pool` block already carries them. `address`, `origins` and `wsdl` were removed too, each having a working spelling elsewhere or no meaning at all.

### Added

- **`introspection` on a GraphQL server does something.** It was accepted by the parser and read by nothing, so asking for introspection to be off left it on — and a schema is a map of everything the service can do. It now defaults off in production and on elsewhere, following the same reasoning as the playground, and can be set either way. String literals are stripped before the check, so a query searching for the text `__schema` is not mistaken for an introspection query.
- **Each connector schema is defined once.** There were two hand-written copies of all 33 of them — the ones under `internal/connector` that the runtime registers, and a second set in `pkg/connectors` that external tooling links against — and nothing compared them. They had drifted for three releases: 5 of 26 connector types differed across 14 attributes, every one present in the runtime and absent from the copy the outside world reads, including `create_if_missing` from 2.0.0 and Slack's `batch` block from 2.5.0. The definitions now live in `pkg/connectors`, one file per connector, and the runtime registers from there — the direction chosen by measurement, since that package depends on nothing beyond `pkg/schema` while delegating the other way would have pulled the 123 modules the runtime needs into any program that just wants completions.
- **Connector schemas are checked against the parser.** Nothing ever did, which is how the gRPC connector came to advertise TLS attributes the parser refused. Each attribute a schema declares is now written into a connector block and parsed. The test carries an explicit list of the 38 attributes that do not parse today — a separate problem, in the parser's hand-written allow-list — so that drift cannot grow, and it fails both when a new one appears and when a listed one starts parsing.
- **Named operations are in the schema.** The connector schema declared no `operation` block at all, so a feature that was parsed, documented and exampled was invisible to completions, `mycel add` and anything else built on the schema. Both directions are now tested: every attribute the schema offers is rendered and parsed, and every attribute the parser accepts is checked for in the schema.

### Security

- **A gRPC server whose TLS could not be built started in plaintext.** `buildServerOptions` discarded the result of `credentials.NewServerTLSFromFile` with an `if err == nil`, so when the certificate failed to load the listener came up unencrypted while the configuration said otherwise. It failed routinely rather than rarely: the parser accepted none of the attribute names that connector reads, so the certificate paths were always empty. The error is now returned and the server refuses to start, and TLS enabled with no certificate at all is reported as such.

### Fixed

- **The `tls` block had three vocabularies and the parser accepted one.** `http` read `ca_cert`, `client_cert`, `client_key` and `insecure_skip_verify`; `grpc` read `enabled`, `cert_file`, `key_file`, `ca_file`, `server_name` and `skip_verify`; `tcp`, `mq` and `mqtt` read `cert`, `key`, `ca_cert` and `insecure_skip_verify`. Only http's four were accepted, which meant `cert` and `key` were rejected everywhere — so mutual TLS could not be configured on `tcp`, `mq`, `mqtt` or `grpc` at all — and `enabled` was rejected while `mq` and `mqtt` build TLS only when it is true, so those two could not be given TLS by any spelling. The canonical names are the ones three of the five connectors already read, plus `server_name` where it applies; every older spelling is still accepted and folded onto its canonical name, so no working configuration breaks. Writing two spellings of one setting is now an error rather than a silent discard, and writing the block enables TLS, following the rule the `mfa` block already set.
- **Connector schemas were never checked against the parser.** The schema parity test covers root blocks, which is how the gRPC connector came to advertise six TLS attributes the parser refused: completions offered names that could not be written. Both schema registries — the runtime's own and the copy in `pkg/connectors` that external tooling links against — are now checked for the `tls` block.

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
