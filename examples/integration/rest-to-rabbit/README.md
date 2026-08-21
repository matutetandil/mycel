# REST → RabbitMQ

Take a request, shape it, put it on a queue, answer immediately. The caller
does not wait for whatever happens next, which is the reason to do this at all.

Part of the [integration patterns](../README.md).

## What it does

```
POST /orders              → flow "create_order"    → order.created
POST /webhooks/:provider  → flow "receive_webhook" → webhook.received
```

The webhook flow reads its event type from a header, falling back to a field in
the body and then to `unknown` — `??` is how a flow says "whichever of these is
there".

## Running

```bash
export RABBIT_HOST=localhost RABBIT_PORT=5672

mycel start --config ./examples/integration/rest-to-rabbit
```

## Try It

```bash
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"customer":"cus_1","items":[{"sku":"WIDGET-1","quantity":2}],"total":59.98}'

curl -X POST http://localhost:8080/webhooks/stripe \
  -H "Content-Type: application/json" \
  -H "X-Event-Type: payment.succeeded" \
  -d '{"id":"evt_1","amount":5998}'
```

Both answer as soon as the message is on the broker. Bind a queue to
`order.created` to see them arrive.
