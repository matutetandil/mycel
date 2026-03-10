# Production Guide

## Checklist

### Security

- [ ] Use environment variables or Kubernetes Secrets for all credentials — never hardcode them in HCL files
- [ ] Add `.env` to `.gitignore`. Commit only `.env.example` with placeholder values
- [ ] Set `MYCEL_ENV=production` and `MYCEL_LOG_FORMAT=json`
- [ ] Enable TLS on gRPC connectors
- [ ] Configure CORS with specific origins (avoid `*` in production)
- [ ] Enable rate limiting in the `service` block
- [ ] Use PostgreSQL `ssl_mode = "verify-full"` in production
- [ ] Review the [Security Guide](../guides/security.md) for sanitization defaults

### Auth (if using auth system)

- [ ] Use `preset = "strict"` or `preset = "standard"`
- [ ] Set a strong `JWT_SECRET` (min 32 bytes, random)
- [ ] Enable brute force protection
- [ ] Use Redis for session storage (not in-memory)
- [ ] Consider enabling MFA for admin users

### Observability

- [ ] Configure Prometheus to scrape `/metrics`
- [ ] Set up health check probes in Kubernetes
- [ ] Enable structured logging (`MYCEL_LOG_FORMAT=json`)
- [ ] Set up alerting on `mycel_connector_errors_total` metrics

### Reliability

- [ ] Run at least 2 replicas (3+ for critical services)
- [ ] Configure resource limits and requests
- [ ] Use `lock` with Redis for scheduled jobs to prevent duplicate execution across instances
- [ ] Use a real cache (Redis, not memory) for multi-instance deployments
- [ ] Configure circuit breakers via aspects for external API dependencies

### Database

- [ ] Use connection pooling (`pool` block in connector)
- [ ] Do not use SQLite in production (no concurrent write support)
- [ ] Configure read replicas via Connector Profiles for read-heavy workloads
- [ ] Set appropriate `ssl_mode` for your PostgreSQL setup

## Configuration Recommendations

### Service Block

```hcl
service {
  name    = "orders-api"
  version = "2.1.0"

  rate_limit {
    requests_per_second = 100
    burst               = 200
    key_extractor       = "ip"
    exclude_paths       = ["/health", "/metrics"]
  }
}
```

### Database Connection Pool

```hcl
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = env("PG_HOST")
  database = env("PG_DATABASE")
  user     = env("PG_USER")
  password = env("PG_PASSWORD")
  ssl_mode = "verify-full"

  pool {
    max          = 50     # Tune based on expected concurrency
    min          = 5
    max_lifetime = 300    # seconds
  }
}
```

### Logging

```bash
MYCEL_ENV=production
MYCEL_LOG_LEVEL=warn    # Reduce noise; use info if you need more detail
MYCEL_LOG_FORMAT=json   # For log aggregation systems (Loki, Datadog, etc.)
```

### Error Handling with DLQ

For critical message queue consumers:

```hcl
flow "process_payment" {
  from {
    connector = "rabbit"
    operation = "payments"
  }

  dedupe {
    storage      = "connector.redis"
    key          = "input.payment_id"
    ttl          = "24h"
    on_duplicate = "skip"
  }

  error_handling {
    retry {
      attempts  = 5
      delay     = "1s"
      max_delay = "60s"
      backoff   = "exponential"
    }

    fallback {
      connector     = "rabbit"
      target        = "payments.failed"
      include_error = true
    }
  }

  to {
    connector = "db"
    target    = "payments"
  }
}
```

## Monitoring

### Key Metrics to Alert On

| Metric | Alert Condition |
|--------|----------------|
| `mycel_requests_total{status="5xx"}` | Rate > 1% of requests |
| `mycel_request_duration_seconds` | p95 > 2s |
| `mycel_connector_errors_total` | Rate > 0 for 5m |
| `mycel_goroutines` | > 10,000 (leak indicator) |

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: 'mycel'
    static_configs:
      - targets: ['my-service:3000']
    metrics_path: /metrics
```

## Common Production Issues

### High Memory Usage

- Check `mycel_goroutines` metric for goroutine leaks
- Review connection pool settings — too many idle connections waste memory
- In-memory cache with `max_items` not set can grow unbounded

### Slow Response Times

- Enable caching for read-heavy flows
- Check database query performance — add indexes where needed
- Review `semaphore` limits for external API calls

### Message Queue Backlog

- Increase `workers` count in consumer configuration
- Add more service replicas
- Check if `on_error = "continue"` is appropriate for batch processing

### Database Connection Exhaustion

- Reduce `pool.max` if hitting the PostgreSQL connection limit
- Use PgBouncer as a connection pooler between Mycel and PostgreSQL
- Ensure `pool.max_lifetime` is set to prevent stale connections

## See Also

- [Docker Deployment](docker.md)
- [Kubernetes Deployment](kubernetes.md)
- [Security Guide](../guides/security.md)
- [Error Handling Guide](../guides/error-handling.md)
- [Observability](../reference/api-endpoints.md)
