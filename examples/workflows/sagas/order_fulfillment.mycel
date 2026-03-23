# Order Fulfillment Workflow
# Demonstrates long-running workflow with delay and await steps.
#
# Flow: create order → wait 5m → await payment signal → ship → confirm
# On failure: compensations run in reverse, then notify.

saga "order_fulfillment" {
  timeout = "24h"

  from {
    connector = "api"
    operation = "POST /orders"
  }

  # Step 1: Create the order record
  step "create_order" {
    action {
      connector = "postgres"
      operation = "INSERT"
      target    = "orders"
      data = {
        status     = "pending"
        user_id    = "input.user_id"
        user_email = "input.user_email"
        amount     = "input.amount"
        items      = "input.items"
      }
    }
    compensate {
      connector = "postgres"
      operation = "UPDATE"
      target    = "orders"
      set = {
        status = "cancelled"
      }
      where = {
        id = "step.create_order.id"
      }
    }
  }

  # Step 2: Wait for processing (e.g., fraud check, validation)
  # Workflow pauses here for 5 minutes, then resumes automatically.
  step "wait_processing" {
    delay = "5m"
  }

  # Step 3: Await external payment confirmation
  # Workflow pauses until: POST /workflows/{id}/signal/payment_confirmed
  step "await_payment" {
    timeout = "1h"
    await   = "payment_confirmed"

    # Once the signal arrives, update the order status
    action {
      connector = "postgres"
      operation = "UPDATE"
      target    = "orders"
      set = {
        status   = "paid"
        paid_at  = "now()"
      }
      where = {
        id = "step.create_order.id"
      }
    }
    compensate {
      connector = "postgres"
      operation = "UPDATE"
      target    = "orders"
      set = {
        status = "payment_reversed"
      }
      where = {
        id = "step.create_order.id"
      }
    }
  }

  # Step 4: Ship the order
  step "ship_order" {
    timeout = "30s"

    action {
      connector = "shipping_api"
      operation = "POST /shipments"
      body = {
        order_id = "step.create_order.id"
        address  = "input.shipping_address"
        items    = "input.items"
      }
    }
    compensate {
      connector = "shipping_api"
      operation = "POST /shipments/cancel"
      body = {
        shipment_id = "step.ship_order.shipment_id"
      }
    }
  }

  # All steps succeeded: confirm the order
  on_complete {
    connector = "postgres"
    operation = "UPDATE"
    target    = "orders"
    set = {
      status       = "fulfilled"
      fulfilled_at = "now()"
    }
    where = {
      id = "step.create_order.id"
    }
  }

  # A step failed: notify the user
  on_failure {
    connector = "notifications"
    operation = "POST /send"
    body = {
      template = "order_failed"
      to       = "input.user_email"
      order_id = "step.create_order.id"
    }
  }
}
