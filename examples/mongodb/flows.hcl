# MongoDB Operations Flows

# List all documents
flow "list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "mongo"
    target    = "users"
  }
}

# Get document by ID
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  to {
    connector    = "mongo"
    target       = "users"
    query_filter = { "_id" = ":id" }
  }
}

# Create document
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  transform {
    name       = "input.name"
    email      = "lower(input.email)"
    status     = "'active'"
    created_at = "now()"
    updated_at = "now()"
  }

  to {
    connector = "mongo"
    target    = "users"
    operation = "INSERT_ONE"
  }
}

# Update document
flow "update_user" {
  from {
    connector = "api"
    operation = "PUT /users/:id"
  }

  to {
    connector    = "mongo"
    target       = "users"
    query_filter = { "_id" = ":id" }
    update = {
      "$set" = {
        name       = "input.name"
        email      = "input.email"
        updated_at = "now()"
      }
    }
    operation = "UPDATE_ONE"
  }
}

# Delete document
flow "delete_user" {
  from {
    connector = "api"
    operation = "DELETE /users/:id"
  }

  to {
    connector    = "mongo"
    target       = "users"
    query_filter = { "_id" = ":id" }
    operation    = "DELETE_ONE"
  }
}

# Query with filters
flow "get_active_users" {
  from {
    connector = "api"
    operation = "GET /users/active"
  }
  to {
    connector    = "mongo"
    target       = "users"
    query_filter = { status = "active" }
  }
}

# Complex query with MongoDB operators
flow "search_users" {
  from {
    connector = "api"
    operation = "GET /users/search"
  }
  to {
    connector = "mongo"
    target    = "users"
    query_filter = {
      "$or" = [
        { name = { "$regex" = ":q", "$options" = "i" } },
        { email = { "$regex" = ":q", "$options" = "i" } }
      ]
    }
  }
}

# Query with range filters
flow "get_recent_users" {
  from {
    connector = "api"
    operation = "GET /users/recent"
  }
  to {
    connector = "mongo"
    target    = "users"
    query_filter = {
      created_at = { "$gte" = "input.since" }
      status     = "active"
    }
  }
}

# Bulk update
flow "deactivate_old_users" {
  from {
    connector = "api"
    operation = "POST /users/deactivate-old"
  }

  to {
    connector = "mongo"
    target    = "users"
    query_filter = {
      last_login = { "$lt" = "input.before" }
      status     = "active"
    }
    update = {
      "$set" = {
        status     = "inactive"
        updated_at = "now()"
      }
    }
    operation = "UPDATE_MANY"
  }
}
