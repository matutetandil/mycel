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
    connector = connector.postgres
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

  # Password history (prevent reuse)
  history = 5

  # Breach check (haveibeenpwned)
  breach_check = true
}
```

### Security Features

```hcl
security {
  brute_force {
    enabled      = true
    max_attempts = 5
    window       = "15m"
    lockout_time = "30m"
    track_by     = "ip+user"  # "ip", "user", "ip+user"
  }

  replay_protection {
    enabled = true
    window  = "5m"
  }

  impossible_travel {
    enabled           = true
    max_speed_kmh     = 1000
    alert_only        = false  # true = alert but allow
  }

  device_binding {
    enabled = true
    fields  = ["user_agent", "screen_resolution"]
  }
}
```

### Session Management

```hcl
sessions {
  max_active       = 5           # Max concurrent sessions
  idle_timeout     = "1h"        # Logout after inactivity
  absolute_timeout = "24h"       # Force logout after this time

  allow_list       = true        # Enable session listing
  allow_revoke     = true        # Enable session revocation

  on_max_reached   = "revoke_oldest"  # "deny", "revoke_oldest", "revoke_all"
}
```

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
    rp_id           = "myapp.com"
    rp_name         = "My Application"
    rp_origins      = ["https://myapp.com"]
    attestation     = "none"  # "none", "indirect", "direct"
    user_verification = "preferred"
  }

  # Recovery codes
  recovery_codes {
    enabled = true
    count   = 10
    length  = 8
  }
}
```

### SSO Configuration

```hcl
sso {
  linking {
    enabled              = true
    match_by             = "email"    # "email", "none"
    require_verification = true       # Require verified email
    on_match             = "link"     # "link", "prompt", "reject"
  }
}
```

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

### User Storage

```hcl
users {
  connector = connector.postgres
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
  connector = connector.postgres
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
  login    { path = "/login",    method = "POST", enabled = true }
  logout   { path = "/logout",   method = "POST", enabled = true }
  register { path = "/register", method = "POST", enabled = true }
  refresh  { path = "/refresh",  method = "POST", enabled = true }
  me       { path = "/me",       method = "GET",  enabled = true }

  # Sessions
  sessions_list   { path = "/sessions",     method = "GET",    enabled = true }
  sessions_revoke { path = "/sessions/:id", method = "DELETE", enabled = true }

  # Password
  password_change { path = "/change-password", method = "POST", enabled = true }
  password_reset  { path = "/reset-password",  method = "POST", enabled = false }

  # MFA
  mfa_setup    { path = "/mfa/setup",    method = "POST", enabled = true }
  mfa_verify   { path = "/mfa/verify",   method = "POST", enabled = true }
  mfa_disable  { path = "/mfa/disable",  method = "POST", enabled = true }

  # SSO
  sso_start    { path = "/sso/:provider",      method = "GET",    enabled = true }
  sso_callback { path = "/callback/:provider", method = "GET",    enabled = true }

  # Account linking
  link_account   { path = "/link/:provider",   method = "POST",   enabled = true }
  unlink_account { path = "/unlink/:provider", method = "DELETE", enabled = true }
  linked_list    { path = "/linked-accounts",  method = "GET",    enabled = true }
}
```

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
  metadata JSONB
);

-- Password history (for reuse prevention)
CREATE TABLE password_history (
  id SERIAL PRIMARY KEY,
  user_id VARCHAR(64) NOT NULL REFERENCES users(id),
  password_hash VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
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
| `/auth/sso/:provider` | GET | Start SSO flow |
| `/auth/callback/:provider` | GET/POST | OAuth callback |
| `/auth/link/:provider` | POST | Link social account |
| `/auth/unlink/:provider` | DELETE | Unlink social account |
| `/auth/linked-accounts` | GET | List linked accounts |

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

See [examples/auth](../examples/auth) for a complete working example.
