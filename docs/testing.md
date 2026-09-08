# How Mycel Is Tested

Mycel's interface is a configuration language, and the failure that matters
most in a declarative runtime is not a crash. It is an attribute that is
parsed, stored, and read by nobody: nothing fails, the documentation says it
works, and the service quietly does something else.

Every one of those found in this codebase now has a test that fails when it
comes back. This page describes the guarantees those tests keep, so you can
check them yourself rather than take the claim.

## The guarantees

**What the schema declares, the parser accepts.** The schema is the single
source of truth behind completions, `mycel add` and the generated reference.
A test renders a document exercising every attribute and every nested block
of every root block the schema declares, and feeds it to the real parser
(`TestSchemaParity`). An attribute the parser does not know fails the test by
name. A handful of attributes are skipped because the language forbids them
next to each other — a cache key and a `key_from`, an action's connector and
flow — and each exclusion is a named case in the test, not a silent gap.

**What the reference documents, the schema declares.** Every root block,
every block a flow can hold, and every attribute they declare has to appear
in the configuration reference, and the reference may not invent blocks that
do not exist (`TestEveryRootBlockIsInTheReference`,
`TestEveryAttributeARootBlockDeclaresIsInTheReference`,
`TestTheReferenceDoesNotInventFlowBlocks`).

**What a connector page lists, a connector accepts**
(`TestEveryAttributeAConnectorsPageListsIsAccepted`).

**The examples in the docs run.** Every CEL snippet in the documentation is
compiled and evaluated, and has to return what the page says it returns
(`TestTheCELExamplesCompile`, `TestTheCELExamplesReturnWhatTheyClaim`). The
README's links resolve and its performance numbers come from the benchmark
results rather than from memory (`TestReadmeRelativeLinksResolve`,
`TestReadmePerformanceNumbersComeFromTheBenchmark`).

**Everything the language has, an example uses.** Every connector, every
block a flow can hold, every reusable block kind, every connector block,
every CEL function and every `auth` block appears in `examples/`, which the
test suite validates and, where the infrastructure exists, runs
(`internal/examples/coverage_test.go`).

**Every metric that is declared is recorded somewhere**
(`TestEveryMetricHasSomethingThatRecordsIt`). A metric defined and never
written is a dashboard that stays at zero while the service works.

**Every route registrar can actually be found by the runtime**
(`TestEveryRegisterRouteCanActuallyBeFound`). This one reads the source
rather than calling the code, because the defect it guards against is a Go
type assertion that fails silently: a named function type does not satisfy an
interface written with the unnamed one, so a source registered no flow at
all, compiled, started, and did nothing.

**Every check `mycel validate` runs, `mycel start` runs, and the editor
surfaces** (`TestValidateAndStartRunTheSameChecks`,
`TestEveryRuntimeCheckReachesTheEditor`). A configuration refused at startup
is refused by `validate` and marked in the editor.

**Every value the schema offers, the code accepts**
(`TestTheSchemaOffersTheValuesTheCodeAccepts`), and every CEL function the
editor offers exists and is described (`pkg/ide/cel_parity_test.go`).

## What is not guaranteed

`mycel validate` does not compile CEL. An expression that references a field
the message does not carry, or that does not parse, fails at request time and
not before. The checks that can be made statically are made — a cache key
written as an expression, `headers` aimed at a connector that sends none, a
step's `on_error` word nothing implements — but the expression language
itself is checked when it runs.

Integration coverage depends on infrastructure. The suite runs about eighteen
services under Docker Compose and covers the live path of every connector
that has one; a connector's rarer options are covered by unit tests against a
fake, which prove that a request was built correctly, not that a real server
accepts it.

## Running it yourself

```bash
go test ./...                          # unit and harness tests
go test -race -cover ./...             # what CI runs
cd tests/integration && bash run.sh    # the full stack, ~300 checks
```

The integration suite starts and stops its own services. Run it whole rather
than picking suites: several of them share ports and fixtures, and a partial
run reports failures that a full run does not have.
