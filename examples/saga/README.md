# Saga Example

Demonstrates the **Saga pattern** for distributed transactions with automatic compensation.

## What This Does

The `create_order` saga orchestrates a three-step process:

1. **Create order** in the database
2. **Reserve inventory** via an external API
3. **Process payment** via a payment API

If any step fails, compensations run **in reverse order**:
- Payment failed → release inventory → delete order
- Inventory failed → delete order

## Files

| File | Purpose |
|------|---------|
| `config.hcl` | Service configuration |
| `connectors.hcl` | Database and API connectors |
| `sagas.hcl` | Saga definition with steps and compensations |

## Running

```bash
mycel start --config ./examples/saga
```

## How It Works

```
POST /orders → saga "create_order"
  ├── step "order"     → INSERT into orders_db
  ├── step "inventory" → POST /reserve
  ├── step "payment"   → POST /charges
  │
  ├── on_complete → UPDATE orders SET status = "confirmed"
  └── on_failure  → POST /send (notification)
```

If step 3 (payment) fails:
```
  ├── compensate "inventory" → POST /release
  ├── compensate "order"     → DELETE from orders_db
  └── on_failure             → POST /send (notification)
```
