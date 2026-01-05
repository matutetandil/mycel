# Data Enrichment Example

This example demonstrates how to use the `enrich` block to fetch data from external microservices during transformation.

## Overview

Data enrichment allows you to:
- Fetch prices from a pricing microservice
- Get inventory levels from a stock service
- Retrieve customer details from a CRM
- Combine data from multiple sources into a single response

## Files

- `config.hcl` - Service configuration
- `connectors.hcl` - Connector definitions (REST API, database, external services)
- `flows.hcl` - Flow definitions with enrichments
- `transforms.hcl` - Reusable transforms with built-in enrichments

## How It Works

### Flow-Level Enrichment

Add `enrich` blocks directly in a flow for specific use cases:

```hcl
flow "get_product_with_price" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  # Fetch price from external pricing microservice
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  # Use enriched data in transform
  transform {
    id       = "input.id"
    name     = "input.name"
    price    = "enriched.pricing.price"      # Access enriched data
    currency = "enriched.pricing.currency"
  }

  to {
    connector = "products_db"
    target    = "products"
  }
}
```

### Multiple Enrichments

Fetch from multiple services:

```hcl
flow "get_product_full" {
  from {
    connector = "api"
    operation = "GET /products/:id/full"
  }

  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params { product_id = "input.id" }
  }

  enrich "inventory" {
    connector = "inventory_service"
    operation = "GET /stock"
    params { sku = "input.sku" }
  }

  transform {
    id              = "input.id"
    name            = "input.name"
    price           = "enriched.pricing.price"
    stock_available = "enriched.inventory.available"
    in_stock        = "enriched.inventory.available > 0"
  }

  to { ... }
}
```

### Reusable Transforms with Enrichment

Put enrichments inside named transforms to reuse them:

```hcl
# transforms.hcl
transform "with_pricing" {
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params { product_id = "input.id" }
  }

  id       = "input.id"
  name     = "input.name"
  price    = "enriched.pricing.price"
  currency = "enriched.pricing.currency"
}
```

Then use in any flow:

```hcl
flow "get_product" {
  from { ... }

  transform {
    use = "transform.with_pricing"
    fetched_at = "now()"  # Add additional fields
  }

  to { ... }
}
```

## Connector Support

Enrichments work with any connector type:

| Connector | How it works |
|-----------|--------------|
| Database (SQLite, PostgreSQL) | Uses `Read()` to query |
| TCP | Uses `Call()` for RPC requests |
| HTTP | Uses `Call()` for HTTP requests |
| gRPC (coming soon) | Uses `Call()` for gRPC methods |

## CEL Expressions with Enriched Data

Access enriched data in CEL expressions:

```cel
# Simple access
enriched.pricing.price

# Nested access
enriched.product.category.name

# Array access
enriched.pricing.tiers[0].price

# Combine with input
double(input.quantity) * enriched.pricing.unit_price

# Conditional
enriched.inventory.available > 0 ? "In Stock" : "Out of Stock"
```

## Running This Example

This example requires external services to be running. For a complete working example, you would need:

1. A TCP pricing service on port 9001
2. An HTTP inventory service on port 8080
3. A SQLite database

```bash
# Start the service
mycel start --config ./examples/enrich
```

## Verify It Works

### 1. Start the service

```bash
mycel start --config ./examples/enrich
```

You should see:
```
INFO  Starting service: enrich-example
INFO  Loaded 4 connectors
INFO  Registered flows with enrichments
INFO  REST server listening on :3000
```

### 2. Test product with pricing (mock scenario)

```bash
curl http://localhost:3000/products/123
```

Expected response (with enriched pricing data):
```json
{
  "id": "123",
  "name": "Widget",
  "price": 29.99,
  "currency": "USD",
  "in_stock": true
}
```

### 3. What to check in logs

```
INFO  GET /products/123 → get_product_with_price
INFO    Enriching from: pricing_service
INFO    Enriching from: inventory_service
INFO  Response sent in 45ms
```

### Common Issues

**"Enrichment failed: connection refused"**

The external service is not running. For testing without external services, use mocks:

```bash
mycel start --config ./examples/enrich --mock=pricing_service,inventory_service
```

## See Also

- [Transformations Guide](../../docs/transformations.md) - Full CEL reference
- [TCP Example](../tcp/README.md) - TCP connector usage
