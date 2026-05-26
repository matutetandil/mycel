# Transactional, multi-statement write

Persist a product aggregate (product → options → option values) **atomically**
with the `to { transaction { } }` primitive. All statements run on one pinned
database connection inside a single `BEGIN`/`COMMIT`: a failure anywhere rolls
back the whole aggregate, and captured ids (`LAST_INSERT_ID`) stay coherent
across statements because they share the connection.

## Run

```bash
mycel validate --config ./examples/transactional-write
mycel start    --config ./examples/transactional-write
```

Create the tables once (SQLite):

```sql
CREATE TABLE product        (id INTEGER PRIMARY KEY AUTOINCREMENT, sku TEXT, name TEXT);
CREATE TABLE product_option (id INTEGER PRIMARY KEY AUTOINCREMENT, product_id INTEGER, code TEXT, position INTEGER);
CREATE TABLE option_value   (id INTEGER PRIMARY KEY AUTOINCREMENT, option_id INTEGER, label TEXT, price REAL);
```

Send an aggregate:

```bash
curl -X POST localhost:8080/products -H 'content-type: application/json' -d '{
  "id": 0,
  "sku": "TSHIRT-1",
  "name": "T-Shirt",
  "options": [
    { "code": "size",  "values": [ {"label": "S", "price": 0}, {"label": "M", "price": 0} ] },
    { "code": "color", "values": [ {"label": "Red", "price": 2.5} ] }
  ]
}'
```

This produces one `product` row, two `product_option` rows linked to it, and
three `option_value` rows — or, if any statement fails, none at all.

## What the flow shows

| Feature | Where |
|---|---|
| Pinned connection + atomic commit/rollback | the whole `transaction` block |
| `exec` with `:named` params from CEL | every statement |
| `when` gate (skip when false) | the `DELETE` runs only when `input.id > 0` |
| `capture` of `LAST_INSERT_ID` | `product_id`, `option_id` |
| `each "<var>" in "<list>"` iteration | `options`, nested `values` |
| `<var>_index` (0-based) | `option_index` → `position` |
| forward reference to captured ids | `captured.product_id`, `captured.option_id` |

The `transaction` is wrapped by the same dedupe / aspects / `error_handling`
envelope as any other `to` write — add a `dedupe {}` or `error_handling {}`
block to the flow and it applies to the transaction as a unit.
