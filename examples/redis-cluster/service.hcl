# Redis Cluster and Sentinel Example
# Demonstrates high-availability Redis configurations

service {
  name = "redis-ha-example"
  version = "1.0.0"
}

# ============================================
# Option 1: Redis Cluster Mode
# ============================================
# For horizontal scaling with automatic sharding
connector "redis_cluster" {
  type   = "cache"
  driver = "redis"

  # Cluster mode configuration
  cluster {
    # Cluster nodes (at least 3 for production)
    nodes = [
      env("REDIS_NODE_1", "redis-node-1:6379"),
      env("REDIS_NODE_2", "redis-node-2:6379"),
      env("REDIS_NODE_3", "redis-node-3:6379"),
    ]

    # Read from replicas for read-heavy workloads
    read_only        = true
    route_by_latency = true

    # Retry settings for cluster redirects
    max_redirects = 3
  }

  # Authentication (if cluster uses AUTH)
  password = env("REDIS_PASSWORD", "")

  # Connection pool
  pool_size     = 10
  min_idle      = 2
  pool_timeout  = "5s"

  # Timeouts
  dial_timeout  = "5s"
  read_timeout  = "3s"
  write_timeout = "3s"
}

# ============================================
# Option 2: Redis Sentinel Mode
# ============================================
# For automatic failover with master-replica setup
connector "redis_sentinel" {
  type   = "cache"
  driver = "redis"

  sentinel {
    # Sentinel nodes
    nodes = [
      env("SENTINEL_1", "sentinel-1:26379"),
      env("SENTINEL_2", "sentinel-2:26379"),
      env("SENTINEL_3", "sentinel-3:26379"),
    ]

    # Master name as configured in Sentinel
    master_name = env("REDIS_MASTER", "mymaster")

    # Optional: Sentinel authentication
    # password = env("SENTINEL_PASSWORD", "")
  }

  # Redis authentication
  password = env("REDIS_PASSWORD", "")

  # Connection pool
  pool_size = 10
}

# ============================================
# Option 3: Simple Standalone (for comparison)
# ============================================
connector "redis_standalone" {
  type   = "cache"
  driver = "redis"

  host     = env("REDIS_HOST", "localhost")
  port     = 6379
  password = env("REDIS_PASSWORD", "")
  database = 0
}

# REST API
connector "api" {
  type = "rest"
  port = 8080
}

# Types
type "session" {
  user_id    = string
  token      = string
  expires_at = timestamp
  metadata   = object
}
