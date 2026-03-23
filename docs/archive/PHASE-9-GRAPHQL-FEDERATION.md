# Phase 9: GraphQL Federation Complete

> **Status:** Planned
> **Priority:** High
> **Estimated Complexity:** High
> **Depends On:** Phase 8 (GraphQL Query Optimization)

## Overview

Extend the existing GraphQL connector to support real-time subscriptions and Apollo/Cosmo Federation subgraph capabilities. This phase closes the remaining gap between Mycel and fully feature-complete GraphQL services by adding three things: a `Subscription` type in the generated schema backed by an in-process PubSub engine, flow-level publishing so any connector (queue, TCP, REST, etc.) can push events to connected clients, and entity resolution directives (`@key`, `@shareable`, `@external`) so Mycel subgraphs can participate in a federated supergraph managed by Apollo Router or Cosmo Router.

**Philosophy:** The user declares subscriptions and entity types in HCL; Mycel handles WebSocket lifecycle, PubSub routing, and SDL exposure automatically.

---

## Features

### 9.1 Subscription Type in Schema

**Goal:** Extend `SchemaBuilder` to emit a `type Subscription { ... }` block in the generated SDL and wire each subscription field to a PubSub channel.

#### How It Works

When a type file declares a subscription field, or a flow explicitly targets `Subscription.*`, the schema builder registers that field under the `Subscription` root type. Each field is backed by a dedicated PubSub topic. The WebSocket transport uses `graphql-go`'s built-in subscription protocol, which calls the field's `Subscribe` function to obtain a Go channel, then streams each value through that channel as it arrives.

#### Schema Generation

```
# Type definitions in HCL result in SDL like:

type Query {
  order(id: ID!): Order
}

type Mutation {
  createOrder(input: OrderInput!): Order
}

type Subscription {
  orderUpdated: Order
  stockChanged: Product
}
```

#### SchemaBuilder Extension

```go
// internal/connector/graphql/schema_builder.go

// RegisterSubscription adds a field to the Subscription root type.
// returnType is the GraphQL output type for the field.
func (sb *SchemaBuilder) RegisterSubscription(fieldName string, returnType graphql.Output)

// RegisterSubscriptionWithFilter adds a field to the Subscription root type
// with a per-subscriber CEL filter expression.
func (sb *SchemaBuilder) RegisterSubscriptionWithFilter(fieldName string, returnType graphql.Output, filter string)

// SetSubscriptionFilter attaches (or replaces) a CEL filter on an already-registered
// subscription field. Called by the parser when a flow's to block carries a filter attribute.
func (sb *SchemaBuilder) SetSubscriptionFilter(fieldName string, filter string)
```

#### PubSub Engine

```go
// internal/connector/graphql/pubsub.go

type PubSub struct {
    mu          sync.RWMutex
    subscribers map[string][]*subscriber
}

type subscriber struct {
    ch       chan interface{}
    filterFn func(data interface{}) bool
    connCtx  context.Context // WebSocket connection context; closed on disconnect
}

// Subscribe returns a channel that receives every published value for topic.
func (ps *PubSub) Subscribe(topic string) (<-chan interface{}, func())

// SubscribeWithFilter returns a channel that receives only values for which
// filterFn(value) returns true.
func (ps *PubSub) SubscribeWithFilter(topic string, filterFn func(interface{}) bool) (<-chan interface{}, func())

// Publish sends data to every subscriber on topic.
func (ps *PubSub) Publish(topic string, data interface{})
```

Subscribers are automatically removed when the WebSocket connection context is cancelled. There is no global state; each `ServerConnector` owns its own `PubSub` instance.

---

### 9.2 Flow-Triggered Subscriptions

**Goal:** Allow any flow to publish events to a subscription field by setting `operation = "Subscription.<fieldName>"` in the `to` block.

#### Detection in the Runtime

When the runtime processes a `to` block, it checks whether the operation string has the `Subscription.` prefix. If it does, it calls `PubSub.Publish` instead of the connector's `Write` method. Transforms and steps run normally before publishing, so the event payload can be shaped arbitrarily.

#### Flow with Queue Source

```hcl
service {
  name    = "orders-service"
  version = "1.0.0"
}

connector "rabbit" {
  type     = "rabbitmq"
  host     = env("RABBIT_HOST")
  exchange = "orders"
}

connector "api" {
  type = "graphql"
  port = 4000
}

flow "order_updates" {
  from {
    connector = "rabbit"
    operation = "order.*"
  }

  transform {
    output.orderId = input.id
    output.status  = input.status
    output.total   = input.total
  }

  to {
    connector = "api"
    operation = "Subscription.orderUpdated"
  }
}
```

#### Flow with REST Source (Webhook Push)

```hcl
flow "stock_webhook" {
  from {
    connector = "api"
    operation = "POST /internal/stock-update"
  }

  transform {
    output.sku      = input.sku
    output.quantity = input.quantity
  }

  to {
    connector = "api"
    operation = "Subscription.stockChanged"
  }
}
```

#### Runtime Integration Points

```go
// internal/runtime/flow_registry.go

// isSubscriptionOperation returns true when op matches "Subscription.<name>".
func isSubscriptionOperation(op string) bool {
    return strings.HasPrefix(op, "Subscription.")
}

// publishToSubscription extracts the field name, resolves the target connector,
// and calls connector.Publish(fieldName, data).
func (r *FlowRegistry) publishToSubscription(ctx context.Context, op string, connectorName string, data interface{}) error
```

---

### 9.3 Per-User Subscription Filtering

**Goal:** Allow each subscriber to receive only events relevant to them, without the publisher needing to know about individual connections.

#### How It Works

When a WebSocket client connects, it sends a `connection_init` message whose `payload` field contains arbitrary key-value data (typically auth tokens, user IDs, or room identifiers). Mycel stores this payload in the connection context and makes it available as `context` in CEL filter expressions.

The `to` block accepts an optional `filter` attribute containing a CEL expression. The expression is compiled once at startup and evaluated per-event per-subscriber using the subscriber's connection params as `context` and the event payload as `input`.

#### HCL

```hcl
flow "order_updates" {
  from {
    connector = "rabbit"
    operation = "order.*"
  }

  transform {
    output.orderId = input.id
    output.userId  = input.user_id
    output.status  = input.status
  }

  to {
    connector = "api"
    operation = "Subscription.orderUpdated"
    filter    = "input.userId == context.auth.user_id"
  }
}
```

#### Connection Params

```go
// internal/connector/graphql/websocket.go

// ConnectionParamsFromContext retrieves the connection_init payload stored
// in the WebSocket handler context.
func ConnectionParamsFromContext(ctx context.Context) map[string]interface{}
```

The WebSocket handler parses `connection_init`, optionally validates the auth token via the configured auth preset, and stores the params map in the request context before handing off to the subscription resolver.

#### Filter Evaluation

```go
// internal/connector/graphql/pubsub.go

// buildCELFilter compiles a CEL expression into a reusable filterFn.
// The expression has access to:
//   input   - the event payload being published
//   context - the subscriber's connection_init params
func buildCELFilter(expr string) (func(data interface{}, connCtx map[string]interface{}) bool, error)
```

The filter function is stored per-subscriber and called inside `PubSub.Publish` before pushing to the channel. Subscribers for which the filter returns false are silently skipped.

---

### 9.4 Automatic Entity Resolution

**Goal:** Enable Mycel subgraphs to participate in a federated supergraph by exposing entity resolvers for `@key` types, compatible with Apollo Federation v2 and Cosmo Router.

#### Type Annotations

Federation metadata is expressed as underscore-prefixed attributes in the `type` block, keeping it consistent with Mycel's existing convention for framework-level directives.

```hcl
type "Product" {
  _key       = "sku"
  _shareable = true

  sku   = string
  name  = string
  price = number
}

type "Review" {
  _key = "id"

  id      = string
  rating  = number
  product = "Product" {
    _provides = "sku"
  }
}

type "Inventory" {
  _key = "sku"

  sku      = string
  quantity = number {
    _external = true
    _requires = "sku"
  }
}
```

The schema builder reads these annotations and emits the appropriate Federation v2 directives in the SDL:

```graphql
type Product @key(fields: "sku") @shareable {
  sku:   String
  name:  String
  price: Float
}

type Review @key(fields: "id") {
  id:      ID
  rating:  Int
  product: Product @provides(fields: "sku")
}

type Inventory @key(fields: "sku") {
  sku:      String
  quantity: Int @external @requires(fields: "sku")
}
```

#### Entity Resolver Flows

A flow with an `entity` attribute is registered as the resolver for `Query._entities` representations of that type. The runtime matches the incoming `__typename` + key fields to the correct flow and executes it.

```hcl
flow "resolve_product_entity" {
  entity  = "Product"
  returns = "Product"

  from {
    connector = "api"
    operation = "Query.product"
  }

  to {
    connector = "db"
    operation = "SELECT * FROM products WHERE sku = ?"
    params    = [input.sku]
  }
}
```

When no explicit `entity` flow is defined but a `Query.<typeName>` flow exists with a matching return type, the runtime reuses that flow for entity resolution automatically.

#### Runtime Entity Registration

```go
// internal/connector/graphql/server.go

// RegisterEntityResolver registers a flow as the resolver for federation
// representations of typeName. Called during startup for each flow that
// carries an entity attribute, and for Query flows whose return type
// matches a @key-annotated type.
func (s *ServerConnector) RegisterEntityResolver(typeName string, handler HandlerFunc)
```

The `_entities(representations: [_Any!]!)` query is automatically added to the schema when at least one `@key` type is detected.

---

### 9.5 Schema Composition (External Router Integration)

**Goal:** Make Mycel subgraphs discoverable and usable by Apollo Router and Cosmo Router without any extra tooling.

#### SDL Introspection Endpoint

Every Mycel GraphQL connector automatically exposes:

```graphql
type Query {
  _service: _Service!
}

type _Service {
  sdl: String!
}
```

The `sdl` field returns the full SDL including Federation directives, `@key`, `@shareable`, `@external`, `@requires`, and `@provides`. Router instances pull this endpoint during schema composition.

#### Configuration

No HCL changes required. The endpoint is enabled automatically when any `@key` type is detected. It can also be forced on or off:

```hcl
connector "api" {
  type = "graphql"
  port = 4000

  federation {
    enabled  = true   # Default: auto-detect
    version  = "2"    # Federation spec version
  }
}
```

#### Docker Compose Integration Pattern

```yaml
# docker-compose.yml

services:
  orders-service:
    image: mycel:latest
    volumes:
      - ./orders:/config
    environment:
      - MYCEL_ENV=production

  products-service:
    image: mycel:latest
    volumes:
      - ./products:/config

  router:
    image: ghcr.io/wundergraph/cosmo/router:latest
    environment:
      ROUTER_CONFIG_PATH: /config/router.yaml
    volumes:
      - ./router.yaml:/config/router.yaml
    depends_on:
      - orders-service
      - products-service
```

```yaml
# router.yaml (Cosmo Router)

subgraphs:
  - name: orders
    routing_url: http://orders-service:4000/graphql
    schema_url:  http://orders-service:4000/graphql

  - name: products
    routing_url: http://products-service:4000/graphql
    schema_url:  http://products-service:4000/graphql
```

The router fetches `{ _service { sdl } }` from each subgraph, composes the supergraph schema, and routes incoming queries to the correct subgraph.

---

## Architecture

```
WebSocket Client
    |
    |  subscribe { orderUpdated { orderId status } }
    v
SubscriptionManager (graphql-go)
    |
    |  graphql.Subscribe() -> calls field.Subscribe fn
    v
Subscription Field Resolver
    |
    |  PubSub.SubscribeWithFilter(topic, filterFn)
    |  returns <-chan interface{}
    v
    [channel blocks until event arrives]
    ^
    |
Flow Handler  (e.g., from RabbitMQ, TCP, REST webhook)
    |
    |  Runs transforms + steps
    |
    +-->  PubSub.Publish(topic, data)
              |
              |  evaluates filterFn per subscriber
              |
              +--> subscriber channel (matching)
              x    subscriber channel (filtered out)
```

For entity resolution:

```
Apollo/Cosmo Router
    |
    |  POST /graphql  { query: "{ _entities(representations: [...]) { ... } }" }
    v
GraphQL Server (Mycel)
    |
    |  Matches __typename to registered entity resolver
    v
Entity Flow Handler
    |
    |  Executes steps + transforms using representation fields as input
    v
Connector (DB, REST, etc.)
    |
    v
Response assembled and returned to router
```

---

## Files Changed

### New Files

```
internal/connector/graphql/
├── pubsub.go                  # PubSub engine with filter support
├── pubsub_test.go
├── websocket.go               # WebSocket upgrade, connection_init handling
├── websocket_test.go
└── federation.go              # SDL federation directives, _service endpoint, entity resolver registration

internal/connector/graphql/schema_builder.go
    # Extended: RegisterSubscription, RegisterSubscriptionWithFilter,
    # SetSubscriptionFilter, federation type annotations
```

### Modified Files

```
internal/connector/graphql/server.go
    # Integrate PubSub, SubscriptionManager, RegisterEntityResolver,
    # expose _service { sdl } endpoint

internal/connector/graphql/resolver.go
    # ConnectionParamsFromContext, connection context propagation

internal/connector/graphql/hcl_to_graphql.go
    # Read _key, _shareable, _external, _requires, _provides from type blocks
    # Emit Federation v2 directives in SDL output

internal/parser/connector.go
    # Parse federation block in connector config
    # Parse _key, _shareable, _external attributes in type blocks

internal/parser/flow.go
    # Parse entity attribute in flow block
    # Parse filter attribute in to block (already partially present)

internal/runtime/flow_registry.go
    # isSubscriptionOperation, publishToSubscription
    # Entity resolver registration at startup
    # Match entity flows to _entities query

internal/flow/flow.go
    # ToConfig: add Filter field (if not already present)
    # FlowConfig: add Entity and Returns fields
```

### New Examples

```
examples/graphql-subscriptions/
├── config.mycel
├── connectors/
│   ├── graphql.mycel
│   └── rabbitmq.mycel
├── types/
│   └── order.mycel
└── flows/
    └── order_updates.mycel

examples/graphql-federation/
├── orders-service/
│   ├── config.mycel
│   ├── connectors/
│   ├── types/
│   └── flows/
├── products-service/
│   ├── config.mycel
│   ├── connectors/
│   ├── types/
│   └── flows/
└── docker-compose.yml
```

---

## API Reference

### SchemaBuilder

| Method | Description |
|--------|-------------|
| `RegisterSubscription(fieldName, returnType)` | Add a field to the Subscription root type backed by a PubSub topic. |
| `RegisterSubscriptionWithFilter(fieldName, returnType, filter)` | Same as above with a default CEL filter applied to all subscribers. |
| `SetSubscriptionFilter(fieldName, filter)` | Attach or replace the CEL filter for an already-registered subscription field. |

### ServerConnector

| Method | Description |
|--------|-------------|
| `Publish(topic, data)` | Push data to all subscribers (and filtered subscribers) on a topic. Called by the runtime when a flow targets `Subscription.<topic>`. |
| `RegisterEntityResolver(typeName, handler)` | Register a flow handler as the entity resolver for a `@key`-annotated type. Called automatically at startup. |

### PubSub

| Method | Description |
|--------|-------------|
| `Subscribe(topic)` | Return a channel that receives every published value and an unsubscribe function. |
| `SubscribeWithFilter(topic, filterFn)` | Return a channel that receives only values for which `filterFn` returns true. |
| `Publish(topic, data)` | Deliver data to all subscribers, evaluating individual filters. |

### WebSocket Helpers

| Function | Description |
|----------|-------------|
| `ConnectionParamsFromContext(ctx)` | Retrieve the `connection_init` payload stored in the WebSocket connection context. Used inside CEL filter evaluation to access `context.auth.*` and other connection-level metadata. |

---

## HCL Reference Summary

### Subscription Flow

```hcl
flow "event_name" {
  from {
    connector = "<source-connector>"
    operation = "<source-operation>"
  }

  transform {
    # Shape the event payload
    output.fieldA = input.fieldA
  }

  to {
    connector = "<graphql-connector>"
    operation = "Subscription.<fieldName>"
    filter    = "<cel-expression>"     # Optional: per-subscriber filtering
  }
}
```

### Entity Type

```hcl
type "TypeName" {
  _key       = "<field>"              # Required for federation: @key(fields: "...")
  _shareable = true                   # Optional: @shareable
  _extends   = true                   # Optional: extend type TypeName

  fieldName = string
  external_field = number {
    _external = true                  # @external
    _requires = "otherField"          # @requires(fields: "...")
  }
  relation_field = "OtherType" {
    _provides = "otherField"          # @provides(fields: "...")
  }
}
```

### Entity Resolver Flow

```hcl
flow "resolve_typename" {
  entity  = "TypeName"                # Registers as _entities resolver for TypeName
  returns = "TypeName"

  from {
    connector = "<graphql-connector>"
    operation = "Query.<queryField>"
  }

  to {
    connector = "<data-connector>"
    operation = "<operation>"
  }
}
```

### Federation Connector Config

```hcl
connector "api" {
  type = "graphql"
  port = 4000

  subscriptions {
    enabled   = true
    transport = "websocket"
    path      = "/graphql/ws"
    keepalive = "30s"
  }

  federation {
    enabled = true
    version = "2"
  }
}
```

---

## Testing Strategy

### Unit Tests

1. **PubSub**
   - Publish to topic with no subscribers (no-op)
   - Multiple subscribers receive the same event
   - `SubscribeWithFilter` blocks events that do not match
   - Unsubscribe removes channel from publisher list
   - Disconnect (context cancel) auto-removes subscriber

2. **Schema Builder**
   - SDL output includes `type Subscription { ... }` when fields registered
   - Federation directives appear in SDL for `@key` types
   - `_service { sdl }` query returns full SDL

3. **CEL Filter**
   - `input.userId == context.auth.user_id` evaluates correctly
   - Invalid CEL expression fails at startup, not at runtime
   - Missing context key evaluates to false without panic

4. **Entity Resolution**
   - `_entities` query routes to the correct flow by `__typename`
   - Auto-registration from `Query.<type>` flows works
   - Missing entity resolver returns `null` with a partial error

### Integration Tests

```go
func TestSubscription_FlowTriggered(t *testing.T) {
    // Setup: GraphQL connector + RabbitMQ flow
    // Action: Publish message to queue
    // Assert: WebSocket subscriber receives transformed event
}

func TestSubscription_PerUserFilter(t *testing.T) {
    // Setup: Two subscribers with different user_id in connection_init
    // Action: Publish event with user_id = "user-1"
    // Assert: Only subscriber "user-1" receives the event
}

func TestFederation_EntityResolution(t *testing.T) {
    // Setup: Product subgraph with @key(fields: "sku")
    // Action: POST _entities query with representations: [{__typename: "Product", sku: "abc"}]
    // Assert: Returns resolved Product with all fields
}

func TestFederation_SDLEndpoint(t *testing.T) {
    // Setup: Connector with @key types
    // Action: Query { _service { sdl } }
    // Assert: SDL contains @key, @shareable directives
}
```

### Benchmarks

```go
func BenchmarkPubSub_1000Subscribers(b *testing.B)
func BenchmarkPubSub_FilteredSubscribers(b *testing.B)
func BenchmarkEntityResolution_100Representations(b *testing.B)
```

---

## Migration / Compatibility

**Zero breaking changes.** Existing GraphQL connectors continue to work exactly as before.

- Subscriptions are only active when a flow targets `Subscription.*`.
- Federation features are only active when a type carries `_key`.
- The `_service { sdl }` endpoint is additive and does not conflict with user-defined queries.
- WebSocket transport is disabled by default and must be explicitly enabled in the connector config.

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Subscription delivery | 100% of published events delivered to matching subscribers |
| Filter accuracy | 0% false positives; 0% false negatives in CEL filter evaluation |
| Federation SDL | Passes Apollo Router and Cosmo Router schema composition without errors |
| Entity resolution | Correct resolution for all registered `@key` types |
| WebSocket disconnect cleanup | Subscriber list cleaned up within 1 second of disconnect |
| Breaking changes | Zero |

---

## References

- GraphQL over WebSocket protocol: https://github.com/graphql/graphql-over-http/blob/main/rfcs/GraphQLOverWebSocket.md
- Apollo Federation v2 spec: https://www.apollographql.com/docs/federation/federation-spec/
- Cosmo Router: https://cosmo-docs.wundergraph.com/router/
- graphql-go subscriptions: https://github.com/graphql-go/graphql
- Phase 8 spec: `docs/PHASE-8-GRAPHQL-OPTIMIZATION.md`
