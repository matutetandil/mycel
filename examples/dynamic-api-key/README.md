# Dynamic API Key Validation Example

This example validates each request's API key **at request time** against an
external HTTP introspection endpoint, instead of a static list baked into the
config. The provider owns the keys (in a database, a secrets manager, an SSO
system — whatever it is); Mycel just asks it "is this credential valid, and who
is it?" on every request.

## Use Case

When you need to:

- Manage API keys outside the service (issue, rotate, revoke without a redeploy)
- Associate keys with users, tenants, roles, or arbitrary metadata
- Support expiration and immediate revocation
- Keep the validation logic (and the key store) in one central place

## How It Works

1. The client sends a request with an `Authorization: Bearer <key>` header.
2. The key is not a local JWT, so Mycel falls through to the configured
   `provider`, calling its `validate` URL with the key.
3. The provider responds with JSON; Mycel evaluates the `response` CEL
   expressions over `status` (the HTTP code) and `body` (the parsed JSON).
4. If `success` is true, the mapped fields populate the auth context, available
   to every flow as `auth.user_id` and `auth.claims.*`.

## The provider block

```hcl
auth {
  secret = env("AUTH_SECRET", "change-me-in-production")

  provider "api_keys" {
    type     = "http"
    validate = env("KEYS_VALIDATE_URL", "http://localhost:9100/introspect")

    # {token} is replaced with the incoming credential.
    request = {
      Authorization = "Bearer {token}"
    }

    # CEL expressions over `status` (HTTP code) and `body` (parsed JSON).
    response {
      success = "status == 200 && body.active == true"
      user_id = "body.user_id"
      email   = "body.email"
      roles   = "body.roles"
      # token = "body.session_id"   # optional, stored on the claims
    }
  }
}
```

A successful introspection response might look like:

```json
{ "active": true, "user_id": "u_123", "email": "ada@example.com",
  "roles": ["admin"], "tenant_id": "acme" }
```

The whole body is exposed as `auth.claims.*`, so `tenant_id` above is reachable
as `auth.claims.tenant_id` even though it isn't one of the explicitly mapped
fields.

## Auth context in flows

```hcl
transform {
  user_id   = "auth.user_id"           # mapped from response.user_id
  tenant_id = "auth.claims.tenant_id"  # any field from the response body
  role      = "auth.claims.role"
}
```

## Running

```bash
# Point at your introspection endpoint
export KEYS_VALIDATE_URL=https://auth.internal/introspect
export AUTH_SECRET=...

# Application database used by the flows
export DB_HOST=localhost DB_USER=mycel DB_PASSWORD=secret DB_NAME=api_keys

mycel start --config ./examples/dynamic-api-key

curl -H "Authorization: Bearer your-api-key" http://localhost:8080/me
```

## Notes & limits

- **Order:** local JWT validation runs first; providers are tried only when the
  credential isn't a valid JWT. Multiple providers are tried in declaration order.
- **Provider down:** a timeout or transport error is treated as a validation
  failure (the request is rejected), not a 5xx from your API.
- **No caching (yet):** every request hits the provider. A TTL cache is a planned
  addition.
- **`sync_to` is parsed but not executed yet** — setting it logs a warning. The
  field is reserved for mirroring the validated identity into a local store.

## Comparison: static vs dynamic keys

| Feature          | Static keys     | Dynamic (provider) |
|------------------|-----------------|--------------------|
| Where keys live  | In the HCL file | In the provider    |
| Rotation/revoke  | Requires redeploy | Immediate        |
| User association | Not supported   | Full support       |
| Expiration       | Not supported   | Provider-side      |
| Metadata/claims  | Not supported   | Full support       |

## Security notes

- Send the key in a header (`request = { Authorization = "Bearer {token}" }`),
  not in the URL.
- Put the introspection endpoint behind TLS.
- Add rate limiting to blunt brute-force attempts against the endpoint.
