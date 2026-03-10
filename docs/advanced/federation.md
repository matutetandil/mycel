# GraphQL Federation

GraphQL Federation v2 lets multiple independent Mycel services compose into a single unified API. Each service owns a slice of the graph, exposes it as a subgraph, and a gateway (Apollo Router, Cosmo Router) stitches everything together at query time.

Every Mycel GraphQL server is automatically federation-ready — no extra configuration needed to get started.

## How Federation Works

A federated graph has three layers:

```
Client ──> Gateway (Apollo Router / Cosmo Router)
                ├── Products subgraph  (Mycel service)
                ├── Orders subgraph    (Mycel service)
                └── Users subgraph     (Mycel service)
```

The gateway discovers each subgraph's schema via `_service { sdl }`, an introspection endpoint Mycel exposes automatically. When a client sends a query that spans multiple subgraphs, the gateway breaks it into sub-requests, fetches each piece in parallel, and assembles the result.

## Auto-Discovery (No Configuration)

Every Mycel GraphQL connector automatically exposes the `_service { sdl }` endpoint. Point your gateway at it and it works:

```hcl
connector "api" {
  type     = "graphql"
  port     = 4001
  # federation block is optional — defaults to v2
}
```

```bash
# Verify it works
curl http://localhost:4001/graphql \
  -d '{"query": "{ _service { sdl } }"}'
```

## Without _key: Standalone Subgraph

If your types don't use `_key`, the service works as a subgraph — the gateway discovers all queries, mutations, and subscriptions. Cross-subgraph entity references are not available, but for many services that is all you need.

```hcl
# products service — standalone subgraph, no cross-references
type "Product" {
  id    = string { required = true }
  name  = string {}
  price = number {}
}

flow "get_products" {
  from { connector = "api", operation = "Query.products" }
  to   { connector = "db", target = "products" }
}
```

## With _key: Federated Entities

Adding `_key` to a type makes it a **federated entity** — another subgraph can reference it by its key field and the gateway resolves it automatically.

```hcl
# products service — entity subgraph
type "Product" {
  _key       = "sku"     # @key(fields: "sku") — gateway routes _entities queries here
  _shareable = true      # @shareable — multiple subgraphs can resolve this type

  sku   = string { required = true }
  name  = string {}
  price = number {}
}

# Entity resolver — called by the gateway when another subgraph needs a Product
flow "resolve_product" {
  entity = "Product"
  from   { connector = "api", operation = "Query.product" }
  to     { connector = "db", operation = "find_by_sku" }
}
```

When the gateway needs to resolve a `Product` referenced from another subgraph, it calls `_entities` with `{ __typename: "Product", sku: "ABC-123" }`. Mycel routes this to the `resolve_product` flow automatically.

## Referencing Entities from Another Subgraph

The Orders subgraph can include a `product` field that references a Product owned by the Products subgraph:

```hcl
# orders service
type "Order" {
  _key = "id"

  id      = string { required = true }
  user_id = string {}
  sku     = string {}

  # Reference to Product entity owned by products service
  product = object {
    sku       = string { _external = true }  # @external — owned by products service
    name      = string { _external = true }
    price     = number { _external = true }
  }
}
```

The gateway automatically fetches the product details from the Products subgraph and merges them into the Order response.

## Federation Directives

| HCL attribute | Generated SDL directive | Purpose |
|---------------|------------------------|---------|
| `_key = "id"` | `@key(fields: "id")` | Marks type as a resolvable entity |
| `_key = "id sku"` | `@key(fields: "id sku")` | Composite key (space-separated fields) |
| `_shareable = true` | `@shareable` | Multiple subgraphs can resolve this type |
| `_external = true` (on field) | `@external` | Field owned by another subgraph |
| `_requires = ["sku"]` (on field) | `@requires(fields: "sku")` | Fields needed from another subgraph before resolving this one |
| `_provides = ["name"]` (on field) | `@provides(fields: "name")` | Fields this subgraph can provide |
| `_inaccessible = true` | `@inaccessible` | Hidden from public schema, visible to gateway |
| `_override = "old-service"` | `@override(from: "old-service")` | This subgraph now owns a field previously owned elsewhere |

## GraphQL Subscriptions in Federation

Subscriptions work in federated setups too. Mycel publishes events that the gateway routes to subscribed clients.

```hcl
# products service — publish price updates
flow "broadcast_price_update" {
  from { connector = "rabbit", operation = "price_updates" }
  to {
    connector = "api"
    operation = "publish"
    target    = "Subscription.priceUpdated"
  }
}

# Define the subscription in the type system
type "PriceUpdate" {
  sku   = string {}
  price = number {}
}
```

Clients subscribe via the gateway's WebSocket endpoint using the `graphql-ws` protocol. Mycel handles the server-side subscription relay automatically.

## Connector Configuration

The `federation` block is optional. It defaults to Federation v2 automatically:

```hcl
connector "api" {
  type = "graphql"
  port = 4001

  # Optional — only needed to override defaults
  federation {
    version = 2        # Default: 2
    enabled = true     # Default: true when type = "graphql"
  }
}
```

## Gateway Setup Example

Point Cosmo Router or Apollo Router at your Mycel subgraphs. With Cosmo Router via Docker Compose:

```yaml
services:
  router:
    image: ghcr.io/wundergraph/cosmo/router:latest
    environment:
      GRAPH_API_TOKEN: ${COSMO_TOKEN}
      SUBGRAPHS: |
        products=http://products:4001/graphql
        orders=http://orders:4002/graphql
        users=http://users:4003/graphql
    ports:
      - "4000:4000"

  products:
    image: ghcr.io/matutetandil/mycel:latest
    volumes:
      - ./products:/etc/mycel
    ports:
      - "4001:4001"

  orders:
    image: ghcr.io/matutetandil/mycel:latest
    volumes:
      - ./orders:/etc/mycel
    ports:
      - "4002:4002"
```

The gateway polls `_service { sdl }` on startup and stitches the schemas together. Queries crossing subgraph boundaries are automatically planned and executed in parallel.

## SDL Generation

Mycel generates Federation v2-compliant SDL from your HCL types and flows. You can inspect what gets sent to the gateway:

```bash
mycel export graphql-schema
```

The SDL includes all `@key`, `@external`, `@shareable`, and other federation directives derived from your HCL type annotations.

## See Also

- [GraphQL Connector](../connectors/graphql.md) — connector configuration, named operations, subscriptions
- [graphql-federation example](../../examples/graphql-federation) — complete multi-service setup with Cosmo Router
- [Configuration Reference — Types](../reference/configuration.md#types) — all field directives including federation annotations
