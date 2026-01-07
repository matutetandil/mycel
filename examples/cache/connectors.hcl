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
connector "memory_cache" {
  type   = "cache"
  driver = "memory"

  max_items   = 10000  # Maximum items in cache
  eviction    = "lru"  # Eviction policy: lru, lfu
  default_ttl = "5m"   # Default TTL for entries
  prefix      = "myapp" # Key prefix
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
#   # Redis connection
#   url         = "redis://localhost:6379"
#   address     = "localhost:6379"
#   prefix      = "myapp"
#   default_ttl = "5m"
#
#   # Connection pool settings
#   pool {
#     max = 10
#     min = 2
#   }
# }

# =====================
# Redis Cluster (High Availability)
# =====================
# For production with high availability requirements.
#
# connector "redis_cluster_cache" {
#   type   = "cache"
#   driver = "redis"
#   mode   = "cluster"
#
#   cluster {
#     nodes = [
#       "redis-1:6379",
#       "redis-2:6379",
#       "redis-3:6379"
#     ]
#     password         = "cluster-password"
#     max_redirects    = 3
#     route_by_latency = true
#   }
# }

# =====================
# Redis Sentinel (Failover)
# =====================
# For production with automatic failover.
#
# connector "redis_sentinel_cache" {
#   type   = "cache"
#   driver = "redis"
#   mode   = "sentinel"
#
#   sentinel {
#     master_name = "mymaster"
#     nodes = [
#       "sentinel-1:26379",
#       "sentinel-2:26379",
#       "sentinel-3:26379"
#     ]
#     password        = "sentinel-password"
#     master_password = "master-password"
#   }
# }
