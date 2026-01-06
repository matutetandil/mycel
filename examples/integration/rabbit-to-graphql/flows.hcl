# Flows: RabbitMQ -> GraphQL

# Pattern 1: Queue consumption -> GraphQL mutation
flow "update_inventory" {
  description = "Consume inventory updates and sync to GraphQL service"

  from {
    connector.rabbit = {
      queue   = "inventory.updates"
      durable = true

      bind {
        exchange    = "inventory"
        routing_key = "stock.changed"
      }

      auto_ack = false
      format   = "json"

      dlq {
        enabled     = true
        queue       = "inventory.updates.dlq"
        max_retries = 3
      }
    }
  }

  to {
    connector.inventory_graphql = {
      query = <<GRAPHQL
        mutation UpdateStock($sku: String!, $quantity: Int!, $warehouse: String!) {
          updateInventory(
            input: {
              sku: $sku
              quantity: $quantity
              warehouseId: $warehouse
              updatedAt: "${now()}"
            }
          ) {
            success
            inventory {
              id
              sku
              quantity
              lastUpdated
            }
            errors {
              code
              message
            }
          }
        }
      GRAPHQL
      variables {
        sku       = "${input.body.sku}"
        quantity  = "${input.body.new_quantity}"
        warehouse = "${input.body.warehouse_id}"
      }
    }
  }
}

# Pattern 2: Event-driven user sync via GraphQL
flow "sync_user_profile" {
  description = "Sync user profile changes to GraphQL backend"

  from {
    connector.rabbit = {
      queue   = "users.profile_updates"
      durable = true

      bind {
        exchange    = "users"
        routing_key = "user.profile.updated"
      }

      format = "json"
    }
  }

  transform {
    output.userId    = "input.body.user_id"
    output.email     = "lower(input.body.email)"
    output.firstName = "input.body.first_name"
    output.lastName  = "input.body.last_name"
    output.avatar    = "input.body.avatar_url"
    output.metadata  = {
      source    = "'event-sync'"
      eventId   = "input.properties.message_id"
      timestamp = "now()"
    }
  }

  to {
    connector.users_graphql = {
      query = <<GRAPHQL
        mutation UpsertUser($input: UpsertUserInput!) {
          upsertUser(input: $input) {
            id
            email
            profile {
              firstName
              lastName
              avatarUrl
            }
            updatedAt
          }
        }
      GRAPHQL
      variables {
        input = "${output}"
      }
    }
  }
}

# Pattern 3: Bulk operations via GraphQL
flow "bulk_price_update" {
  description = "Process bulk price updates from queue"

  from {
    connector.rabbit = {
      queue   = "pricing.bulk_updates"
      durable = true

      bind {
        exchange    = "pricing"
        routing_key = "prices.bulk"
      }

      format = "json"
    }
  }

  to {
    connector.inventory_graphql = {
      query = <<GRAPHQL
        mutation BulkUpdatePrices($items: [PriceUpdateInput!]!) {
          bulkUpdatePrices(items: $items) {
            successCount
            failedCount
            failures {
              sku
              reason
            }
          }
        }
      GRAPHQL
      variables {
        items = "${input.body.price_updates}"
      }
    }
  }
}

# Pattern 4: Query before mutation (enrich flow)
flow "create_order_with_validation" {
  description = "Validate stock via query before creating order"

  from {
    connector.rabbit = {
      queue   = "orders.validation"
      durable = true
      format  = "json"
    }
  }

  # First: Query to validate stock
  steps {
    step "check_stock" {
      connector.inventory_graphql = {
        query = <<GRAPHQL
          query CheckStock($items: [StockCheckInput!]!) {
            checkStock(items: $items) {
              allAvailable
              unavailableItems {
                sku
                requested
                available
              }
            }
          }
        GRAPHQL
        variables {
          items = "${input.body.items}"
        }
      }
    }

    step "create_order" {
      when = "steps.check_stock.result.checkStock.allAvailable == true"

      connector.inventory_graphql = {
        query = <<GRAPHQL
          mutation CreateOrder($order: CreateOrderInput!) {
            createOrder(input: $order) {
              id
              status
              items {
                sku
                quantity
                price
              }
            }
          }
        GRAPHQL
        variables {
          order = "${input.body}"
        }
      }
    }
  }
}
