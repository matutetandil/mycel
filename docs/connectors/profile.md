# Profile (Multi-backend Routing)

A profile connector wraps multiple backend connectors behind a single logical name. Use it when the same data can come from different sources depending on environment, configuration, or availability — for example, fetching prices from a Magento API in production and a local database in development.

## Configuration

```hcl
connector "pricing" {
  type = "profiled"

  select   = "env('PRICE_SOURCE')"    # CEL expression to pick active profile
  default  = "magento"                 # Fallback if select returns empty
  fallback = ["erp", "legacy"]         # Try these if the active profile fails

  profile "magento" {
    type     = "http"
    driver   = "client"
    base_url = "http://magento/api"

    auth {
      type  = "bearer"
      token = env("MAGENTO_TOKEN")
    }

    transform {
      product_id = "input.entity_id"
      sku        = "input.sku"
      price      = "double(input.price)"
      source     = "'magento'"
    }
  }

  profile "erp" {
    type     = "database"
    driver   = "postgres"
    host     = env("ERP_HOST")
    database = "erp"
    user     = env("ERP_USER")
    password = env("ERP_PASSWORD")

    transform {
      product_id = "string(input.id)"
      sku        = "input.codigo"
      price      = "input.precio"
      source     = "'erp'"
    }
  }
}
```

## Options

| Option | Type | Description |
|--------|------|-------------|
| `select` | string | CEL expression to determine the active profile |
| `default` | string | Default profile when `select` returns empty |
| `fallback` | list | Ordered fallback chain if the active profile fails |

Each `profile` block uses standard connector options for its type (`http`, `database`, `graphql`, etc.) plus an optional `transform` block to normalize responses to a common format.

## Fallback Behavior

When the active profile fails with a retriable error (connection timeout, 5xx):
1. Mycel tries the next profile in the `fallback` list
2. If all profiles fail, the last error is returned
3. Non-retriable errors (4xx, validation) do not trigger fallback

## Metrics

| Metric | Description |
|--------|-------------|
| `mycel_connector_profile_active` | Currently active profile |
| `mycel_connector_profile_requests_total` | Requests per profile |
| `mycel_connector_profile_errors_total` | Errors per profile |
| `mycel_connector_profile_fallback_total` | Fallback events |

## Example

```hcl
flow "get_product_price" {
  from { connector = "api", operation = "GET /products/:sku/price" }
  to   { connector = "pricing" }
}
```

The flow always targets `pricing` — the profile connector handles backend selection, fallback, and response normalization transparently.

See the [profiles example](../../examples/profiles/) for a complete working setup.
