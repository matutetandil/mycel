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
  -H 'Content-Type: application/json' \
  -d '{"event": "pay"}'
# {"machine":"order_status","previous_state":"pending","current_state":"paid","event":"pay"}

# Ship order (requires tracking number — guard)
curl -X POST localhost:3000/orders/1/status \
  -H 'Content-Type: application/json' \
  -d '{"event": "ship", "data": {"tracking_number": "TRK123"}}'
```

The `ship` transition runs an action against the `notifications` connector,
which points at a service on port 6000 that this example does not include — so
that second command answers with a connection error unless something is
listening. Anything that accepts a POST will do:

```bash
python3 -m http.server 6000    # in another terminal
```

## Files

| File | Purpose |
|------|---------|
| `config.mycel` | Service configuration |
| `connectors.mycel` | REST API and database connectors |
| `order_status.mycel` | State machine definition |
| `update_order_status.mycel` | Flow that triggers state transitions |

## Running

The database file is created where the service is started from, so run these
from this directory.

```bash
cd examples/state-machine

# Create the orders table, with one order to transition
mycel migrate --config .

mycel start --config .
```

## Features

- **Guards**: CEL expressions that must be true for a transition (e.g., tracking number required for shipping)
- **Actions**: Connector operations executed during transitions (e.g., send notification on ship)
- **Final states**: Terminal states that cannot transition further
- **Automatic state persistence**: State stored in entity's `status` column
