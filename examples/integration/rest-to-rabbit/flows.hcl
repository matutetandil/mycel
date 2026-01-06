# Flows: REST -> RabbitMQ

# Pattern 1: Simple API -> Queue (fire and forget)
flow "create_order" {
  description = "Receive order via API and queue for processing"

  from {
    connector.api = "POST /orders"
  }

  transform {
    # Generate order ID if not provided
    output.order_id   = "input.order_id ?? uuid()"
    output.customer   = "input.customer"
    output.items      = "input.items"
    output.total      = "input.total"
    output.created_at = "now()"
    output.status     = "'pending'"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "order.created"
      persistent  = true

      headers {
        "x-source"     = "api"
        "x-request-id" = "${context.request_id}"
      }

      correlation_id = "${output.order_id}"
      message_id     = "${uuid()}"

      format = "json"
    }
  }

  response {
    status = 202
    body = {
      message  = "Order received"
      order_id = "${output.order_id}"
      status   = "pending"
    }
  }
}

# Pattern 2: Webhook receiver -> Queue
flow "receive_webhook" {
  description = "Receive external webhooks and queue for processing"

  from {
    connector.api = "POST /webhooks/:provider"
  }

  transform {
    output.provider   = "input.params.provider"
    output.event_type = "input.headers['x-event-type'] ?? input.body.type ?? 'unknown'"
    output.payload    = "input.body"
    output.received_at = "now()"
    output.signature  = "input.headers['x-webhook-signature']"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "'webhook.' + input.params.provider + '.' + output.event_type"
      persistent  = true

      headers {
        "x-provider"   = "${output.provider}"
        "x-event-type" = "${output.event_type}"
        "x-signature"  = "${output.signature}"
      }

      format = "json"
    }
  }

  response {
    status = 200
    body = {
      received = true
    }
  }
}

# Pattern 3: Command API -> Queue with validation
flow "trigger_job" {
  description = "Trigger background job via API"

  input_type = type.job_request

  from {
    connector.api = "POST /jobs"
  }

  transform {
    output.job_id     = "uuid()"
    output.job_type   = "input.type"
    output.params     = "input.params"
    output.priority   = "input.priority ?? 'normal'"
    output.created_by = "context.user.id ?? 'anonymous'"
    output.created_at = "now()"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "'job.' + output.job_type"
      persistent  = true

      # Priority queue support
      priority = "output.priority == 'high' ? 10 : (output.priority == 'low' ? 1 : 5)"

      headers {
        "x-job-id"   = "${output.job_id}"
        "x-job-type" = "${output.job_type}"
        "x-priority" = "${output.priority}"
      }

      format = "json"
    }
  }

  response {
    status = 202
    body = {
      job_id   = "${output.job_id}"
      status   = "queued"
      priority = "${output.priority}"
    }
  }
}

# Pattern 4: Bulk ingestion -> Queue (batch publish)
flow "ingest_events" {
  description = "Bulk event ingestion API"

  from {
    connector.api = "POST /events/batch"
  }

  # Process each event in the batch
  foreach "event" in "input.events" {
    transform {
      output.event_id   = "event.id ?? uuid()"
      output.event_type = "event.type"
      output.data       = "event.data"
      output.timestamp  = "event.timestamp ?? now()"
      output.source     = "event.source ?? 'api'"
    }

    to {
      connector.rabbit = {
        exchange    = "events"
        routing_key = "'event.' + output.event_type"
        persistent  = true

        headers {
          "x-event-id"   = "${output.event_id}"
          "x-event-type" = "${output.event_type}"
          "x-batch-id"   = "${context.request_id}"
        }

        format = "json"
      }
    }
  }

  response {
    status = 202
    body = {
      received = "${size(input.events)}"
      batch_id = "${context.request_id}"
    }
  }
}

# Pattern 5: REST -> Queue with sync response (request-reply)
flow "process_sync" {
  description = "Synchronous request via queue (request-reply pattern)"

  from {
    connector.api = "POST /process"
  }

  transform {
    output.request_id = "uuid()"
    output.data       = "input"
    output.reply_to   = "'amq.rabbitmq.reply-to'"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "process.sync"
      persistent  = false  # Temp message

      reply_to       = "${output.reply_to}"
      correlation_id = "${output.request_id}"
      expiration     = "30000"  # 30s TTL

      format = "json"
    }
  }

  # Wait for reply (timeout 30s)
  await_reply {
    correlation_id = "${output.request_id}"
    timeout        = "30s"
  }

  response {
    status = 200
    body   = "${reply.body}"
  }
}

# Pattern 6: GraphQL mutation -> Queue
flow "graphql_create_order" {
  description = "GraphQL mutation that queues for processing"

  from {
    connector.api = {
      graphql = true
      mutation = "createOrder"
      args = {
        input = "CreateOrderInput!"
      }
    }
  }

  transform {
    output.order_id   = "uuid()"
    output.customer   = "input.customer"
    output.items      = "input.items"
    output.created_at = "now()"
  }

  to {
    connector.rabbit = {
      exchange    = "events"
      routing_key = "order.created"
      persistent  = true
      format      = "json"
    }
  }

  response {
    body = {
      id       = "${output.order_id}"
      status   = "PENDING"
      message  = "Order queued for processing"
    }
  }
}
