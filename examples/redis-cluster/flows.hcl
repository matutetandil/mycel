# Flows for Redis Cluster/Sentinel Example

# Store session in Redis cluster
flow "create_session" {
  from {
    connector.api = "POST /sessions"
  }

  input {
    type = "session"
  }

  transform {
    key   = "'session:' + input.user_id"
    value = "input"
    ttl   = "3600"  # 1 hour
  }

  # Uses cluster - data is sharded across nodes
  to {
    connector.redis_cluster = "SET"
  }

  response {
    transform {
      success = "true"
      key     = "key"
    }
  }
}

# Get session from cluster (reads from replicas)
flow "get_session" {
  from {
    connector.api = "GET /sessions/:user_id"
  }

  transform {
    key = "'session:' + input.params.user_id"
  }

  to {
    connector.redis_cluster = "GET"
  }

  response {
    type = "session"
  }
}

# Using Sentinel for auth tokens (automatic failover)
flow "store_auth_token" {
  from {
    connector.api = "POST /auth/token"
  }

  transform {
    key   = "'auth:' + input.token"
    value = "input"
    ttl   = "86400"  # 24 hours
  }

  # Sentinel handles master discovery and failover
  to {
    connector.redis_sentinel = "SET"
  }
}

# Cache with cluster - demonstrates sharding
flow "cache_product" {
  from {
    connector.api = "POST /cache/products/:id"
  }

  transform {
    # Key determines which cluster node stores this
    key   = "'product:' + input.params.id"
    value = "input.body"
    ttl   = "300"  # 5 minutes
  }

  to {
    connector.redis_cluster = "SET"
  }
}
