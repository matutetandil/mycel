# Elasticsearch

Full-text search and analytics over Elasticsearch's REST API. Use it for search APIs, product catalogs, log analytics, or any scenario where you need powerful text matching, filtering, and aggregations beyond what SQL offers.

Multi-node clusters use round-robin load balancing automatically.

## Configuration

```hcl
connector "es" {
  type     = "elasticsearch"
  url      = "http://localhost:9200"
  username = env("ES_USER")
  password = env("ES_PASSWORD")
  timeout  = "30s"
}
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | `"http://localhost:9200"` | Elasticsearch node URL |
| `username` | string | — | Basic auth username |
| `password` | string | — | Basic auth password |
| `timeout` | duration | `"30s"` | Request timeout |

The index is specified per-flow via the `target` attribute in flow `to` or `step` blocks.

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `search` | read | Full-text query DSL |
| `get` | read | Document by ID |
| `count` | read | Count matching documents |
| `aggregate` | read | Aggregation queries |
| `index` | write | Create or replace a document |
| `update` | write | Partial document update |
| `delete` | write | Delete by ID |
| `bulk` | write | Batch operations |

Mycel's standard query model maps to Elasticsearch: filters become `bool.must` terms, pagination maps to `size`/`from`, ordering maps to `sort`, and field selection maps to `_source` includes.

## Example

```hcl
# Full-text search with query DSL
flow "search_products" {
  from { connector = "api", operation = "GET /search" }

  step "results" {
    connector = "es"
    operation = "search"
    target    = "products"
    body = {
      "query" = {
        "multi_match" = {
          "query"  = "input.query.q"
          "fields" = ["name^2", "description"]
        }
      }
    }
  }

  transform { output.results = "step.results" }
  to { response }
}

# Index a document
flow "index_product" {
  from { connector = "api", operation = "POST /products" }
  to   { connector = "es", target = "products", operation = "index" }
}
```

See the [elasticsearch example](../../examples/elasticsearch/) for a complete working setup.

---

> **Full configuration reference:** See [Elasticsearch](../reference/configuration.md#elasticsearch) in the Configuration Reference.
