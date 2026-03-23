# User flows — security sanitization applies automatically to all flows.
# No special annotations needed: null bytes, control chars, oversized
# payloads, deep nesting, and more are blocked before reaching your flow.

# List all users
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

# Create user — input is sanitized before reaching the database
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}

# Update user
flow "update_user" {
  from {
    connector = "api"
    operation = "PUT /users/:id"
  }

  to {
    connector = "sqlite"
    target    = "users"
  }
}
