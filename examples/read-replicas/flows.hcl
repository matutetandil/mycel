# Flows for Read Replicas Example

# ============================================
# Automatic Routing (Recommended)
# ============================================

# SELECT queries automatically go to replicas
flow "list_users" {
  from {
    connector.api = "GET /users"
  }

  # This SELECT goes to a replica (round-robin)
  to {
    connector.postgres = "SELECT * FROM users ORDER BY created_at DESC LIMIT 100"
  }

  response {
    type = "array:user"
  }
}

# Get single user - also routed to replica
flow "get_user" {
  from {
    connector.api = "GET /users/:id"
  }

  to {
    connector.postgres = "SELECT * FROM users WHERE id = :id"
  }

  response {
    type = "user"
  }
}

# INSERT/UPDATE/DELETE automatically go to primary
flow "create_user" {
  from {
    connector.api = "POST /users"
  }

  input {
    type = "user"
  }

  # This INSERT goes to primary
  to {
    connector.postgres = "INSERT INTO users (name, email) VALUES (:name, :email) RETURNING *"
  }

  response {
    type = "user"
  }
}

flow "update_user" {
  from {
    connector.api = "PUT /users/:id"
  }

  # UPDATE goes to primary
  to {
    connector.postgres = "UPDATE users SET name = :name, email = :email WHERE id = :id RETURNING *"
  }
}

flow "delete_user" {
  from {
    connector.api = "DELETE /users/:id"
  }

  # DELETE goes to primary
  to {
    connector.postgres = "DELETE FROM users WHERE id = :id"
  }
}

# ============================================
# Force Primary for Read-After-Write
# ============================================

# Sometimes you need to read from primary immediately after write
flow "create_and_get_user" {
  from {
    connector.api = "POST /users/sync"
  }

  input {
    type = "user"
  }

  # Create on primary
  to {
    connector.postgres = "INSERT INTO users (name, email) VALUES (:name, :email) RETURNING id"
  }

  # Force read from primary (avoid replication lag)
  transform {
    user_id = "output.id"
    force_primary = "true"
  }

  to {
    # With force_primary, SELECT also goes to primary
    connector.postgres = "SELECT * FROM users WHERE id = :user_id"
  }
}

# ============================================
# MySQL Example
# ============================================

flow "list_orders" {
  from {
    connector.api = "GET /orders"
  }

  # SELECT goes to MySQL replica
  to {
    connector.mysql = "SELECT * FROM orders ORDER BY created_at DESC"
  }
}

flow "create_order" {
  from {
    connector.api = "POST /orders"
  }

  # INSERT goes to MySQL primary
  to {
    connector.mysql = "INSERT INTO orders (user_id, total) VALUES (:user_id, :total)"
  }
}
