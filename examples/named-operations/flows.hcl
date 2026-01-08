# Named Operations Example - Flows
# All operations must be defined in the connector configuration

# Using named operations: "list_users" instead of "GET /users"
flow "get_users" {
  from {
    connector = "api"
    operation = "list_users"
  }

  to {
    connector = "db"
    target    = "all_users"
  }
}

# Named operation with path parameter
flow "get_user" {
  from {
    connector = "api"
    operation = "get_user"
  }

  to {
    connector = "db"
    target    = "user_by_id"
  }
}

# Create operation
flow "create_user" {
  from {
    connector = "api"
    operation = "create_user"
  }

  to {
    connector = "db"
    target    = "insert_user"
  }

  transform {
    id         = "uuid()"
    email      = "lower(input.email)"
    name       = "input.name"
    created_at = "now()"
  }
}

# Update operation
flow "update_user" {
  from {
    connector = "api"
    operation = "update_user"
  }

  to {
    connector = "db"
    target    = "update_user"
  }

  transform {
    email      = "lower(input.email)"
    name       = "input.name"
    updated_at = "now()"
  }
}

# Delete operation
flow "delete_user" {
  from {
    connector = "api"
    operation = "delete_user"
  }

  to {
    connector = "db"
    target    = "delete_user"
  }
}

# Health check
flow "health_check" {
  from {
    connector = "api"
    operation = "health_check"
  }

  to {
    connector = "db"
    target    = "health"
  }
}
