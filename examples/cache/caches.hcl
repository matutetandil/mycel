# Named Cache Definitions
# ========================
# Reusable cache configurations that can be referenced by multiple flows.
# Define once, use everywhere with `cache { use = "cache_name" }`.

# =====================
# Products Cache
# =====================
# Longer TTL since products change less frequently.
# Used by: get_product, list_products flows
cache "products" {
  # Which cache connector to use
  storage = "memory_cache"

  # Time-to-live for cached entries
  ttl = "10m"

  # Prefix added to all keys using this cache
  # Key "123" becomes "products:123"
  prefix = "products"

  # Events that invalidate this cache (for future event-driven invalidation)
  invalidate_on = [
    "products:updated",
    "products:deleted"
  ]
}

# =====================
# Users Cache
# =====================
# Shorter TTL since user data changes more often.
# Used by: get_user, list_users flows
cache "users" {
  storage = "memory_cache"
  ttl     = "2m"
  prefix  = "users"

  invalidate_on = [
    "users:updated",
    "users:deleted"
  ]
}

# =====================
# Lists Cache
# =====================
# Very short TTL for frequently changing aggregate data.
# Used by: list_products, list_users (all-items endpoints)
cache "lists" {
  storage = "memory_cache"
  ttl     = "30s"
  prefix  = "lists"
}

# =====================
# Sessions Cache
# =====================
# Example: User session data with longer TTL.
cache "sessions" {
  storage = "memory_cache"
  ttl     = "24h"
  prefix  = "session"
}
