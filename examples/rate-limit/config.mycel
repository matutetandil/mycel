# Rate Limiting Example Configuration
# This example shows how to configure rate limiting

service {
  name    = "rate-limit-example"
  version = "1.0.0"

  # Rate limiting configuration
  rate_limit {
    # Requests per second (token refill rate)
    requests_per_second = 10

    # Maximum burst size (bucket capacity)
    burst = 20

    # How to identify clients:
    # - "ip" (default): Use client IP address
    # - "header:X-API-Key": Use value from header
    # - "query:api_key": Use value from query parameter
    key_extractor = "ip"

    # Paths to exclude from rate limiting
    exclude_paths = [
      "/health",
      "/health/live",
      "/health/ready",
      "/metrics"
    ]

    # Add X-RateLimit-* headers to responses
    enable_headers = true
  }
}

# REST API
connector "api" {
  type = "rest"
  port = 3000
}

# Database
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
