# Timeout Handling — per-class error dispositions

Demonstrates `on_timeout` / `on_error` inside `error_handling`: deciding the
broker disposition (ack / retry / requeue / reject) per **class** of failure.

## The problem

A consumer reads items off RabbitMQ and `POST`s each to a backend (Magento)
with `timeout = "60s"`. When the backend takes longer than 60s:

1. The HTTP client aborts with `context deadline exceeded`.
2. **The backend keeps processing the original request** — the timeout only
   cancelled the local wait.
3. By default Mycel treats a timeout as transient → retries → fires a **second,
   concurrent** `POST` for the same resource → race (deadlock / FK violation) →
   loop.

## The fix

The request is idempotent and the upstream redelivers duplicates, so on timeout
we **ack (drop)** instead of retrying:

```hcl
error_handling {
  retry {
    attempts  = 3
    delay     = "2s"
    max_delay = "30s"
    backoff   = "exponential"
  }

  on_timeout { action = "ack" }      # timeout → drop, no retry, no requeue
  on_error   { action = "requeue" }  # other transient errors → requeue
}
```

## Actions

| `action`  | Effect |
|-----------|--------|
| `ack`     | Acknowledge and drop. No retry, no requeue. |
| `retry`   | Use the `retry {}` budget (the default for transient errors). |
| `requeue` | Nack with requeue — broker redelivers. |
| `reject`  | Nack without requeue — routes to a DLQ if one is configured. |

## Classes

- **`on_timeout`** — timeout / `context.DeadlineExceeded` failures.
- **`on_error`** — transient, non-timeout, non-permanent failures.

Permanent errors (HTTP 4xx) are never routed here; they keep their ack-and-drop
behavior. **Backward compatible:** without `on_timeout`, a timeout stays
transient (retry budget → requeue), exactly as before.

## Run

```bash
mycel validate --config ./examples/timeout-handling
mycel start --config ./examples/timeout-handling
```
