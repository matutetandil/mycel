# Cache test flows
# Cache connectors don't implement ReadWriter, so they're used as a caching
# layer on database flows via the `cache` block.

# GET users with Redis cache
flow "cached_redis_get" {
  from {
    connector = "api"
    operation = "GET /cache/redis/users"
  }
  cache {
    storage = "redis_cache"
    key     = "'cached_users'"
    ttl     = "30s"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}

# GET users with memory cache
flow "cached_memory_get" {
  from {
    connector = "api"
    operation = "GET /cache/memory/users"
  }
  cache {
    storage = "memory_cache"
    key     = "'cached_users_mem'"
    ttl     = "30s"
  }
  to {
    connector = "postgres"
    target    = "users"
  }
}
