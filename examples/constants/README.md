# Constants

A value declared once and used from several places — an HCL attribute and a CEL
expression alike.

The case this exists for: a list of SKUs the service ignores, needed by more
than one query and by the check that refuses them on the way in. Written into
each of them, it is three copies to keep in step.

## Files

| File | Purpose |
|------|---------|
| `constants.mycel` | The values |
| `service.mycel` | Connectors |
| `flows.mycel` | Three flows, each reading a constant a different way |
| `migrations/001_create_items.sql` | The table and a few rows |

## What it shows

```
GET  /items          → the query's LIMIT comes from ${constants.page_size}
POST /items          → accept refuses a sku in constants.skus_to_skip
GET  /items/skipped  → the same list again, in a second query
```

`${constants.page_size}` is HCL's interpolation, folded in when the file is
read. `constants.skus_to_skip` inside `accept` is CEL's, evaluated for every
request. Same name, and you do not have to know which of the two you are
writing for.

## Running

```bash
mycel migrate --config ./examples/constants
mycel start --config ./examples/constants
```

## Try It

The listing stops at three rows, because that is what the constant says:

```bash
curl http://localhost:3000/items
```

A sku the constant lists is refused:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"sku":"SKU-SAMPLE","name":"Free sample","total":0}'
```

One it does not list is written, with the region and the large-order flag both
worked out from constants:

```bash
curl -X POST http://localhost:3000/items \
  -H "Content-Type: application/json" \
  -d '{"sku":"SKU-0009","name":"Machine","total":2500}'
```

And the second query reads the same list:

```bash
curl http://localhost:3000/items/skipped
```

## Changing one

Change `page_size` in `constants.mycel` and restart: the listing and anything
else that reads it move together. That is the whole point — there is one place
to change.

`mycel validate` prints what it found:

```
  Constants: 4
    - large_order_total = 1000
    - page_size = 3
    - region = us
    - skus_to_skip = [SKU-DISCONTINUED SKU-SAMPLE SKU-INTERNAL]
```
