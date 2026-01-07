# Cache Example - Connectors Configuration
# Demonstrates caching with both memory and Redis drivers.

# =====================
# REST API Server
# =====================
connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
  }
}

# =====================
# SQLite Database
# =====================
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"  # In-memory for demo, use "./data.db" for persistence
}

# =====================
# Memory Cache (Development)
# =====================
# Fast in-memory cache with LRU eviction.
# Best for: development, testing, single-instance deployments.
#
# NOTE: Memory cache uses built-in defaults:
# - LRU eviction policy
# - 10000 max items
# - 5m default TTL
# See internal/connector/cache/factory.go for defaults.
connector "memory_cache" {
  type   = "cache"
  driver = "memory"
}

# =====================
# Redis Cache (Production)
# =====================
# Distributed cache for production deployments.
# Best for: production, multi-instance, shared cache.
#
# Uncomment to use Redis instead of memory cache:
#
# connector "redis_cache" {
#   type   = "cache"
#   driver = "redis"
#
#   # Redis connection URL
#   # Format: redis://[:password@]host:port[/db]
#   url = env("REDIS_URL", "redis://localhost:6379")
#
#   # Key prefix for namespace isolation
#   # All keys will be prefixed: "myapp:products:123"
#   prefix = "myapp"
#
#   # Default TTL for entries without explicit TTL
#   default_ttl = "5m"
#
#   # Connection pool settings
#   pool {
#     max_connections = 10    # Maximum number of connections
#     min_idle        = 2     # Minimum idle connections to maintain
#     max_idle_time   = "30s" # Close idle connections after this time
#     connect_timeout = "5s"  # Timeout for new connections
#   }
# }
