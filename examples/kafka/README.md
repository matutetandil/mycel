# Kafka

An order posted over HTTP, published to a Kafka topic, consumed back out of it
and written to a database — one service, one connector, both directions.

The point of the example is the two blocks inside that connector. A `producer`
block says where writes go; a `consumer` block says what to subscribe to and
under which group. A flow picks a direction by which side of it names the
connector.

## Files

| File | Purpose |
|------|---------|
| `connectors.mycel` | REST, the Kafka connector with both blocks, SQLite |
| `flows.mycel` | Publish, consume, and list what was consumed |
| `migrations/001_orders.sql` | The table the consumer writes to |

## Running

Kafka has to be reachable. With the integration stack up it is on `:29092`:

```bash
export KAFKA_BROKERS=localhost:29092
mycel migrate --config ./examples/kafka
mycel start --config ./examples/kafka
```

The startup banner lists `store_order` as a flow with no HTTP route: it is the
consumer, and it runs for as long as the service does.

## Try It

Publish an order. The flow answers as soon as Kafka has accepted the message —
`acks = "all"` means every in-sync replica has it:

```bash
curl -X POST http://localhost:3000/orders \
  -H 'Content-Type: application/json' \
  -d '{"id": "A-1", "customer": "Acme Inc", "total": 240.00}'
```

```json
{ "id": "A-1", "status": "published" }
```

Nothing wrote to the database on that request. The consumer did, a moment
later, on its own:

```bash
curl http://localhost:3000/orders
```

```json
[{ "id": "A-1", "customer": "Acme Inc", "total": 240, "written": "2026-08-26T09:41:02Z" }]
```

## Notes

**The group is what makes replicas cooperate.** Two copies of this service with
the same `group_id` split the topic's partitions between them and each order is
written once. Give them different groups and both write every order — which is
what you want for two different consumers, and never what you want for two
copies of one.

**`auto_offset_reset` only matters the first time.** It decides where a group
with no committed offset starts: `earliest` reads the backlog, `latest` ignores
anything published before the consumer existed. Once the group has committed an
offset it resumes from there, which is why a restarted consumer does not
reprocess everything.

**`acks` is the durability dial.** `all` waits for the in-sync replicas, `leader`
for one broker, `none` for nobody. The faster settings lose messages exactly
when you would mind most — the leader dying between accepting a write and
replicating it.

A topic other than the producer's default is a `target` on the `to` block. A
partition key — which is how Kafka keeps a customer's orders in order — comes
from the message key; see [the connector page](../../docs/connectors/message-queues.md).
