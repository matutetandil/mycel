# Versioning and Support

Mycel follows [Semantic Versioning](https://semver.org/). For a runtime whose
interface is a configuration file rather than a set of function signatures,
the useful way to read the three numbers is **what happens to a `.mycel` file
you already have**.

## What the numbers mean

| Release | What it may do to a configuration that works today |
|---|---|
| **PATCH** (3.7.0 → 3.7.1) | Nothing. It parses the same and means the same. Fixes only. |
| **MINOR** (3.7.0 → 3.8.0) | Nothing is taken away. New blocks, attributes, connectors and functions become available. |
| **MAJOR** (3.x → 4.0.0) | A configuration that started before may be refused, or may mean something different. |

That is why 3.0.0 was a major release: it was not about the size of the
change. Configurations that used to start began to be refused, and settings
in `auth` that had been parsed and ignored started to act. The version number
has one job there, which is to make someone read the notes before upgrading.

## The grey area, stated

A fix changes behaviour. That is what a fix is. The rule we apply:

- If the old behaviour was **silently wrong** — an attribute that was parsed
  and never read, a write that was dropped, a field that answered an empty
  list — the fix ships in a **patch**, and the changelog entry says exactly
  what changes and what it looked like before. Nobody could have deliberately
  built on a behaviour that was reporting success while doing nothing.
- If someone could reasonably have built on the old behaviour, the change
  waits for a **minor** and is announced there, or for a **major** when it
  cannot be made compatible at all.

## Deprecation

An attribute or a block is never removed in a minor release. It is marked
deprecated, keeps working, and `mycel validate` warns about it. The earliest
it can be removed is the next major, and the changelog for that major lists
every removal under a `Breaking` heading, which is also what the release
notes are built from.

## Supported versions

**The latest minor of the current major** receives fixes, including security
fixes. Earlier minors do not.

This is deliberately modest. Mycel is maintained by one person, and a support
promise that cannot be kept is worse than a narrow one that can. If you need
to stay on an older line, the source is MIT and the release you are on is
tagged; the practical advice is to pin a version and upgrade deliberately
rather than to expect backports.

## Release cadence

Releases are cut when something is ready, not on a calendar, so there are a
lot of them. That is not the same as instability: what a patch is allowed to
do is bounded by the table above, so upgrading within a minor is meant to be
uneventful. Pin an exact version in production and read the changelog before
moving.

```bash
go install github.com/matutetandil/mycel/v3/cmd/mycel@v3.7.0   # exact
docker pull mdenda/mycel:3.7.0                                  # exact
helm install mycel oci://ghcr.io/matutetandil/charts/mycel --version 3.7.0
```

The floating tags — `3.7`, `3`, `latest` — exist for convenience and are not
what a production deployment should follow.

## The Go toolchain

The `go` directive in `go.mod` may move in a **minor** release, never in a
patch. It is the version the published container images are built with, and
moving it is a change to the build, not to a configuration.

It moves on purpose only. A dependency update can raise it on its own: during
3.7.1 three `golang.org/x` updates each required Go 1.26 and silently rewrote
the directive, which would have broken a release whose images pin Go 1.25.
The directive is checked against both Dockerfiles after every dependency
change.

## Dependencies

In a **patch** release we take security fixes and patch-level updates of
direct dependencies. Nothing else.

Anything that changes an engine waits for a minor and is verified against
built binaries rather than only against tests: the CEL evaluator behind every
transform, a database driver, the GraphQL layer. A test suite that passes
does not prove that thousands of existing expressions still evaluate to the
same values.

When a vulnerability is reported against a dependency, the fix goes out as a
patch release with a `Security` section in the changelog, which is what the
release notes lead with. See [Security](https://github.com/matutetandil/mycel/blob/main/SECURITY.md)
for how to report one.
