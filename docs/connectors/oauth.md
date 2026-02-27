# OAuth (Social Login)

Declarative social login as a standard Mycel connector. Instead of writing callback handlers and managing tokens, you declare a provider and wire the authorize/callback cycle with flows. Use it for "Login with Google/GitHub/Apple" or any OAuth2-compatible identity provider.

## Configuration

```hcl
connector "google" {
  type   = "oauth"
  driver = "google"

  client_id     = env("GOOGLE_CLIENT_ID")
  client_secret = env("GOOGLE_CLIENT_SECRET")
  redirect_uri  = "http://localhost:3000/auth/google/callback"
  scopes        = ["openid", "email", "profile"]
}
```

| Option | Type | Description |
|--------|------|-------------|
| `driver` | string | Provider: `google`, `github`, `apple`, `oidc`, `custom` |
| `client_id` | string | OAuth2 client ID |
| `client_secret` | string | OAuth2 client secret |
| `redirect_uri` | string | Callback URL |
| `scopes` | list | Requested scopes |

For `oidc` driver, add `issuer` (discovery URL). For `custom` driver, add `auth_url`, `token_url`, `userinfo_url`.

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `authorize` | read | Generate CSRF state token and redirect URL |
| `callback` | read | Exchange authorization code for user info |
| `userinfo` | read | Fetch user profile with access token |
| `refresh` | read | Refresh an expired access token |

State management is automatic — each `authorize` generates a CSRF-safe state token with 10-minute expiry, and `callback` validates it before exchanging the code.

The `callback` operation returns: `email`, `name`, `picture`, `provider_id`, `access_token`, `refresh_token`.

## Example

```hcl
# Start login — redirects to Google's consent screen
flow "google_login" {
  from { connector = "api", operation = "GET /auth/google" }
  to   { connector = "google", operation = "authorize" }
}

# Handle callback — exchange code for user info, store in DB
flow "google_callback" {
  from { connector = "api", operation = "GET /auth/google/callback" }

  step "auth" {
    connector = "google"
    operation = "callback"
    params    = { code = "input.query.code", state = "input.query.state" }
  }

  transform {
    output.email       = "step.auth.email"
    output.name        = "step.auth.name"
    output.provider_id = "step.auth.provider_id"
  }

  to { connector = "db", target = "users" }
}
```

See the [oauth example](../../examples/oauth/) for a complete working setup.
