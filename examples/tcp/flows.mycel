# TCP Flows - Handle incoming TCP messages

# Handle create_user message type
flow "tcp_create_user" {
  from {
    connector = "tcp_api"
    operation = "create_user"  # Message type field
  }

  transform {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    name       = "input.name"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "users"
  }
}

# Handle get_user message type
flow "tcp_get_user" {
  from {
    connector = "tcp_api"
    operation = "get_user"
  }

  to {
    connector = "db"
    target    = "users"
    filter    = "id = :id"
  }
}

# Handle list_users message type
flow "tcp_list_users" {
  from {
    connector = "tcp_api"
    operation = "list_users"
  }

  to {
    connector = "db"
    target    = "users"
  }
}

# HTTP endpoint that also creates users (for testing)
flow "http_create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    id         = "uuid()"
    email      = "lower(trim(input.email))"
    name       = "input.name"
    created_at = "now()"
  }

  to {
    connector = "db"
    target    = "users"
  }
}

# HTTP endpoint to list users (for verification)
flow "http_list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "db"
    target    = "users"
  }
}
