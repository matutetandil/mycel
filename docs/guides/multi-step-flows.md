# Multi-Step Flows

Steps add multi-step orchestration to a flow. Each step calls a connector and makes its result available to subsequent steps and the final transform. Use steps when a single flow needs to assemble data from multiple sources before producing a response.

## Basic Step Flow

```hcl
flow "get_order_detail" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  step "order" {
    connector = "db"
    operation = "query"
    query     = "SELECT * FROM orders WHERE id = :id"
    params {
      id = "input.id"
    }
  }

  step "customer" {
    connector = "customers_api"
    operation = "GET /customers/:customer_id"
    params {
      customer_id = "step.order.customer_id"
    }
  }

  transform {
    id       = "step.order.id"
    status   = "step.order.status"
    customer = "step.customer"
  }

  to {
    connector = "api"
    target    = "response"
  }
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

### Passing a set — `IN (:name)`

A `params` entry that evaluates to a **list** is expanded into one placeholder per member, which is what makes the common two-step shape work: one step returns rows, the next asks about all of them at once.

```hcl
step "orders" {
  connector = "db"
  query     = "SELECT * FROM orders WHERE user_id = :user_id"
  params    = { user_id = "input.user_id" }
}

step "items" {
  connector = "db"
  query     = "SELECT * FROM order_items WHERE order_id IN (:order_ids)"
  when      = "size(step.orders ?? []) > 0"
  params    = { order_ids = "pluck(step.orders, 'id')" }
}
```

`IN (:order_ids)` with three ids becomes `IN (?, ?, ?)` and three arguments. `NOT IN` works the same way; a string stays one value.

!!! warning "An empty list needs the `when`"
    `IN ()` is not valid SQL, and there is no expansion right for both directions — `IN (NULL)` matches nothing, which is what an empty set means, but `NOT IN (NULL)` also matches nothing, which is its opposite. So an empty list is refused, naming the parameter, and the guard above is the answer: a user with no orders skips the step rather than running a statement that cannot parse.

    Note `?? []` in the condition: a step whose query matched no rows yields `null`, not an empty list.

Full rules, including what happens when a list is written where a scalar belongs, in [binding a set](../reference/destination-properties.md#binding-a-set--in-name).

## Conditional Steps

Skip expensive steps when their data is not needed:

```hcl
flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  step "product" {
    connector = "db"
    query     = "SELECT * FROM products WHERE id = :id"
    params {
      id = "input.id"
    }
  }

  step "inventory" {
    connector = "inventory_api"
    operation = "GET /stock/:sku"
    params {
      sku = "step.product.sku"
    }
    when      = "step.product.track_inventory == true"
  }

  step "reviews" {
    connector = "reviews_api"
    operation = "GET /reviews/:id"
    params {
      id = "input.id"
    }
    # A JSON body field. Sent as `?include_reviews=true` it would be the
    # string "true" instead — see Input and Output.
    when      = "input.include_reviews == true"
  }

  transform {
    id        = "step.product.id"
    name      = "step.product.name"
    sku       = "step.product.sku"
    in_stock  = "step.inventory.available > 0"
    reviews   = "step.reviews"
  }

  to {
    connector = "api"
    target    = "response"
  }
}
```

When `when` is false, the step is skipped entirely. Any subsequent `step.NAME` reference returns the `default` value or an empty map.

## Error Handling in Steps

### Skip on Error

Continue the flow even if a step fails:

```hcl
step "optional_data" {
  connector = "external_api"
  operation = "GET /extras/:id"
  params {
    id = "input.id"
  }
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
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }
  to {
    connector = "db"
    target    = "UPDATE products"
  }

  after {
    invalidate {
      storage  = "redis_cache"
      keys     = ["product:${input.id}"]
      patterns = ["products:list:*"]
    }
  }
}
```

The `after` block currently supports `invalidate` for cache invalidation. It runs after all `to` blocks complete successfully — and on a flow with **no** `to` at all, which is what an endpoint whose only job is invalidation looks like:

```hcl
flow "invalidate_products" {
  from {
    connector = "api"
    operation = "POST /cache/invalidate"
  }

  # Which keys the caller's write made stale. The number of them is the number
  # of rows, so it cannot be written out in `keys`.
  step "affected" {
    connector = "db"
    query     = "SELECT product_id, store_code FROM product_stores WHERE product_id IN (:ids)"
    params    = { ids = "input.ids" }
    when      = "size(input.ids ?? []) > 0"
  }

  after {
    invalidate {
      storage   = "redis_cache"
      keys_from = "(step.affected ?? []).map(r, 'product:' + string(r.product_id) + ':' + r.store_code)"
    }
  }

  response {
    invalidated = "size(step.affected ?? [])"
  }
}
```

`keys` and `patterns` are `${...}` templates, one key out per template in. `keys_from` and `patterns_from` take a CEL expression yielding a list, with `input.*`, `output.*` and `step.*` in scope — see [a key set whose size depends on the data](caching.md#a-key-set-whose-size-depends-on-the-data).

### When the invalidation fails

The flow's own work is already done and committed by then, so the two ways it can fail are answered differently:

- **The cache could not be reached.** Logged at warn, counted as `mycel_cache_invalidate_errors_total`, and the request still succeeds — failing it after the write is committed would be wrong. But it is worth alerting on: the cache is now serving what that write made stale, and nothing will correct it.
- **`keys_from` / `patterns_from` could not be evaluated**, or did not yield a list of strings. The request **fails**. This is not transient — it will fail identically on every message for as long as the flow is deployed, and `mycel validate` does not evaluate CEL, so it is not caught beforehand either.

That distinction matters most for the destination-less shape above: it writes nothing, so a silent no-op would answer `200` forever and the only symptom would appear somewhere else entirely, hours later.

## Complex Example: E-Commerce Checkout

```hcl
flow "checkout" {
  from {
    connector = "api"
    operation = "POST /checkout"
  }

  # Validate cart
  step "cart" {
    connector = "db"
    query     = "SELECT * FROM carts WHERE id = ? AND user_id = ?"
    params {
      cart_id = "input.cart_id"
      user_id = "input.user_id"
    }
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
    params {
      user_id = "input.user_id"
    }
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
- [Examples: Steps](https://github.com/matutetandil/mycel/tree/main/examples/steps) — runnable step examples
