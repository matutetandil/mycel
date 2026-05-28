# Contributing to Mycel

Thanks for your interest in contributing! Mycel is a declarative microservice
framework — *configuration, not code*. The runtime interprets HCL2 config; users
describe **what** they want and Mycel handles the **how**. Keeping that promise
is the north star for every change.

By participating you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to contribute

- **Report bugs** and **request features** via [issues](../../issues) (use the templates).
- **Improve docs** — the `docs/` tree, `README.md`, and example READMEs.
- **Add or fix examples** under `examples/` (every example must validate).
- **Fix bugs / add features** with tests.
- **Report security issues privately** — see [SECURITY.md](SECURITY.md), *not* a public issue.

## Development setup

Mycel is **pure Go (no CGO)**. You need Go (see `go.mod` for the version).

```bash
git clone https://github.com/matutetandil/mycel
cd mycel
go build ./...        # build everything
go test ./...         # run all unit tests
```

Common commands while developing:

```bash
go build -o bin/mycel ./cmd/mycel
./bin/mycel validate --config ./examples/basic   # validate a config
./bin/mycel start    --config ./examples/basic   # run a service
go vet ./...                                      # static checks
```

Integration tests that need external services (Postgres, RabbitMQ, Kafka, …)
live under `tests/integration/` and run via Docker Compose:

```bash
tests/integration/run.sh
```

## Project conventions

These are enforced in review — following them gets your PR merged faster:

1. **Pure Go, no CGO.** All connectors must compile without CGO.
2. **HCL first.** All configuration is HCL2; the binary is the same, only config
   differs. Config files use the `.mycel` extension.
3. **Connector Owns Config.** New connectors self-describe their parameters
   (via `SourceValidator`/`TargetValidator` and the schema in `pkg/schema/`)
   rather than requiring parser changes.
4. **One concern per file.** Keep types, parsers, and executors in their own
   logical files — avoid dumping unrelated code into one file.
5. **Standard protocols in/out.** Connectors speak standard protocols (REST,
   gRPC, queues, …); a Mycel service is indistinguishable from a hand-written one.
6. **Connector names use underscores**, not hyphens (e.g. `my_api`, not `my-api`).
7. **Recursive scanning.** All config directories are scanned recursively for
   `.mycel` files.

## Tests

Every package must have tests, and the full suite must stay green:

```bash
go test ./...
```

- Add tests with your change. For runtime/flow behavior, an in-memory SQLite
  connector or a mock connector is usually enough (see existing `*_test.go`
  files for the patterns).
- New or changed `examples/` must pass `mycel validate`.
- For features touching external services, add an integration suite under
  `tests/integration/` (skip gracefully when the service isn't available).

## Commit & PR guidelines

- Branch off `main` (e.g. `feature/...`, `fix/...`, `docs/...`, `chore/...`).
- Write **clear commit messages in English**. We follow
  [Conventional Commits](https://www.conventionalcommits.org/) loosely
  (`feat:`, `fix:`, `docs:`, `chore:`, …) with a focused subject and a body that
  explains the *why*.
- **Update docs with code.** If you change behavior or add a feature, update the
  relevant `docs/`, `README.md`, and the example.
- **Update `CHANGELOG.md`** for any user-facing change (the project keeps a
  version-by-version changelog).
- Make sure `go build ./...`, `go vet ./...`, and `go test ./...` all pass before
  opening the PR.
- Fill in the pull request template and link any related issue.

We use Semantic Versioning: additive, backward-compatible changes are minor
bumps; breaking changes are major and must be called out explicitly in the PR
and `CHANGELOG.md`.

## Questions

Open a [discussion](../../discussions) or a question issue. For anything
security-related, follow [SECURITY.md](SECURITY.md) instead of filing publicly.
