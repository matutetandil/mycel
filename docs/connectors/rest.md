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
    count    = 3
    interval = "1s"
    backoff  = 2.0
  }
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | — | Base URL for all requests |
| `timeout` | duration | `"30s"` | Request timeout |
| `auth.type` | string | — | Auth method: `bearer`, `api_key`, `basic`, `oauth2` |
| `retry.count` | int | `0` | Max retry attempts |
| `retry.interval` | duration | `"1s"` | Initial retry interval |
| `retry.backoff` | float | `2.0` | Backoff multiplier |

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

---

> **Full configuration reference:** See [REST Server](../reference/configuration.md#rest-server) in the Configuration Reference.
