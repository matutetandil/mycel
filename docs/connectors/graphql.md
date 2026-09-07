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
    enabled             = true
    path                = "/graphql/ws"
    keep_alive_interval = "30s"
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

## How a flow's answer fits the field

A flow answers with whatever its last stage produced: a `transform` produces an object, a step with one row is flattened to that row, a step with no rows is `null`. The server fits that answer to the type the field declares, so the same flow shapes serve any field:

| Field declared as | Flow answered with | Served as |
|---|---|---|
| `[Item!]!` | a list | the list |
| `[Item!]!` | an object with **one** key holding a list — `transform { items = "as_list(step.rows).map(r, {'name': r.name})" }` | that list |
| `[Item!]!` | a single object (a one-row step) | `[object]` |
| `[Item!]!` | `null` (a step that matched nothing) | `[]` |
| `String!`, `Int!`, `Float!`, `ID!`, an enum | an object with **one** key — `transform { value = "step.page.title" }` | that key's value |
| `String!` | an object with several keys | an error naming the field and the keys |
| `Boolean!` | `{"affected": n}` or a record | whether the write happened |
| `JSON`, a custom scalar | an object | the object, untouched |
| an object type | an object | the object |

So a list field is answered by a transform with a single mapping whose value is the list, and a scalar field by a transform with a single mapping whose value is the scalar. A `mycel validate` does not check this against the schema; the request reports it.

## Key Features

- **Auto-schema**: Types defined in HCL become GraphQL types automatically
- **GraphiQL IDE**: Built-in when `playground = true`
- **Federation v2**: Always exposes `_service { sdl }` — no config needed
- **Subscriptions**: Flow-triggered via `Subscription.name` in `to` blocks
- **Query Optimization**: Automatic field selection and step skipping
- **Concurrent resolution**: the fields of one query are resolved together rather than one after another, and two fields asking for the same thing run once

## Example

```hcl
flow "get_users" {
  from {
    connector = "api"
    operation = "Query.users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

flow "create_user" {
  from {
    connector = "api"
    operation = "Mutation.createUser"
  }
  to {
    connector = "db"
    target    = "users"
  }
}

# Subscription triggered by queue
flow "order_updates" {
  from {
    connector = "rabbit"
    operation = "order.updated"
  }
  to {
    connector = "api"
    operation = "Subscription.orderUpdated"
    filter    = "input.user_id == context.connection_params.userId"
  }
}
```

See the [graphql example](https://github.com/matutetandil/mycel/tree/main/examples/graphql), [graphql-federation example](https://github.com/matutetandil/mycel/tree/main/examples/graphql-federation), and [graphql-optimization example](https://github.com/matutetandil/mycel/tree/main/examples/graphql-optimization) for complete setups.

---

> **Full configuration reference:** See [GraphQL](../reference/configuration.md#graphql) in the Configuration Reference.
