# OAuth Example

Social login with Google and GitHub via declarative OAuth flows.

## Setup

1. Create OAuth apps on Google/GitHub and get credentials.

2. Set environment variables:
   ```bash
   export GOOGLE_CLIENT_ID=your_google_client_id
   export GOOGLE_CLIENT_SECRET=your_google_client_secret
   export GITHUB_CLIENT_ID=your_github_client_id
   export GITHUB_CLIENT_SECRET=your_github_client_secret
   ```

3. Start Mycel:
   ```bash
   mycel start --config ./examples/oauth
   ```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /auth/google | Start Google login |
| GET | /auth/google/callback | Google callback |
| GET | /auth/github | Start GitHub login |
| GET | /auth/github/callback | GitHub callback |

## Supported Drivers

- `google` - Google OAuth2
- `github` - GitHub OAuth2
- `apple` - Sign in with Apple
- `oidc` - Generic OpenID Connect
- `custom` - Custom OAuth2 provider

## Supported Operations

- `authorize` - Generate auth URL + state
- `callback` - Exchange code for user info
- `userinfo` - Fetch profile with existing token
- `refresh` - Refresh expired token
