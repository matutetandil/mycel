# Start Google OAuth flow - returns redirect URL
flow "google_login" {
  from {
    connector = "api"
    operation = "GET /auth/google"
  }

  to {
    connector = "google_auth"
    operation = "authorize"
  }
}

# Handle Google callback - exchange code for user info
flow "google_callback" {
  from {
    connector = "api"
    operation = "GET /auth/google/callback"
  }

  step "auth" {
    connector = "google_auth"
    operation = "callback"
    params = {
      code  = "input.query.code"
      state = "input.query.state"
    }
  }

  transform {
    output.email       = "step.auth.email"
    output.name        = "step.auth.name"
    output.picture     = "step.auth.picture"
    output.provider    = "'google'"
    output.provider_id = "step.auth.provider_id"
  }

  to {
    connector = "db"
    target    = "users"
  }
}

# Start GitHub OAuth flow
flow "github_login" {
  from {
    connector = "api"
    operation = "GET /auth/github"
  }

  to {
    connector = "github_auth"
    operation = "authorize"
  }
}

# Handle GitHub callback
flow "github_callback" {
  from {
    connector = "api"
    operation = "GET /auth/github/callback"
  }

  step "auth" {
    connector = "github_auth"
    operation = "callback"
    params = {
      code  = "input.query.code"
      state = "input.query.state"
    }
  }

  transform {
    output.email       = "step.auth.email"
    output.name        = "step.auth.name"
    output.picture     = "step.auth.picture"
    output.provider    = "'github'"
    output.provider_id = "step.auth.provider_id"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
