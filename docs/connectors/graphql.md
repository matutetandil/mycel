# GraphQL

Expose a GraphQL schema (server) or query external GraphQL APIs (client). The server auto-generates a schema from your types and flows, includes a built-in GraphiQL IDE, and supports Federation v2 out of the box. The client can execute queries, mutations, and subscribe to real-time events.

## Server Configuration

```hcl
connector "api" {
  type       = "graphql"
  driver     = "server"
  port       = 4000
  endpoint   = "/graphql"
  playground = true

  cors {
    origins = ["*"]
    methods = ["GET", "POST", "OPTIONS"]
  }

  # Optional: subscriptions
  subscriptions {
    enabled   = true
    transport = "websocket"
    path      = "/graphql/ws"
    keepalive = "30s"
  }

  # Optional: federation (auto-enabled, override version if needed)
  federation {
    enabled = true
    version = 2
  }
}
```

## Client Configuration

```hcl
connector "external_gql" {
  type        = "graphql"
  driver      = "client"
  endpoint    = "https://api.example.com/graphql"
  timeout     = "30s"
  retry_count = 3

  auth {
    type  = "bearer"
    token = env("GRAPHQL_TOKEN")
  }

  # Optional: subscribe to remote events
  subscriptions {
    enabled = true
    path    = "/subscriptions"
  }
}
```

## Operations

**Server (source):** `Query.fieldName`, `Mutation.fieldName`, `Subscription.fieldName`.

**Client (target):** GraphQL query/mutation strings or `Subscription.fieldName` for real-time.

## Key Features

- **Auto-schema**: Types defined in HCL become GraphQL types automatically
- **GraphiQL IDE**: Built-in when `playground = true`
- **Federation v2**: Always exposes `_service { sdl }` — no config needed
- **Subscriptions**: Flow-triggered via `Subscription.name` in `to` blocks
- **Query Optimization**: Automatic field selection, step skipping, DataLoader

## Example

```hcl
flow "get_users" {
  from { connector = "api", operation = "Query.users" }
  to   { connector = "db", target = "users" }
}

flow "create_user" {
  from { connector = "api", operation = "Mutation.createUser" }
  to   { connector = "db", target = "users" }
}

# Subscription triggered by queue
flow "order_updates" {
  from { connector = "rabbit", operation = "order.updated" }
  to {
    connector = "api"
    operation = "Subscription.orderUpdated"
    filter    = "input.user_id == context.connection_params.userId"
  }
}
```

See the [graphql example](../../examples/graphql/), [graphql-federation example](../../examples/graphql-federation/), and [graphql-optimization example](../../examples/graphql-optimization/) for complete setups.
