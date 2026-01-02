# Aspects (AOP) Configuration
# Aspects define cross-cutting concerns that are automatically applied to flows
# based on pattern matching. They execute before, after, or around flow execution.

# Audit logging for all write operations (create, update, delete)
# Applied AFTER the flow completes successfully
aspect "audit_writes" {
  on   = ["**/create_*.hcl", "**/update_*.hcl", "**/delete_*.hcl"]
  when = "after"

  # Only log if the operation was successful (has affected rows)
  if = "result.affected > 0"

  action {
    connector = "audit_db"
    target    = "audit_logs"

    transform {
      id         = "uuid()"
      flow       = "_flow"
      operation  = "_operation"
      target     = "_target"
      input      = "string(input)"
      affected   = "result.affected"
      timestamp  = "now()"
    }
  }
}

# Cache all GET operations for products
# Applied AROUND the flow (checks cache before, stores after)
aspect "cache_products" {
  on   = ["**/get_product*.hcl"]
  when = "around"

  cache {
    storage = "cache"
    ttl     = "10m"
    key     = "products:${input.id}"
  }
}

# Cache invalidation after product mutations
# Applied AFTER create/update/delete operations
aspect "invalidate_product_cache" {
  on   = ["**/create_product*.hcl", "**/update_product*.hcl", "**/delete_product*.hcl"]
  when = "after"

  invalidate {
    storage  = "cache"
    keys     = ["products:${input.id}"]
    patterns = ["products:*"]
  }
}

# Request logging (before all flows)
# This would log every incoming request
aspect "request_log" {
  on       = ["**/*.hcl"]
  when     = "before"
  priority = 1  # Execute first

  action {
    connector = "audit_db"
    target    = "request_logs"

    transform {
      id        = "uuid()"
      flow      = "_flow"
      operation = "_operation"
      timestamp = "now()"
    }
  }
}

# Rate limiting example (before execution)
# Limits requests per second per IP/key
# aspect "rate_limit_api" {
#   on   = ["**/api/**/*.hcl"]
#   when = "before"
#
#   rate_limit {
#     key                 = "client_ip"
#     requests_per_second = 100
#     burst               = 200
#   }
# }

# Circuit breaker example (around execution)
# Protects against cascading failures
# aspect "circuit_breaker_external" {
#   on   = ["**/external/**/*.hcl"]
#   when = "around"
#
#   circuit_breaker {
#     name              = "external_api"
#     failure_threshold = 5
#     success_threshold = 2
#     timeout           = "30s"
#   }
# }
