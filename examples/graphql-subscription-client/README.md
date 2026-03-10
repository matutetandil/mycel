# GraphQL Subscription Client Example

This example demonstrates Mycel acting as a GraphQL subscription **client** — connecting to an external GraphQL server's WebSocket and receiving real-time events. Think of it like a message queue consumer, but for GraphQL subscriptions.

## Directory Structure

```
graphql-subscription-client/
├── config.hcl          # Service configuration (price-tracker)
├── connectors.hcl      # GraphQL client with subscriptions, SQLite, REST
├── flows.hcl           # Subscription handlers + REST API for stored data
└── README.md           # This file
```

## Features Demonstrated

| Feature | Description |
|---------|-------------|
| **Client Subscriptions** | Subscribe to remote GraphQL events via WebSocket |
| **graphql-ws Protocol** | Standard `connection_init` → `subscribe` → `next` handshake |
| **Auto Reconnect** | Exponential backoff on disconnect (1s → 60s) |
| **Event Processing** | Transform and store each received event |

## How It Works

```
External GraphQL Server                    This Service (price-tracker)
┌─────────────────────┐                   ┌──────────────────────────┐
│                     │   WebSocket       │                          │
│  Subscription.      │◄──────────────────│  products_api (client)   │
│  priceChanged       │   graphql-ws      │  + subscriptions block   │
│                     │──────────────────►│                          │
│  (publishes events) │   next messages   │  flow: track_price_changes│
│                     │                   │    → transform → db      │
└─────────────────────┘                   │                          │
                                          │  api (REST :3000)        │
                                          │  GET /prices/:sku/history│
                                          └──────────────────────────┘
```

1. Mycel opens a WebSocket connection to the external GraphQL server
2. Subscribes to `priceChanged` and `productCreated` using the graphql-ws protocol
3. Each event triggers the corresponding flow, which transforms and stores the data
4. A REST API exposes the stored data for querying
5. If the connection drops, Mycel reconnects automatically with exponential backoff

## Quick Start

```bash
# Start the external GraphQL server first (e.g., the graphql-federation example)
cd ../graphql-federation
mycel start --config .

# In another terminal, start this service
cd ../graphql-subscription-client
mycel start --config .
```

## Configuration

The key is the `subscriptions` block on a `driver = "client"` connector:

```hcl
connector "products_api" {
  type     = "graphql"
  driver   = "client"
  endpoint = "http://localhost:4000/graphql"

  subscriptions {
    enabled = true
    path    = "/subscriptions"    # WebSocket path on the remote server
  }
}
```

Then use it as a flow source:

```hcl
flow "track_price_changes" {
  from {
    connector = "products_api"
    operation = "Subscription.priceChanged"
  }
  to {
    connector = "db"
    target    = "price_history"
  }
}
```

## Testing

```bash
# Check tracked products
curl http://localhost:3000/products

# Check price history for a specific SKU
curl http://localhost:3000/prices/ABC-123/history
```

## See Also

- [GraphQL Federation](../graphql-federation) — Server-side subscriptions (the other end of this pattern)
- [GraphQL Server & Client](../graphql) — Basic GraphQL queries and mutations
- [Message Queue](../mq) — RabbitMQ/Kafka consumers (similar event-driven pattern)
