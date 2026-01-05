# Authentication Service Example

This example demonstrates how to create a complete authentication microservice using Mycel's declarative auth system.

## Features

- User registration and login
- JWT-based authentication with refresh tokens
- Password hashing (argon2id)
- Brute force protection
- Session management (list/revoke)
- MFA support (TOTP)
- Audit logging

## Quick Start

```bash
# Set environment variables
export JWT_SECRET="your-secret-key-here"
export DB_HOST="localhost"
export DB_NAME="auth"
export DB_USER="postgres"
export DB_PASSWORD="postgres"

# Start the service
mycel start --config ./examples/auth
```

## Endpoints

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

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_audit_user ON auth_audit_log(user_id);
CREATE INDEX idx_audit_event ON auth_audit_log(event);
```
