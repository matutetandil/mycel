# Steps Example

This example demonstrates **multi-step flow orchestration** with intermediate connector calls.

## Overview

Steps allow you to call multiple connectors within a single flow, with results from each step available to subsequent steps and the final transform.

## Key Concepts

### Step Blocks

```hcl
step "step_name" {
  connector = "my_connector"    # Required: connector to call
  query     = "SELECT ..."      # SQL query (for database connectors)
  operation = "GET /endpoint"   # HTTP operation (for REST/HTTP connectors)
  target    = "table_name"      # Target table/collection
  params    = { ... }           # Parameters for the query/operation
  body      = { ... }           # Request body (for HTTP connectors)
  when      = "condition"       # CEL condition for conditional execution
  on_error  = "fail|skip|default"  # Error handling strategy
  default   = { ... }           # Default value if on_error = "default"
  timeout   = "30s"             # Step timeout
}
```

### Accessing Step Results

Step results are available in:
- Subsequent step params: `step.previous_step.field`
- Transform expressions: `step.step_name.field`

```hcl
step "user" {
  connector = "db"
  query     = "SELECT * FROM users WHERE id = :id"
  params    = { id = "input.user_id" }
}

step "orders" {
  connector = "db"
  query     = "SELECT * FROM orders WHERE user_id = :user_id"
  params    = { user_id = "step.user.id" }  # References previous step!
}

transform {
  user_name  = "step.user.name"
  order_count = "size(step.orders)"
}
```

### Conditional Execution

Use `when` to conditionally execute steps:

```hcl
step "pricing" {
  connector = "pricing_service"
  operation = "GET /prices"
  params    = { product_id = "input.product_id" }
  when      = "input.include_prices == true"  # Only execute if condition is true
  on_error  = "default"
  default   = { price = 0 }
}
```

### Error Handling

- `fail` (default): Fail the entire flow if the step fails
- `skip`: Skip the step and continue (step result will be nil)
- `default`: Use the default value if the step fails

```hcl
step "optional_enrichment" {
  connector = "external_api"
  operation = "GET /data"
  on_error  = "skip"  # Continue even if this fails
}

step "required_validation" {
  connector = "db"
  query     = "SELECT 1 FROM users WHERE id = :id"
  params    = { id = "input.user_id" }
  on_error  = "fail"  # Fail the flow if user doesn't exist
}

step "enrichment_with_fallback" {
  connector = "external_api"
  operation = "GET /rates"
  on_error  = "default"
  default   = { rate = 1.0, currency = "USD" }
}
```

## Request Filtering

Use `filter` in the `from` block to skip requests that don't match a condition:

```hcl
flow "process_external_orders" {
  from {
    connector = "api"
    operation = "POST /orders"
    filter    = "input.metadata.origin != 'internal'"  # Skip internal orders
  }
  # ... rest of flow
}

flow "high_value_only" {
  from {
    connector = "api"
    operation = "POST /orders"
    filter    = "input.total >= 1000"  # Only process orders >= $1000
  }
  # ... rest of flow
}
```

When filter evaluates to `false`, the request is skipped (returns `FilteredResult`).

## Array Helper Functions

Mycel provides powerful array manipulation functions for use in transforms:

| Function | Description | Example |
|----------|-------------|---------|
| `first(list)` | Get the first element | `first(step.orders)` |
| `last(list)` | Get the last element | `last(step.orders)` |
| `unique(list)` | Remove duplicates | `unique(step.items)` |
| `reverse(list)` | Reverse list order | `reverse(step.orders)` |
| `flatten(list)` | Flatten nested lists | `flatten(step.nested)` |
| `pluck(list, key)` | Extract field from list of maps | `pluck(step.orders, 'total')` |
| `sum(list)` | Sum numeric values | `sum(pluck(step.orders, 'total'))` |
| `avg(list)` | Average of values | `avg(pluck(step.orders, 'total'))` |
| `min_val(list)` | Minimum value | `min_val(pluck(step.orders, 'total'))` |
| `max_val(list)` | Maximum value | `max_val(pluck(step.orders, 'total'))` |
| `sort_by(list, key)` | Sort list of maps by key | `sort_by(step.orders, 'created_at')` |

These can be combined for powerful data transformations:

```hcl
transform {
  total_spent     = "sum(pluck(step.orders, 'total'))"
  average_order   = "avg(pluck(step.orders, 'total'))"
  first_order     = "first(sort_by(step.orders, 'created_at'))"
  unique_products = "size(unique(pluck(step.items, 'product_id')))"
}
```

## Map Helper Functions

Mycel provides functions for composing and manipulating maps (objects):

| Function | Description | Example |
|----------|-------------|---------|
| `merge(map1, map2, ...)` | Combine maps (later values override) | `merge(step.user, step.prefs)` |
| `omit(map, key1, ...)` | Remove specified keys | `omit(step.user, 'password')` |
| `pick(map, key1, ...)` | Select only specified keys | `pick(step.user, 'id', 'name')` |

### Response Composition with Merge

Use `merge` to combine data from multiple sources without listing every field:

```hcl
step "user" {
  connector = "db"
  query     = "SELECT * FROM users WHERE id = :id"
  params    = { id = "input.id" }
}

step "preferences" {
  connector = "db"
  query     = "SELECT * FROM preferences WHERE user_id = :id"
  params    = { id = "input.id" }
}

step "subscription" {
  connector = "billing_db"
  query     = "SELECT plan, status FROM subscriptions WHERE user_id = :id"
  params    = { id = "input.id" }
}

transform {
  # Merge all data (later maps override earlier for duplicate keys)
  profile = "merge(step.user, step.preferences, step.subscription)"

  # Remove sensitive fields before merging
  safe_profile = "merge(omit(step.user, 'password', 'ssn'), step.preferences)"

  # Pick only specific fields from each source
  summary = "merge(pick(step.user, 'id', 'name'), pick(step.subscription, 'plan'))"
}
```

### Use Cases

- **API Gateway Aggregation**: Merge responses from multiple microservices
- **Data Sanitization**: Remove sensitive fields with `omit` before returning
- **Selective Responses**: Use `pick` to create different views (admin vs public)
- **Default Values**: Merge user data with defaults `merge(defaults, step.user)`

## Error Handling with Retry and Fallback

Use `error_handling` block to define retry policies and DLQ (Dead Letter Queue) fallback:

```hcl
flow "process_order" {
  from {
    connector = "rabbit"
    operation = "orders.new"
  }

  error_handling {
    retry {
      attempts  = 3           # Max retry attempts
      delay     = "1s"        # Initial delay between retries
      max_delay = "30s"       # Max delay (for exponential backoff)
      backoff   = "exponential"  # exponential, linear, or constant
    }

    fallback {
      connector     = "dlq"
      target        = "orders.failed"
      include_error = true    # Include error details in DLQ message
    }
  }

  # ... steps and transform ...

  to {
    connector = "db"
    target    = "orders"
  }
}
```

### Backoff Strategies

- `constant`: Same delay between each retry
- `linear`: Delay increases by initial delay each retry (1s, 2s, 3s, ...)
- `exponential`: Delay doubles each retry (1s, 2s, 4s, 8s, ...) capped by max_delay

### Fallback Options

| Option | Description |
|--------|-------------|
| `connector` | Target connector for failed messages (required) |
| `target` | Destination (table, topic, exchange, etc.) (required) |
| `include_error` | Include error details in fallback message |

## Examples in This Directory

1. **create_order**: Basic multi-step flow - lookup user, product, pricing, then create order
2. **get_product_details**: Conditional steps - optionally include pricing and inventory
3. **get_order_details**: Chained steps - each step uses results from previous steps
4. **process_payment**: Error handling - different strategies for different steps
5. **process_external_orders**: Request filtering - skip internal requests
6. **process_high_value_orders**: Request filtering - only process high-value orders
7. **get_order_summary**: Array transforms - aggregate data using array helper functions
8. **process_order_with_retry**: Error handling - retry with exponential backoff and DLQ fallback
9. **get_customer_profile**: Response composition - merge, omit, and pick functions
10. **get_product_full**: API Gateway aggregation - merge multiple microservice responses

## Running the Example

```bash
# Start Mycel
mycel start --config ./examples/steps

# Test create_order flow
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{"user_id": "1", "product_id": "100", "quantity": 2}'

# Test get_product_details with pricing
curl "http://localhost:3000/products/100?include_price=true&include_inventory=true"

# Test get_order_details
curl http://localhost:3000/orders/ord-123
```

## Comparison: Steps vs Enrichments

| Feature | Steps | Enrichments |
|---------|-------|-------------|
| Purpose | Multi-step orchestration | Data lookup/augmentation |
| Chaining | Can reference previous steps | Independent lookups |
| Execution order | Sequential | Potentially parallel |
| Error handling | on_error: fail/skip/default | Fail on error |
| Conditional | when clause | No |
| Use case | Complex flows, sagas | Simple data enrichment |

Use **steps** when you need:
- Sequential execution with dependencies
- Conditional execution based on previous results
- Fine-grained error handling per step
- Complex orchestration patterns

Use **enrichments** when you need:
- Simple data lookups
- Independent data sources
- Parallel execution is acceptable
