# MongoDB Operations Flows
#
# NOTE: This is a simplified example. Some MongoDB operations require
# parser support for 'operation' in 'to' blocks.

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

# Get document by ID using query_filter
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

# =========================================
# Advanced Features (Documented, Need Parser Support)
# =========================================
# The following patterns require 'operation' attribute in 'to' block:
#
# 1. Create document:
#    to {
#      connector = "mongo"
#      target    = "users"
#      operation = "INSERT_ONE"
#    }
#
# 2. Update document:
#    to {
#      connector    = "mongo"
#      target       = "users"
#      query_filter = { "_id" = ":id" }
#      update = {
#        "$set" = { name = "input.name", updated_at = "now()" }
#      }
#      operation = "UPDATE_ONE"
#    }
#
# 3. Delete document:
#    to {
#      connector    = "mongo"
#      target       = "users"
#      query_filter = { "_id" = ":id" }
#      operation    = "DELETE_ONE"
#    }
#
# 4. Bulk update:
#    to {
#      connector = "mongo"
#      target    = "users"
#      query_filter = { last_login = { "$lt" = "input.before" } }
#      update = { "$set" = { status = "inactive" } }
#      operation = "UPDATE_MANY"
#    }
#
# See docs/INTEGRATION-PATTERNS.md for full documentation.
