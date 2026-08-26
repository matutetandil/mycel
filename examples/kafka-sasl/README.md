# Kafka with SASL

A broker that asks who you are, and the block that answers.

The `sasl` block is the credentials a connector presents. Both halves use
them: the producer authenticates when it publishes and the consumer when it
joins its group, so one block covers the whole connector.

## Files

| File | Purpose |
|------|---------|
| `connectors.mycel` | The Kafka connector with its credentials, and SQLite |
| `flows.mycel` | Publish, consume, and list what was consumed |
| `migrations/001_orders.sql` | The table the consumer writes to |

## Running

You need a broker with SASL enabled. The integration stack has one: the same
broker as the plain example, with a second listener that authenticates.

```bash
export KAFKA_SASL_BROKERS=localhost:29094
export KAFKA_USER=mycel
export KAFKA_PASSWORD=mycel-secret

mycel migrate --config ./examples/kafka-sasl
mycel start --config ./examples/kafka-sasl
```

The `secure_orders` topic has to exist — a consumer group cannot subscribe to
one that is not there:

```bash
kafka-topics.sh --create --if-not-exists --bootstrap-server localhost:29092 \
  --topic secure_orders --partitions 1 --replication-factor 1
```

## Try It

```bash
curl -X POST http://localhost:3000/orders \
  -H 'Content-Type: application/json' \
  -d '{"id": "S-1", "customer": "Acme Inc"}'
```

```json
{ "id": "S-1", "status": "published" }
```

A moment later the consumer — authenticated with the same credentials — has
written it:

```bash
curl http://localhost:3000/orders
```

Get the password wrong and neither half works: the publish is refused and the
consumer never joins its group. Which is the point.

## Notes

**PLAIN sends the password as it is.** It belongs behind TLS: add a `tls` block
beside the `sasl` one and the broker's listener becomes SASL_SSL rather than
SASL_PLAINTEXT. See [the TLS example](../tls) for what that block takes.

**`SCRAM-SHA-256` and `SCRAM-SHA-512` are a straight swap** — change
`mechanism` and nothing else. The broker decides which it accepts.

**Credentials belong in the environment**, not in the file. `env()` reads them
at startup, and Kubernetes puts them there from a Secret.
