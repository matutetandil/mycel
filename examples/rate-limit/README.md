# Rate Limiting Example

This example demonstrates rate limiting configuration.

## Features

- Token bucket rate limiting
- Configurable requests per second and burst size
- Multiple key extraction methods (IP, header, query)
- Path exclusions
- Rate limit headers in responses

## Files

- `config.hcl` - Service configuration with rate limiting
- `flows.hcl` - Sample API flows

## Usage

```bash
# Start the service
mycel start --config ./examples/rate-limit

# Make requests (limited to 10/s, burst 20)
for i in {1..25}; do curl -s -o /dev/null -w "%{http_code}\n" http://localhost:3000/items; done

# Check rate limit headers
curl -i http://localhost:3000/items
```

## Response Headers

When `enable_headers = true`, responses include:

```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 9
X-RateLimit-Reset: 1704067200
```

When rate limited (HTTP 429):

```json
{
  "error": "rate limit exceeded",
  "retry_after": 100
}
```

## Configuration

```hcl
service {
  name = "my-api"

  rate_limit {
    requests_per_second = 10    # Token refill rate
    burst               = 20    # Max tokens (burst capacity)
    key_extractor       = "ip"  # How to identify clients
    exclude_paths       = ["/health", "/metrics"]
    enable_headers      = true
  }
}
```

## Key Extractors

| Type | Example | Description |
|------|---------|-------------|
| `ip` | `"ip"` | Client IP address (default) |
| `header:X` | `"header:X-API-Key"` | Value from header |
| `query:x` | `"query:api_key"` | Value from query param |

## Notes

- Rate limiting is **disabled by default**
- Only enabled when `rate_limit {}` block is present
- Health and metrics endpoints are excluded by default
- Uses token bucket algorithm for smooth rate limiting
