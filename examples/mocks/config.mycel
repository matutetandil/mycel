# Example: Mock System
# This example demonstrates how to use mocks for testing.

service {
  name    = "mock-example"
  version = "1.0.0"
}

# Enable mocks with path to mock files
mocks {
  enabled = true
  path    = "./mocks"

  # Optional: per-connector settings
  connectors {
    db = {
      latency = "50ms"  # Simulate database latency
    }
  }
}
