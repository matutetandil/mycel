# GraphQL Query Optimization Example

This example demonstrates Mycel's **automatic GraphQL query optimization** features. No code changes needed - the same HCL configuration produces optimized execution!

## Features Demonstrated

### 1. Field Selection Optimization

When a client requests only specific fields, Mycel automatically rewrites database queries:

```graphql
# Client request
query {
  users {
    id
    name
  }
}
```

```sql
-- Without optimization (standard)
SELECT * FROM users

-- With Mycel optimization (automatic!)
SELECT id, name FROM users
```

**Benefits:**
- Reduced network bandwidth
- Lower database load
- Less memory usage

### 2. Step Skipping

When a flow has multiple steps fetching data from external services, Mycel skips steps whose output isn't requested:

```graphql
# Only base fields - ALL external API calls skipped!
query {
  product(id: "prod-1") {
    id
    name
    description
  }
}

# Only price requested - only pricing API called
query {
  product(id: "prod-1") {
    id
    name
    price
  }
}

# All fields - all APIs called
query {
  product(id: "prod-1") {
    id
    name
    price        # triggers pricing step
    stock        # triggers inventory step
    rating       # triggers reviews step
    reviewCount  # triggers reviews step
  }
}
```

### 3. DataLoader (N+1 Prevention)

When fetching nested data, Mycel batches queries automatically:

```graphql
query {
  orders {
    id
    user {      # Would cause N+1 without DataLoader
      name
    }
    product {   # Would cause N+1 without DataLoader
      name
    }
  }
}
```

```sql
-- Without DataLoader (N+1 problem) - 10 orders = 21 queries!
SELECT * FROM orders
SELECT * FROM users WHERE id = 'user-1'
SELECT * FROM products WHERE id = 'prod-1'
SELECT * FROM users WHERE id = 'user-2'
SELECT * FROM products WHERE id = 'prod-2'
... (continues for each order)

-- With DataLoader (automatic!) - only 3 queries
SELECT * FROM orders
SELECT * FROM users WHERE id IN ('user-1', 'user-2', 'user-3')
SELECT * FROM products WHERE id IN ('prod-1', 'prod-2', 'prod-3')
```

### 4. CEL-based Conditional Steps

Use `has_field()` for explicit control over step execution:

```hcl
step "pricing" {
  when      = "has_field(input, 'price') || has_field(input, 'discount')"
  connector = "pricing_api"
  query     = "SELECT * FROM prices WHERE product_id = :id"
}
```

## Quick Start

```bash
# 1. Navigate to example directory
cd examples/graphql-optimization

# 2. Create and populate database
sqlite3 demo.db < setup.sql

# 3. Start Mycel
mycel start

# 4. Open GraphQL Playground
open http://localhost:4000/playground
```

## Test the Optimizations

### Test 1: Field Selection

```graphql
# Request only 2 fields - database query optimized
query {
  users {
    id
    name
  }
}
```

Compare with:

```graphql
# Request all fields
query {
  users {
    id
    email
    name
    avatar
    bio
    createdAt
    updatedAt
  }
}
```

### Test 2: Step Skipping

```graphql
# Base fields only - NO external API calls
query {
  product(id: "prod-1") {
    id
    name
    description
    category
  }
}
```

```graphql
# Add price - triggers pricing step ONLY
query {
  product(id: "prod-1") {
    id
    name
    price
  }
}
```

```graphql
# Add stock - triggers inventory step
query {
  product(id: "prod-1") {
    id
    name
    stock
  }
}
```

```graphql
# Add rating - triggers reviews step
query {
  product(id: "prod-1") {
    id
    name
    rating
    reviewCount
  }
}
```

### Test 3: DataLoader Batching

```graphql
# Fetches all orders with nested user/product
# Only 3 queries total (not 13!)
query {
  orders {
    id
    total
    status
    user {
      id
      name
      email
    }
    product {
      id
      name
      sku
    }
  }
}
```

## Available CEL Functions

| Function | Description | Example |
|----------|-------------|---------|
| `has_field(input, path)` | Check if field was requested | `has_field(input, 'price')` |
| `field_requested(input, path)` | Alias for has_field | `field_requested(input, 'stock')` |
| `requested_fields(input)` | Get all requested field paths | `requested_fields(input)` |
| `requested_top_fields(input)` | Get top-level fields only | `requested_top_fields(input)` |

## How It Works

### 1. Field Analysis

When a GraphQL query arrives, Mycel:
1. Parses the query AST
2. Extracts requested field names
3. Stores them in `__requested_fields` and `__requested_top_fields`

### 2. Database Optimization

Before executing a database query:
1. Checks if `__requested_top_fields` is available
2. Maps GraphQL field names to SQL column names (camelCase → snake_case)
3. Rewrites `SELECT *` to `SELECT specific_columns`

### 3. Step Optimization

Before executing each step:
1. Analyzes transform expressions to find which fields use each step
2. Compares with `__requested_top_fields`
3. Skips steps whose output isn't needed

### 4. DataLoader

For each GraphQL request:
1. Creates a new `LoaderCollection`
2. Batches all loads that happen within 1ms window
3. Executes single batched query instead of N individual queries

## Performance Impact

| Scenario | Without Optimization | With Optimization | Improvement |
|----------|---------------------|-------------------|-------------|
| Select 2 of 10 fields | 10 columns fetched | 2 columns fetched | 80% less data |
| Product with 3 external APIs | 4 API calls | 1 API call (if only base fields) | 75% fewer calls |
| 10 orders with nested data | 21 queries | 3 queries | 86% fewer queries |

## File Structure

```
graphql-optimization/
├── service.hcl     # Service configuration
├── connectors.hcl  # GraphQL server + databases
├── flows.hcl       # Flows with optimization examples
├── types.hcl       # GraphQL type definitions
├── setup.sql       # Database schema + sample data
└── README.md       # This file
```

## Troubleshooting

### Optimization Not Working?

1. **Check field names**: GraphQL uses camelCase, SQL uses snake_case. Mycel converts automatically.

2. **Check transform expressions**: Step optimizer analyzes `step.X` references in transforms.

3. **Verify __requested_fields**: Add a log or inspect input to see what fields were detected.

### DataLoader Not Batching?

1. **Check timing**: Loads within 1ms are batched. Async operations might miss the window.

2. **Check context**: DataLoader requires the GraphQL context to be passed through.

## Related Documentation

- [Phase 8 Specification](../../docs/PHASE-8-GRAPHQL-OPTIMIZATION.md)
- [CEL Transform Reference](../../docs/transformations.md)
- [Step Blocks Documentation](../../docs/step-blocks.md)
