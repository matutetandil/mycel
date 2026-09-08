# Security Policy

## Supported versions

**The latest minor of the current major** receives security fixes. Earlier
minors do not: this is a project with one maintainer, and a support promise
that cannot be kept is worse than a narrow one that can. Pin an exact version
and upgrade deliberately. See
[Versioning and Support](docs/versioning.md) for what each release
number is allowed to change.

| Version | Supported |
|---|---|
| 3.7.x | Yes |
| < 3.7 | No |

## Vulnerabilities in dependencies

Mycel's dependency tree is scanned on every pull request and the build fails
on a HIGH or CRITICAL finding that has a fix available. A finding with no fix
published is reported and not blocked, since blocking would only stop
unrelated work.

A vulnerability reachable from Mycel's own code goes out as a patch release
with a `Security` section in the changelog, which is what the release notes
lead with. The published container image is scanned as well, and its report
is visible on
[Artifact Hub](https://artifacthub.io/packages/helm/mycel/mycel).

## Reporting a vulnerability

**Please do not report security issues through public GitHub issues, discussions,
or pull requests.**

Report them privately through GitHub's **Private Vulnerability Reporting**:

> Go to the repository's **Security** tab → **Report a vulnerability**.

(If that option is not visible, ask a maintainer to enable it under
*Settings → Security → Private vulnerability reporting*.)

Please include enough detail to reproduce and assess the issue:

- A description of the issue and its impact.
- Steps to reproduce (a minimal `.mycel` config and request, if applicable).
- The Mycel version (`mycel version`) and how it is deployed.
- Any relevant logs (with secrets redacted).

You can expect an initial acknowledgement within a few business days. We will
keep you informed about progress toward a fix and coordinate disclosure timing
with you.

## Supported versions

Mycel follows Semantic Versioning. Security fixes are released against the
**latest minor version** on the `main` line. Please upgrade to the latest
release before reporting, in case the issue is already fixed.

## Scope

**In scope** — issues in the Mycel runtime and connectors, such as:

- Input-handling flaws reachable through a connector (injection, parsing, etc.).
- Authentication/authorization bypasses in the built-in auth system.
- Sandbox-escape from WASM plugins/validators into the host process.
- Denial-of-service that a single malformed request/message can trigger
  (algorithmic blow-ups, unbounded memory).
- Secret leakage (e.g. credentials surfacing in logs or traces).

**Out of scope:**

- Vulnerabilities in **your** configuration or in the backend services Mycel
  talks to (databases, brokers, HTTP APIs). Mycel runs the config you give it.
- Issues requiring a malicious operator who already controls the config files or
  the host.
- Findings against outdated versions already fixed in a newer release.

## A note on the framework vs. your service

Mycel is a **framework/runtime**: it gives you the security tools
(secure-by-default input sanitization, injection/XXE protections, an auth system
with MFA/WebAuthn, rate limiting and circuit breakers, retry/DLQ). The framework
itself is not what carries a compliance certification (PCI, SOC 2, …) — those
apply to the **service you deploy**. Mycel hardens the boundary; the security
posture of the running service still depends on how you configure and operate it.

Thank you for helping keep Mycel and its users safe.
