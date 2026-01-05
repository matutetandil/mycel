# Authentication Service Example

This example demonstrates how to create a complete authentication microservice using Mycel's declarative auth system.

## Features

- User registration and login
- JWT-based authentication with refresh tokens
- Password hashing (argon2id)
- Brute force protection
- Session management (list/revoke)
- MFA support (TOTP, WebAuthn/Passkeys)
- **SSO/Social Login** (Google, GitHub, Apple)
- **Enterprise OIDC** (Okta, Azure AD, Auth0)
- Account linking
- Audit logging

## Quick Start

```bash
# Set environment variables
export JWT_SECRET="your-secret-key-here"
export DB_HOST="localhost"
export DB_NAME="auth"
export DB_USER="postgres"
export DB_PASSWORD="postgres"

# For Social Login (optional)
export GOOGLE_CLIENT_ID="your-google-client-id"
export GOOGLE_CLIENT_SECRET="your-google-client-secret"
export GITHUB_CLIENT_ID="your-github-client-id"
export GITHUB_CLIENT_SECRET="your-github-client-secret"

# Start the service
mycel start --config ./examples/auth
```

## Endpoints

### Standard Auth

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/register` | POST | Register a new user |
| `/auth/login` | POST | Login and get tokens |
| `/auth/logout` | POST | Logout current session |
| `/auth/refresh` | POST | Refresh access token |
| `/auth/me` | GET | Get current user info |
| `/auth/sessions` | GET | List active sessions |
| `/auth/sessions/:id` | DELETE | Revoke a session |
| `/auth/change-password` | POST | Change password |

### SSO / Social Login

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/sso/:provider` | GET | Start SSO flow (redirects to provider) |
| `/auth/callback/:provider` | GET/POST | OAuth callback handler |
| `/auth/link/:provider` | POST | Link social account to existing user |
| `/auth/unlink/:provider` | DELETE | Unlink social account |
| `/auth/linked-accounts` | GET | List linked social accounts |

Supported providers: `google`, `github`, `apple`, or any configured OIDC provider name.

## API Examples

### Register

```bash
curl -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "MyP@ssw0rd!"}'
```

### Login

```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "MyP@ssw0rd!"}'
```

Response:
```json
{
  "user": {
    "id": "abc123",
    "email": "user@example.com"
  },
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

### Get Current User

```bash
curl http://localhost:8080/auth/me \
  -H "Authorization: Bearer <access_token>"
```

### Refresh Token

```bash
curl -X POST http://localhost:8080/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token": "<refresh_token>"}'
```

### List Sessions

```bash
curl http://localhost:8080/auth/sessions \
  -H "Authorization: Bearer <access_token>"
```

### Revoke Session

```bash
curl -X DELETE http://localhost:8080/auth/sessions/<session_id> \
  -H "Authorization: Bearer <access_token>"
```

### SSO - Start Google Login

```bash
# This will redirect to Google's login page
curl -L http://localhost:8080/auth/sso/google

# After Google authentication, user is redirected back to:
# /auth/callback/google?code=...&state=...
```

### SSO - List Linked Accounts

```bash
curl http://localhost:8080/auth/linked-accounts \
  -H "Authorization: Bearer <access_token>"
```

Response:
```json
{
  "accounts": [
    {
      "provider": "google",
      "email": "user@gmail.com",
      "linked_at": "2024-01-15T10:30:00Z"
    },
    {
      "provider": "github",
      "email": "user@users.noreply.github.com",
      "linked_at": "2024-01-16T14:20:00Z"
    }
  ]
}
```

### SSO - Unlink Account

```bash
curl -X DELETE http://localhost:8080/auth/unlink/github \
  -H "Authorization: Bearer <access_token>"
```

## Configuration

### Presets

The auth system comes with presets for common security levels:

| Preset | Description |
|--------|-------------|
| `strict` | Maximum security: MFA required, short tokens, strong passwords |
| `standard` | Balanced: MFA optional, 1h access tokens, moderate passwords |
| `relaxed` | Minimal: No MFA, long tokens, basic passwords |
| `development` | For dev: No security features, very long tokens |

### Security Features

- **Brute Force Protection**: Locks accounts after failed attempts
- **Token Rotation**: Refresh tokens are rotated on each use
- **Session Limits**: Maximum concurrent sessions per user
- **Replay Protection**: Prevents token reuse

## Database Schema

Create these tables in your PostgreSQL database:

```sql
CREATE TABLE users (
  id VARCHAR(64) PRIMARY KEY,
  email VARCHAR(255) UNIQUE NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE auth_audit_log (
  id SERIAL PRIMARY KEY,
  event VARCHAR(50) NOT NULL,
  user_id VARCHAR(64),
  email VARCHAR(255),
  ip VARCHAR(45),
  user_agent TEXT,
  success BOOLEAN,
  error_reason TEXT,
  created_at TIMESTAMP DEFAULT NOW()
);

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
  UNIQUE(provider, provider_id)
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_audit_user ON auth_audit_log(user_id);
CREATE INDEX idx_audit_event ON auth_audit_log(event);
CREATE INDEX idx_linked_user ON linked_accounts(user_id);
CREATE INDEX idx_linked_provider ON linked_accounts(provider, provider_id);
```

## SSO Provider Setup

### Google

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing
3. Enable "Google+ API" and "Google Identity"
4. Go to Credentials > Create OAuth 2.0 Client ID
5. Set authorized redirect URI: `http://localhost:8080/auth/callback/google`
6. Copy Client ID and Client Secret to environment variables

### GitHub

1. Go to [GitHub Developer Settings](https://github.com/settings/developers)
2. Create a new OAuth App
3. Set callback URL: `http://localhost:8080/auth/callback/github`
4. Copy Client ID and Client Secret to environment variables

### Apple

1. Go to [Apple Developer Portal](https://developer.apple.com/)
2. Create a Services ID and enable "Sign In with Apple"
3. Configure domains and return URLs
4. Create a key for Sign In with Apple
5. Set environment variables: `APPLE_CLIENT_ID`, `APPLE_TEAM_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY`

### Enterprise OIDC (Okta, Azure AD, Auth0)

Configure your identity provider and add to config.hcl:

```hcl
oidc "okta" {
  issuer        = "https://your-org.okta.com"
  client_id     = env("OKTA_CLIENT_ID")
  client_secret = env("OKTA_CLIENT_SECRET")
  scopes        = ["openid", "email", "profile"]
}
```
