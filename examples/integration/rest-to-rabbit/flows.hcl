# Flows: REST -> RabbitMQ
# NOTE: Some advanced features (inline queue config, foreach) are planned

# Pattern 1: Simple API -> Queue (fire and forget)
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  transform {
    order_id   = "input.order_id ?? uuid()"
    customer   = "input.customer"
    items      = "input.items"
    total      = "input.total"
    created_at = "now()"
    status     = "'pending'"
  }

  to {
    connector = "rabbit"
    operation = "order.created"
  }
}

# Pattern 2: Webhook receiver -> Queue
flow "receive_webhook" {
  from {
    connector = "api"
    operation = "POST /webhooks/:provider"
  }

  transform {
    provider    = "input.params.provider"
    event_type  = "input.headers['x-event-type'] ?? input.body.type ?? 'unknown'"
    payload     = "input.body"
    received_at = "now()"
  }

  to {
    connector = "rabbit"
    operation = "webhook.received"
  }
}

# Pattern 3: Command API -> Queue with validation
flow "trigger_job" {
  from {
    connector = "api"
    operation = "POST /jobs"
  }

  transform {
    job_id     = "uuid()"
    job_type   = "input.type"
    params     = "input.params"
    priority   = "input.priority ?? 'normal'"
    created_at = "now()"
  }

  to {
    connector = "rabbit"
    operation = "job.created"
  }
}
