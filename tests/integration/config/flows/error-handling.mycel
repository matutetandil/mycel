# Error handling flows

flow "error_with_fallback" {
  from {
    connector = "api"
    operation = "POST /test/error"
  }

  error_handling {
    retry {
      attempts = 2
      delay    = "100ms"
      backoff  = "constant"
    }
    fallback {
      connector     = "postgres"
      target        = "dlq_failed"
      include_error = true
    }
  }

  # This step will fail because the table doesn't exist
  step "bad_query" {
    connector = "postgres"
    query     = "SELECT * FROM nonexistent_table WHERE id = 1"
    on_error  = "fail"
  }

  to {
    connector = "postgres"
    target    = "step_results"
  }
}
