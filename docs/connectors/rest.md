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
