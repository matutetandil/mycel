# Async Jobs and Idempotency

Two answers to the same awkward moment: the caller does not know whether the
work happened.

A report takes too long to hold an HTTP connection open for, so the flow hands
back a job id and finishes later. And a client whose connection dropped mid-POST
does not know whether the order was placed, so it retries — and the retry must
not place a second one.

## Files

| File | Purpose |
|------|---------|
| `connectors.mycel` | REST, a cache for job state and keys, SQLite |
| `flows.mycel` | Two flows: one `async`, one `idempotency` |
| `migrations/001_orders.sql` | The `orders` and `reports` tables |

## What it shows

```
POST /reports        → 202 and a job id; the work runs in the background
GET  /jobs/:job_id   → registered by Mycel, not by a flow
POST /orders         → an Idempotency-Key makes the retry safe
GET  /orders         → what was actually written
```

## Running

```bash
mycel migrate --config ./examples/async-jobs
mycel start --config ./examples/async-jobs
```

## Try It

### A request that does not wait

The report flow answers immediately, with `202 Accepted`:

```bash
curl -i -X POST http://localhost:3000/reports \
  -H 'Content-Type: application/json' \
  -d '{"month": "2026-08"}'
```

```
HTTP/1.1 202 Accepted
{"job_id":"9c0f96d1fc5595fef5183711b55b6eb7","status":"pending"}
```

Ask about the job with the id you got back. Mycel registers `GET /jobs/:job_id`
on the connector by itself as soon as one flow declares `async` — there is no
flow for it in `flows.mycel`:

```bash
curl http://localhost:3000/jobs/$JOB_ID
```

While it runs, `{"status":"pending"}`. Once it is done, the result the flow
produced:

```json
{
  "job_id": "9c0f96d1fc5595fef5183711b55b6eb7",
  "status": "completed",
  "flow": "request_report",
  "result": { "affected": 1, "id": "c5776822-8ffb-45f3-aca4-a956f349757a" }
}
```

A flow that fails in the background reports `"status": "failed"` with the error
instead — the caller already has its 202, so this is the only place it can be
told.

### A retry that does not double-charge

Place an order with a key of your choosing:

```bash
curl -X POST http://localhost:3000/orders \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-777' \
  -d '{"customer": "ACME", "total": 120.50}'
```

Send exactly the same request again. The answer is identical — the same `id`,
because it is the stored answer to the first one and no second row was written:

```bash
curl -X POST http://localhost:3000/orders \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-777' \
  -d '{"customer": "ACME", "total": 120.50}'
```

A request with no key is not idempotent — the expression evaluates to nothing
and the flow runs as usual:

```bash
curl -X POST http://localhost:3000/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer": "OTHER", "total": 9.99}'
```

Two orders, not three:

```bash
curl http://localhost:3000/orders
```

## Notes

Both blocks keep their state in a **cache connector**, and which one matters in
production. With `driver = "memory"`, as here, the state lives in one process:
a client that polls a different replica is told its job does not exist, and a
retry that lands elsewhere places a second order. Point `storage` at a Redis
cache and every replica sees the same jobs and the same keys.

`ttl` is how long the answer is worth keeping — long enough to cover the
retries a client will actually make (a day is generous for orders), and short
enough that the cache is not an archive.

The idempotency key is a CEL expression, so it need not be a header:
`"input.order_id"` keys on a field of the payload, and
`"input.customer + ':' + input.order_ref"` on a pair of them.
