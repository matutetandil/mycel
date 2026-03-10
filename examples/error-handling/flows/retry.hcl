# Flow with retry, exponential backoff, and DLQ fallback
#
# If the database write fails:
# 1. Retry up to 5 times with exponential backoff (1s, 2s, 4s, 8s, 16s capped at 30s)
# 2. After all retries exhausted, send the failed message to RabbitMQ dead letter queue

flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  validate {
    input = "type.order"
  }

  transform {
    output.product_id = "input.product_id"
    output.quantity   = "input.quantity"
    output.email      = "input.email.lowerAscii()"
    output.status     = "'pending'"
    output.created_at = "now()"
  }

  to {
    connector = "postgres"
    target    = "orders"
  }

  error_handling {
    retry {
      attempts  = 5
      delay     = "1s"
      max_delay = "30s"
      backoff   = "exponential"
    }

    # Send to DLQ after all retries are exhausted
    fallback {
      connector     = "rabbit"
      target        = "dead_letters"
      include_error = true

      transform {
        output.original_payload = "input"
        output.flow             = "'create_order'"
        output.failed_at        = "now()"
      }
    }
  }
}
