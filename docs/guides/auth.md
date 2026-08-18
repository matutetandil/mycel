# Authentication System

Mycel includes a complete, enterprise-grade authentication system that can be configured entirely through HCL. No code required.

## Overview

The auth system provides:

- **Core Authentication**: JWT tokens, sessions, password hashing
- **Security Features**: Brute force protection, rate limiting, audit logging
- **Multi-Factor Authentication**: TOTP, WebAuthn/Passkeys, recovery codes
- **SSO & Social Login**: Google, GitHub, Apple, OIDC (Okta, Azure AD, Auth0)
- **Account Linking**: Automatic or manual linking of social accounts

## Quick Start

```hcl
auth {
  preset = "standard"  # strict, standard, relaxed, development

  jwt {
    secret = env("JWT_SECRET")
  }

  users {
    connector = "postgres"
    table     = "users"
  }
}
```

## Configuration Reference

### Presets

| Preset | Access Token | Refresh Token | MFA | Password Policy |
|--------|--------------|---------------|-----|-----------------|
| `strict` | 15m | 1d | Required | Strong (12+ chars, all types) |
| `standard` | 1h | 7d | Optional | Moderate (8+ chars) |
| `relaxed` | 24h | 30d | Off | Basic (6+ chars) |
| `development` | 24h | 90d | Off | None |

### JWT Configuration

```hcl
jwt {
  # Secret key (required for HMAC)
  secret = env("JWT_SECRET")

  # Or use RSA keys
  # private_key = file("./keys/private.pem")
  # public_key  = file("./keys/public.pem")

  # Algorithm: HS256, HS384, HS512, RS256, RS384, RS512
  algorithm = "HS256"

  # Token lifetimes
  access_lifetime  = "1h"
  refresh_lifetime = "7d"

  # Token claims
  issuer   = "my-service"
  audience = ["my-app"]

  # Enable refresh token rotation
  rotation = true
}
```

### Password Policy

```hcl
password {
  min_length      = 8
  max_length      = 128
  require_upper   = true
  require_lower   = true
  require_number  = true
  require_special = false

  # Refuse the last N passwords, the one in use counting as the most recent
  history = 5

  # How long a password may be used for, and how much notice to give
  max_age     = "90d"
  warn_before = "7d"

  # Breach check (haveibeenpwned)
  breach_check = true
}
```

`history` refuses a password this account has used before. The hashes are kept
in `password_history` for a database-backed deployment and in the process for
any other, so a service holding its users in memory loses the history with the
accounts on a restart.

`max_age` expires a password. Signing in keeps working past it — the endpoint
that fixes it needs a token — but every other endpoint answers `403` with the
code `password_expired` until the password is changed. Signing out keeps
working too. `warn_before` puts `password_expires_at` in the sign-in response
that much ahead of time, which is what a client needs to ask somebody to change
it before it starts refusing them.

Where the password's age is read from depends on the store. In memory it is
recorded for you. On a database it is a column, opt-in like `roles`, so a users
table that already exists need not grow one:

```hcl
users {
  connector = "app_db"
  fields {
    password_changed_at = "password_changed_at"
  }
}
```

Without that column nothing expires, and the service says so at startup rather
than leaving a policy that looks configured. An account whose age is not
recorded — one that predates the column — is never treated as expired.

### Security Features

```hcl
security {
  brute_force {
    enabled      = true
    max_attempts = 5
    window       = "15m"
    lockout_time = "30m"
    track_by     = "ip+user"  # "ip", "user", "ip+user"

    # How long a failed sign-in waits before answering, give or take a
    # quarter. Defaults to 1.5s; "0" answers immediately, which is only
    # sensible in a test.
    fail_delay = "1s"

    # Each further failure makes the next attempt slower still.
    progressive_delay {
      enabled    = true
      initial    = "1s"
      max        = "30s"
      multiplier = 2
    }
  }

  replay_protection {
    enabled = true
    window  = "5m"
  }

  impossible_travel {
    enabled       = true
    max_speed_kmh = 900        # faster than a flight
    on_detect     = "notify"   # "notify", "challenge", "block"

    geoip {
      # One of these. A MaxMind City database you have downloaded...
      database = "/var/lib/mycel/GeoLite2-City.mmdb"
      # ...or an HTTP service, with {ip} standing for the address:
      # api = "https://geo.example.com/lookup/{ip}"
    }
  }

  device_binding {
    enabled        = true
    fingerprint    = ["user_agent", "ip"]
    trust_duration = "30d"
    max_devices    = 5
    on_new_device  = "notify"   # "allow", "challenge", "block", "notify"
  }
}
```

### Impossible Travel

Two sign-ins from places too far apart for the time between them. What is
measured is the straight line over the ground divided by the hours between: any
journey somebody could really make is longer than a straight line, so the speed
this produces is the slowest they could have been going, and calling *that*
impossible is a claim that holds.

`geoip` is where an address is turned into a place, and one of the two is
required — the block does nothing without it, and a service that enables
detection without one is refused at startup rather than left believing it is on.

- **`database`** is a MaxMind GeoLite2 City file. Mycel does not ship it and
  cannot: it is MaxMind's, downloaded under their licence with an account of
  your own. Point this at one that is already on disk.
- **`api`** is any HTTP service that answers with JSON, with `{ip}` standing for
  the address. The answer is read leniently — `latitude`/`longitude`, `lat`/`lon`,
  `lat`/`lng`, or the same nested under `location` — so this works with whichever
  provider you already pay for. A lookup is cached for an hour, and a service
  that is down is not cached at all.

`on_detect` decides what a detection means: `notify` runs the
`on_suspicious_activity` hook with `auth.reason = "impossible_travel"`,
`challenge` requires a second factor, `block` refuses the sign-in.

Three things never happen, because each would be worse than a missed detection:
an address nothing can place is not held against anybody, a local address is not
looked up at all, and a geolocation service having a bad day does not stop
people signing in.

### Recognising a Device

`device_binding` notices when an account signs in from something it has not
used before. A device is identified by what the request already carries — no
agent is installed on it — so `fingerprint` chooses from what a server can
actually see:

| Field | What it is |
|-------|------------|
| `user_agent` | The browser string. The default, and the only thing every request carries |
| `ip` | The network the address belongs to, not the address: a phone changes address between one street and the next, and a device that is new every time is no device at all |
| `device_id` | An identifier the client keeps and sends in the sign-in body |

That is weak on its own — two people on the same browser version look alike —
which is why the useful settings are the ones that ask for more or tell
somebody, rather than the one that refuses:

| `on_new_device` | What happens |
|-----------------|--------------|
| `allow` | Remember it and carry on. The default |
| `notify` | Remember it and run the `on_suspicious_activity` hook, with `auth.reason = "new_device"` |
| `challenge` | Require a second factor for this sign-in. An account with no MFA enrolled is let through and the service says so, because there is nothing to challenge with and locking somebody out of a new laptop is worse |
| `block` | Refuse the sign-in |

`trust_duration` is how long a device stays recognised without being used: past
it, the same machine is new again. `max_devices` caps how many are remembered,
dropping the one nobody has signed in from for longest.

A request carrying nothing to identify a device — a proxy that stopped
forwarding the browser string — is let through rather than refused. An outage
is a worse failure than an unrecognised device.

### Session Management

```hcl
sessions {
  max_active       = 5           # How many sessions one person may hold at once
  idle_timeout     = "1h"        # End a session left untouched this long
  absolute_timeout = "24h"       # End it this long after it began, however active

  extend_on_activity = true      # Using the session pushes the idle timeout forward

  allow_list       = true        # Serve GET /auth/sessions
  allow_revoke     = true        # Serve DELETE /auth/sessions/{id}

  track            = ["ip", "user_agent"]  # What is recorded about a sign-in

  on_max_reached   = "revoke_oldest"  # "reject_new", "revoke_oldest"
}
```

`allow_list` and `allow_revoke` are on unless you write them false: a service
with no `sessions` block still lets somebody see where they are signed in and
end a session they no longer recognise. Writing either false stops that endpoint
being served at all, rather than serving it and refusing — a client asking for it
gets a 404, not a 403.

`extend_on_activity` is the difference between a sliding session and a fixed
one. On, which is the default, each request pushes the idle timeout forward, so
somebody working steadily is never signed out. Written false, the session ends
one `idle_timeout` after it began however busy it was — a policy worth having
where a long-lived session is the risk. What it does not change is what the
session listing shows: when it was last used stays truthful either way.

`track` names what is recorded about a sign-in. Naming none records both the
address and the browser string; naming some records only those, so a service
that must not keep addresses writes `track = ["user_agent"]` and the `ip` field
on a session stays empty. Whatever is recorded is what `GET /auth/sessions`
shows.

### Multi-Factor Authentication

```hcl
mfa {
  required = "optional"  # "required", "optional", "off"
  methods  = ["totp", "webauthn"]

  # TOTP Configuration
  totp {
    issuer = "My App"
    digits = 6
    period = 30  # seconds
  }

  # WebAuthn Configuration
  webauthn {
    rp_id             = "myapp.com"
    rp_name           = "My Application"
    rp_display_name   = "My Application"      # Shown in the browser prompt
    rp_origins        = ["https://myapp.com"]
    attestation       = "none"  # "none", "indirect", "direct"
    user_verification = "preferred"
    timeout           = 60000   # Milliseconds a ceremony is given
  }

  # Recovery codes
  recovery {
    enabled     = true
    code_count  = 10
    code_length = 8
  }
}
```

### What a wrong password costs

A failed sign-in answers slowly, and by an amount that varies. Two reasons, and
the second is the one that is easy to miss.

The wait is what makes guessing expensive. Without it an attacker is limited
only by the network, and locking an account after five tries is easy to walk
around by spreading the guesses across many accounts.

The variation is what stops the answer from being an oracle. Before this
existed, an address with no account answered in 0.4ms and an address with one in
46ms — the missing case returned before the password hash was computed. That
hundredfold difference is a way to harvest which addresses have accounts without
guessing a single password. A constant delay would not close it, since a
constant is something an attacker subtracts, so the wait is randomised and both
outcomes pay it: a login for an address with no account verifies the password
against a hash that matches nothing, so the work is the same either way.

This is what `pam_unix` does on Linux, for the same reasons.

| Setting | Default | What it does |
|---|---|---|
| `fail_delay` | `1.5s` | How long a failure waits, give or take a quarter. `"0"` answers immediately |
| `max_attempts` | preset | Failures before the account is locked |
| `lockout_time` | preset | How long it stays locked. The right password is refused too — the account is locked, not the guess |
| `progressive_delay` | off | Makes each further attempt slower still, on top of the wait above |

### Reading who the caller is

A connector that authenticates a request publishes what it learned, and flows
read it as `auth`:

| Expression | What it holds |
|---|---|
| `auth.authenticated` | Whether the request carried a valid credential |
| `auth.user_id` | Who it belongs to — the subject of a JWT, the name for basic auth |
| `auth.email` | From the `email` claim, when there is one |
| `auth.roles` | From the `roles` claim, as a list |
| `auth.claims.*` | Everything else the credential carried, so a field nobody mapped is still reachable |

```hcl
connector "api" {
  type = "rest"
  port = 8080

  auth {
    type   = "jwt"
    secret = env("JWT_SECRET")
    public = ["/health"]
  }
}

flow "my_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }

  # Only the caller's own rows, decided by the credential rather than by a
  # parameter the caller controls.
  to {
    connector = "db"
    query     = "SELECT * FROM orders WHERE user_id = :user_id"
  }

  transform {
    user_id = "auth.user_id"
  }
}
```

Authorisation is written the same way, as a condition rather than as a separate
mechanism:

```hcl
flow "admin_report" {
  from {
    connector = "api"
    operation = "GET /admin/report"
  }

  accept {
    when = "'admin' in auth.roles"
  }

  to {
    connector = "db"
    query     = "SELECT * FROM report"
  }
}
```

On a request with no credential — a public path — `auth.authenticated` is false
and the rest is empty, so an expression that reads it answers rather than fails.

### Rate limiting the auth endpoints

Three protections answer different questions, and this is the one about volume:

| | What it stops |
|---|---|
| `brute_force` | Repeated failures against **one account**, which it locks |
| `rate_limit` | A flood across **many accounts** from one caller — what credential stuffing looks like |
| A connector's own `rate_limit` | Traffic to the **whole server**, auth endpoints included |

```hcl
auth {
  security {
    rate_limit {
      enabled = true
      key_by  = "ip"          # ip | user | ip+user

      # Everything not named below
      rate   = 100
      window = "1m"

      login {
        rate   = 5
        window = "1m"
      }

      register {
        rate   = 3
        window = "1m"
      }
    }
  }
}
```

Each endpoint is counted on its own, so a limit on `login` does not cap
registration. A refused request is answered `429` with `Retry-After`.

`burst` may be set per endpoint; left out, it follows the rate that was written,
so `rate = 5` means five, not five plus an unwritten allowance. Endpoints with
no block of their own use defaults that suit them — logging in is limited more
tightly than listing sessions.

`key_by = "user"` adds the authenticated user to the count where there is one to
read. A login carries its identity in a body that has not been parsed when the
limit is applied, so those are counted per address.

### Signing in through an identity provider

Declaring a provider is the whole of the setup: the endpoints that drive the
flow are mounted from the same configuration, and a sign-in ends in the same
session and token pair a password login produces.

One attribute is needed beyond the provider itself. A provider sends the browser
back to this service after a sign-in, to an absolute address it has on record,
so it has to be told what that address is:

```hcl
auth {
  # The address this service is reached at from outside — not the address it
  # listens on. Register the callback below with each provider.
  base_url = env("PUBLIC_URL", "https://app.example.com")

  social {
    google {
      client_id     = env("GOOGLE_CLIENT_ID")
      client_secret = env("GOOGLE_CLIENT_SECRET")
    }
  }
}
```

Starting without it is a startup error rather than a failure at the first
sign-in, where the provider's message names none of this.

Two endpoints exist per family. The provider is named in the path, so one route
serves every provider declared:

| Endpoint | Purpose |
|---|---|
| `GET /auth/social/{provider}` | Redirects to the provider — `/auth/social/google` |
| `GET /auth/social/callback` | Where the provider returns; issues the tokens |
| `GET /auth/sso/{provider}` | The same, for an OIDC provider — `/auth/sso/okta` |
| `GET /auth/sso/callback` | The OIDC callback |

Register `{base_url}/auth/social/callback` with each social provider, and
`{base_url}/auth/sso/callback` with each OIDC one. Both paths can be moved with
`endpoints { social_callback { path = "..." } }`, and the address handed to the
provider follows, so the two cannot disagree.

The callback answers with the token pair:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "action": "created",
  "provider": "google",
  "user": { "id": "...", "email": "person@example.com" }
}
```

`action` says what happened to the account: `created` for a first sign-in,
`existing` for a return, `linked` when the identity was attached to an account
that was already there.

### Social Login Providers

```hcl
social {
  google {
    client_id     = env("GOOGLE_CLIENT_ID")
    client_secret = env("GOOGLE_CLIENT_SECRET")
    scopes        = ["openid", "email", "profile"]
  }

  github {
    client_id     = env("GITHUB_CLIENT_ID")
    client_secret = env("GITHUB_CLIENT_SECRET")
    scopes        = ["read:user", "user:email"]
  }

  apple {
    client_id   = env("APPLE_CLIENT_ID")
    team_id     = env("APPLE_TEAM_ID")
    key_id      = env("APPLE_KEY_ID")
    private_key = env("APPLE_PRIVATE_KEY")
  }
}
```

### Enterprise OIDC

```hcl
# Okta
oidc "okta" {
  issuer        = "https://your-org.okta.com"
  client_id     = env("OKTA_CLIENT_ID")
  client_secret = env("OKTA_CLIENT_SECRET")
  scopes        = ["openid", "email", "profile", "groups"]

  # Custom claim mappings
  claims {
    groups = "groups"
    role   = "role"
  }
}

# Azure AD
oidc "azure" {
  issuer        = "https://login.microsoftonline.com/${TENANT_ID}/v2.0"
  client_id     = env("AZURE_CLIENT_ID")
  client_secret = env("AZURE_CLIENT_SECRET")
  scopes        = ["openid", "email", "profile"]
}

# Auth0
oidc "auth0" {
  issuer        = "https://your-tenant.auth0.com/"
  client_id     = env("AUTH0_CLIENT_ID")
  client_secret = env("AUTH0_CLIENT_SECRET")
  scopes        = ["openid", "email", "profile"]
}
```

### External Identity Providers

A `provider` block validates an incoming credential (typically an API key or
opaque bearer token) against an external HTTP endpoint at request time, rather
than against local JWTs or a static list. Use it for dynamic API keys, an
upstream introspection service, or any "is this token valid, and who is it?"
backend.

```hcl
auth {
  secret = env("AUTH_SECRET")

  provider "api_keys" {
    type     = "http"                                  # only "http" is supported
    validate = env("KEYS_VALIDATE_URL")                # URL; supports {token}

    # Headers sent to the validate URL. {token} is replaced with the credential.
    request = {
      Authorization = "Bearer {token}"
    }

    # Response mapping. Each value is a CEL expression evaluated over:
    #   status — the HTTP status code (int)
    #   body   — the parsed JSON response (object)
    response {
      success = "status == 200 && body.active == true"  # required; must be truthy
      user_id = "body.user_id"
      email   = "body.email"
      roles   = "body.roles"        # list<string>, or a comma-separated string
      token   = "body.session_id"   # optional; stored on the claims
    }
  }
}
```

**Behavior**

- **Order:** local JWT validation runs first. Providers are tried only when the
  credential is not a valid JWT, in declaration order; the first whose `success`
  expression is truthy wins.
- **Auth context:** the full response `body` is exposed to flows as
  `auth.claims.*` (so any field is reachable, not just the mapped ones), and the
  mapped `user_id` is available as `auth.user_id`.
- **Provider unavailable:** a timeout or transport error is treated as a
  validation failure (the request is rejected), never a 5xx from your service.
- **Fail-fast:** an unsupported `type`, a missing `validate`/`success`, or an
  invalid CEL expression fails at startup, not silently at runtime.

**Not yet implemented:** response caching (every request hits the provider) and
`sync_to` (parsed, but setting it only logs a warning today).

See the [`dynamic-api-key` example](https://github.com/matutetandil/mycel/tree/main/examples/dynamic-api-key) for a complete setup.

### User Storage

```hcl
users {
  connector = "postgres"
  table     = "users"

  # Field mappings (if different from defaults)
  fields {
    id            = "id"
    email         = "email"
    password_hash = "password_hash"
    mfa_enabled   = "mfa_enabled"
    created_at    = "created_at"
    updated_at    = "updated_at"
  }
}
```

### Token Storage

```hcl
# In-memory (default, not for production)
storage {
  driver = "memory"
}

# Redis (recommended for production)
storage {
  driver   = "redis"
  url      = env("REDIS_URL", "redis://localhost:6379")
  password = env("REDIS_PASSWORD", "")
  db       = 0
}
```

### Audit Logging

```hcl
audit {
  enabled   = true
  connector = "postgres"
  table     = "auth_audit_log"
  events    = [
    "login",
    "logout",
    "failed_login",
    "register",
    "password_change",
    "mfa_enabled",
    "mfa_disabled",
    "sso_login",
    "account_linked",
    "account_unlinked"
  ]
}
```

### Custom Endpoints

```hcl
endpoints {
  prefix = "/auth"

  # Standard auth
  login {
    path    = "/login"
    method  = "POST"
    enabled = true
  }
  logout {
    path    = "/logout"
    method  = "POST"
    enabled = true
  }
  register {
    path    = "/register"
    method  = "POST"
    enabled = true
  }
  refresh {
    path    = "/refresh"
    method  = "POST"
    enabled = true
  }
  me {
    path    = "/me"
    method  = "GET"
    enabled = true
  }

  # Sessions
  sessions_list {
    path    = "/sessions"
    method  = "GET"
    enabled = true
  }
  sessions_revoke {
    path    = "/sessions/:id"
    method  = "DELETE"
    enabled = true
  }

  # Password
  password_change {
    path    = "/change-password"
    method  = "POST"
    enabled = true
  }
  password_reset {
    path    = "/reset-password"
    method  = "POST"
    enabled = false
  }

  # MFA
  mfa_setup {
    path    = "/mfa/setup"
    method  = "POST"
    enabled = true
  }
  mfa_verify {
    path    = "/mfa/verify"
    method  = "POST"
    enabled = true
  }
  mfa_disable {
    path    = "/mfa/disable"
    method  = "POST"
    enabled = true
  }

  # SSO. The route that starts a flow follows the callback's path and is not
  # configured on its own.
  social_callback {
    path    = "/social/callback"
    method  = "GET"
    enabled = true
  }
  sso_callback {
    path    = "/sso/callback"
    method  = "GET"
    enabled = true
  }

  # Account linking
  link_account {
    path    = "/link/:provider"
    method  = "POST"
    enabled = true
  }
  unlink_account {
    path    = "/unlink/:provider"
    method  = "DELETE"
    enabled = true
  }
  linked_list {
    path    = "/linked-accounts"
    method  = "GET"
    enabled = true
  }
}
```

## Forgotten Passwords

Two endpoints, on by default:

| Endpoint | Body | What happens |
|----------|------|--------------|
| `POST /auth/forgot-password` | `{"email": "..."}` | Issues a reset token and hands it to the `on_password_reset` hook |
| `POST /auth/reset-password` | `{"token": "...", "new_password": "..."}` | Spends the token and sets the password |

Getting the token to the person is not auth's job — a flow already knows how to
send an email:

```hcl
auth {
  password {
    reset_token_ttl = "1h"   # the default
  }

  hooks {
    on_password_reset { flow = "send_reset_email" }
  }
}

flow "send_reset_email" {
  from { connector = "internal" }
  to {
    connector = "smtp"
    template  = "Reset your password: https://app.example.com/reset?token=${auth.reset_token}"
  }
}
```

Without that hook the token cannot reach anybody, and the service says so in the
log rather than leaving somebody waiting for an email.

What the endpoints do and do not say:

- **Asking for a reset answers the same way whether or not the address has an
  account.** Answering differently would turn it into a way to find out who has
  one here.
- **A token is good once, and for `reset_token_ttl`.** It is stored hashed, so
  a store somebody can read is not a store somebody can reset accounts from.
- **A reset ends every session the account had.** Somebody resetting a password
  either forgot it or is taking the account back from whoever did not, and in
  the second case leaving the other sessions open would defeat the reset.
- **The password policy still applies** — length, complexity and `history`. A
  reset is not a way around the rules a deliberate change obeys.

Tokens live in the process by default, which is the wrong place for more than
one replica: a link issued by one is unknown to the next. A `storage` block on
`redis` keeps them where every replica can see them, along with the sessions.

## Hooks

A hook runs a flow when something happens to an account. Auth does not send
email or write to Slack itself — a flow already knows how, and this is how the
rest of the runtime does it.

```hcl
auth {
  hooks {
    after_register { flow = "send_welcome_email" }
    after_login    { flow = "record_sign_in" }

    on_failed_login {
      flow      = "alert_security"
      condition = "auth.ip != '203.0.113.10'"
    }

    before_login {
      flow     = "check_allowlist"
      on_error = "fail"
    }
  }
}
```

| Hook | When it runs | The flow is told |
|------|--------------|------------------|
| `before_login` | Before a sign-in is checked at all | `email`, `ip`, `user_agent` |
| `after_login` | Once somebody has signed in | `user_id`, `email`, `ip`, `user_agent` |
| `after_register` | Once an account has been created | `user_id`, `email` |
| `on_failed_login` | A sign-in was refused | `email`, `ip`, `user_agent`, `reason`, `code` |
| `on_suspicious_activity` | Something about a sign-in was out of the ordinary | `user_id`, `email`, `ip`, `user_agent`, `reason` |
| `on_password_reset` | Somebody asked to reset a forgotten password | `user_id`, `email`, `reset_token`, `expires_at`, `ip`, `user_agent` |
| `before_password_change` | Before a password is changed | `user_id`, `email` |
| `after_password_change` | Once a password has been changed | `user_id`, `email` |

The event arrives under `auth`, so the flow reads `auth.email`, `auth.event`,
and so on. `auth.event` is always the hook's own name.

`condition` is CEL over that event, so one hook can serve a case narrower than
the event itself. `on_error = "fail"` refuses whatever the hook is attached to —
only meaningful on a `before_` hook, and a service that writes it on an `after_`
one is refused at startup, because refusing after the change has been made
cannot undo it. Everything else is the default: a flow that fails is logged and
the sign-in goes through, since an account must not become unusable because a
notification could not be sent.

A hook naming a flow no `flow` block declares is refused by `mycel validate`.

## Database Schema

### PostgreSQL / MySQL

```sql
-- Users table
CREATE TABLE users (
  id VARCHAR(64) PRIMARY KEY,
  email VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255),
  mfa_enabled BOOLEAN DEFAULT FALSE,
  mfa_secret VARCHAR(255),
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW(),
  -- Only needed for password { max_age }, and only when the users fields block
  -- names it. Rows written before it existed are null, which is not expired.
  password_changed_at TIMESTAMP,
  metadata JSONB
);

-- Password history (for reuse prevention)
CREATE TABLE password_history (
  id SERIAL PRIMARY KEY,
  user_id VARCHAR(64) NOT NULL REFERENCES users(id),
  password_hash VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);

-- Sessions and revoked tokens, on MySQL. These are the names the runtime uses;
-- on PostgreSQL both are held in memory unless Redis is configured for them.
CREATE TABLE auth_sessions (
  id VARCHAR(64) PRIMARY KEY,
  user_id VARCHAR(64) NOT NULL,
  ip VARCHAR(45),
  user_agent TEXT,
  created_at TIMESTAMP,
  last_active_at TIMESTAMP,
  expires_at TIMESTAMP,
  device_id VARCHAR(64)
);

CREATE TABLE auth_tokens (
  token_id VARCHAR(64) PRIMARY KEY,
  expires_at TIMESTAMP
);

-- Linked accounts (SSO/Social)
CREATE TABLE linked_accounts (
  id VARCHAR(64) PRIMARY KEY,
  user_id VARCHAR(64) NOT NULL REFERENCES users(id),
  provider VARCHAR(50) NOT NULL,
  provider_id VARCHAR(255) NOT NULL,
  email VARCHAR(255),
  name VARCHAR(255),
  picture TEXT,
  access_token TEXT,
  refresh_token TEXT,
  expires_at TIMESTAMP,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW(),
  metadata JSONB,
  UNIQUE(provider, provider_id)
);

-- MFA recovery codes
CREATE TABLE mfa_recovery_codes (
  id SERIAL PRIMARY KEY,
  user_id VARCHAR(64) NOT NULL REFERENCES users(id),
  code_hash VARCHAR(255) NOT NULL,
  used BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMP DEFAULT NOW()
);

-- WebAuthn credentials
CREATE TABLE webauthn_credentials (
  id VARCHAR(255) PRIMARY KEY,
  user_id VARCHAR(64) NOT NULL REFERENCES users(id),
  name VARCHAR(255),
  public_key BYTEA NOT NULL,
  attestation_type VARCHAR(50),
  authenticator_aaguid BYTEA,
  sign_count INTEGER DEFAULT 0,
  created_at TIMESTAMP DEFAULT NOW()
);

-- Audit log
CREATE TABLE auth_audit_log (
  id SERIAL PRIMARY KEY,
  event VARCHAR(50) NOT NULL,
  user_id VARCHAR(64),
  email VARCHAR(255),
  ip VARCHAR(45),
  user_agent TEXT,
  success BOOLEAN,
  error_reason TEXT,
  metadata JSONB,
  created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_password_history_user ON password_history(user_id);
CREATE INDEX idx_linked_accounts_user ON linked_accounts(user_id);
CREATE INDEX idx_linked_accounts_provider ON linked_accounts(provider, provider_id);
CREATE INDEX idx_recovery_codes_user ON mfa_recovery_codes(user_id);
CREATE INDEX idx_webauthn_user ON webauthn_credentials(user_id);
CREATE INDEX idx_audit_user ON auth_audit_log(user_id);
CREATE INDEX idx_audit_event ON auth_audit_log(event);
CREATE INDEX idx_audit_created ON auth_audit_log(created_at);
```

## API Reference

### Standard Auth

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/register` | POST | Register new user |
| `/auth/login` | POST | Login with email/password |
| `/auth/logout` | POST | Logout (invalidate session) |
| `/auth/refresh` | POST | Refresh access token |
| `/auth/me` | GET | Get current user info |
| `/auth/change-password` | POST | Change password |

### Sessions

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/sessions` | GET | List active sessions |
| `/auth/sessions/:id` | DELETE | Revoke specific session |

### MFA

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/mfa/setup` | POST | Begin MFA setup (returns QR code) |
| `/auth/mfa/verify` | POST | Verify TOTP code and enable MFA |
| `/auth/mfa/disable` | POST | Disable MFA |

### SSO / Social Login

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/social/{provider}` | GET | Start a social sign-in |
| `/auth/social/callback` | GET | Where a social provider returns |
| `/auth/sso/{provider}` | GET | Start an OIDC sign-in |
| `/auth/sso/callback` | GET | Where an OIDC provider returns |

| `/auth/linked-accounts` | GET | The identities attached to the caller's account |
| `/auth/unlink/{provider}` | DELETE | Detach one of them |

An identity is attached during a sign-in, by matching the address it carries
against an account that already exists. What that match does is configurable:

```hcl
auth {
  sso {
    linking {
      enabled              = true
      match_by             = "email"   # email | phone | custom
      require_verification = true      # only an address the provider says is verified
      on_match             = "link"    # link | prompt | reject
    }

    oidc "corp" {
      issuer        = env("OIDC_ISSUER")
      client_id     = env("OIDC_CLIENT_ID")
      client_secret = env("OIDC_CLIENT_SECRET")
    }
  }
}
```

`on_match = "prompt"` answers the callback with `needs_confirmation` instead of
signing the person in, so an account is never joined to an identity without its
owner saying so. `reject` refuses the sign-in outright.

Unlinking refuses to remove the last way into an account: someone who signed up
through a provider and never set a password would otherwise lock themselves out,
and that refusal is a `400` naming the reason.

## Security Considerations

### Production Checklist

- [ ] Use strong JWT secret (32+ random bytes)
- [ ] Enable HTTPS
- [ ] Use Redis for token storage (not memory)
- [ ] Enable brute force protection
- [ ] Set appropriate token lifetimes
- [ ] Enable audit logging
- [ ] Configure CORS properly
- [ ] Use `strict` or `standard` preset

### Best Practices

1. **Never log tokens or passwords** - Mycel redacts these automatically
2. **Rotate secrets periodically** - Use key rotation features
3. **Monitor audit logs** - Set up alerts for suspicious activity
4. **Use MFA** - Require or encourage MFA for sensitive operations
5. **Limit sessions** - Prevent unlimited concurrent sessions

## Examples

See [examples/auth](https://github.com/matutetandil/mycel/tree/main/examples/auth) for a complete working example.
