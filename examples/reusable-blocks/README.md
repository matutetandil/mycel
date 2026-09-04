# Reusable Blocks

Demonstrates the **recommended** way to write Mycel configs: declare a block
**once** at the top level with a name and reference it from multiple flows with
`use = "<kind>.<name>"` (with optional inline overrides), instead of
copy-pasting the same block into every flow. Think named vs anonymous functions
— inline is fine for a one-off, but the moment a policy is shared, name it.
Introduced in Mycel v2.6.0.

Twelve kinds of block can be named this way, and all twelve are here:
`accept`, `cache`, `coordinate`, `dedupe`, `error_handling`, `lock`,
`response`, `retry`, `semaphore`, `sequence_guard`, `transaction` and
`transform`.

## Files

- `reusable.mycel` — the named blocks. `dedupe "standard"`, `retry
  "resilient"`, `error_handling "resilient"` (which references the named
  retry), `accept "mine"` and `response "envelope"` are the ones an ingestion
  flow reaches for first; `lock "per_item"`, `semaphore "pricing_api"`,
  `coordinate "item_first"`, `sequence_guard "by_job"` and `transaction
  "item_and_price"` are the rest.
- `flows.mycel` — three flows:
  - `ingest_products` references the named blocks as-is.
  - `ingest_orders` references the same blocks but overrides a few attributes
    inline (dedupe key/ttl, retry attempts/backoff, response status).
  - `ingest_stock` declares no policy of its own at all: the key it locks on,
    the ceiling it waits under, the order it observes, the sequence it refuses
    to go backwards on and the statements it writes are every one of them a
    reference.
- `connectors.mycel` — a REST entry point, a downstream target, a memory cache
  for the dedupe fingerprints, and the SQLite database the transaction writes.
- `migrations/001_items.sql` — the two tables that transaction writes to.

## Run

```bash
mycel migrate  --config ./examples/reusable-blocks
mycel start    --config ./examples/reusable-blocks
```

```bash
# Accepted (tenant matches the named accept gate), deduped, written downstream,
# and returned through the named response envelope.
curl -X POST localhost:8080/products \
  -H 'Content-Type: application/json' \
  -d '{"id":"p1","name":"Widget","price":10,"tenant":"acme"}'

# Same building blocks, with the order flow's inline overrides applied.
curl -X POST localhost:8080/orders \
  -H 'Content-Type: application/json' \
  -d '{"id":"o1","customer":"Acme Inc","total":99}'
```

See [docs/core-concepts/reusable-blocks.md](../../docs/core-concepts/reusable-blocks.md)
for the full matrix of what is reusable and the override/merge rules.

## The other seven, in one flow

`ingest_stock` names nothing itself. Send it an item and the transaction writes
the item and its price together, and the response block reports what it did:

```bash
curl -X POST localhost:8080/stock \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"acme","stage":"ingest","id":"SKU-1","name":"Widget","price":19.9,"job_id":10}'
```

```json
{ "id": "SKU-1", "written": 2, "rows": 1, "status": "ok" }
```

Send it again with an older `job_id`. The named `sequence_guard` refuses it —
a message that arrived late must not overwrite a newer one — and says which
gate decided:

```bash
curl -X POST localhost:8080/stock \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"acme","stage":"ingest","id":"SKU-1","name":"Stale","price":1,"job_id":9}'
```

```json
{ "status": "dropped", "reason": "sequence_older" }
```

Send it as another tenant and the named `accept` gate refuses it the same way,
with `"reason": "accept"`.

Both tables, to see what the transaction actually wrote:

```bash
curl localhost:8080/stock
```

## A named transform over steps

`stock_summary` gathers two counts in `step` blocks and its `transform` block
is nothing but `use = "transform.stock_summary"` — the shape is declared once in
`reusable.mycel`, and any flow that gathers the same rows can answer in it:

```bash
curl localhost:8080/stock/summary
```

```json
{ "items": 1, "total_value": 19.9 }
```

## Notes

A `transaction` answers with what it did rather than with the row it wrote, so
`ingest_stock` shapes its own reply instead of reusing `response.envelope`:
`output.affected` counts the statements that ran and `output.captured.<name>`
holds whatever an `exec` asked to keep.

The sync blocks here use `storage { driver = "memory" }`, which is per process.
A lock that only holds inside one replica is not a lock — point them at
`driver = "redis"` and every replica takes the same one.
