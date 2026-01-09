# Lock Example - User Payment Processing
# Ensures only one payment is processed per user at a time

flow "process_payment" {
  from {
    connector = "rabbitmq"
    operation = "queue:payments"
  }

  # Distributed lock by user_id
  # Only one payment per user can be processed at a time
  lock {
    storage = "redis"
    key     = "'user:' + input.body.user_id"
    timeout = "30s"
    wait    = true
    retry   = "100ms"
  }

  transform {
    user_id    = "input.body.user_id"
    amount     = "input.body.amount"
    status     = "'processed'"
    created_at = "now()"
  }

  to {
    connector = "postgres"
    operation = "payments"
  }
}

# REST endpoint to manually process a payment (for testing)
flow "process_payment_rest" {
  from {
    connector = "api"
    operation = "POST /payments"
  }

  lock {
    storage = "redis"
    key     = "'user:' + string(input.data.user_id)"
    timeout = "30s"
    wait    = true
    retry   = "100ms"
  }

  transform {
    user_id    = "input.data.user_id"
    amount     = "input.data.amount"
    status     = "'processed'"
    created_at = "now()"
  }

  to {
    connector = "postgres"
    operation = "payments"
  }
}
