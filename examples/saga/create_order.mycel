# Create Order Saga
# Orchestrates: create order → reserve inventory → process payment
# On failure: compensations run in reverse order

saga "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  # Step 1: Create the order record
  step "order" {
    action {
      connector = "orders_db"
      operation = "INSERT"
      target    = "orders"
      data = {
        status  = "pending"
        user_id = "input.user_id"
        amount  = "input.amount"
      }
    }
    compensate {
      connector = "orders_db"
      operation = "DELETE"
      target    = "orders"
      where = {
        id = "step.order.id"
      }
    }
  }

  # Step 2: Reserve inventory
  step "inventory" {
    action {
      connector = "inventory_api"
      operation = "POST /reserve"
      body = {
        sku      = "input.sku"
        quantity = "input.quantity"
      }
    }
    compensate {
      connector = "inventory_api"
      operation = "POST /release"
      body = {
        reservation_id = "step.inventory.reservation_id"
      }
    }
  }

  # Step 3: Process payment
  step "payment" {
    action {
      connector = "payments_api"
      operation = "POST /charges"
      body = {
        amount   = "input.amount"
        customer = "input.customer_id"
      }
    }
    compensate {
      connector = "payments_api"
      operation = "POST /refunds"
      body = {
        charge = "step.payment.charge_id"
      }
    }
  }

  # All steps succeeded: confirm the order
  on_complete {
    connector = "orders_db"
    operation = "UPDATE"
    target    = "orders"
    set = {
      status = "confirmed"
    }
    where = {
      id = "step.order.id"
    }
  }

  # A step failed and compensations ran: notify user
  on_failure {
    connector = "notifications"
    operation = "POST /send"
    body = {
      template = "order_failed"
      to       = "input.user_email"
    }
  }
}
