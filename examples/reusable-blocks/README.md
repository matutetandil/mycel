# Reusable Inline Blocks

Demonstrates declaring inline blocks **once** at the top level and referencing
them from multiple flows with `use = "<kind>.<name>"`, with optional inline
overrides. Introduced in Mycel v2.6.0.

## Files

- `reusable.mycel` — named blocks: `dedupe "standard"`, `retry "resilient"`,
  `error_handling "resilient"` (which references the named retry),
  `accept "mine"`, `response "envelope"`.
- `flows.mycel` — two flows:
  - `ingest_products` references the named blocks as-is.
  - `ingest_orders` references the same blocks but overrides a few attributes
    inline (dedupe key/ttl, retry attempts/backoff, response status).
- `connectors.mycel` — a REST entry point, a downstream target, and a memory
  cache for the dedupe fingerprints.

## Run

```bash
mycel validate --config .
mycel start --config .
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
