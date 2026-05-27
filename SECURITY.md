# Security Policy

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
