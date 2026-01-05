# Phase 5.1: Authentication System

## Overview

Mycel Auth es un sistema de autenticación enterprise-grade completamente declarativo. Con unas pocas líneas de HCL, obtenés un microservicio de autenticación completo.

**Filosofía:** Presets para empezar rápido, configuración granular para casos avanzados.

## Quick Start

### Ejemplo Mínimo (5 líneas)

```hcl
auth {
  preset  = "standard"
  storage = connector.postgres
  secret  = env("JWT_SECRET")
}
```

Esto te da:
- Login/logout/register endpoints
- JWT con refresh tokens
- Password hashing (argon2id)
- Brute force protection
- Session management básico

### Ejemplo Completo

```hcl
auth {
  preset = "strict"

  # Storage para tokens/sessions
  storage {
    driver  = "redis"
    address = env("REDIS_URL")
  }

  # Storage para usuarios (si es local)
  users {
    connector = connector.postgres
    table     = "users"
  }

  jwt {
    secret           = env("JWT_SECRET")
    access_lifetime  = "15m"
    refresh_lifetime = "7d"
    issuer           = "mycel-auth"
    audience         = ["api.example.com"]
    rotation         = true  # Rotate refresh token on use
  }

  password {
    min_length      = 12
    require_upper   = true
    require_lower   = true
    require_number  = true
    require_special = true
    history         = 5       # Can't reuse last 5 passwords
    max_age         = "90d"   # Force change every 90 days
    breach_check    = true    # Check HaveIBeenPwned
  }

  mfa {
    required = true
    methods  = ["totp", "webauthn"]

    totp {
      issuer = "MyApp"
      digits = 6
      period = 30
    }

    webauthn {
      rp_name   = "My Application"
      rp_id     = "example.com"
      origins   = ["https://example.com"]
    }
  }

  security {
    brute_force {
      max_attempts = 5
      lockout_time = "15m"
      track_by     = "ip+user"  # ip, user, ip+user
    }

    impossible_travel {
      enabled       = true
      max_speed_kmh = 900  # Faster = airplane
    }

    device_binding {
      enabled = true
      trust_duration = "30d"
      max_devices    = 5
    }

    replay_protection {
      enabled = true
      window  = "5m"
    }
  }

  sessions {
    max_active      = 5
    idle_timeout    = "30m"
    absolute_timeout = "24h"
    allow_list      = true  # Can list active sessions
    allow_revoke    = true  # Can revoke other sessions
  }

  # OAuth2/OIDC providers for social login
  social {
    google {
      client_id     = env("GOOGLE_CLIENT_ID")
      client_secret = env("GOOGLE_CLIENT_SECRET")
      scopes        = ["email", "profile"]
    }

    github {
      client_id     = env("GITHUB_CLIENT_ID")
      client_secret = env("GITHUB_CLIENT_SECRET")
      scopes        = ["user:email"]
    }
  }

  # SSO Configuration
  sso {
    oidc "okta" {
      issuer        = "https://company.okta.com"
      client_id     = env("OKTA_CLIENT_ID")
      client_secret = env("OKTA_CLIENT_SECRET")
      scopes        = ["openid", "profile", "email"]
    }

    saml "azure" {
      metadata_url = "https://login.microsoftonline.com/.../metadata"
      entity_id    = "mycel-auth"
      acs_url      = "https://api.example.com/auth/saml/callback"
    }
  }

  # External identity provider (validate against existing system)
  provider "magento" {
    type     = "http"
    validate = "POST ${env("MAGENTO_URL")}/rest/V1/integration/customer/token"

    request {
      username = "input.email"
      password = "input.password"
    }

    response {
      success = "status == 200"
      token   = "body"  # Magento returns token directly
      user_id = "decode_jwt(body).sub"
    }
  }

  # Account linking (same email = same account)
  account_linking {
    enabled  = true
    match_by = "email"  # email, phone, custom
    require_verification = true
  }

  # Endpoints customization
  endpoints {
    prefix = "/auth"

    login    { path = "/login",    method = "POST", enabled = true }
    logout   { path = "/logout",   method = "POST", enabled = true }
    register { path = "/register", method = "POST", enabled = true }
    refresh  { path = "/refresh",  method = "POST", enabled = true }
    me       { path = "/me",       method = "GET",  enabled = true }

    password_forgot { path = "/forgot-password", enabled = true }
    password_reset  { path = "/reset-password",  enabled = true }
    password_change { path = "/change-password", enabled = true }

    sessions_list   { path = "/sessions",     method = "GET",    enabled = true }
    sessions_revoke { path = "/sessions/:id", method = "DELETE", enabled = true }

    mfa_setup    { path = "/mfa/setup",    enabled = true }
    mfa_verify   { path = "/mfa/verify",   enabled = true }
    mfa_disable  { path = "/mfa/disable",  enabled = true }
    mfa_recovery { path = "/mfa/recovery", enabled = true }

    # Social/SSO callbacks
    social_callback { path = "/social/:provider/callback" }
    sso_callback    { path = "/sso/:provider/callback" }
  }

  # Hooks for custom logic
  hooks {
    after_login {
      # Update last_login timestamp
      connector.postgres = "UPDATE users SET last_login = NOW() WHERE id = ${user.id}"
    }

    after_register {
      # Send welcome email
      connector.email = {
        to      = user.email
        subject = "Welcome!"
        template = "welcome"
      }
    }

    on_suspicious_activity {
      # Alert security team
      connector.slack = {
        channel = "#security"
        message = "Suspicious activity detected for user ${user.id}"
      }
    }
  }

  # Audit logging
  audit {
    enabled   = true
    connector = connector.audit_db
    events    = ["login", "logout", "failed_login", "password_change", "mfa_change"]
  }
}
```

## Presets

### `strict` (Default for production)
- MFA required
- Short token lifetime (15m access, 1d refresh)
- Strong password policy
- Device binding enabled
- Impossible travel detection
- Brute force: 3 attempts, 30m lockout
- Max 3 sessions

### `standard`
- MFA optional
- Medium token lifetime (1h access, 7d refresh)
- Moderate password policy
- Brute force: 5 attempts, 15m lockout
- Max 5 sessions

### `relaxed`
- MFA disabled
- Long token lifetime (24h access, 30d refresh)
- Basic password policy
- Brute force: 10 attempts, 5m lockout
- Unlimited sessions

### `development`
- No MFA
- Very long tokens (7d access, 30d refresh)
- No password requirements
- No brute force protection
- No security features
- Verbose logging

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Mycel Auth                              │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   Endpoints  │  │   Providers  │  │   Security   │       │
│  │              │  │              │  │              │       │
│  │ /login       │  │ Local DB     │  │ Brute Force  │       │
│  │ /logout      │  │ External API │  │ Imp. Travel  │       │
│  │ /register    │  │ OIDC         │  │ Device Bind  │       │
│  │ /refresh     │  │ SAML         │  │ Replay Prot. │       │
│  │ /mfa/*       │  │ Social       │  │              │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│                           │                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                    Token Manager                      │   │
│  │  JWT Generation │ Refresh Rotation │ Revocation      │   │
│  └──────────────────────────────────────────────────────┘   │
│                           │                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   Storage    │  │   Sessions   │  │    Audit     │       │
│  │              │  │              │  │              │       │
│  │ Redis        │  │ Multi-device │  │ Login events │       │
│  │ PostgreSQL   │  │ List/Revoke  │  │ Changes      │       │
│  │ Memory       │  │ Timeouts     │  │ Suspicious   │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. Identity Providers

#### Local (Database)
```hcl
auth {
  users {
    connector = connector.postgres
    table     = "users"

    # Field mappings (defaults shown)
    fields {
      id            = "id"
      email         = "email"
      password_hash = "password_hash"
      created_at    = "created_at"
      updated_at    = "updated_at"
    }
  }
}
```

#### External API
```hcl
auth {
  provider "legacy_system" {
    type = "http"

    # Validate credentials against external system
    validate = "POST ${env("LEGACY_API")}/auth/validate"

    request {
      email    = "input.email"
      password = "input.password"
    }

    response {
      success = "status == 200 && body.valid == true"
      user_id = "body.user_id"
      email   = "body.email"
      roles   = "body.roles"
    }

    # Optional: sync user to local DB after first login
    sync_to = connector.postgres
  }
}
```

#### OIDC (OpenID Connect)
```hcl
auth {
  sso {
    oidc "okta" {
      issuer        = "https://company.okta.com"
      client_id     = env("OKTA_CLIENT_ID")
      client_secret = env("OKTA_CLIENT_SECRET")
      scopes        = ["openid", "profile", "email", "groups"]

      # Map OIDC claims to user attributes
      claims {
        email  = "email"
        name   = "name"
        roles  = "groups"
        avatar = "picture"
      }
    }
  }
}
```

#### SAML 2.0
```hcl
auth {
  sso {
    saml "azure_ad" {
      # Option 1: Metadata URL (recommended)
      metadata_url = "https://login.microsoftonline.com/.../metadata"

      # Option 2: Manual config
      # idp_sso_url     = "https://..."
      # idp_certificate = file("./certs/idp.pem")

      entity_id = "mycel-auth"
      acs_url   = "https://api.example.com/auth/saml/callback"

      # Attribute mappings
      attributes {
        email = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
        name  = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name"
        roles = "http://schemas.microsoft.com/ws/2008/06/identity/claims/role"
      }
    }
  }
}
```

#### Social Login
```hcl
auth {
  social {
    google {
      client_id     = env("GOOGLE_CLIENT_ID")
      client_secret = env("GOOGLE_CLIENT_SECRET")
      scopes        = ["email", "profile"]
    }

    apple {
      client_id   = env("APPLE_CLIENT_ID")
      team_id     = env("APPLE_TEAM_ID")
      key_id      = env("APPLE_KEY_ID")
      private_key = env("APPLE_PRIVATE_KEY")
    }

    github {
      client_id     = env("GITHUB_CLIENT_ID")
      client_secret = env("GITHUB_CLIENT_SECRET")
      scopes        = ["user:email"]
    }
  }
}
```

### 2. JWT Configuration

```hcl
auth {
  jwt {
    # Signing
    algorithm = "RS256"  # HS256, RS256, RS384, RS512, ES256, ES384, ES512
    secret    = env("JWT_SECRET")  # For HS* algorithms

    # Or use key pair for RS*/ES*
    private_key = file("./keys/private.pem")
    public_key  = file("./keys/public.pem")

    # Lifetimes
    access_lifetime  = "15m"
    refresh_lifetime = "7d"

    # Claims
    issuer   = "mycel-auth"
    audience = ["api.example.com", "admin.example.com"]

    # Security
    rotation = true  # Rotate refresh token on each use

    # Custom claims (CEL expressions)
    claims {
      roles       = "user.roles"
      permissions = "user.permissions"
      tenant_id   = "user.tenant_id"
    }
  }
}
```

### 3. Password Policy

```hcl
auth {
  password {
    # Complexity
    min_length      = 12
    max_length      = 128
    require_upper   = true
    require_lower   = true
    require_number  = true
    require_special = true

    # Patterns to reject
    reject_patterns = [
      "password",
      "123456",
      "qwerty",
      user.email,      # Can't contain email
      user.name,       # Can't contain name
    ]

    # History
    history = 5  # Can't reuse last N passwords

    # Expiration
    max_age = "90d"
    warn_before = "14d"  # Warn user before expiration

    # Breach check
    breach_check = true  # Check against HaveIBeenPwned

    # Hashing (defaults shown, don't change unless needed)
    algorithm   = "argon2id"
    memory      = 65536   # 64MB
    iterations  = 3
    parallelism = 2
    salt_length = 16
    key_length  = 32
  }
}
```

### 4. MFA (Multi-Factor Authentication)

```hcl
auth {
  mfa {
    required = true  # or "optional", "admin_only"
    methods  = ["totp", "webauthn", "sms", "email"]

    # Require MFA for specific actions
    require_for = ["password_change", "email_change", "session_revoke"]

    # Grace period for first-time setup
    grace_period = "7d"

    # Recovery codes
    recovery {
      enabled     = true
      code_count  = 10
      code_length = 8
    }

    totp {
      issuer  = "MyApp"
      digits  = 6
      period  = 30
      algorithm = "SHA1"  # SHA1, SHA256, SHA512
    }

    # WebAuthn supports ALL of these:
    # - Hardware keys: YubiKey, Titan, Feitian, SoloKey
    # - Biometrics: FaceID, TouchID, Windows Hello, Android fingerprint
    # - Passkeys: iCloud Keychain, Google Password Manager, 1Password
    webauthn {
      rp_name = "My Application"
      rp_id   = "example.com"
      origins = ["https://example.com", "https://admin.example.com"]

      # Authenticator type
      # - "platform": Built-in (FaceID, TouchID, Windows Hello, Android biometrics)
      # - "cross-platform": External hardware keys (YubiKey, Titan, etc.)
      # - "any": Allow both (recommended)
      authenticator_attachment = "any"

      # User verification (biometric/PIN check on device)
      # - "required": Always require (most secure)
      # - "preferred": Request but don't fail if unavailable
      # - "discouraged": Skip if possible
      user_verification = "preferred"

      # Resident key / Passkey support
      # - "required": Must be a passkey (stored on device, synced)
      # - "preferred": Request passkey, allow non-resident
      # - "discouraged": Traditional WebAuthn (key on server)
      resident_key = "preferred"

      # Allow multiple authenticators per user
      max_credentials = 10

      # Attestation (verify authenticator is genuine)
      # - "none": Don't verify (recommended for most apps)
      # - "indirect": Request but accept self-attestation
      # - "direct": Require full attestation chain
      attestation = "none"

      # Trusted authenticator AAGUIDs (optional, for enterprise)
      # Only allow specific hardware keys
      # allowed_aaguids = ["2fc0579f-8113-47ea-b116-bb5a8db9202a"]  # YubiKey 5
    }

    sms {
      provider = "twilio"

      twilio {
        account_sid = env("TWILIO_ACCOUNT_SID")
        auth_token  = env("TWILIO_AUTH_TOKEN")
        from_number = env("TWILIO_FROM_NUMBER")
      }

      code_length = 6
      expiry      = "5m"
      rate_limit  = "3/hour"
    }

    email {
      connector = connector.email
      template  = "mfa_code"

      code_length = 6
      expiry      = "10m"
      rate_limit  = "5/hour"
    }

    push {
      provider = "firebase"

      firebase {
        credentials = file("./firebase-credentials.json")
      }

      expiry = "2m"
    }
  }
}
```

### Common MFA Configurations

#### Enterprise: Hardware Keys Only (YubiKey, Titan)
```hcl
auth {
  mfa {
    required = true
    methods  = ["webauthn"]

    webauthn {
      rp_name = "Acme Corp"
      rp_id   = "acme.com"
      origins = ["https://acme.com"]

      # Only allow external hardware keys
      authenticator_attachment = "cross-platform"
      user_verification = "required"
      resident_key = "discouraged"

      # Only allow approved hardware keys (optional)
      allowed_aaguids = [
        "2fc0579f-8113-47ea-b116-bb5a8db9202a",  # YubiKey 5 NFC
        "fa2b99dc-9e39-4257-8f92-4a30d23c4118",  # YubiKey 5 FIPS
        "ee882879-721c-4913-9775-3dfcce97072a",  # YubiKey 5C
      ]
    }
  }
}
```

#### Consumer App: Biometrics (FaceID, TouchID, Windows Hello)
```hcl
auth {
  mfa {
    required = false  # Optional for consumer apps
    methods  = ["webauthn", "totp"]

    webauthn {
      rp_name = "My App"
      rp_id   = "myapp.com"
      origins = ["https://myapp.com", "https://app.myapp.com"]

      # Prefer built-in biometrics
      authenticator_attachment = "platform"
      user_verification = "required"  # Always require biometric
      resident_key = "preferred"
    }

    totp {
      issuer = "My App"
    }
  }
}
```

#### Modern: Passkeys (Passwordless)
```hcl
auth {
  # Passkeys can replace passwords entirely
  password {
    required = false  # Allow passwordless registration
  }

  mfa {
    required = true
    methods  = ["webauthn"]

    webauthn {
      rp_name = "Modern App"
      rp_id   = "modern.app"
      origins = ["https://modern.app"]

      # Allow any authenticator (hardware, biometric, passkey)
      authenticator_attachment = "any"
      user_verification = "required"

      # Require passkeys (synced credentials)
      resident_key = "required"
    }
  }
}
```

#### Maximum Security: Multiple Factors Required
```hcl
auth {
  mfa {
    required = true
    methods  = ["webauthn", "totp"]

    # Require BOTH a hardware key AND TOTP
    require_multiple = true
    min_factors = 2

    webauthn {
      rp_name = "High Security App"
      rp_id   = "secure.example.com"
      origins = ["https://secure.example.com"]
      authenticator_attachment = "cross-platform"  # Hardware key
      user_verification = "required"
    }

    totp {
      issuer = "High Security App"
    }
  }
}
```

### 5. Security Features

```hcl
auth {
  security {
    # Brute force protection
    brute_force {
      enabled      = true
      max_attempts = 5
      window       = "15m"
      lockout_time = "30m"
      track_by     = "ip+user"  # ip, user, ip+user

      # Progressive delays (optional)
      progressive_delay {
        enabled   = true
        initial   = "1s"
        multiplier = 2
        max       = "30s"
      }
    }

    # Impossible travel detection
    impossible_travel {
      enabled       = true
      max_speed_kmh = 900  # Anything faster triggers alert

      on_detect = "block"  # block, challenge, notify

      # Use IP geolocation
      geoip {
        database = "./GeoLite2-City.mmdb"
        # or
        api = "https://ipinfo.io/{ip}?token=${env('IPINFO_TOKEN')}"
      }
    }

    # Device binding / fingerprinting
    device_binding {
      enabled        = true
      trust_duration = "30d"
      max_devices    = 5

      # What to collect
      fingerprint = ["user_agent", "screen", "timezone", "language"]

      on_new_device = "challenge"  # allow, challenge, block, notify
    }

    # Token replay protection
    replay_protection {
      enabled = true
      window  = "5m"  # Reject tokens used within this window
    }

    # IP allowlist/blocklist
    ip_rules {
      allowlist = ["10.0.0.0/8", "192.168.0.0/16"]
      blocklist = ["1.2.3.4"]

      # Geo-blocking
      block_countries = ["XX", "YY"]
      allow_countries = ["US", "CA", "AR"]  # If set, only these allowed
    }

    # Rate limiting (separate from brute force)
    rate_limit {
      login    = "10/minute"
      register = "5/hour"
      refresh  = "30/minute"
      password_reset = "3/hour"
    }
  }
}
```

### 6. Session Management

```hcl
auth {
  sessions {
    # Limits
    max_active = 5  # Max concurrent sessions per user

    # Timeouts
    idle_timeout     = "30m"   # Inactive session expires
    absolute_timeout = "24h"   # Session expires regardless of activity

    # Features
    allow_list   = true  # GET /auth/sessions
    allow_revoke = true  # DELETE /auth/sessions/:id

    # Session data to store
    track = ["ip", "user_agent", "location", "device_id"]

    # Behavior
    on_max_reached = "revoke_oldest"  # revoke_oldest, reject_new

    # Sliding window
    extend_on_activity = true
  }
}
```

### 7. Account Linking

```hcl
auth {
  account_linking {
    enabled  = true
    match_by = "email"  # email, phone, custom

    # Require email verification before linking
    require_verification = true

    # What happens when social login matches existing account
    on_match = "link"  # link, prompt, reject

    # Custom matching logic
    custom_match = "input.email == user.email && input.domain == user.company_domain"
  }
}
```

### 8. Hooks

```hcl
auth {
  hooks {
    # Before login (can reject)
    before_login {
      # Check if user is banned
      condition = "!user.is_banned"
      on_fail   = { status = 403, message = "Account suspended" }
    }

    # After successful login
    after_login {
      # Update last login
      connector.postgres = "UPDATE users SET last_login = NOW() WHERE id = ${user.id}"

      # Send notification for new device
      when = "session.is_new_device"
      connector.email = {
        to       = user.email
        template = "new_device_login"
        data     = { device = session.device, location = session.location }
      }
    }

    # After registration
    after_register {
      # Create default settings
      connector.postgres = "INSERT INTO user_settings (user_id) VALUES (${user.id})"

      # Send welcome email
      connector.email = { to = user.email, template = "welcome" }

      # Notify admin
      connector.slack = { channel = "#signups", message = "New user: ${user.email}" }
    }

    # On failed login
    on_failed_login {
      # Log attempt
      connector.audit_db = {
        table = "failed_logins"
        data  = { email = input.email, ip = request.ip, reason = error.reason }
      }
    }

    # On suspicious activity
    on_suspicious_activity {
      connector.slack = {
        channel = "#security"
        message = "Alert: ${event.type} for user ${user.id} from ${event.ip}"
      }

      connector.email = {
        to       = user.email
        template = "security_alert"
      }
    }

    # Before password change
    before_password_change {
      # Require current password
      condition = "verify_password(input.current_password, user.password_hash)"
      on_fail   = { status = 400, message = "Current password incorrect" }
    }

    # After password change
    after_password_change {
      # Revoke all other sessions
      revoke_other_sessions = true

      # Notify user
      connector.email = { to = user.email, template = "password_changed" }
    }
  }
}
```

### 9. Audit Logging

```hcl
auth {
  audit {
    enabled   = true
    connector = connector.audit_db
    table     = "auth_audit_log"

    # Events to log
    events = [
      "login",
      "logout",
      "failed_login",
      "register",
      "password_change",
      "password_reset_request",
      "password_reset_complete",
      "mfa_setup",
      "mfa_verify",
      "mfa_disable",
      "session_revoke",
      "token_refresh",
      "account_locked",
      "suspicious_activity",
    ]

    # What to include in each log entry
    include = [
      "user_id",
      "email",
      "ip",
      "user_agent",
      "location",
      "device_id",
      "timestamp",
      "success",
      "error_reason",
    ]

    # Retention
    retention = "90d"

    # Real-time streaming (optional)
    stream_to = connector.kafka
  }
}
```

## Exposed Endpoints

All endpoints are automatically created based on configuration:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/login` | POST | Login with credentials |
| `/auth/logout` | POST | Logout current session |
| `/auth/register` | POST | Register new user |
| `/auth/refresh` | POST | Refresh access token |
| `/auth/me` | GET | Get current user info |
| `/auth/forgot-password` | POST | Request password reset |
| `/auth/reset-password` | POST | Reset password with token |
| `/auth/change-password` | POST | Change password (authenticated) |
| `/auth/sessions` | GET | List active sessions |
| `/auth/sessions/:id` | DELETE | Revoke a session |
| `/auth/mfa/setup` | POST | Setup MFA |
| `/auth/mfa/verify` | POST | Verify MFA code |
| `/auth/mfa/disable` | POST | Disable MFA |
| `/auth/mfa/recovery` | POST | Use recovery code |
| `/auth/social/:provider` | GET | Initiate social login |
| `/auth/social/:provider/callback` | GET | Social login callback |
| `/auth/sso/:provider` | GET | Initiate SSO |
| `/auth/sso/:provider/callback` | POST | SSO callback (SAML) |
| `/auth/verify-email` | POST | Verify email address |
| `/auth/resend-verification` | POST | Resend verification email |

## Middleware Integration

The auth system automatically provides middleware for protecting other endpoints:

```hcl
connector "api" {
  type = "rest"
  port = 8080

  # Apply auth middleware
  auth {
    # Use the auth configuration
    required = true

    # Except for these paths
    exclude = ["/health", "/metrics", "/public/*"]

    # Require specific roles for paths
    rules {
      "/admin/*" = { roles = ["admin"] }
      "/api/users" = { roles = ["admin", "user_manager"] }
      "/api/reports" = { permissions = ["reports:read"] }
    }
  }
}
```

In flows, access user info via `auth`:

```hcl
flow "get_my_orders" {
  from { connector.api = "GET /orders" }
  to   {
    connector.postgres = "orders"
    where = "user_id = ${auth.user_id}"
  }
}
```

## Implementation Plan

### Phase 5.1a: Core Auth
- [ ] Auth config parser
- [ ] Local user storage
- [ ] Password hashing (argon2id)
- [ ] JWT generation/validation
- [ ] Basic endpoints (login/logout/register/refresh/me)
- [ ] Presets (strict, standard, relaxed, development)

### Phase 5.1b: Security Features
- [ ] Brute force protection
- [ ] Session management
- [ ] Token rotation
- [ ] Rate limiting per endpoint

### Phase 5.1c: MFA
- [ ] TOTP support (Google Authenticator, Authy, etc.)
- [ ] Recovery codes
- [ ] WebAuthn/FIDO2:
  - [ ] Hardware security keys (YubiKey, Titan, Feitian, SoloKey)
  - [ ] Platform biometrics (FaceID, TouchID, Windows Hello, Android fingerprint)
  - [ ] Passkeys (iCloud Keychain, Google Password Manager, 1Password)
  - [ ] Attestation verification (enterprise)
- [ ] SMS (Twilio integration)
- [ ] Email codes

### Phase 5.1d: External Providers
- [ ] External API provider
- [ ] Social login (Google, GitHub, Apple)
- [ ] OIDC integration
- [ ] SAML 2.0 integration

### Phase 5.1e: Advanced Security
- [ ] Impossible travel detection
- [ ] Device binding
- [ ] Replay protection
- [ ] IP rules (allowlist/blocklist/geo)

### Phase 5.1f: Audit & Hooks
- [ ] Audit logging
- [ ] Hook system
- [ ] Real-time event streaming

## Dependencies

```go
// Core
"github.com/golang-jwt/jwt/v5"     // JWT handling
"golang.org/x/crypto/argon2"       // Password hashing
"golang.org/x/crypto/bcrypt"       // Fallback hashing

// MFA
"github.com/pquerna/otp"           // TOTP
"github.com/go-webauthn/webauthn"  // WebAuthn/Passkeys

// External providers
"golang.org/x/oauth2"              // OAuth2 flows
"github.com/crewjam/saml"          // SAML 2.0
"github.com/coreos/go-oidc/v3"     // OIDC

// Security
"github.com/oschwald/geoip2-golang" // GeoIP for impossible travel

// SMS
"github.com/twilio/twilio-go"       // Twilio SMS
```

## Example: Complete Auth Microservice

```hcl
# config.hcl
service {
  name = "auth-service"
  port = 8080
}

# connectors/database.hcl
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = env("DB_HOST")
  database = env("DB_NAME")
  user     = env("DB_USER")
  password = env("DB_PASSWORD")
}

connector "redis" {
  type    = "cache"
  driver  = "redis"
  address = env("REDIS_URL")
}

# auth/config.hcl
auth {
  preset = "strict"

  storage {
    driver  = "redis"
    address = env("REDIS_URL")
  }

  users {
    connector = connector.postgres
    table     = "users"
  }

  jwt {
    algorithm = "RS256"
    private_key = file("./keys/private.pem")
    public_key  = file("./keys/public.pem")
    access_lifetime  = "15m"
    refresh_lifetime = "7d"
    issuer = "auth.example.com"
  }

  mfa {
    required = true
    methods  = ["totp", "webauthn"]
  }

  social {
    google {
      client_id     = env("GOOGLE_CLIENT_ID")
      client_secret = env("GOOGLE_CLIENT_SECRET")
    }
  }

  sso {
    oidc "okta" {
      issuer        = env("OKTA_ISSUER")
      client_id     = env("OKTA_CLIENT_ID")
      client_secret = env("OKTA_CLIENT_SECRET")
    }
  }

  hooks {
    after_register {
      connector.email = { to = user.email, template = "welcome" }
    }
  }

  audit {
    enabled   = true
    connector = connector.postgres
  }
}
```

This configuration gives you a complete authentication microservice with:
- User registration/login
- JWT with RS256
- Refresh token rotation
- MFA (TOTP + WebAuthn)
- Google social login
- Okta SSO
- Strict security (brute force, device binding, etc.)
- Audit logging
- Welcome email on registration
