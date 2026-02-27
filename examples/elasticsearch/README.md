# Elasticsearch Example

Full-text search and analytics with Elasticsearch.

## Setup

1. Start Elasticsearch:
   ```bash
   docker run -d -p 9200:9200 -e "discovery.type=single-node" elasticsearch:8.12.0
   ```

2. Set credentials:
   ```bash
   export ES_USER=elastic
   export ES_PASSWORD=changeme
   ```

3. Start Mycel:
   ```bash
   mycel start --config ./examples/elasticsearch
   ```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /search?q=... | Full-text search |
| GET | /products/:id | Get product by ID |
| POST | /products | Index a product |
| PUT | /products/:id | Update a product |
| DELETE | /products/:id | Delete a product |
| GET | /products/count | Count products |

## Supported Operations

**Read:** `search`, `get`, `count`, `aggregate`
**Write:** `index`, `update`, `delete`, `bulk`
