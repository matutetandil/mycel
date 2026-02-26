# GraphQL Federation Example

This example demonstrates a federated GraphQL subgraph built with Mycel. It shows how to expose a **product catalog** as a Federation v2 subgraph with entity resolvers, subscriptions, and standard CRUD operations -- all without writing code.

## Directory Structure

```
graphql-federation/
├── config.hcl          # Service configuration (product-subgraph)
├── connectors.hcl      # GraphQL server (federation + subscriptions), SQLite, RabbitMQ
├── types.hcl           # Product and Review types with federation directives
├── flows.hcl           # Queries, mutations, entity resolvers, subscriptions
└── README.md           # This file
```

## Features Demonstrated

| Feature | Description |
|---------|-------------|
| **Federation v2** | `_key`, `_shareable` directives on types |
| **Entity Resolvers** | `resolve_product` and `resolve_review` flows with `entity = "..."` |
| **Subscriptions** | Queue-driven real-time updates via `Subscription.productUpdated` |
| **Queries** | List/get products and reviews |
| **Mutations** | Create products, update prices, add reviews |

## Quick Start

```bash
# From project root
cd examples/graphql-federation

# Start the service
mycel start --config .

# Access GraphQL Playground
open http://localhost:4000/playground
```

## Federation Directives

Types declare federation directives using underscore-prefixed attributes:

```hcl
type "Product" {
  _key       = "sku"       # @key(fields: "sku")
  _shareable = true        # @shareable

  sku   = string { required = true }
  name  = string { required = true }
  price = number { min = 0 }
}
```

Entity resolvers are declared with the `entity` attribute on a flow:

```hcl
flow "resolve_product" {
  entity = "Product"
  # ...resolves Product by its @key field (sku)
}
```

## Subscriptions

Subscriptions are event-driven. A message queue (RabbitMQ) publishes events, and Mycel pushes them to GraphQL subscribers:

```
RabbitMQ (product.updated) --> flow --> Subscription.productUpdated --> WebSocket clients
```

## Testing

```bash
# List all products
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ products { sku name price category } }"}'

# Get a single product
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "{ product(sku: \"ABC-123\") { sku name price inStock } }"}'

# Create a product
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { createProduct(input: { sku: \"ABC-123\", name: \"Widget\", price: 19.99, category: \"gadgets\" }) { sku name price } }"}'

# Add a review
curl -X POST http://localhost:4000/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { addReview(input: { productSku: \"ABC-123\", rating: 5, comment: \"Excellent product!\", author: \"Alice\" }) { id rating comment } }"}'

# Subscribe to product updates (WebSocket)
# Use the GraphQL Playground at http://localhost:4000/playground
# and run:
#   subscription { productUpdated { sku name price updatedAt } }
```

## How It Fits in a Federated Graph

This subgraph is designed to be composed with other subgraphs via a federation gateway (Apollo Router, Cosmo Router, etc.):

```
                    ┌─────────────────┐
                    │  Gateway/Router  │
                    └──────┬──────────┘
               ┌───────────┼───────────┐
               v           v           v
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Products │ │  Orders  │ │  Users   │
        │ (this)   │ │ subgraph │ │ subgraph │
        └──────────┘ └──────────┘ └──────────┘
```

Other subgraphs can reference `Product` by its `@key(fields: "sku")`, and the gateway will call the `resolve_product` entity resolver to fetch the data.

## See Also

- [GraphQL Example](../graphql) - Basic GraphQL server (non-federated)
- [GraphQL Optimization](../graphql-optimization) - Field selection, step skipping, DataLoader
- [Message Queue Example](../mq) - RabbitMQ publish/subscribe patterns
