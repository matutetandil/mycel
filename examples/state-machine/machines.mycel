# Order Status State Machine
# Defines valid states and transitions for order lifecycle

state_machine "order_status" {
  initial = "pending"

  state "pending" {
    on "pay" {
      transition_to = "paid"
    }
    on "cancel" {
      transition_to = "cancelled"
    }
  }

  state "paid" {
    on "ship" {
      transition_to = "shipped"
      guard         = "input.tracking_number != ''"
      action {
        connector = "notifications"
        operation = "POST /send"
        body = {
          template = "order_shipped"
          tracking = "input.tracking_number"
        }
      }
    }
    on "refund" {
      transition_to = "refunded"
    }
  }

  state "shipped" {
    on "deliver" {
      transition_to = "delivered"
    }
    on "return" {
      transition_to = "returned"
    }
  }

  state "delivered" {
    final = true
  }

  state "cancelled" {
    final = true
  }

  state "refunded" {
    final = true
  }

  state "returned" {
    on "refund" {
      transition_to = "refunded"
    }
  }
}
