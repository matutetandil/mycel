# Error Handling Example

Demonstrates all error handling layers in Mycel: retry with exponential backoff, dead letter queues, custom error responses, step-level skip/default, circuit breakers, and rate limiting.

## What This Example Does

- Retries failed database writes with exponential backoff
- Sends exhausted retries to a RabbitMQ dead letter queue (DLQ)
- Returns custom HTTP error responses (503, 404) instead of generic 500s
- Skips non-critical enrichment steps when external data is unavailable
- Protects all flows with a circuit breaker aspect
- Applies global rate limiting (50 req/s, burst 100)

## Quick Start

```bash
# Requires PostgreSQL and RabbitMQ running locally
mycel start --config ./examples/error-handling

# Or with Docker
docker run \
  -e DB_HOST=host.docker.internal \
  -e RABBITMQ_URL=amqp://guest:guest@host.docker.internal:5672/ \
  -v $(pwd)/examples/error-handling:/etc/mycel \
  -p 3000:3000 \
  ghcr.io/matutetandil/mycel
```

## Error Handling Layers

### 1. Retry with Exponential Backoff + DLQ (`flows/retry.hcl`)

```hcl
error_handling {
  retry {
    attempts  = 5
    delay     = "1s"
    max_delay = "30s"
    backoff   = "exponential"
  }

  fallback {
    connector     = "rabbit"
    target        = "dead_letters"
    include_error = true
  }
}
```

If `POST /orders` fails, Mycel retries 5 times (1s, 2s, 4s, 8s, 16s capped at 30s). After exhaustion, the message goes to RabbitMQ `dead_letters` exchange with the original payload and error details.

### 2. Custom Error Responses (`flows/custom_errors.hcl`)

```hcl
error_handling {
  error_response {
    status = 503
    headers = { "Retry-After" = "30" }
    body {
      output.error   = "'service_unavailable'"
      output.message = "'Payment service is temporarily unavailable.'"
    }
  }
}
```

Returns structured error responses with specific HTTP status codes instead of generic 500 errors.

### 3. Step-Level Error Handling (`flows/skip_steps.hcl`)

```hcl
step "customer" {
  connector = "postgres"
  query     = "SELECT name, email FROM customers WHERE id = :id"
  params    = { id = "step.order.customer_id" }
  on_error  = "skip"     # Continue without this data
}

step "shipping" {
  connector = "postgres"
  query     = "SELECT estimated_days FROM shipping_estimates WHERE order_id = :id"
  params    = { id = "input.id" }
  on_error  = "default"  # Use fallback values
  default   = { estimated_days = 7, carrier = "standard" }
}
```

Three modes: `fail` (abort flow), `skip` (continue without data), `default` (use fallback values).

### 4. Circuit Breaker (`aspects/circuit_breaker.hcl`)

```hcl
aspect "db_circuit_breaker" {
  on   = ["*"]
  when = "around"

  circuit_breaker {
    failure_threshold = 5
    success_threshold = 2
    timeout           = "30s"
  }
}
```

After 5 consecutive failures, the circuit opens and requests fail fast for 30s. After timeout, allows 1 request through; if it succeeds 2 times, the circuit closes.

### 5. Rate Limiting (`config.hcl`)

```hcl
service {
  rate_limit {
    requests_per_second = 50
    burst               = 100
    key_extractor       = "ip"
    enable_headers      = true
  }
}
```

Token bucket rate limiting per client IP. Returns `429 Too Many Requests` with `X-RateLimit-*` headers when exceeded.

## File Structure

```
error-handling/
├── config.hcl                     # Service config with rate limiting
├── connectors/
│   ├── api.hcl                    # REST API on port 3000
│   ├── database.hcl               # PostgreSQL connection
│   └── queue.hcl                  # RabbitMQ for DLQ
├── flows/
│   ├── retry.hcl                  # Retry + backoff + DLQ
│   ├── custom_errors.hcl          # Custom HTTP error responses
│   └── skip_steps.hcl             # Step-level skip/default
├── types/
│   └── order.hcl                  # Order validation schema
├── aspects/
│   └── circuit_breaker.hcl        # Circuit breaker for all flows
└── README.md
```

## Verify It Works

### Test retry + DLQ (stop PostgreSQL first to trigger failures)

```bash
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{"product_id": "abc-123", "quantity": 2, "email": "user@example.com"}'
```

With PostgreSQL down, the request retries 5 times then sends to the `dead_letters` queue.

### Test custom error response

```bash
curl -i http://localhost:3000/orders/nonexistent
```

Expected: `404 Not Found` with structured JSON body.

### Test step skipping

```bash
curl http://localhost:3000/orders/1/details
```

Returns order data with available enrichment fields. Missing fields from skipped steps are omitted; defaulted fields use fallback values.

### Test rate limiting

```bash
# Rapid-fire requests to trigger rate limit
for i in $(seq 1 200); do
  curl -s -o /dev/null -w "%{http_code}\n" http://localhost:3000/orders
done
```

After burst capacity is exhausted, returns `429 Too Many Requests`.

## Next Steps

- Add message queue consumers with filter rejection: See [MQ filter docs](../../docs/connectors/rabbitmq.md)
- Add error logging aspects: See [examples/aspects](../aspects)
- Add saga pattern for distributed transactions: See [docs/CONCEPTS.md](../../docs/CONCEPTS.md#sagas)
