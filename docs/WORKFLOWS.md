# Long-Running Workflows

Mycel supports long-running workflows for sagas that need to pause execution, wait for external events, or enforce timeouts. Simple sagas (no delay/await) continue to execute synchronously — no configuration changes needed.

## When to Use

Use workflow persistence when your saga has:
- **Delay steps**: pause for a duration before continuing (`delay = "5m"`)
- **Await steps**: wait for an external event/signal (`await = "payment_confirmed"`)
- **Timeouts**: enforce maximum duration for the entire saga or individual steps

## Configuration

### Service Block

Enable workflow persistence by adding a `workflow` block to your service configuration:

```hcl
service {
  name    = "order-service"
  version = "1.0.0"

  workflow {
    storage     = "orders_db"    # Connector name (must be a database connector)
    table       = "mycel_workflows"  # Table name (default: mycel_workflows)
    auto_create = true           # Auto-create table on startup
  }
}
```

The `storage` field references a database connector (PostgreSQL, MySQL, or SQLite). The workflow engine reuses the existing database connection.

### Saga with Delay

```hcl
saga "order_fulfillment" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  step "create_order" {
    action {
      connector = "orders_db"
      operation = "INSERT"
      target    = "orders"
    }
  }

  step "wait_processing" {
    delay = "5m"    # Pause for 5 minutes
  }

  step "send_confirmation" {
    action {
      connector = "email"
      operation = "send"
    }
  }
}
```

When the workflow reaches the delay step, it saves its state to the database and pauses. A background ticker (every 5 seconds) checks for workflows whose delay has expired and resumes them automatically.

### Saga with Await/Signal

```hcl
saga "payment_flow" {
  timeout = "24h"    # Maximum workflow duration

  from {
    connector = "api"
    operation = "POST /payments"
  }

  step "create_payment" {
    action {
      connector = "payments_db"
      operation = "INSERT"
      target    = "payments"
    }
  }

  step "wait_confirmation" {
    await   = "payment_confirmed"   # Wait for external signal
    timeout = "1h"                  # Step-level timeout
  }

  step "complete_payment" {
    action {
      connector = "payments_db"
      operation = "UPDATE"
      target    = "payments"
      set = {
        status = "completed"
      }
    }
  }
}
```

When the workflow reaches the await step, it pauses until an external signal is received via the REST API.

## REST API

The workflow engine automatically registers these endpoints:

### Get Workflow Status

```
GET /workflows/{id}
```

Returns the current state of a workflow instance:

```json
{
  "id": "wf_1234567890",
  "saga_name": "payment_flow",
  "status": "paused",
  "current_step": 2,
  "await_event": "payment_confirmed",
  "created_at": "2026-03-09T10:00:00Z"
}
```

### Signal a Workflow

```
POST /workflows/{id}/signal/{event}
Content-Type: application/json

{
  "amount": 99.99,
  "method": "credit_card"
}
```

Resumes a paused workflow that is awaiting the specified event. The request body is stored as signal data and available to subsequent steps via `input.signal`.

### Cancel a Workflow

```
POST /workflows/{id}/cancel
```

Cancels an active workflow. Runs compensation for completed steps if defined.

## Workflow States

| Status | Description |
|--------|-------------|
| `running` | Actively executing steps |
| `paused` | Waiting for delay expiration or external signal |
| `completed` | All steps finished successfully |
| `failed` | A step failed (compensation executed) |
| `timeout` | Saga or step timeout expired |
| `cancelled` | Manually cancelled via API |

## How It Works

1. **Execution starts**: When a saga with delay/await steps is triggered, it returns HTTP 202 with a `workflow_id`
2. **Steps execute**: Each step runs sequentially. Results are checkpointed to the database after each step
3. **Pause on delay/await**: The workflow saves its state and returns control
4. **Background ticker**: Every 5 seconds, the engine checks for:
   - Delayed workflows whose `resume_at` has passed → resumes execution
   - Expired workflows whose timeout has passed → marks as timed out, runs compensation
5. **Signal resumes**: External signals via REST API resume awaiting workflows immediately
6. **Crash recovery**: On restart, the engine queries active workflows from the database and resumes them

## Backward Compatibility

Sagas without delay or await steps continue to execute synchronously, exactly as before. The workflow engine only activates for sagas that need persistence. The `NeedsPersistence()` function detects this automatically — no configuration flag needed.

## Database Support

The workflow store supports three SQL dialects:

| Database | Placeholders | UPSERT Strategy | Timestamp Type |
|----------|-------------|-----------------|----------------|
| SQLite | `?` | `INSERT OR REPLACE` | TEXT (RFC3339) |
| PostgreSQL | `$1, $2, ...` | `ON CONFLICT DO UPDATE` | TIMESTAMP |
| MySQL | `?` | `ON DUPLICATE KEY UPDATE` | DATETIME |

The table is created with indexes on `status`, `(status, resume_at)`, and `await_event` for efficient background queries.
