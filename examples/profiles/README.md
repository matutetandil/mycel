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

## Verify It Works

### 1. Start with default profile (Magento)

```bash
mycel start --config ./examples/profiles
```

You should see:
```
INFO  Starting service: profiles-example
INFO  Loaded connector: pricing (profiled)
INFO    Active profile: magento
INFO  REST server listening on :3000
```

### 2. Test the pricing endpoint

```bash
curl http://localhost:3000/products/123/price
```

Expected response (normalized from Magento):
```json
{
  "product_id": "123",
  "sku": "PROD-123",
  "price": 99.99,
  "currency": "USD",
  "source": "magento"
}
```

### 3. Switch to ERP profile

```bash
PRICE_SOURCE=erp mycel start --config ./examples/profiles
```

You should see:
```
INFO    Active profile: erp
```

Same request returns:
```json
{
  "product_id": "123",
  "sku": "PROD-123",
  "price": 99.99,
  "currency": "USD",
  "source": "erp"
}
```

### 4. Test fallback (if primary fails)

With `fallback = ["erp", "legacy"]`, if Magento is down:
```
WARN  Profile 'magento' failed: connection refused
INFO  Falling back to profile: erp
```

### 5. Check metrics

```bash
curl http://localhost:3000/metrics | grep profile
```

Expected:
```
mycel_connector_profile_active{connector="pricing",profile="magento"} 1
mycel_connector_profile_requests_total{connector="pricing",profile="magento"} 5
```

### Common Issues

**"Unknown profile: xxx"**

The `PRICE_SOURCE` value must match a profile name defined in the connector.

**"No profile selected"**

Either set `PRICE_SOURCE` or ensure `default` is set in the connector config.

**Transforms not applying**

Check that the profile's `transform {}` block uses correct field names for that backend.
