# Exec flows

flow "run_command" {
  from {
    connector = "api"
    operation = "GET /test/exec"
  }

  step "result" {
    connector = "exec"
    operation = "hello-exec"
  }

  transform {
    output = "step.result"
  }

  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "postgres"
    target    = "step_results"
  }
}
