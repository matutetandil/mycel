# Multi-Step Flows

Steps add multi-step orchestration to a flow. Each step calls a connector and makes its result available to subsequent steps and the final transform. Use steps when a single flow needs to assemble data from multiple sources before producing a response.

## Basic Step Flow

```hcl
flow "get_order_detail" {
  from { connector = "api", operation = "GET /orders/:id" }

  step "order" {
    connector = "db"
    operation = "query"
    query     = "SELECT * FROM orders WHERE id = ?"
    params    = [input.params.id]
  }

  step "customer" {
    connector = "customers_api"
    operation = "GET /customers/${step.order.customer_id}"
  }

  transform {
    id       = "step.order.id"
    status   = "step.order.status"
    customer = "step.customer"
  }

  to { connector = "api", target = "response" }
}
```

Step results are available as `step.NAME` in subsequent steps and in the transform block.

## Step Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `connector` | string | yes | Connector to call |
| `operation` | string | no | Endpoint, operation name, or method |
| `query` | string | no | SQL query (database connectors) |
| `target` | string | no | Table or resource name |
| `params` | map/list | no | Query parameters or request params |
| `body` | map | no | Request body (HTTP/gRPC connectors) |
| `when` | string | no | CEL condition — skip step if false |
| `timeout` | string | no | Step timeout: `"5s"`, `"30s"`, `"2m"` |
| `on_error` | string | no | `"skip"` — continue flow if step fails |
| `default` | any | no | Value to use when step is skipped or fails |
| `format` | string | no | Data format for this step (`json`, `xml`) |

## Conditional Steps

Skip expensive steps when their data is not needed:

```hcl
flow "get_product" {
  from { connector = "api", operation = "GET /products/:id" }

  step "product" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = ?"
    params    = [input.params.id]
  }

  step "inventory" {
    connector = "inventory_api"
    operation = "GET /stock/${step.product.sku}"
    when      = "step.product.track_inventory == true"
  }

  step "reviews" {
    connector = "reviews_api"
    operation = "GET /reviews/${input.params.id}"
    when      = "input.include_reviews == true"
  }

  transform {
    id        = "step.product.id"
    name      = "step.product.name"
    sku       = "step.product.sku"
    in_stock  = "step.inventory.available > 0"
    reviews   = "step.reviews"
  }

  to { connector = "api", target = "response" }
}
```

When `when` is false, the step is skipped entirely. Any subsequent `step.NAME` reference returns the `default` value or an empty map.

## Error Handling in Steps

### Skip on Error

Continue the flow even if a step fails:

```hcl
step "optional_data" {
  connector = "external_api"
  operation = "GET /extras/${input.id}"
  on_error  = "skip"
  default   = { extras: [] }  # Value used when step is skipped
}

transform {
  extras = "step.optional_data.extras"  # Safe: returns [] if step failed
}
```

### Timeout

Set a maximum wait time per step:

```hcl
step "slow_service" {
  connector = "legacy_api"
  operation = "GET /compute"
  timeout   = "10s"
  on_error  = "skip"
  default   = {}
}
```

## Enrich vs Step

Both `enrich` and `step` call external services. The difference:

| | `step` | `enrich` |
|--|--------|---------|
| Results available as | `step.NAME` | `enriched.NAME` |
| Used in | Steps and transforms | Transforms only |
| Named transforms | No | Yes (can be defined inside named transforms) |
| Conditional | `when` attribute | — |
| Error handling | `on_error = "skip"` | — |

Use `step` for multi-step orchestration logic. Use `enrich` for simple data enrichment within a transform.

## After Block: Cache Invalidation

Run side effects after the flow completes successfully:

```hcl
flow "update_product" {
  from { connector = "api", operation = "PUT /products/:id" }
  to   { connector = "db", target = "UPDATE products" }

  after {
    invalidate {
      storage  = "redis_cache"
      keys     = ["product:${input.params.id}"]
      patterns = ["products:list:*"]
    }
  }
}
```

The `after` block currently supports `invalidate` for cache invalidation. It runs after all `to` blocks complete successfully.

## Complex Example: E-Commerce Checkout

```hcl
flow "checkout" {
  from { connector = "api", operation = "POST /checkout" }

  # Validate cart
  step "cart" {
    connector = "db"
    query     = "SELECT * FROM carts WHERE id = ? AND user_id = ?"
    params    = [input.cart_id, input.user_id]
  }

  # Check each item's inventory (parallel — depends on cart)
  step "inventory" {
    connector = "inventory_api"
    operation = "POST /check-availability"
    body      = { items = "step.cart.items" }
    when      = "step.cart.items.size() > 0"
    timeout   = "5s"
    on_error  = "skip"
    default   = { all_available = true }
  }

  # Fetch customer shipping address
  step "customer" {
    connector = "db"
    query     = "SELECT * FROM users WHERE id = ?"
    params    = [input.user_id]
  }

  # Calculate shipping cost
  step "shipping" {
    connector = "shipping_api"
    operation = "POST /calculate"
    body      = {
      items   = "step.cart.items"
      address = "step.customer.address"
    }
    timeout = "3s"
    on_error = "skip"
    default  = { cost = 5.99 }
  }

  transform {
    cart_id          = "step.cart.id"
    user_id          = "input.user_id"
    items            = "step.cart.items"
    subtotal         = "step.cart.total"
    shipping_cost    = "step.shipping.cost"
    total            = "step.cart.total + step.shipping.cost"
    all_items_available = "step.inventory.all_available"
    status           = "'pending_payment'"
    created_at       = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
    when      = "step.inventory.all_available == true"
  }

  to {
    connector = "rabbit"
    target    = "checkout.initiated"
  }
}
```

## See Also

- [Core Concepts: Flows](../core-concepts/flows.md) — complete flow reference
- [Core Concepts: Transforms](../core-concepts/transforms.md) — CEL functions
- [Examples: Steps](../../examples/steps) — runnable step examples
