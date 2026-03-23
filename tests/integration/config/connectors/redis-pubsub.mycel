# Redis Pub/Sub connector for integration tests
connector "redis_pub" {
  type   = "mq"
  driver = "redis"

  host = env("REDIS_HOST", "localhost")
  port = env("REDIS_PORT", 6379)
}

connector "redis_sub" {
  type   = "mq"
  driver = "redis"

  host     = env("REDIS_HOST", "localhost")
  port     = env("REDIS_PORT", 6379)
  channels = ["test_events"]
}
