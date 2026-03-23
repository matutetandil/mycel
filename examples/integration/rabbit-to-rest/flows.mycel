# Flows: RabbitMQ -> REST

# Pattern 1: Simple queue consumption -> API call
flow "process_order" {
  from {
    connector = "rabbit"
    operation = "orders.pending"
  }

  transform {
    external_id   = "input.body.order_id"
    customer      = "input.body.customer"
    items         = "input.body.items"
    shipping_addr = "input.body.shipping_address"
    priority      = "input.body.priority == 'express' ? 'high' : 'normal'"
  }

  to {
    connector = "fulfillment_api"
    operation = "POST /v1/shipments"
  }
}

# Pattern 2: Queue -> Transform -> API call
flow "notify_order_status" {
  from {
    connector = "rabbit"
    operation = "orders.status"
  }

  transform {
    order_id = "input.body.order_id"
    status   = "input.body.status"
    message  = "input.body.status == 'shipped' ? 'Your order has been shipped!' : 'Order status: ' + input.body.status"
    channel  = "input.body.customer.preferred_channel"
    contact  = "input.body.customer.email"
  }

  to {
    connector = "notification_api"
    operation = "POST /v1/send"
  }
}

# Pattern 3: With semaphore for rate-limited external API
flow "sync_to_crm" {
  semaphore {
    storage     = "memory"
    key         = "'crm_api'"
    max_permits = 5
    timeout     = "30s"
  }

  from {
    connector = "rabbit"
    operation = "customers.sync"
  }

  transform {
    crm_id    = "input.body.external_crm_id"
    email     = "input.body.email"
    name      = "input.body.first_name + ' ' + input.body.last_name"
    phone     = "input.body.phone"
    company   = "input.body.company"
    synced_at = "now()"
  }

  to {
    connector = "fulfillment_api"
    operation = "PUT /v1/customers/:id"
  }
}
