# Multi-step orchestration flows

flow "multi_step" {
  from {
    connector = "api"
    operation = "GET /test/steps/:user_id"
  }

  step "user" {
    connector = "postgres"
    query     = "SELECT * FROM users WHERE id = :user_id"
    params = {
      user_id = "input.user_id"
    }
  }

  step "items" {
    connector = "postgres"
    query     = "SELECT * FROM items WHERE status = 'active' LIMIT 5"
  }

  transform {
    user_name  = "step.user.name"
    user_email = "step.user.email"
    item_count = "size(step.items)"
  }

  # Required by runtime registration (ignored for GET+steps)
  to {
    connector = "postgres"
    target    = "step_results"
  }
}
