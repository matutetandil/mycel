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

A `step`, `to` or `enrich` calling the client can add request headers of its own — `headers = { Store = "input.store" }` — evaluated per request and sent over the connector's `headers` on the same name. See [Headers per request](rest.md#headers-per-request).

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

So a list field is answered by a transform with a single mapping whose value is the list, and a scalar field by a transform with a single mapping whose value is the scalar. A `mycel validate` does not check this against the schema; the request reports it. A `Subscription` field is fitted the same way: what a flow publishes to it is shaped to the field's declared type before it reaches the subscriber.

## A value with a default may be left out

A field of an input object, or an argument, that declares a default is optional — including when its type is non-null, which is how several servers write "not optional, but you rarely need to set it":

```graphql
input EchoInput {
  name: String!
  loud: Boolean! = false
}
```

`echo(input: {name: "hello"})` is accepted and the flow sees `input.loud` as `false`. The same holds for values sent as variables, for arguments (`page(limit: Int! = 25)`), and inside a subscription's selection. A non-null value with **no** default is still required, and a value the caller does send is never overwritten.

Earlier versions refused such a request during validation — `In field "loud": Expected "Boolean!", found null` — before any flow ran, so a faithful copy of another server's SDL rejected traffic the original accepted.

## A Subscription field declared in SDL

A `Subscription` field written in the schema file keeps what it declares — its type, its arguments, its description — and the flow whose `to` publishes to it supplies the events:

```graphql
type Subscription {
  orderPlaced(store: String! = "main"): Order
}
```

`subscription { orderPlaced { id } }` selects subfields because the field returns `Order`. A flow that sets `returns` decides the type instead, the same way it does for a query field, and a field that no schema declares is published as `JSON`.

Earlier versions read only the Query and Mutation types out of the SDL: a declared subscription field ran as `JSON` whatever it said, its arguments were dropped, and `_service { sdl }` published a contract the running schema did not implement.

## Key Features

- **Auto-schema**: Types defined in HCL become GraphQL types automatically
- **GraphiQL IDE**: Built-in when `playground = true`
- **Federation v2**: Always exposes `_service { sdl }` — no config needed
- **Subscriptions**: Flow-triggered via `Subscription.name` in `to` blocks
- **Query Optimization**: Automatic field selection and step skipping. Skipping applies to a field returning an object, whose requested names are the transform's mapping names; a list field is asked for its element's fields, so every step runs
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
