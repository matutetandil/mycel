# Flows for Dynamic API Key Example

# Get current user info (uses auth context from validated API key)
flow "get_me" {
  from {
    connector = "api"
    operation = "GET /me"
  }

  # The auth context includes user_id and metadata from the API key validation
  transform {
    user_id  = "auth.user_id"
    metadata = "auth.claims"
  }

  to {
    connector = "keys_db"
    operation = "SELECT * FROM users WHERE id = :user_id"
  }

}

# List resources (protected endpoint)
flow "list_resources" {
  from {
    connector = "api"
    operation = "GET /resources"
  }

  # Filter by user's tenant from API key metadata
  transform {
    tenant_id = "auth.claims.tenant_id"
  }

  to {
    connector = "keys_db"
    operation = "SELECT * FROM resources WHERE tenant_id = :tenant_id"
  }
}

# Admin endpoint - check role from API key metadata
flow "admin_stats" {
  from {
    connector = "api"
    operation = "GET /admin/stats"
  }

  # Validate admin role from API key metadata
  transform {
    require_role = "'admin'"
    actual_role  = "auth.claims.role"
  }

  # This would fail if role doesn't match (handled by middleware)
  to {
    connector = "keys_db"
    operation = "SELECT COUNT(*) as total_users FROM users"
  }
}
