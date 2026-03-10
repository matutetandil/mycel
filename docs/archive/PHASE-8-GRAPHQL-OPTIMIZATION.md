# Phase 8: GraphQL Query Optimization

> **Status:** Planned
> **Priority:** High
> **Estimated Complexity:** Medium-High
> **Reference:** LazyQL library concepts (`/Users/matute/Documents/Personal/LAZYQL`)

## Overview

Implement transparent query optimization for GraphQL connectors. The user writes normal HCL configuration, and Mycel automatically optimizes execution based on which fields the client actually requests.

**Philosophy:** Zero configuration required. Same HCL, automatic performance gains.

## Problem Statement

Currently, when a GraphQL query requests specific fields:

```graphql
query { users { id, name } }
```

Mycel executes the full flow regardless:

```sql
-- What happens now:
SELECT * FROM users
JOIN addresses ON ...    -- unnecessary
JOIN orders ON ...       -- unnecessary
-- Returns ALL columns, then GraphQL filters
```

This causes:
- Unnecessary database load
- Wasted network bandwidth
- Slower response times
- N+1 query problems with nested resolvers

## Goals

1. **Field Pruning** - Only return requested fields
2. **Query Optimization** - Modify SQL/queries to fetch only needed data
3. **Step Skipping** - Skip flow steps whose output isn't used
4. **DataLoader** - Automatic batching for N+1 patterns
5. **Relation Detection** - Smart JOIN decisions based on nested fields

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    GraphQL Request                          │
│         query { users { id, name, orders { total } } }      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 1. FIELD ANALYZER                           │
│  Location: internal/graphql/analyzer/                       │
│  • Parse GraphQL AST                                        │
│  • Extract requested fields tree                            │
│  • Detect nested relations                                  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               2. REQUEST CONTEXT                            │
│  Location: internal/runtime/context.go                      │
│  • RequestedFields in flow context                          │
│  • CEL functions: has_field(), requested_fields()           │
│  • Propagates through steps                                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              3. CONNECTOR OPTIMIZERS                        │
│  Location: internal/connector/*/optimizer.go                │
│  • Database: Rewrite SELECT clauses                         │
│  • GraphQL Client: Forward field selection                  │
│  • HTTP: Use sparse fieldsets if supported                  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│               4. STEP OPTIMIZER                             │
│  Location: internal/runtime/step_optimizer.go               │
│  • Analyze transform dependencies                           │
│  • Skip steps whose output isn't used                       │
│  • Lazy execution of conditional data                       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 5. DATALOADER                               │
│  Location: internal/dataloader/                             │
│  • Request-scoped batching                                  │
│  • Automatic N+1 detection                                  │
│  • Configurable batch size and timing                       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                6. RESULT PRUNER                             │
│  Location: internal/graphql/pruner/                         │
│  • Final safety net                                         │
│  • Remove unrequested fields from response                  │
│  • Handle computed fields                                   │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 8.1: Field Analyzer & Request Context

**Goal:** Extract requested fields and make them available in flow execution.

#### New Files

```
internal/graphql/analyzer/
├── analyzer.go       # Main field extraction logic
├── field_tree.go     # FieldTree data structure
└── analyzer_test.go
```

#### Data Structures

```go
// FieldTree represents the hierarchical structure of requested fields
type FieldTree struct {
    Fields   map[string]*FieldNode
    Typename string // For union/interface types
}

type FieldNode struct {
    Name      string
    Alias     string
    Arguments map[string]interface{}
    Children  *FieldTree // For nested objects
    IsLeaf    bool
}

// RequestedFields provides query helpers
type RequestedFields struct {
    tree *FieldTree
}

func (rf *RequestedFields) Has(path string) bool
func (rf *RequestedFields) Get(path string) *FieldNode
func (rf *RequestedFields) List() []string
func (rf *RequestedFields) ListFlat() []string // ["id", "name", "orders.total"]
```

#### Integration Points

```go
// In resolver.go - extract fields before calling handler
func CreateOptimizedResolver(handler HandlerFunc) graphql.FieldResolveFn {
    return func(p graphql.ResolveParams) (interface{}, error) {
        // Extract requested fields from AST
        fields := analyzer.ExtractFields(p.Info)

        // Add to context
        ctx := context.WithValue(p.Context, RequestedFieldsKey, fields)

        // Build input with field info
        input := MapArgsToInput(p)
        input["__requested_fields"] = fields.ListFlat()

        // Call handler with enriched context
        result, err := handler(ctx, input)
        if err != nil {
            return nil, err
        }

        // Prune result to only requested fields
        return pruner.Prune(result, fields), nil
    }
}
```

#### CEL Functions

```go
// New CEL functions for transforms
"has_field":         // has_field("orders") -> bool
"requested_fields":  // requested_fields() -> ["id", "name", "orders.total"]
"field_requested":   // field_requested("orders.items") -> bool
```

**Example usage in HCL (optional, for advanced users):**

```hcl
flow "get_user" {
  from { connector.api = "Query.user" }

  step "orders" {
    when      = "has_field('orders')"  # Only if orders requested
    connector = "postgres"
    query     = "SELECT * FROM orders WHERE user_id = ?"
  }

  transform {
    output.id   = input.id
    output.name = input.name
    output.orders = has_field("orders") ? step.orders : null
  }

  to { response }
}
```

---

### Phase 8.2: Database Query Optimizer

**Goal:** Automatically rewrite database queries to fetch only requested columns.

#### Approach

1. Parse the SQL query to identify SELECT columns
2. Match columns to GraphQL field names (with snake_case/camelCase conversion)
3. Rewrite SELECT clause to only include needed columns
4. Handle JOINs based on nested field requests

#### Implementation

```go
// internal/connector/database/optimizer.go

type QueryOptimizer struct {
    requestedFields *RequestedFields
    tableSchema     *TableSchema // From introspection or config
}

// OptimizeSelect rewrites SELECT * to SELECT specific columns
func (o *QueryOptimizer) OptimizeSelect(query string) (string, error) {
    // Parse SQL
    parsed, err := sqlparser.Parse(query)
    if err != nil {
        return query, nil // Fall back to original if can't parse
    }

    // If SELECT *, replace with specific columns
    if isSelectStar(parsed) {
        columns := o.requestedFields.ListFlat()
        dbColumns := o.mapToDBColumns(columns)
        return rewriteSelect(parsed, dbColumns), nil
    }

    return query, nil
}

// OptimizeJoins removes unnecessary JOINs
func (o *QueryOptimizer) OptimizeJoins(query string) (string, error) {
    // Analyze which tables are needed based on requested fields
    // Remove JOINs for tables whose columns aren't requested
}
```

#### Automatic Behavior

```hcl
# User writes this:
flow "get_users" {
  from { connector.api = "Query.users" }
  to   {
    connector = "postgres"
    query     = "SELECT * FROM users"
  }
}
```

```
# Client requests:
query { users { id, email } }

# Mycel automatically executes:
SELECT id, email FROM users
```

#### Edge Cases

1. **Computed fields** - Fields that don't map to columns (handled in transform)
2. **Aliased columns** - `SELECT user_name AS name`
3. **Expressions** - `SELECT CONCAT(first, ' ', last) AS name`
4. **Aggregations** - `SELECT COUNT(*) as total`

For complex cases, fall back to original query + result pruning.

---

### Phase 8.3: Step Optimizer

**Goal:** Skip flow steps whose output isn't used in the final response.

#### Dependency Analysis

```go
// internal/runtime/step_optimizer.go

type StepOptimizer struct {
    steps           []*StepConfig
    transform       *TransformConfig
    requestedFields *RequestedFields
}

// AnalyzeDependencies determines which steps are needed
func (o *StepOptimizer) AnalyzeDependencies() map[string]bool {
    needed := make(map[string]bool)

    // Parse transform to find step.X references
    for _, field := range o.requestedFields.ListFlat() {
        // Find which steps contribute to this field
        deps := o.findDependencies(field, o.transform)
        for _, dep := range deps {
            needed[dep] = true
        }
    }

    return needed
}

// ShouldExecuteStep returns true if the step output is needed
func (o *StepOptimizer) ShouldExecuteStep(stepName string) bool {
    return o.needed[stepName]
}
```

#### Example

```hcl
flow "get_product" {
  from { connector.api = "Query.product" }

  step "base" {
    connector = "postgres"
    query     = "SELECT * FROM products WHERE id = ?"
  }

  step "reviews" {
    connector = "reviews_api"
    operation = "GET /products/${input.id}/reviews"
  }

  step "inventory" {
    connector = "inventory_api"
    operation = "GET /stock/${input.id}"
  }

  transform {
    output.id       = step.base.id
    output.name     = step.base.name
    output.reviews  = step.reviews
    output.inStock  = step.inventory.quantity > 0
  }

  to { response }
}
```

```
# Request: { product(id: 1) { id, name } }
# Executes: step.base only
# Skips: step.reviews, step.inventory

# Request: { product(id: 1) { id, name, reviews { rating } } }
# Executes: step.base, step.reviews
# Skips: step.inventory
```

---

### Phase 8.4: DataLoader (Automatic Batching)

**Goal:** Automatically batch N+1 queries without user configuration.

#### The N+1 Problem

```graphql
query {
  users {           # 1 query: SELECT * FROM users
    id
    orders {        # N queries: SELECT * FROM orders WHERE user_id = ?
      total         #            (one per user)
    }
  }
}
```

#### Solution: Request-Scoped DataLoader

```go
// internal/dataloader/dataloader.go

type DataLoader struct {
    batchFn    BatchFunction
    cache      map[interface{}]*Result
    queue      []interface{}
    batchSize  int
    waitTime   time.Duration
    mu         sync.Mutex
}

type BatchFunction func(ctx context.Context, keys []interface{}) ([]interface{}, error)

// Load queues a key and returns a thunk
func (dl *DataLoader) Load(key interface{}) func() (interface{}, error) {
    dl.mu.Lock()

    // Check cache
    if result, ok := dl.cache[key]; ok {
        dl.mu.Unlock()
        return func() (interface{}, error) { return result.Value, result.Error }
    }

    // Add to queue
    dl.queue = append(dl.queue, key)

    // If batch is full or timer expired, execute
    if len(dl.queue) >= dl.batchSize {
        dl.executeBatch()
    }

    dl.mu.Unlock()

    // Return thunk that waits for result
    return dl.createThunk(key)
}
```

#### Automatic Detection

```go
// internal/dataloader/detector.go

// DetectBatchablePattern analyzes a flow to see if it can be batched
func DetectBatchablePattern(flow *FlowConfig) *BatchPattern {
    // Look for patterns like:
    // - Nested resolver that queries by parent ID
    // - Step that uses input.parent_id
    // - WHERE user_id = ? patterns

    // If found, automatically wrap the connector call with DataLoader
}
```

#### Integration

```go
// In flow execution
func (r *FlowRegistry) executeNestedResolver(ctx context.Context, flow *FlowConfig, parent interface{}) {
    // Get or create request-scoped DataLoader
    loader := GetDataLoader(ctx, flow.Name)

    // Extract key from parent (e.g., user_id)
    key := extractKey(parent, flow)

    // Load via DataLoader (automatically batched)
    result := loader.Load(key)

    return result()
}
```

#### Batch Query Generation

For database connectors, automatically generate batch queries:

```sql
-- Individual (N+1):
SELECT * FROM orders WHERE user_id = 1
SELECT * FROM orders WHERE user_id = 2
SELECT * FROM orders WHERE user_id = 3

-- Batched (1 query):
SELECT * FROM orders WHERE user_id IN (1, 2, 3)
```

---

### Phase 8.5: Result Pruner (Safety Net)

**Goal:** Final cleanup to ensure only requested fields are returned.

```go
// internal/graphql/pruner/pruner.go

// Prune removes fields not in the requested set
func Prune(data interface{}, requested *RequestedFields) interface{} {
    switch v := data.(type) {
    case map[string]interface{}:
        result := make(map[string]interface{})
        for key, value := range v {
            if requested.Has(key) {
                if node := requested.Get(key); node.Children != nil {
                    result[key] = Prune(value, &RequestedFields{tree: node.Children})
                } else {
                    result[key] = value
                }
            }
        }
        return result
    case []interface{}:
        result := make([]interface{}, len(v))
        for i, item := range v {
            result[i] = Prune(item, requested)
        }
        return result
    default:
        return data
    }
}
```

This is the safety net - even if optimizations fail, we never return more data than requested.

---

## Configuration (Optional)

For users who want fine-grained control:

```hcl
# config.hcl
graphql {
  optimization {
    enabled = true  # Default: true

    field_pruning    = true  # Remove unrequested fields from response
    query_rewriting  = true  # Rewrite SELECT * queries
    step_skipping    = true  # Skip unused steps
    dataloader       = true  # Automatic N+1 batching

    dataloader_config {
      batch_size = 100       # Max keys per batch
      wait_time  = "5ms"     # Wait before executing batch
    }
  }
}
```

**Default:** All optimizations enabled with sensible defaults.

---

## Testing Strategy

### Unit Tests

1. **Field Analyzer**
   - Simple queries: `{ id, name }`
   - Nested queries: `{ user { orders { items } } }`
   - Fragments: `{ ...UserFields }`
   - Aliases: `{ userName: name }`

2. **Query Optimizer**
   - SELECT * rewriting
   - JOIN removal
   - Complex queries (subqueries, CTEs)

3. **Step Optimizer**
   - Dependency detection
   - Conditional step skipping

4. **DataLoader**
   - Batching correctness
   - Cache behavior
   - Error handling

### Integration Tests

```go
func TestOptimization_FieldSelection(t *testing.T) {
    // Setup: Flow with SELECT *
    // Query: { users { id, name } }
    // Assert: Only id, name returned
    // Assert: SQL executed was SELECT id, name (via query log)
}

func TestOptimization_StepSkipping(t *testing.T) {
    // Setup: Flow with 3 steps
    // Query: Only needs step 1
    // Assert: Steps 2, 3 not executed (via mock)
}

func TestOptimization_DataLoader(t *testing.T) {
    // Setup: Nested resolver (users -> orders)
    // Query: 10 users with orders
    // Assert: Only 2 queries executed (users + batched orders)
}
```

### Benchmarks

```go
func BenchmarkWithoutOptimization(b *testing.B) { ... }
func BenchmarkWithFieldPruning(b *testing.B) { ... }
func BenchmarkWithQueryRewriting(b *testing.B) { ... }
func BenchmarkWithDataLoader(b *testing.B) { ... }
```

---

## Migration / Compatibility

**Zero breaking changes.** All optimizations are:
- Enabled by default
- Transparent to existing configurations
- Disable-able via config if needed

Existing HCL files work exactly as before, just faster.

---

## Files to Create/Modify

### New Files

```
internal/graphql/
├── analyzer/
│   ├── analyzer.go
│   ├── field_tree.go
│   └── analyzer_test.go
├── pruner/
│   ├── pruner.go
│   └── pruner_test.go
└── optimizer/
    ├── optimizer.go
    └── optimizer_test.go

internal/dataloader/
├── dataloader.go
├── detector.go
├── cache.go
└── dataloader_test.go

internal/runtime/
├── step_optimizer.go
└── step_optimizer_test.go
```

### Modified Files

```
internal/connector/graphql/resolver.go    # Integrate analyzer
internal/connector/graphql/server.go      # Request context
internal/connector/database/connector.go  # Query optimization
internal/runtime/flow_registry.go         # Step optimization
internal/transform/cel.go                 # New functions
```

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Field pruning | 100% - never return unrequested fields |
| Query optimization | 80%+ of SELECT * queries rewritten |
| Step skipping | Skip unused steps when detectable |
| N+1 prevention | Automatic batching for nested resolvers |
| Performance | 2-10x improvement for complex queries |
| Breaking changes | Zero |

---

## References

- LazyQL library: `/Users/matute/Documents/Personal/LAZYQL`
- Facebook DataLoader: https://github.com/graphql/dataloader
- GraphQL Best Practices: https://graphql.org/learn/best-practices/
- Apollo Server DataLoader: https://www.apollographql.com/docs/apollo-server/data/fetching-data/
