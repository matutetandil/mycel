# Sagas, State Machines, and Long-Running Workflows

This guide covers three related patterns for managing complex, multi-step processes:

- **Sagas** — distributed transactions with automatic compensation (rollback)
- **State Machines** — state-based entity lifecycle management
- **Long-Running Workflows** — sagas that pause for timers or external events

## Sagas

A saga orchestrates a multi-step distributed transaction. Each step has an `action` (forward operation) and an optional `compensate` (rollback). If any step fails, compensations run in reverse order for all previously completed steps.

```hcl
saga "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  step "order" {
    action {
      connector = "orders_db"
      operation = "INSERT"
      target    = "orders"
      data      = { status = "pending", user_id = "input.user_id" }
    }
    compensate {
      connector = "orders_db"
      operation = "DELETE"
      target    = "orders"
      where     = { id = "step.order.id" }
    }
  }

  step "payment" {
    action {
      connector = "stripe"
      operation = "POST /charges"
      body      = { amount = "input.amount", currency = "input.currency" }
    }
    compensate {
      connector = "stripe"
      operation = "POST /refunds"
      body      = { charge = "step.payment.charge_id" }
    }
  }

  step "inventory" {
    action {
      connector = "inventory_db"
      operation = "UPDATE"
      target    = "inventory"
      set       = { reserved = "reserved + input.quantity" }
      where     = { product_id = "input.product_id" }
    }
    compensate {
      connector = "inventory_db"
      operation = "UPDATE"
      target    = "inventory"
      set       = { reserved = "reserved - input.quantity" }
      where     = { product_id = "input.product_id" }
    }
  }

  on_complete {
    connector = "orders_db"
    operation = "UPDATE"
    target    = "orders"
    set       = { status = "confirmed" }
    where     = { id = "step.order.id" }
  }

  on_failure {
    connector = "notifications"
    operation = "POST /send"
    template  = "order_failed"
    data      = { user_id = "input.user_id", reason = "error.message" }
  }
}
```

### How Sagas Work

1. Steps execute in order
2. Each completed step's result is available as `step.NAME.*` in subsequent steps
3. If a step fails, all previously completed compensations run in **reverse order**
4. `on_complete` runs after all steps succeed
5. `on_failure` runs after all compensations complete (regardless of compensation result)

### Step-Level Error Handling

Mark non-critical steps with `on_error = "skip"` to continue without triggering compensation:

```hcl
step "send_notification" {
  on_error = "skip"
  action {
    connector = "email"
    operation = "send"
  }
  # No compensate block — skip means: failure is acceptable
}
```

### Simple Sagas (No Delay/Await)

Simple sagas execute synchronously and return a response when all steps complete. No extra configuration is needed — just define the saga block.

## State Machines

A state machine defines valid states and transitions for an entity. State is persisted in the entity's `status` column. Transitions can have guards (CEL conditions) and side-effect actions.

```hcl
state_machine "order_status" {
  initial = "pending"

  state "pending" {
    on "pay"    { transition_to = "paid" }
    on "cancel" { transition_to = "cancelled" }
  }

  state "paid" {
    on "ship" {
      transition_to = "shipped"
      guard         = "input.tracking_number != ''"
      action {
        connector = "notifications"
        operation = "POST /send"
        template  = "order_shipped"
        data      = { tracking = "input.tracking_number" }
      }
    }
    on "refund" { transition_to = "refunded" }
  }

  state "shipped" {
    on "deliver" { transition_to = "delivered" }
  }

  state "delivered" { final = true }
  state "cancelled" { final = true }
  state "refunded"  { final = true }
}
```

### Triggering Transitions

Use the `state_transition` block in a flow:

```hcl
flow "update_order_status" {
  from { connector = "api", operation = "POST /orders/:id/events" }

  state_transition {
    machine = "order_status"  # Name of the state_machine block
    entity  = "orders"        # Database table
    id      = "input.params.id"
    event   = "input.event"   # e.g., "pay", "ship", "cancel"
    data    = "input.data"    # Additional data for guards and actions
  }

  to { connector = "db", target = "orders" }
}
```

### State Machine Features

- **Guards** — CEL expressions that prevent invalid transitions (e.g., require tracking number before shipping)
- **Actions** — side effects executed during a transition (e.g., send notification)
- **Final states** — states that cannot transition further (`final = true`)
- **Initial state** — applied when the entity has no `status` value yet

### Error Handling

If a guard fails (returns false), the transition is rejected with an error. If no valid transition exists for the event in the current state, an error is returned.

## Long-Running Workflows

When a saga includes steps that pause execution, Mycel persists the workflow state to a database so it survives restarts.

### Configuration

Enable workflow persistence in the service block:

```hcl
service {
  name    = "order-service"
  version = "1.0.0"

  workflow {
    storage     = "db"              # Database connector name
    table       = "mycel_workflows" # Table name (default)
    auto_create = true              # Create table on startup
  }
}
```

### Delay Steps

Pause execution for a duration:

```hcl
saga "onboarding" {
  from { connector = "api", operation = "POST /onboard" }

  step "create_account" {
    action { connector = "db", operation = "INSERT", target = "accounts" }
  }

  step "wait_24h" {
    delay = "24h"
  }

  step "send_welcome_email" {
    action {
      connector = "email"
      operation = "send"
      data      = { template = "welcome", user_id = "step.create_account.id" }
    }
  }
}
```

When the saga reaches the delay step, it saves state to the database and pauses. A background ticker checks for workflows whose delay has expired and resumes them automatically.

### Await/Signal Steps

Pause until an external system sends a signal:

```hcl
saga "loan_approval" {
  timeout = "7d"

  from { connector = "api", operation = "POST /loans" }

  step "submit" {
    action { connector = "db", operation = "INSERT", target = "loans" }
  }

  step "wait_approval" {
    await   = "loan_approved"    # Pause until this event is signaled
    timeout = "48h"              # Step-level timeout
  }

  step "disburse" {
    action { connector = "banking", operation = "POST /transfers" }
  }

  on_failure {
    connector = "notifications"
    operation = "POST /send"
    template  = "loan_rejected"
  }
}
```

Signal the workflow via the auto-registered REST API:

```bash
POST /workflows/{workflow_id}/signal/loan_approved
Content-Type: application/json

{ "approved_by": "manager@company.com", "note": "Approved" }
```

The signal data is available in subsequent steps as `input.signal`.

### Workflow REST API

The engine auto-registers three endpoints:

| Endpoint | Description |
|----------|-------------|
| `GET /workflows/{id}` | Get workflow status, current step, timestamps |
| `POST /workflows/{id}/signal/{event}` | Resume a paused workflow awaiting an event |
| `POST /workflows/{id}/cancel` | Cancel an active workflow (runs compensation) |

When a saga with delay/await steps is triggered, the response is HTTP 202 with the workflow ID:

```json
{
  "workflow_id": "wf-abc123",
  "status": "running",
  "started_at": "2024-01-15T10:30:00Z"
}
```

### Supported Databases

Workflow state can be persisted to: PostgreSQL, MySQL, SQLite.

## See Also

- [Sagas](#sagas)
- [State Machines](#state-machines)
- [Long-Running Workflows](#long-running-workflows)
- [Examples: Saga](../../examples/saga)
- [Examples: State Machine](../../examples/state-machine)
