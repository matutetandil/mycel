# Phase 4.3: Connector Profiles

> **Status:** Specification Ready
> **Priority:** High
> **Use Case:** Same API, different data sources based on configuration

## Overview

Connector Profiles allow a single logical connector to have multiple backend implementations. The active profile is selected via environment variable, independent of `MYCEL_ENV`.

**Real-world example:** A pricing microservice that exposes `GET /prices/:id` but fetches data from:
- Magento API in some deployments
- ERP database in others
- Legacy API as fallback

Each backend returns data in different formats, but the flow always receives normalized data.

## Syntax

### Basic Profile Definition

```hcl
connector "prices" {
  # Environment variable that selects the active profile
  select = env("PRICE_SOURCE")  # Value: "magento", "erp", or "legacy"

  profile "magento" {
    type     = "http"
    base_url = env("MAGENTO_URL")

    auth {
      type  = "bearer"
      token = env("MAGENTO_TOKEN")
    }

    # Transform normalizes Magento's response format
    transform {
      price          = "float(input.special_price ?? input.price)"
      original_price = "float(input.price)"
      currency       = "input.currency"
      sku            = "input.sku"
    }
  }

  profile "erp" {
    type     = "database"
    driver   = "postgres"
    host     = env("ERP_DB_HOST")
    database = "erp"
    user     = env("ERP_DB_USER")
    password = env("ERP_DB_PASSWORD")

    # Transform normalizes ERP's column names (Spanish)
    transform {
      price          = "input.precio_oferta ?? input.precio_base"
      original_price = "input.precio_base"
      currency       = "'ARS'"
      sku            = "input.codigo_producto"
    }
  }

  profile "legacy" {
    type     = "http"
    base_url = env("LEGACY_API_URL")

    # Transform normalizes deeply nested legacy structure
    transform {
      price          = "input.data.pricing.current"
      original_price = "input.data.pricing.list"
      currency       = "input.data.pricing.currency_code"
      sku            = "input.data.product.sku"
    }
  }
}
```

### Flow Usage (Unchanged)

```hcl
flow "get_price" {
  from { connector = "api", operation = "GET /prices/:id" }
  to   { connector = "prices", target = "products" }
}

flow "get_price_enriched" {
  from { connector = "api", operation = "GET /products/:id/full" }

  enrich "pricing" {
    connector = "prices"
    operation = "get"
    params { sku = "input.sku" }
  }

  to { connector = "db", target = "products" }

  transform {
    id       = "input.id"
    name     = "input.name"
    # Data is already normalized by the profile's transform
    price    = "enriched.pricing.price"
    currency = "enriched.pricing.currency"
  }
}
```

### Fallback Chain

```hcl
connector "prices" {
  select   = env("PRICE_SOURCE")
  fallback = ["erp", "legacy"]  # Ordered list of fallback profiles

  profile "magento" { ... }
  profile "erp" { ... }
  profile "legacy" { ... }
}
```

When the active profile fails (connection error, timeout, etc.), Mycel automatically tries the next profile in the fallback chain.

### Default Profile

```hcl
connector "prices" {
  select  = env("PRICE_SOURCE")
  default = "erp"  # Used when PRICE_SOURCE is not set

  profile "magento" { ... }
  profile "erp" { ... }
}
```

## Configuration Reference

### Connector-level attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `select` | string (CEL) | Yes | Expression that returns the profile name to use |
| `default` | string | No | Profile to use when `select` evaluates to empty/null |
| `fallback` | list(string) | No | Ordered list of profiles to try if active profile fails |

### Profile-level attributes

Each profile contains:
1. **All standard connector attributes** for that connector type (type, driver, host, auth, etc.)
2. **Optional `transform` block** to normalize the response

### Transform in Profiles

The profile transform:
- **On read (source):** Normalizes the response before passing to the flow
- **On write (target):** Transforms the data before sending to the backend

```hcl
profile "magento" {
  type = "http"
  base_url = "..."

  # Applied after receiving response (read) or before sending (write)
  transform {
    # CEL expressions with 'input' as the raw data
    normalized_field = "input.weird_field_name"
  }
}
```

## Behavior

### Profile Selection Flow

```
1. Evaluate `select` expression
2. If result is empty/null and `default` is set, use default
3. If result is empty/null and no default, error at startup
4. Look up profile by name
5. If profile not found, error at startup
```

### Fallback Behavior

```
1. Try active profile
2. On failure (connection error, timeout, 5xx):
   a. Log warning with error details
   b. Try next profile in fallback list
   c. If all fail, return error to flow
3. On success, return normalized data
```

**Note:** Fallback is NOT triggered by:
- 4xx errors (client errors - these are expected)
- Empty results (valid response)
- Transform errors (configuration issue)

### Circuit Breaker Integration

When circuit breaker is enabled globally, each profile has its own circuit breaker state:

```
Profile "magento" circuit: CLOSED
Profile "erp" circuit: OPEN (failures: 5)
Profile "legacy" circuit: HALF-OPEN
```

If active profile's circuit is OPEN, immediately try fallback without waiting.

## Examples

### Multi-region Deployment

```hcl
connector "inventory" {
  select  = env("REGION")
  default = "us-east"

  profile "us-east" {
    type     = "http"
    base_url = "https://inventory-us-east.internal"
  }

  profile "us-west" {
    type     = "http"
    base_url = "https://inventory-us-west.internal"
  }

  profile "eu" {
    type     = "http"
    base_url = "https://inventory-eu.internal"
  }
}
```

### Read vs Write Backends

```hcl
connector "products" {
  select = env("PRODUCT_SOURCE")

  profile "read_replica" {
    type     = "database"
    driver   = "postgres"
    host     = env("DB_READ_REPLICA_HOST")
    # Read-only replica for queries
  }

  profile "primary" {
    type     = "database"
    driver   = "postgres"
    host     = env("DB_PRIMARY_HOST")
    # Primary for writes
  }
}

# Use read replica for GET
flow "list_products" {
  from { connector = "api", operation = "GET /products" }
  to   { connector = "products", target = "products" }
  # PRODUCT_SOURCE=read_replica
}

# Use primary for writes
flow "create_product" {
  from { connector = "api", operation = "POST /products" }
  to   { connector = "products", target = "products" }
  # PRODUCT_SOURCE=primary (or use a different connector)
}
```

### Migration Between Systems

```hcl
connector "orders" {
  select   = env("ORDER_SYSTEM")
  default  = "legacy"
  fallback = ["legacy"]  # Always fall back to legacy during migration

  profile "new_system" {
    type     = "http"
    base_url = env("NEW_ORDER_API")

    transform {
      order_id = "input.id"
      total    = "input.total_amount"
      status   = "input.order_status"
    }
  }

  profile "legacy" {
    type     = "database"
    driver   = "mysql"
    host     = env("LEGACY_DB_HOST")

    transform {
      order_id = "string(input.ORDER_ID)"
      total    = "float(input.TOTAL_AMT) / 100"  # Stored as cents
      status   = "input.STATUS_CODE == 1 ? 'active' : 'closed'"
    }
  }
}
```

## Metrics

Prometheus metrics for profile usage:

```
# Active profile
mycel_connector_profile_active{connector="prices", profile="magento"} 1

# Requests per profile
mycel_connector_profile_requests_total{connector="prices", profile="magento"} 1234
mycel_connector_profile_requests_total{connector="prices", profile="erp"} 56

# Fallback events
mycel_connector_profile_fallback_total{connector="prices", from="magento", to="erp"} 12

# Profile errors
mycel_connector_profile_errors_total{connector="prices", profile="magento", error="timeout"} 3
```

## Implementation Notes

### Parser Changes

1. Detect `profile` blocks inside connector
2. Parse each profile as a "virtual connector" with its own type
3. Store profiles in connector config
4. Validate `select`, `default`, `fallback` attributes

### Runtime Changes

1. `ConnectorFactory` creates a `ProfiledConnector` wrapper
2. `ProfiledConnector` implements the `Connector` interface
3. On each operation, resolve active profile and delegate
4. Apply profile transform before returning/sending data

### Data Flow

```
Request → Flow → ProfiledConnector
                      ↓
              Resolve active profile
                      ↓
              Get underlying connector
                      ↓
              Execute operation
                      ↓
              Apply profile transform
                      ↓
              Return normalized data
```

## Migration Path

Existing connectors without profiles continue to work unchanged. Profiles are opt-in.

```hcl
# This still works (no profiles)
connector "db" {
  type   = "database"
  driver = "postgres"
  host   = env("DB_HOST")
}

# This adds profiles (new feature)
connector "prices" {
  select = env("PRICE_SOURCE")
  profile "a" { ... }
  profile "b" { ... }
}
```
