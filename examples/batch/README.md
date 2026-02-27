# Batch Processing Example

Process large datasets in chunks — data migrations, ETL, reindexing.

## What This Demonstrates

- **Chunked reads:** Paginate through a source connector in configurable chunk sizes
- **Per-item transforms:** Apply CEL transforms to each record during processing
- **Error handling:** Continue on failure or stop at first error
- **Parameterized queries:** Pass runtime parameters via CEL expressions

## Run

```bash
mycel start --config ./examples/batch
```

## Test

Migrate users from old database to new one (with per-item transform):

```bash
curl -X POST http://localhost:3000/admin/migrate
# Reads all users in chunks of 100, lowercases emails, writes to new DB
```

Reindex products to Elasticsearch (chunks of 500):

```bash
curl -X POST http://localhost:3000/admin/reindex
```

Export recent orders with a date parameter:

```bash
curl -X POST http://localhost:3000/admin/export-orders \
  -H "Content-Type: application/json" \
  -d '{"since": "2026-01-01"}'
```

## Response

Every batch flow returns processing stats:

```json
{
  "processed": 950,
  "failed": 50,
  "chunks": 10,
  "errors": ["write error on chunk 4: connection timeout"]
}
```

## Batch Block Reference

```hcl
batch {
  source     = "source_connector"    # Source connector (must implement Reader)
  query      = "SELECT * FROM ..."   # SQL query or operation
  params     = { since = "input.x" } # Query parameters (CEL expressions)
  chunk_size = 100                   # Records per chunk (default: 100)
  on_error   = "continue"            # "continue" or "stop" (default: "stop")

  transform { ... }                  # Optional per-item transform

  to {
    connector = "target_connector"   # Target connector (must implement Writer)
    target    = "table_name"
    operation = "INSERT"
  }
}
```

## Error Handling

| Mode | Behavior |
|------|----------|
| `stop` (default) | Halt on first chunk failure, return partial stats |
| `continue` | Skip failed chunks, record errors, process remaining |

In the transform block, each item's fields are available as `input.*` (standard Mycel convention). The original flow input is accessible as `input._batch_input`.
