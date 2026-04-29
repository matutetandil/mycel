# REST

Expose HTTP endpoints (server) or call external REST APIs (client). The server connector is the most common way to create a Mycel microservice — it receives HTTP requests and triggers flows. The client connector calls external APIs as a step or target.

## Server Configuration

```hcl
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    headers = ["Content-Type", "Authorization"]
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | int | — | Listen port |
| `cors.origins` | list | — | Allowed CORS origins |
| `cors.methods` | list | — | Allowed HTTP methods |
| `cors.headers` | list | — | Allowed headers |

## Client Configuration

```hcl
connector "external_api" {
  type     = "http"
  base_url = "https://api.example.com"
  timeout  = "30s"

  auth {
    type  = "bearer"    # "bearer", "api_key", "basic", "oauth2"
    token = env("API_TOKEN")
  }

  retry {
    attempts = 3
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | — | Base URL for all requests |
| `timeout` | duration | `"30s"` | Request timeout |
| `auth.type` | string | — | Auth method: `bearer`, `api_key`, `basic`, `oauth2` |
| `retry.attempts` | int | `1` | Maximum retry attempts. The connector applies a fixed exponential backoff. |
| `retry_count` | int | — | Shorthand for `retry { attempts = N }`. |
| `tls.ca_cert` | string | — | Path to a custom CA certificate (PEM) used to verify the server. |
| `tls.client_cert` | string | — | Path to client certificate (PEM) for mTLS. |
| `tls.client_key` | string | — | Path to client private key (PEM) for mTLS. |
| `tls.insecure_skip_verify` | bool | `false` | Disable TLS certificate verification. **Dev only — never use in production.** |

### TLS

For HTTPS endpoints whose certificate is signed by a private CA (e.g. an internal corporate CA, or a `mkcert`-signed dev proxy) point the connector at the CA bundle:

```hcl
connector "internal_api" {
  type     = "http"
  base_url = "https://internal.example.com"

  tls {
    ca_cert = "/etc/ssl/private-ca.pem"
  }
}
```

For mutual TLS, add the client certificate pair:

```hcl
tls {
  ca_cert     = "/etc/ssl/ca.pem"
  client_cert = "/etc/ssl/client.pem"
  client_key  = "/etc/ssl/client.key"
}
```

For local development against a self-signed certificate (e.g. an `nginx-proxy` container in `docker compose`), skip verification entirely:

```hcl
connector "magento" {
  type     = "http"
  base_url = env("MAGENTO_BASE_URL")

  tls {
    insecure_skip_verify = true   # dev only
  }
}
```

When `insecure_skip_verify` is enabled, Mycel logs a single `WARN` at connector startup with the connector name and base URL — loud enough that an accidental production deploy is obvious in the logs.

### Wrapping the request body — `envelope`

Some REST frameworks (Magento webapi, Spring `@RequestBody`, several SOAP-derived REST APIs) require the request body nested under a single root key matching the service method's parameter name:

```json
{ "productData": { "style_number": "AI02LT", "name": "..." } }
```

Rather than wrap the body inside a CEL map literal, set `envelope` on the `to` block. The transform stays clean — one line per attribute — and Mycel wraps the entire transform output under the named key just before it reaches the connector:

```hcl
flow "magento_create_style" {
  from {
    connector = "rabbit"
    target    = "all.in.magento.q"
  }

  transform {
    style_number = "input.body.payload.styleNumber"
    name         = "coalesce(input.body.payload.styleName, '')"
    websites     = "input.body.payload.websites"
    # ...30 more lines, one mapping each
  }

  to {
    connector = "magento"
    target    = "/rest/V1/mercury/products/styles"
    operation = "POST"
    envelope  = "productData"
  }
}
```

`envelope` also works on `step` blocks for intermediate HTTP calls that need the same shape. The wrap is a single key with the entire payload as its value — chained / nested wrappers are not supported (parenthesize manually with another transform if you need them).

## Operations

**Server (source):** Any HTTP method + path pattern — `GET /users`, `POST /users`, `PUT /users/:id`, `DELETE /users/:id`.

**Client (target):** Same method + path syntax, resolved against `base_url`.

## Example

```hcl
flow "list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  transform {
    id         = "uuid()"
    email      = "lower(input.email)"
    created_at = "now()"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
```

See the [basic example](../../examples/basic/) for a complete working setup.

## File Upload (multipart/form-data)

The REST server connector auto-detects `multipart/form-data` requests and parses file uploads. The maximum upload size is 32MB.

Each uploaded file is encoded as a map with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `filename` | string | Original file name |
| `content_type` | string | MIME type (e.g., `image/png`) |
| `size` | int | File size in bytes |
| `data` | string | File content encoded as base64 |

Files are available in transforms as `input.files.<field_name>`, where `<field_name>` matches the form field name used in the multipart request. Regular (non-file) form fields are available as `input.<field_name>`.

### Example

```hcl
flow "upload_avatar" {
  from {
    connector = "api"
    operation = "POST /users/:id/avatar"
  }

  transform {
    user_id      = "input.params.id"
    filename     = "input.files.avatar.filename"
    content_type = "input.files.avatar.content_type"
    size         = "input.files.avatar.size"
    data         = "input.files.avatar.data"
    uploaded_at  = "now()"
  }

  to {
    connector = "db"
    operation = "INSERT user_avatars"
  }
}
```

Upload with curl:

```bash
curl -X POST http://localhost:3000/users/42/avatar \
  -F "avatar=@photo.jpg"
```

---

> **Full configuration reference:** See [REST Server](../reference/configuration.md#rest-server) in the Configuration Reference.
