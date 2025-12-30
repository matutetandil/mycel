# Cache Example - Flow Definitions
# =================================
# Demonstrates different caching strategies with Mycel.
#
# Caching patterns shown:
# 1. Inline cache configuration (simple, one-off)
# 2. Named cache references (reusable, DRY)
# 3. Cache invalidation after writes
# 4. Pattern-based invalidation with wildcards

# =========================================
# PATTERN 1: Inline Cache Configuration
# =========================================
# Use for simple, one-off cache needs.
# All settings are defined directly in the flow.

flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  to {
    connector = "db"
    target    = "products"
  }

  # Inline cache - all settings defined here
  cache {
    storage = "memory_cache"      # Which cache connector to use
    ttl     = "5m"                # Cache for 5 minutes
    key     = "product:${input.id}"  # Cache key with variable interpolation
  }
}

# =========================================
# PATTERN 2: Named Cache Reference
# =========================================
# Use for consistent caching across multiple flows.
# Settings are defined in caches.hcl and referenced here.

flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  to {
    connector = "db"
    target    = "users"
  }

  # Reference named cache - inherits TTL, prefix from cache definition
  cache {
    use = "users"                    # References cache "users" from caches.hcl
    key = "user:${input.id}"         # Custom key for this flow
  }
}

# =========================================
# PATTERN 3: List Caching
# =========================================
# Cache list endpoints with appropriate TTLs.

flow "list_products" {
  from {
    connector = "api"
    operation = "GET /products"
  }

  to {
    connector = "db"
    target    = "products"
  }

  cache {
    use = "lists"
    key = "products:all"
  }
}

flow "list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "db"
    target    = "users"
  }

  cache {
    use = "lists"
    key = "users:all"
  }
}

# =========================================
# PATTERN 4: Pagination Caching
# =========================================
# Cache paginated results with query parameters in key.

flow "list_products_paginated" {
  from {
    connector = "api"
    operation = "GET /products/page"
  }

  to {
    connector = "db"
    target    = "products"
  }

  cache {
    storage = "memory_cache"
    ttl     = "1m"
    # Include pagination params in cache key
    key = "products:page=${input.page}:limit=${input.limit}"
  }
}

# =========================================
# PATTERN 5: Cache Invalidation on Create
# =========================================
# Invalidate related caches when new data is created.

flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  to {
    connector = "db"
    target    = "products"
  }

  # Invalidate caches after successful creation
  after {
    invalidate {
      storage = "memory_cache"
      patterns = [
        "products:*",       # All individual product caches
        "lists:products:*"  # All product list caches
      ]
    }
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  to {
    connector = "db"
    target    = "users"
  }

  after {
    invalidate {
      storage  = "memory_cache"
      patterns = ["users:*", "lists:users:*"]
    }
  }
}

# =========================================
# PATTERN 6: Targeted Invalidation on Update
# =========================================
# Invalidate only the specific item + related lists.

flow "update_product" {
  from {
    connector = "api"
    operation = "PUT /products/:id"
  }

  to {
    connector = "db"
    target    = "products"
  }

  after {
    invalidate {
      storage = "memory_cache"
      # Specific key invalidation - more efficient than patterns
      keys = [
        "product:${input.id}"  # Only this product
      ]
      # Pattern invalidation for related caches
      patterns = [
        "lists:products:*"     # All product lists (they might contain this product)
      ]
    }
  }
}

flow "update_user" {
  from {
    connector = "api"
    operation = "PUT /users/:id"
  }

  to {
    connector = "db"
    target    = "users"
  }

  after {
    invalidate {
      storage = "memory_cache"
      keys     = ["user:${input.id}"]
      patterns = ["lists:users:*"]
    }
  }
}

# =========================================
# PATTERN 7: Complete Invalidation on Delete
# =========================================
# Clean up all related cache entries when deleting.

flow "delete_product" {
  from {
    connector = "api"
    operation = "DELETE /products/:id"
  }

  to {
    connector = "db"
    target    = "products"
  }

  after {
    invalidate {
      storage = "memory_cache"
      keys = [
        "product:${input.id}"
      ]
      patterns = [
        "lists:products:*"
      ]
    }
  }
}

flow "delete_user" {
  from {
    connector = "api"
    operation = "DELETE /users/:id"
  }

  to {
    connector = "db"
    target    = "users"
  }

  after {
    invalidate {
      storage = "memory_cache"
      keys     = ["user:${input.id}"]
      patterns = ["lists:users:*"]
    }
  }
}

# =========================================
# PATTERN 8: No Cache (Write-Through)
# =========================================
# Some endpoints shouldn't be cached.
# Simply omit the cache block.

flow "health_check" {
  from {
    connector = "api"
    operation = "GET /status"
  }

  to {
    connector = "db"
    target    = "products"  # Just count products as a health check
  }
  # No cache block - always hits the database
}
