# Batch Processing Example

Process large datasets in chunks — data migrations, ETL, reindexing.

## Setup

```bash
mycel start --config ./examples/batch
```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /admin/migrate | Migrate users with transform |
| POST | /admin/reindex | Reindex products to ES |
| POST | /admin/export-orders | Export orders with params |

## Batch Block

```hcl
batch {
  source     = "source_connector"    # Source connector (Reader)
  query      = "SELECT * FROM ..."   # SQL or operation
  params     = { since = "input.x" } # Query parameters (CEL)
  chunk_size = 100                   # Records per chunk (default: 100)
  on_error   = "continue"            # "continue" or "stop" (default: "stop")

  transform { ... }                  # Optional per-item transform

  to {
    connector = "target_connector"   # Target connector (Writer)
    target    = "table_name"
    operation = "INSERT"
  }
}
```

## Response

```json
{
  "processed": 950,
  "failed": 50,
  "chunks": 10,
  "errors": ["write error: ..."]
}
```
