# Real-Time and Event-Driven Patterns

Mycel provides several mechanisms for real-time data delivery and event-driven architectures.

## Overview

| Mechanism | Protocol | Direction | Best For |
|-----------|----------|-----------|----------|
| [WebSocket](#websocket) | WebSocket | Bidirectional | Chat, collaboration, gaming |
| [SSE](#server-sent-events-sse) | HTTP | Server → Client | Dashboards, feeds, notifications |
| [CDC](#change-data-capture-cdc) | PostgreSQL WAL | Database → Flow | Audit logs, sync, event sourcing |
| [GraphQL Subscriptions](#graphql-subscriptions) | WebSocket/graphql-ws | Server → Client | Real-time GraphQL data |

## WebSocket

The WebSocket connector provides bidirectional real-time communication with support for rooms (topic-based channels).

```hcl
connector "ws" {
  type = "websocket"
  port = 8080
  path = "/ws"
}
```

### Broadcast to All Clients

```hcl
flow "broadcast_price" {
  from {
    connector = "rabbit"
    operation = "price.updated"
  }
  to {
    connector = "ws"
    operation = "broadcast"
  }
}
```

### Room-Based Broadcasting

```hcl
flow "send_to_room" {
  from {
    connector = "rabbit"
    operation = "messages"
  }
  to {
    connector = "ws"
    operation = "room:${input.channel_id}"
  }
}
```

### Per-User Filtering

Only deliver events to subscribers that match a condition:

```hcl
flow "send_user_notification" {
  from {
    connector = "rabbit"
    operation = "notifications"
  }
  to {
    connector = "ws"
    operation = "room:notifications"
    filter    = "input.user_id == context.params.user_id"
  }
}
```

The `filter` expression compares event data against `context.params` set during the WebSocket handshake.

### Receiving Messages (Flow Trigger)

WebSocket can also be a flow source — when a client sends a message, it triggers a flow:

```hcl
flow "handle_ws_message" {
  from {
    connector = "ws"
    operation = "message"
  }
  to {
    connector = "db"
    target    = "messages"
  }
}
```

See the [WebSocket connector docs](../connectors/websocket.md) for full configuration.

## Server-Sent Events (SSE)

SSE provides unidirectional push from server to client over standard HTTP. Simpler than WebSockets when clients only need to receive events.

```hcl
connector "sse" {
  type = "sse"
  port = 8080
  path = "/events"
}
```

### Push Events to All Clients

```hcl
flow "push_updates" {
  from {
    connector = "rabbit"
    operation = "system.updates"
  }
  to {
    connector = "sse"
    operation = "broadcast"
  }
}
```

### Push to a Specific Stream

```hcl
flow "push_order_update" {
  from {
    connector = "rabbit"
    operation = "order.updated"
  }
  to {
    connector = "sse"
    operation = "stream:orders"
    filter    = "input.user_id == context.params.user_id"
  }
}
```

Clients subscribe to `GET /events/orders?user_id=123` and receive a `text/event-stream` response.

SSE automatically handles heartbeats to keep connections alive and respects CORS configuration.

See the [SSE connector docs](../connectors/sse.md) for full configuration.

## Change Data Capture (CDC)

CDC streams database changes as flow events using PostgreSQL's logical replication. This enables reactive patterns without polling.

```hcl
connector "cdc" {
  type              = "cdc"
  driver            = "postgres"
  connection_string = env("PG_REPLICATION_URL")
  tables            = ["orders", "products", "users"]
}
```

The connection string must be for a user with replication privileges.

### React to Any Change on a Table

```hcl
flow "sync_to_elastic" {
  from {
    connector = "cdc"
    operation = "orders.*"
  }
  to {
    connector = "es"
    target    = "orders"
    operation = "index"
  }
}
```

The `.*` wildcard matches any operation (`INSERT`, `UPDATE`, `DELETE`).

### Filter by Operation Type

```hcl
flow "notify_on_insert" {
  from {
    connector = "cdc"
    operation = "orders.INSERT"
  }
  to {
    connector = "rabbit"
    target    = "order.created"
  }
}

flow "audit_updates" {
  from {
    connector = "cdc"
    operation = "users.UPDATE"
  }
  to {
    connector = "audit_db"
    target    = "INSERT audit_log"
  }
}
```

### Available Operations

| Operation | Description |
|-----------|-------------|
| `TABLE.*` | All changes on the table |
| `TABLE.INSERT` | New row insertions |
| `TABLE.UPDATE` | Row updates |
| `TABLE.DELETE` | Row deletions |

CDC input data includes: `_operation` (INSERT/UPDATE/DELETE), `_table`, `_old` (previous values on UPDATE/DELETE), and the current row fields.

See the [CDC connector docs](../connectors/cdc.md) for PostgreSQL setup and full configuration.

## GraphQL Subscriptions

GraphQL subscriptions push data to clients in real time over WebSocket connections (using the graphql-ws protocol).

### Server-Side Subscriptions

Configure the GraphQL connector with subscriptions enabled:

```hcl
connector "gql" {
  type = "graphql"
  port = 4000

  subscriptions {
    enabled   = true
    transport = "websocket"
    path      = "/graphql/ws"
    keepalive = "30s"
  }
}
```

Publish to a subscription topic from any flow:

```hcl
flow "order_updates" {
  from {
    connector = "rabbit"
    operation = "order.updated"
  }

  transform {
    id     = "input.order_id"
    status = "input.status"
  }

  to {
    connector = "gql"
    operation = "Subscription.orderUpdated"
    filter    = "input.user_id == context.connection_params.userId"
  }
}
```

The `filter` expression enables per-user filtering — each subscriber only receives events that match their connection parameters (passed during WebSocket `connection_init`).

### Client-Side Subscriptions

Mycel can subscribe to an external GraphQL service and use each event as a flow trigger:

```hcl
connector "external_gql" {
  type     = "graphql"
  driver   = "client"
  endpoint = "http://other-service:4000/graphql"

  subscriptions {
    enabled = true
    path    = "/subscriptions"
  }
}

flow "react_to_price_change" {
  from {
    connector = "external_gql"
    operation = "Subscription.priceChanged"
  }
  to {
    connector = "db"
    target    = "price_updates"
  }
}
```

The client automatically reconnects with exponential backoff if the connection drops.

See the [GraphQL connector docs](../connectors/graphql.md) and [federation example](../../examples/graphql-federation) for complete setup.

## Combining Real-Time Patterns

CDC → WebSocket (database changes pushed to browser):

```hcl
flow "live_order_updates" {
  from {
    connector = "cdc"
    operation = "orders.*"
  }

  transform {
    order_id   = "input.id"
    status     = "input.status"
    updated_at = "input.updated_at"
    operation  = "input._operation"
  }

  to {
    connector = "ws"
    operation = "room:orders"
    filter    = "input.user_id == context.params.user_id"
  }
}
```

Queue → SSE (message queue events streamed to browser):

```hcl
flow "stream_notifications" {
  from {
    connector = "rabbit"
    operation = "notifications"
  }

  to {
    connector = "sse"
    operation = "stream:notifications"
    filter    = "input.user_id == context.params.user_id"
  }
}
```

## See Also

- [WebSocket connector](../connectors/websocket.md)
- [SSE connector](../connectors/sse.md)
- [CDC connector](../connectors/cdc.md)
- [GraphQL connector](../connectors/graphql.md)
