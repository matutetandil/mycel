# Named Operations Example - Connectors
# This demonstrates how to define named operations on connectors

connector "api" {
  type = "rest"
  port = 8080

  # Named operations encapsulate the connector's capabilities
  # Flows reference these by name instead of inline syntax

  operation "list_users" {
    method      = "GET"
    path        = "/users"
    description = "List all users with pagination"

    param "limit" {
      type        = "number"
      default     = 100
      description = "Maximum number of users to return"
    }

    param "offset" {
      type        = "number"
      default     = 0
      description = "Number of users to skip"
    }
  }

  operation "get_user" {
    method      = "GET"
    path        = "/users/:id"
    description = "Get a single user by ID"

    param "id" {
      type        = "string"
      required    = true
      description = "User ID"
    }
  }

  operation "create_user" {
    method      = "POST"
    path        = "/users"
    description = "Create a new user"
    input       = "user_input"
    output      = "user"
  }

  operation "update_user" {
    method      = "PUT"
    path        = "/users/:id"
    description = "Update an existing user"

    param "id" {
      type     = "string"
      required = true
    }
  }

  operation "delete_user" {
    method      = "DELETE"
    path        = "/users/:id"
    description = "Delete a user"

    param "id" {
      type     = "string"
      required = true
    }
  }

  operation "health_check" {
    method      = "GET"
    path        = "/health"
    description = "Health check endpoint"
  }
}

connector "db" {
  type   = "database"
  driver = "sqlite"
  database = ":memory:"

  operation "all_users" {
    table       = "users"
    description = "Get all users from the database"

    param "limit" {
      type    = "number"
      default = 100
    }

    param "offset" {
      type    = "number"
      default = 0
    }
  }

  operation "user_by_id" {
    query       = "SELECT * FROM users WHERE id = :id"
    description = "Get a user by ID"

    param "id" {
      type     = "string"
      required = true
    }
  }

  operation "insert_user" {
    table       = "users"
    description = "Insert a new user"
  }

  operation "update_user" {
    table       = "users"
    description = "Update an existing user"
  }

  operation "delete_user" {
    query       = "DELETE FROM users WHERE id = :id"
    description = "Delete a user by ID"

    param "id" {
      type     = "string"
      required = true
    }
  }

  operation "health" {
    query       = "SELECT 1"
    description = "Database health check"
  }
}
