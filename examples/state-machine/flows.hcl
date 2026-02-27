# Trigger state transition via REST API
# POST /orders/:id/status { "event": "pay", "data": {...} }

flow "update_order_status" {
  from {
    connector = "api"
    operation = "POST /orders/:id/status"
  }

  state_transition {
    machine = "order_status"
    entity  = "orders"
    id      = "input.params.id"
    event   = "input.event"
    data    = "input.data"
  }

  to {
    connector = "orders_db"
    target    = "orders"
  }
}
