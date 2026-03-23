# Flows: RabbitMQ -> GraphQL
# NOTE: Some advanced features (inline queue config, GraphQL queries) are planned

# Pattern 1: Queue consumption -> GraphQL mutation
flow "update_inventory" {
  from {
    connector = "rabbit"
    operation = "inventory.updates"
  }

  transform {
    sku       = "input.body.sku"
    quantity  = "input.body.new_quantity"
    warehouse = "input.body.warehouse_id"
  }

  to {
    connector = "inventory_graphql"
    operation = "updateInventory"
  }
}

# Pattern 2: Event-driven user sync via GraphQL
flow "sync_user_profile" {
  from {
    connector = "rabbit"
    operation = "users.profile_updates"
  }

  transform {
    userId    = "input.body.user_id"
    email     = "lower(input.body.email)"
    firstName = "input.body.first_name"
    lastName  = "input.body.last_name"
    avatar    = "input.body.avatar_url"
  }

  to {
    connector = "users_graphql"
    operation = "upsertUser"
  }
}

# Pattern 3: Bulk operations via GraphQL
flow "bulk_price_update" {
  from {
    connector = "rabbit"
    operation = "pricing.bulk_updates"
  }

  to {
    connector = "inventory_graphql"
    operation = "bulkUpdatePrices"
  }
}
