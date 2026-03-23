# Flows with custom error responses
#
# Uses error_response to return specific HTTP status codes and bodies
# instead of generic 500 errors.

# POST /payments - Returns 422 on validation failure, 503 on downstream failure
flow "create_payment" {
  from {
    connector = "api"
    operation = "POST /payments"
  }

  validate {
    input = "type.order"
  }

  to {
    connector = "postgres"
    target    = "payments"
  }

  error_handling {
    retry {
      attempts = 3
      delay    = "500ms"
      backoff  = "exponential"
    }

    # Custom error response when all retries fail
    error_response {
      status = 503
      headers = {
        "Retry-After" = "30"
      }
      body {
        output.error   = "'service_unavailable'"
        output.message = "'Payment service is temporarily unavailable. Please retry later.'"
      }
    }
  }
}

# GET /orders/:id - Returns 404 with structured error
flow "get_order" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }

  to {
    connector = "postgres"
    target    = "orders"
  }

  error_handling {
    error_response {
      status = 404
      body {
        output.error   = "'not_found'"
        output.message = "'The requested order was not found.'"
      }
    }
  }
}
