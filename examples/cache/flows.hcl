# Cache Example - Flow Definitions
# =================================
# Demonstrates caching with Mycel.
#
# NOTE: This is a simplified example. Advanced features like
# variable interpolation in cache keys (${input.id}) and
# after/invalidate blocks are documented but require parser support.

# =========================================
# PATTERN 1: Simple Cache Configuration
# =========================================
# Basic caching with static keys.

flow "get_product" {
  from {
    connector = "api"
    operation = "GET /products/:id"
  }

  to {
    connector = "db"
    target    = "products"
  }

  # Cache with static key
  cache {
    storage = "memory_cache"
    ttl     = "5m"
    key     = "products:item"
  }
}

# =========================================
# PATTERN 2: List Caching
# =========================================
# Cache list endpoints.

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
    storage = "memory_cache"
    ttl     = "1m"
    key     = "products:all"
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
    storage = "memory_cache"
    ttl     = "1m"
    key     = "users:all"
  }
}

# =========================================
# PATTERN 3: Write Operations (No Cache)
# =========================================
# Write operations should not be cached.

flow "create_product" {
  from {
    connector = "api"
    operation = "POST /products"
  }

  to {
    connector = "db"
    target    = "products"
  }
  # No cache block - writes go directly to database
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
}

# =========================================
# PATTERN 4: Health Check (No Cache)
# =========================================
# Some endpoints shouldn't be cached.

flow "health_check" {
  from {
    connector = "api"
    operation = "GET /status"
  }

  to {
    connector = "db"
    target    = "products"
  }
  # No cache block - always hits the database
}

# =========================================
# Advanced Features (Documented, Need Parser Support)
# =========================================
# The following patterns are documented but require parser enhancements:
#
# 1. Dynamic cache keys with variable interpolation:
#    cache {
#      key = "product:${input.id}"
#    }
#
# 2. Cache invalidation after writes:
#    after {
#      invalidate {
#        storage  = "memory_cache"
#        keys     = ["product:${input.id}"]
#        patterns = ["products:*"]
#      }
#    }
#
# 3. Named cache references from caches.hcl:
#    cache {
#      use = "users"
#    }
#
# See docs/INTEGRATION-PATTERNS.md for full documentation.
