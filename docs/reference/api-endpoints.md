# Auto-Generated API Endpoints

Mycel automatically registers several endpoints on every service. These are always available regardless of your flow configuration.

## Health Endpoints

Available on the REST connector port (if configured) or the admin port (default: 9090).

### `GET /health`

Full health check with per-component status.

**Response:**

```json
{
  "status": "healthy",
  "service": "orders-api",
  "version": "2.1.0",
  "uptime": 3600,
  "components": {
    "postgres": { "status": "healthy", "latency_ms": 2 },
    "redis": { "status": "healthy", "latency_ms": 1 },
    "rabbit": { "status": "healthy", "latency_ms": 3 }
  }
}
```

### `GET /health/live`

Kubernetes liveness probe. Returns 200 if the service is running.

**Response:**

```json
{ "status": "alive" }
```

### `GET /health/ready`

Kubernetes readiness probe. Returns 200 only if all connectors are ready to serve traffic.

**Response (healthy):**

```json
{ "status": "ready" }
```

**Response (not ready, HTTP 503):**

```json
{
  "status": "not_ready",
  "reason": "postgres: connection refused"
}
```

## Metrics Endpoint

### `GET /metrics`

Prometheus-compatible metrics in text format.

Includes standard Go runtime metrics plus Mycel-specific metrics:

```
# HELP mycel_requests_total Total HTTP requests
# TYPE mycel_requests_total counter
mycel_requests_total{service="orders-api",method="GET",path="/orders",status="200"} 1234

# HELP mycel_request_duration_seconds HTTP request duration
# TYPE mycel_request_duration_seconds histogram
mycel_request_duration_seconds_bucket{...} ...

# HELP mycel_goroutines Current goroutines
# TYPE mycel_goroutines gauge
mycel_goroutines{service="orders-api",version="2.1.0"} 42

# HELP mycel_uptime_seconds Service uptime
# TYPE mycel_uptime_seconds gauge
mycel_uptime_seconds{service="orders-api",version="2.1.0"} 3600

# HELP mycel_connector_errors_total Connector error count
# TYPE mycel_connector_errors_total counter
mycel_connector_errors_total{service="orders-api",connector="postgres"} 0
```

## Workflow Endpoints

Available when a saga with `delay` or `await` steps is running and `workflow` block is configured in the service.

### `GET /workflows/{id}`

Get workflow status.

**Response:**

```json
{
  "id": "wf-abc123",
  "saga": "loan_approval",
  "status": "waiting",
  "current_step": "wait_approval",
  "started_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:31:00Z",
  "resume_after": null,
  "awaiting_event": "loan_approved"
}
```

Status values: `running`, `waiting`, `completed`, `failed`, `cancelled`

### `POST /workflows/{id}/signal/{event}`

Resume a workflow waiting for an external event.

**Request:**

```http
POST /workflows/wf-abc123/signal/loan_approved
Content-Type: application/json

{
  "approved_by": "manager@company.com",
  "note": "Looks good"
}
```

The signal data is available in subsequent workflow steps as `input.signal`.

**Response:**

```json
{
  "workflow_id": "wf-abc123",
  "status": "running",
  "message": "Signal received, resuming workflow"
}
```

### `POST /workflows/{id}/cancel`

Cancel an active workflow. This triggers compensation for all completed steps.

**Response:**

```json
{
  "workflow_id": "wf-abc123",
  "status": "cancelled",
  "message": "Workflow cancelled, compensations initiated"
}
```

## Auth Endpoints

Available when the `auth` block is configured. All endpoints are prefixed with `/auth` by default (configurable via `auth.routes.prefix`).

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/auth/register` | POST | Register a new user |
| `/auth/login` | POST | Authenticate and get tokens |
| `/auth/logout` | POST | Invalidate session/tokens |
| `/auth/refresh` | POST | Refresh access token |
| `/auth/me` | GET | Get current user |
| `/auth/password/change` | POST | Change password |
| `/auth/password/reset` | POST | Request password reset |
| `/auth/password/reset/confirm` | POST | Confirm password reset |
| `/auth/mfa/setup` | POST | Set up MFA (TOTP or WebAuthn) |
| `/auth/mfa/verify` | POST | Verify MFA code |
| `/auth/mfa/disable` | DELETE | Disable MFA |

See the [Auth Guide](../guides/auth.md) for complete documentation.

## Admin Server

Services without a REST connector (e.g., queue workers, CDC pipelines) automatically start a lightweight admin server on port 9090. This exposes health and metrics for Kubernetes probes and monitoring.

Customize the port:

```hcl
service {
  name       = "queue-worker"
  version    = "1.0.0"
  admin_port = 8081
}
```
