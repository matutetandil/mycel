# Flows for gRPC Load Balancing Example

# Get user via load-balanced gRPC pool
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  transform {
    user_id = "input.params.id"
  }

  # This call is load-balanced across all healthy backends
  to {
    connector = "backend_pool"
    operation = "UserService/GetUser"
  }

}

# List users - distributed across backends
flow "list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "backend_pool"
    operation = "UserService/ListUsers"
  }
}

# Stateful operation - uses pick_first for session affinity
flow "update_session" {
  from {
    connector = "api"
    operation = "POST /session"
  }

  # pick_first ensures same backend handles the session
  to {
    connector = "stateful_backend"
    operation = "SessionService/Update"
  }
}
