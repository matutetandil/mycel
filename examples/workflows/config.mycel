service {
  name    = "order-workflow-service"
  version = "1.0.0"

  # Enable long-running workflow persistence.
  # Workflows survive restarts and support delay/await steps.
  workflow {
    storage      = "db"
    connector    = "postgres"
    auto_create  = true
  }
}
