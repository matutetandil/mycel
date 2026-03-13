# Aspects (AOP) Example

This example demonstrates Mycel's Aspect-Oriented Programming (AOP) feature, which allows you to apply cross-cutting concerns to flows based on pattern matching.

## What are Aspects?

Aspects are reusable behaviors that can be automatically applied to multiple flows. They're perfect for:

- **Audit logging** - Log all write operations
- **Caching** - Cache read operations automatically
- **Cache invalidation** - Clear cache after writes
- **Rate limiting** - Limit requests per second
- **Circuit breakers** - Protect against cascading failures
- **Metrics** - Collect timing/count data

## Aspect Timing (When)

- **before** - Executes before the flow
- **after** - Executes after the flow completes
- **around** - Wraps the flow (e.g., for caching)

## Pattern Matching

Aspects use glob patterns to match flow names:

```hcl
aspect "audit_writes" {
  on = ["create_*", "update_*", "delete_*"]
  # ...
}
```

Common patterns:
- `*` - All flows
- `create_*` - All create operations
- `*_user` - All operations ending with _user
- `get_product*` - All flows starting with get_product

## Running this Example

```bash
# Start the service
mycel start --config ./examples/aspects

# Test GET (will be cached)
curl http://localhost:3000/products

# Test POST (will trigger audit log)
curl -X POST -H "Content-Type: application/json" \
  -d '{"name":"Widget","price":9.99}' \
  http://localhost:3000/products

# Check audit logs (in audit_logs.db)
sqlite3 audit_logs.db "SELECT * FROM audit_logs"
```

## Files

- `config.hcl` - Service configuration
- `connectors.hcl` - Database and cache connectors
- `flows.hcl` - API flows (CRUD operations)
- `aspects.hcl` - Cross-cutting concerns

## Aspect Configuration Reference

### Action Aspect (logging, writes)

```hcl
aspect "audit_log" {
  on   = ["create_*"]
  when = "after"
  if   = "result.affected > 0"  # Conditional execution

  action {
    connector = "audit_db"
    target    = "audit_logs"
    transform {
      flow      = "_flow"
      timestamp = "now()"
    }
  }
}
```

### Cache Aspect

```hcl
aspect "cache_reads" {
  on   = ["get_*"]
  when = "around"

  cache {
    storage = "cache"
    ttl     = "10m"
    key     = "entity:${input.id}"
  }
}
```

### Invalidate Aspect

```hcl
aspect "invalidate_cache" {
  on   = ["update_*"]
  when = "after"

  invalidate {
    storage  = "cache"
    keys     = ["entity:${input.id}"]
    patterns = ["list:*"]
  }
}
```

### Rate Limit Aspect

```hcl
aspect "rate_limit" {
  on   = ["*"]
  when = "before"

  rate_limit {
    key                 = "client_ip"
    requests_per_second = 100
    burst               = 200
  }
}
```

### Circuit Breaker Aspect

```hcl
aspect "circuit_breaker" {
  on   = ["external_*"]
  when = "around"

  circuit_breaker {
    name              = "external_api"
    failure_threshold = 5
    success_threshold = 2
    timeout           = "30s"
  }
}
```

## Priority

Aspects can have priority for execution order (lower = first):

```hcl
aspect "auth_check" {
  on       = ["*"]
  when     = "before"
  priority = 1  # Runs first
}

aspect "logging" {
  on       = ["*"]
  when     = "before"
  priority = 10  # Runs second
}
```

## Available Variables

In aspect expressions (`if`, `transform`):

- `input.*` - Original request input
- `result.affected` - Rows affected (after)
- `result.data` - Result data (after)
- `error` - Error message if failed (after)
- `_flow` - Flow name
- `_operation` - Operation string
- `_target` - Target table/collection
- `_timestamp` - Request timestamp
