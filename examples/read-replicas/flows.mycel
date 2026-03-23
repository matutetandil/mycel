# Flows for Read Replicas Example

# ============================================
# Automatic Routing (Recommended)
# ============================================

# SELECT queries automatically go to replicas
flow "list_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  # This SELECT goes to a replica (round-robin)
  to {
    connector = "postgres"
    operation = "SELECT * FROM users ORDER BY created_at DESC LIMIT 100"
  }
}

# Get single user - also routed to replica
flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  to {
    connector = "postgres"
    operation = "SELECT * FROM users WHERE id = :id"
  }
}

# INSERT/UPDATE/DELETE automatically go to primary
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }

  # This INSERT goes to primary
  to {
    connector = "postgres"
    operation = "INSERT INTO users (name, email) VALUES (:name, :email) RETURNING *"
  }
}

flow "update_user" {
  from {
    connector = "api"
    operation = "PUT /users/:id"
  }

  # UPDATE goes to primary
  to {
    connector = "postgres"
    operation = "UPDATE users SET name = :name, email = :email WHERE id = :id RETURNING *"
  }
}

flow "delete_user" {
  from {
    connector = "api"
    operation = "DELETE /users/:id"
  }

  # DELETE goes to primary
  to {
    connector = "postgres"
    operation = "DELETE FROM users WHERE id = :id"
  }
}

# ============================================
# MySQL Example
# ============================================

flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }

  # SELECT goes to MySQL replica
  to {
    connector = "mysql"
    operation = "SELECT * FROM orders ORDER BY created_at DESC"
  }
}

flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  # INSERT goes to MySQL primary
  to {
    connector = "mysql"
    operation = "INSERT INTO orders (user_id, total) VALUES (:user_id, :total)"
  }
}
