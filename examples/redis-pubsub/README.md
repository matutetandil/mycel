# Redis Pub/Sub

A service that consumes events from Redis channels and keeps them, and can
publish to a channel over HTTP.

Redis Pub/Sub is fire-and-forget: a message published while nobody is
subscribed is gone. That is the difference from the queue examples, and it is
why this one records what it receives rather than acting on it.

## Files

| File | Purpose |
|------|---------|
| `config.mycel` | Connectors and flows |
| `migrations/001_create_order_events.sql` | The table the events are kept in |

## What it does

```
Redis channel "orders"   → flow "process_order" → postgres order_events
POST /events/:channel    → flow "publish_event" → Redis channel
```

`input._channel` is which channel a message arrived on — one flow can subscribe
to several.

## Running

```bash
export REDIS_HOST=localhost
export REDIS_PORT=6379
export DB_HOST=localhost DB_PORT=5432 DB_NAME=events DB_USER=postgres DB_PASSWORD=postgres

mycel migrate --config ./examples/redis-pubsub
mycel start --config ./examples/redis-pubsub
```

## Try It

Publish an event through the service, which puts it on the Redis channel; the
subscribing flow then receives it and stores it:

```bash
curl -X POST http://localhost:3000/events/orders \
  -H "Content-Type: application/json" \
  -d '{"order_id":"ord-1","status":"paid"}'
```

Or publish straight to Redis and watch the same flow pick it up:

```bash
redis-cli PUBLISH orders '{"order_id":"ord-2","status":"shipped"}'
```

Either way the row lands in `order_events`, with `channel` saying where it came
from.
