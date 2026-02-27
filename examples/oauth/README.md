# OAuth (Social Login) Example

Declarative social login with Google and GitHub — no callback handlers, just flows.

## What This Demonstrates

- **Authorize:** Generate OAuth2 auth URL with CSRF state protection
- **Callback:** Exchange authorization code for user info and store in database
- **Multiple providers:** Google and GitHub configured side by side

## Prerequisites

1. Create OAuth apps on [Google](https://console.developers.google.com/) and/or [GitHub](https://github.com/settings/developers)
2. Set the redirect URIs to `http://localhost:3000/auth/google/callback` and `http://localhost:3000/auth/github/callback`

Set credentials:

```bash
export GOOGLE_CLIENT_ID=your_google_client_id
export GOOGLE_CLIENT_SECRET=your_google_client_secret
export GITHUB_CLIENT_ID=your_github_client_id
export GITHUB_CLIENT_SECRET=your_github_client_secret
```

## Run

```bash
mycel start --config ./examples/oauth
```

## How It Works

1. User visits `/auth/google` — Mycel generates a state token and redirects to Google's consent screen
2. User authorizes — Google redirects back to `/auth/google/callback?code=...&state=...`
3. Mycel validates the state (CSRF protection), exchanges the code for an access token, fetches user info
4. The flow transforms the response and stores the user in the database

## Test

Start the Google login flow:

```bash
curl -v http://localhost:3000/auth/google
# Returns a redirect URL to Google's consent screen
```

After authorization, Google redirects back:

```bash
curl "http://localhost:3000/auth/google/callback?code=AUTH_CODE&state=STATE_TOKEN"
# Exchanges code for user info and stores in DB
```

Same flow for GitHub:

```bash
curl -v http://localhost:3000/auth/github
```

## Supported Drivers

| Driver | Provider | Notes |
|--------|----------|-------|
| `google` | Google OAuth2 | OpenID Connect, email + profile scopes |
| `github` | GitHub OAuth2 | `read:user` and `user:email` scopes |
| `apple` | Sign in with Apple | Requires key file and team ID |
| `oidc` | OpenID Connect | Any OIDC provider with `issuer_url` |
| `custom` | Custom OAuth2 | Manual `auth_url`, `token_url`, `userinfo_url` |

## Operations

| Operation | Description |
|-----------|-------------|
| `authorize` | Generate state + return auth URL as redirect |
| `callback` | Exchange code for tokens + fetch user info |
| `userinfo` | Fetch user profile with existing access token |
| `refresh` | Refresh an expired access token |
