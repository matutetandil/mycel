# Workflows Example

Demonstrates **long-running workflows** with delay steps, external signals, and persistence. Extends the saga pattern with time-based and event-based pauses.

## What This Does

An order fulfillment workflow that:

1. **Creates an order** in PostgreSQL
2. **Waits 5 minutes** (delay step for fraud check / validation)
3. **Awaits payment confirmation** via external signal (up to 1 hour)
4. **Ships the order** via a shipping API
5. **Marks fulfilled** on success, or **compensates and notifies** on failure

The entire workflow has a 24-hour timeout. Workflow state is persisted to PostgreSQL, so it survives restarts.

## Files

| File | Purpose |
|------|---------|
| `config.hcl` | Service config with workflow persistence |
| `connectors/api.hcl` | REST API on port 3000 |
| `connectors/database.hcl` | PostgreSQL, shipping API, notifications |
| `sagas/order_fulfillment.hcl` | Workflow with delay and await steps |

## Running

```bash
mycel start --config ./examples/workflows

# Or with Docker
docker run -v $(pwd)/examples/workflows:/etc/mycel -p 3000:3000 \
  -e DATABASE_URL=postgres://mycel:mycel@host.docker.internal:5432/orders?sslmode=disable \
  ghcr.io/matutetandil/mycel
```

## How It Works

```
POST /orders → saga "order_fulfillment" (timeout: 24h)
  ├── step "create_order"    → INSERT into orders (status: pending)
  ├── step "wait_processing" → delay 5m (paused, persisted)
  ├── step "await_payment"   → await "payment_confirmed" (timeout: 1h)
  │                            └── signal received → UPDATE status = "paid"
  ├── step "ship_order"      → POST /shipments
  │
  ├── on_complete → UPDATE orders SET status = "fulfilled"
  └── on_failure  → POST /send (notification)
```

If shipping fails after payment:
```
  ├── compensate "ship_order"    → (no shipment to cancel)
  ├── compensate "await_payment" → UPDATE status = "payment_reversed"
  ├── compensate "create_order"  → UPDATE status = "cancelled"
  └── on_failure                 → POST /send (notification)
```

## Try It Out

### 1. Create an order

```bash
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "u_123",
    "user_email": "john@example.com",
    "amount": 99.99,
    "items": ["item_a", "item_b"],
    "shipping_address": "123 Main St"
  }'
```

Response:
```json
{
  "saga_name": "order_fulfillment",
  "instance_id": "wf_abc123",
  "status": "running",
  "steps": { "create_order": { "id": 1, "status": "pending" } }
}
```

### 2. Check workflow status

The workflow is now paused at the delay step. Check its status:

```bash
curl http://localhost:3000/workflows/wf_abc123
```

Response:
```json
{
  "id": "wf_abc123",
  "saga": "order_fulfillment",
  "status": "waiting",
  "current_step": "wait_processing",
  "started_at": "2026-03-09T10:00:00Z",
  "resumes_at": "2026-03-09T10:05:00Z"
}
```

### 3. Signal payment confirmation

After the delay step completes, the workflow pauses at `await_payment`. Send the signal:

```bash
curl -X POST http://localhost:3000/workflows/wf_abc123/signal/payment_confirmed \
  -H "Content-Type: application/json" \
  -d '{"transaction_id": "txn_456"}'
```

Response:
```json
{
  "id": "wf_abc123",
  "status": "resumed",
  "step": "await_payment"
}
```

### 4. Cancel a workflow

If you need to abort a workflow at any point:

```bash
curl -X POST http://localhost:3000/workflows/wf_abc123/cancel
```

Response:
```json
{
  "id": "wf_abc123",
  "status": "cancelled"
}
```

## Key Concepts

### Delay Steps

Pause the workflow for a duration. The runtime persists the timer and resumes automatically:

```hcl
step "wait" {
  delay = "5m"  # Supports: 30s, 5m, 1h, 24h
}
```

### Await Steps

Pause until an external event arrives via the REST API:

```hcl
step "wait_for_approval" {
  await   = "manager_approved"
  timeout = "1h"  # Fail if no signal within 1 hour
}
```

### Workflow Persistence

Enable in `config.hcl` to persist workflow state across restarts:

```hcl
service {
  workflow {
    storage     = "db"
    connector   = "postgres"    # Reuse an existing database connector
    auto_create = true          # Auto-create workflow tables
  }
}
```

## Next Steps

- Basic sagas without persistence: See [examples/saga](../saga)
- State machines for complex transitions: See [examples/state-machine](../state-machine)
- Add notifications on failure: See [examples/notifications](../notifications)
