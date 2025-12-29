# Message Queue Example

This example demonstrates Mycel's integration with RabbitMQ for event-driven architecture patterns.

## Features

- **Publish/Subscribe**: REST API triggers message publishing to RabbitMQ
- **Event Processing**: Consume messages and store in database
- **Fan-out Pattern**: Single event triggers multiple flows
- **Topic Routing**: Route messages using AMQP topic patterns (`order.*`, `notification.#`)

## Prerequisites

1. **RabbitMQ Server**:
   ```bash
   # Using Docker
   docker run -d --name rabbitmq \
     -p 5672:5672 \
     -p 15672:15672 \
     rabbitmq:3-management
   ```

2. **Build Mycel**:
   ```bash
   make build
   ```

## Configuration

### Connectors

| Connector | Type | Purpose |
|-----------|------|---------|
| `api` | REST | HTTP endpoints for order management |
| `order_events` | MQ (Consumer) | Consume order events from queue |
| `notifications` | MQ (Publisher) | Publish notifications |
| `db` | SQLite | Persist processed orders |

### Flows

| Flow | Source | Target | Description |
|------|--------|--------|-------------|
| `publish_order` | POST /orders | notifications | Create order, publish event |
| `process_order` | order_events | db | Consume events, store in DB |
| `notify_order` | order_events | notifications | Send email notifications |
| `get_order` | GET /orders/:id | db | Query order by ID |
| `list_orders` | GET /orders | db | List all orders |

## Usage

### Start the Service

```bash
./bin/mycel start --config ./examples/mq
```

Expected output:
```
  ╔══════════════════════════════════════╗
  ║           M Y C E L                  ║
  ╚══════════════════════════════════════╝

    Connectors:
      ✓ api (rest) listening on :3000
      ✓ order_events (mq/rabbitmq) consuming from orders
      ✓ notifications (mq/rabbitmq) publishing to notifications_exchange
      ✓ db (database) → ./data/mq_demo.db

    Flows:
      POST   /orders          → notifications:order.created
      MQ     order.*          → db:orders
      MQ     order.created    → notifications:notification.email
      GET    /orders/:id      → db:orders
      GET    /orders          → db:orders

    Ready! Press Ctrl+C to stop.
```

### Create an Order

```bash
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{
    "product": "Widget Pro",
    "quantity": 5,
    "customer": {
      "name": "John Doe",
      "email": "john@example.com"
    }
  }'
```

This triggers:
1. Message published to `order.created` routing key
2. Consumer picks up message, stores in database
3. Notification flow sends email notification

### Check Order Status

```bash
curl http://localhost:3000/orders/<order_id>
```

### List All Orders

```bash
curl http://localhost:3000/orders
```

## RabbitMQ Management

Access the RabbitMQ management UI at http://localhost:15672 (guest/guest):

- **Exchanges**: View `orders_exchange` and `notifications_exchange`
- **Queues**: Monitor `orders` queue
- **Connections**: See active Mycel connections

## Architecture

```
┌─────────────┐     ┌─────────────────┐     ┌─────────────┐
│   Client    │────>│  REST API (3000)│────>│  RabbitMQ   │
│  (curl/app) │     │   publish_order │     │  Publisher  │
└─────────────┘     └─────────────────┘     └──────┬──────┘
                                                   │
                                                   v
                    ┌─────────────────┐     ┌─────────────┐
                    │    SQLite DB    │<────│  RabbitMQ   │
                    │   (orders.db)   │     │  Consumer   │
                    └─────────────────┘     └──────┬──────┘
                                                   │
                                                   v
                                            ┌─────────────┐
                                            │ Notification│
                                            │  Publisher  │
                                            └─────────────┘
```

## Configuration Options

### Consumer Options

```hcl
consumer {
  auto_ack    = false    # Manual ack for reliability
  concurrency = 2        # Parallel consumers
  prefetch    = 10       # Messages to prefetch
  tag         = "mycel"  # Consumer tag
}
```

### Publisher Options

```hcl
publisher {
  exchange     = "my_exchange"
  routing_key  = "my.key"
  persistent   = true           # Survive broker restart
  mandatory    = false          # Fail if no queue bound
  confirms     = false          # Publisher confirms
  content_type = "application/json"
}
```

### Exchange Binding

```hcl
exchange {
  name        = "orders"
  type        = "topic"     # direct, fanout, topic, headers
  durable     = true
  routing_key = "order.#"   # Binding pattern
}
```

## Routing Key Patterns

RabbitMQ topic exchanges support pattern matching:

| Pattern | Matches | Example |
|---------|---------|---------|
| `order.created` | Exact match | `order.created` |
| `order.*` | One word after `order.` | `order.created`, `order.updated` |
| `order.#` | Zero or more words | `order`, `order.created`, `order.created.urgent` |
| `*.created` | Any word before `.created` | `order.created`, `user.created` |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `RABBITMQ_HOST` | localhost | RabbitMQ server host |
| `RABBITMQ_PORT` | 5672 | RabbitMQ server port |
| `RABBITMQ_USER` | guest | RabbitMQ username |
| `RABBITMQ_PASS` | guest | RabbitMQ password |
