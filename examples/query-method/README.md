# HTTP QUERY Method Example

Demonstrates the **HTTP QUERY method (RFC 10008, June 2026)** — a safe, idempotent, cacheable read whose query travels in the request **body**. Think "GET with a body": POST's ergonomics for complex search criteria, GET's semantics (retryable, cacheable, never mutates state).

## Why QUERY?

Before QUERY you had two bad options for complex searches:

- `GET /search?filters=...` — query strings hit URL length limits, leak sensitive criteria into access logs, and get awkward fast for nested filters.
- `POST /search` — carries a body fine, but declares "this may mutate state", so proxies won't cache it and clients won't auto-retry it.

QUERY closes the gap: a body-carrying request that is explicitly safe and idempotent.

## What This Example Does

- Exposes `QUERY /products/search` — the JSON body feeds raw SQL named parameters
- Exposes `QUERY /products` and `GET /products` on the same path (methods coexist)
- Stores data in a local SQLite database

## Quick Start

```bash
# From this directory: create the database
mkdir -p data && sqlite3 data/app.db < setup.sql

# From the repository root
mycel start --config ./examples/query-method
```

## Verify It Works

### 1. Search with the query in the body

```bash
curl -X QUERY http://localhost:3000/products/search \
  -H "Content-Type: application/json" \
  -d '{"name_like": "%Pro%", "max_price": 1500}'
```

Expected response (body fields fed the SQL named params):
```json
[{"id":5,"name":"Pro Mouse","price":89},{"id":4,"name":"Pro Keyboard","price":149}]
```

The Laptop Pro 14/16 also match `%Pro%`, but they're filtered out by `max_price` — raise it to 2500 to see them appear.

### 2. Plain QUERY read

```bash
curl -X QUERY http://localhost:3000/products \
  -H "Content-Type: application/json" -d '{}'
```

Returns all products — same read semantics as GET.

### 3. Content-Type is mandatory (RFC 10008)

```bash
curl -i -X QUERY http://localhost:3000/products/search \
  -H "Content-Type:" -d '{"name_like": "%Pro%"}'
```

Expected: `415 Unsupported Media Type` — a QUERY with content must declare its media type.

### 4. Discovery via Accept-Query

```bash
curl -si http://localhost:3000/products | grep -i accept-query
```

Expected: `Accept-Query: application/json, application/xml` — responses on QUERY-capable paths advertise the accepted media types.

## File Structure

```
query-method/
├── config.mycel              # Service name and version
├── connectors/
│   ├── api.mycel             # REST API configuration
│   └── database.mycel        # SQLite database connection
├── flows/
│   └── search.mycel          # QUERY + GET flows
├── data/
│   └── app.db                # SQLite database file
└── setup.sql                 # Schema + sample products
```

## How It Works

A QUERY flow is declared exactly like any other REST flow — only the verb changes:

```hcl
flow "search_products" {
  from {
    connector = "api"
    operation = "QUERY /products/search"
  }

  to {
    connector = "sqlite"
    target    = "products"
    query     = "SELECT id, name, price FROM products WHERE name LIKE :name_like AND price <= :max_price ORDER BY price"
  }
}
```

The request body is decoded (JSON or XML, by `Content-Type`) and merged into the flow input — so body fields work everywhere input works: raw SQL named parameters, transforms, CEL expressions, validation, caching keys.

Inside Mycel, QUERY runs the **read path**: it can use steps/orchestration and the flow `cache {}` block, and it never triggers write-side behavior. The default cache key includes the body fields, as the RFC requires.

## Notes

- **Browsers preflight QUERY** (it is not a CORS-safelisted method). Mycel's permissive dev-mode CORS already advertises it; in production list it explicitly in your `cors { methods = [...] }`.
- **OpenAPI export**: OpenAPI 3.0 has no `query` operation slot, so `mycel export openapi` skips QUERY flows (siblings on the same path are unaffected).
- Older proxies/load balancers may not know the verb yet — test your edge before relying on it end-to-end.

## Next Steps

- Add caching to the search: See [examples/cache](../cache)
- Add input validation: See [examples/basic](../basic)
- Complex orchestration with steps: See [examples/steps](../steps)
