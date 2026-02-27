# Elasticsearch Example

Full-text search and analytics over Elasticsearch's REST API.

## What This Demonstrates

- **Search:** Full-text queries using Elasticsearch query DSL with field boosting
- **CRUD:** Index, get, update, and delete documents via REST endpoints
- **Count:** Count matching documents with filters

## Prerequisites

Start Elasticsearch:

```bash
docker run -d -p 9200:9200 -e "discovery.type=single-node" elasticsearch:8.12.0
```

Set credentials:

```bash
export ES_USER=elastic
export ES_PASSWORD=changeme
```

## Run

```bash
mycel start --config ./examples/elasticsearch
```

## Test

Index a product:

```bash
curl -X POST http://localhost:3000/products \
  -H "Content-Type: application/json" \
  -d '{"name": "Wireless Mouse", "description": "Ergonomic wireless mouse", "price": 29.99}'
```

Search by keyword:

```bash
curl "http://localhost:3000/search?q=wireless"
```

Get a product by ID:

```bash
curl http://localhost:3000/products/abc123
```

Update a product:

```bash
curl -X PUT http://localhost:3000/products/abc123 \
  -H "Content-Type: application/json" \
  -d '{"price": 24.99}'
```

Delete a product:

```bash
curl -X DELETE http://localhost:3000/products/abc123
```

Count products:

```bash
curl http://localhost:3000/products/count
```

## Operations

| Operation | Direction | Description |
|-----------|-----------|-------------|
| `search` | Read | Full-text search with query DSL |
| `get` | Read | Get document by ID |
| `count` | Read | Count matching documents |
| `aggregate` | Read | Run aggregation queries |
| `index` | Write | Create or replace a document |
| `update` | Write | Partial document update |
| `delete` | Write | Delete a document by ID |
| `bulk` | Write | Batch multiple operations |
