# Flows: RabbitMQ -> REST

# Pattern 1: Simple queue consumption -> API call
flow "process_order" {
  description = "Consume order from queue and send to fulfillment service"

  from {
    connector.rabbit = {
      queue   = "orders.pending"
      durable = true

      bind {
        exchange    = "orders"
        routing_key = "order.created"
      }

      auto_ack = false
      format   = "json"

      dlq {
        enabled     = true
        queue       = "orders.pending.dlq"
        max_retries = 3
      }
    }
  }

  transform {
    output.external_id   = "input.body.order_id"
    output.customer      = "input.body.customer"
    output.items         = "input.body.items"
    output.shipping_addr = "input.body.shipping_address"
    output.priority      = "input.body.priority == 'express' ? 'high' : 'normal'"
    output.metadata      = {
      source         = "'mycel'"
      correlation_id = "input.properties.correlation_id"
      received_at    = "now()"
    }
  }

  to {
    connector.fulfillment_api = "POST /v1/shipments"
  }
}

# Pattern 2: Queue -> Transform -> Multiple API calls (fan-out)
flow "notify_order_status" {
  description = "Consume status updates and notify via multiple channels"

  from {
    connector.rabbit = {
      queue   = "orders.status"
      durable = true

      bind {
        exchange    = "orders"
        routing_key = "order.status.*"
      }

      format = "json"
    }
  }

  transform {
    output.order_id = "input.body.order_id"
    output.status   = "input.body.status"
    output.message  = "input.body.status == 'shipped' ? 'Your order has been shipped!' : 'Order status: ' + input.body.status"
    output.channel  = "input.body.customer.preferred_channel"
    output.contact  = "input.body.customer.email"
  }

  to {
    connector.notification_api = "POST /v1/send"
  }
}

# Pattern 3: With semaphore for rate-limited external API
flow "sync_to_crm" {
  description = "Sync customer data to CRM with rate limiting"

  semaphore {
    key          = "crm_api"
    permits      = 5
    storage      = "memory"
    timeout      = "30s"
    on_fail      = "wait"
    wait_timeout = "2m"
  }

  from {
    connector.rabbit = {
      queue   = "customers.sync"
      durable = true
      format  = "json"
    }
  }

  transform {
    output.crm_id    = "input.body.external_crm_id"
    output.email     = "input.body.email"
    output.name      = "input.body.first_name + ' ' + input.body.last_name"
    output.phone     = "input.body.phone"
    output.company   = "input.body.company"
    output.synced_at = "now()"
  }

  to {
    connector.fulfillment_api = "PUT /v1/customers/${input.body.external_crm_id}"
  }
}
