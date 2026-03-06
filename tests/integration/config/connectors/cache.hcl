# Redis cache
connector "redis_cache" {
  type   = "cache"
  driver = "redis"
  url    = "redis://redis:6379"
}

# Memory cache
connector "memory_cache" {
  type   = "cache"
  driver = "memory"
  default_ttl = "60s"
}
