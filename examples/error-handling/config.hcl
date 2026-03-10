# Error Handling Example Configuration

service {
  name    = "error-handling-example"
  version = "1.0.0"

  # Global rate limiting to prevent overload
  rate_limit {
    requests_per_second = 50
    burst               = 100
    key_extractor       = "ip"

    exclude_paths = [
      "/health",
      "/health/live",
      "/health/ready",
      "/metrics"
    ]

    enable_headers = true
  }
}
