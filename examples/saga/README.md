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
| `config.mycel` | Service configuration |
| `connectors.mycel` | Database and API connectors |
| `create_order.mycel` | Saga definition with steps and compensations |

## Running

The three services the saga calls — inventory, payments, notifications — are
not part of this example. Point it at your own, or at anything that answers:

```bash
export INVENTORY_URL=http://localhost:5000
export PAYMENTS_URL=http://localhost:4000
export NOTIFICATIONS_URL=http://localhost:6000

mycel migrate --config ./examples/saga
mycel start --config ./examples/saga
```

## Try It

```bash
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","amount":99.5,"sku":"WIDGET-1","quantity":2,"customer_id":"cus_1","user_email":"buyer@example.com"}'
```

The order is written, the inventory is reserved and the payment is charged. Make
the payment service answer with an error and the compensations run backwards
from there: the reservation is released and the order row is deleted, which you
can see afterwards.

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
