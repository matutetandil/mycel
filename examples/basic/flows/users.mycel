# User flows configuration

# Get all users
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

# Get user by ID
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

# Create new user
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  validate {
    input = "type.user"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

# Delete user (requires admin role)
flow "delete_user" {
  from {
    connector = "api"
    operation = "DELETE /users/:id"
  }

  require {
    roles = ["admin"]
  }

  to {
    connector = "sqlite"
    target    = "users"
  }

  error_handling {
    retry {
      attempts = 3
      delay    = "1s"
      backoff  = "exponential"
    }
  }
}
