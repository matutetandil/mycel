// Flow Definitions for Message Queue Example

// Flow: Create order via REST API -> Publish to RabbitMQ
// When a POST request is made to /orders, the order is published to the message queue
flow "publish_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  validate {
    input = "type.order_request"
  }

  transform {
    order_id   = "uuid()"
    created_at = "now()"
    status     = "'pending'"
    product    = "input.product"
    quantity   = "input.quantity"
    customer   = "input.customer"
  }

  to {
    connector = "notifications"
    target    = "order.created"   // Routing key
  }
}

// Flow: Consume order events -> Store in database
// When a message arrives on the orders queue, it's processed and stored
flow "process_order" {
  from {
    connector = "order_events"
    operation = "order.*"         // Routing key pattern
  }

  transform {
    id           = "input.order_id"
    product      = "input.product"
    quantity     = "input.quantity"
    customer     = "input.customer"
    status       = "'processed'"
    processed_at = "now()"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

// Flow: Consume order events -> Send notification
// Fan-out pattern: same message triggers multiple flows
flow "notify_order" {
  from {
    connector = "order_events"
    operation = "order.created"   // Exact routing key match
  }

  transform {
    notification_type = "'email'"
    recipient         = "input.customer.email"
    subject           = "'Order Confirmation'"
    order_id          = "input.order_id"
    message           = "concat('Your order ', input.order_id, ' has been received.')"
  }

  to {
    connector = "notifications"
    target    = "notification.email"
  }
}

// Flow: REST endpoint to check order status
flow "get_order" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}

// Flow: List all processed orders
flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
