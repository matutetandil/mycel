# React to new user inserts
flow "on_user_created" {
  from {
    connector = "pg_cdc"
    operation = "INSERT:users"
  }

  transform {
    output.event      = "'user.created'"
    output.user_id    = "input.new.id"
    output.email      = "input.new.email"
    output.created_at = "input.timestamp"
  }

  to {
    connector = "events_db"
    target    = "events"
  }
}

# React to order status changes (only when status actually changed)
flow "on_order_updated" {
  from {
    connector = "pg_cdc"
    operation = "UPDATE:orders"
    filter    = "input.new.status != input.old.status"
  }

  transform {
    output.event      = "'order.status_changed'"
    output.order_id   = "input.new.id"
    output.old_status = "input.old.status"
    output.new_status = "input.new.status"
    output.changed_at = "input.timestamp"
  }

  to {
    connector = "events_db"
    target    = "events"
  }
}

# React to session deletions
flow "on_session_deleted" {
  from {
    connector = "pg_cdc"
    operation = "DELETE:sessions"
  }

  transform {
    output.event   = "'session.ended'"
    output.user_id = "input.old.user_id"
    output.token   = "input.old.token"
  }

  to {
    connector = "events_db"
    target    = "events"
  }
}

# Catch all changes on the products table
flow "on_product_change" {
  from {
    connector = "pg_cdc"
    operation = "*:products"
  }

  transform {
    output.event = "'product.' + lower(input.trigger)"
    output.data  = "input.new != null ? input.new : input.old"
  }

  to {
    connector = "events_db"
    target    = "events"
  }
}
