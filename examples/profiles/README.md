# Profiles Example

This example demonstrates **connector profiles** - a feature that allows a single logical connector to have multiple backend implementations.

## Use Case

A pricing microservice needs to fetch product prices from different sources depending on deployment:
- **Development**: Magento API
- **Staging**: ERP Database
- **Production**: Legacy API with fallback chain

## Features Demonstrated

1. **Profile Selection**: Switch backends via environment variable
2. **Per-profile Transforms**: Normalize data from different sources
3. **Fallback Chains**: Automatic failover between backends

## Configuration

```hcl
connector "pricing" {
  type = "profiled"

  # Select profile via PRICE_SOURCE env var
  select  = "env('PRICE_SOURCE')"
  default = "magento"

  # Fallback chain if primary fails
  fallback = ["erp", "legacy"]

  profile "magento" {
    type     = "http"
    base_url = "http://magento/api"

    transform {
      product_id = "input.entity_id"
      price      = "double(input.price)"
    }
  }

  profile "erp" {
    type     = "database"
    driver   = "sqlite"
    database = "erp.db"

    transform {
      product_id = "string(input.id)"
      price      = "input.precio"
    }
  }
}
```

## Running

```bash
# Use Magento (default)
mycel start --config ./examples/profiles

# Use ERP database
PRICE_SOURCE=erp mycel start --config ./examples/profiles

# Use Legacy API
PRICE_SOURCE=legacy mycel start --config ./examples/profiles
```

## Response Format

Regardless of which backend is used, the response is always normalized:

```json
{
  "product_id": "123",
  "sku": "PROD-001",
  "price": 99.99,
  "currency": "USD",
  "source": "magento"
}
```

## Metrics

Profile usage is tracked in Prometheus metrics:
- `mycel_connector_profile_active` - Currently active profile
- `mycel_connector_profile_requests_total` - Requests per profile
- `mycel_connector_profile_errors_total` - Errors per profile
- `mycel_connector_profile_fallback_total` - Fallback events
