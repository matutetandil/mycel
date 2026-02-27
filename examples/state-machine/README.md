# State Machine Example

Demonstrates **state machines** for entity lifecycle management.

## What This Does

Defines an `order_status` state machine with valid transitions:

```
pending → paid → shipped → delivered (final)
pending → cancelled (final)
paid → refunded (final)
shipped → returned → refunded (final)
```

A REST endpoint triggers transitions:

```bash
# Pay for order
curl -X POST localhost:3000/orders/1/status \
  -d '{"event": "pay"}'

# Ship order (requires tracking number — guard)
curl -X POST localhost:3000/orders/1/status \
  -d '{"event": "ship", "data": {"tracking_number": "TRK123"}}'
```

## Files

| File | Purpose |
|------|---------|
| `config.hcl` | Service configuration |
| `connectors.hcl` | REST API and database connectors |
| `machines.hcl` | State machine definition |
| `flows.hcl` | Flow that triggers state transitions |

## Running

```bash
mycel start --config ./examples/state-machine
```

## Features

- **Guards**: CEL expressions that must be true for a transition (e.g., tracking number required for shipping)
- **Actions**: Connector operations executed during transitions (e.g., send notification on ship)
- **Final states**: Terminal states that cannot transition further
- **Automatic state persistence**: State stored in entity's `status` column
