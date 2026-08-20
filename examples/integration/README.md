# Integration Patterns

Five services, each one shape of connecting two systems that were not built to
talk to each other. They are the shapes that keep coming up, written out in
full rather than described.

Each directory is a service of its own: `mycel start --config ./<directory>`.

| Directory | What it connects | The point |
|-----------|------------------|-----------|
| [`rest-to-rabbit`](rest-to-rabbit) | HTTP in → queue | Take a request, validate it, put it on a queue and answer immediately. Includes a webhook receiver that reads its event type from a header or the body. |
| [`rabbit-to-rest`](rabbit-to-rest) | Queue → HTTP out | Consume a message and call somebody's API with it — with retries, because the API will be down at some point. |
| [`rabbit-to-graphql`](rabbit-to-graphql) | Queue → GraphQL | The same, where the far side is a GraphQL mutation rather than a REST route. |
| [`rabbit-to-exec`](rabbit-to-exec) | Queue → a program | Hand the message to a command — generating a PDF, resizing an image — and keep what it printed. |
| [`file-to-rabbit`](file-to-rabbit) | A watched folder or bucket → queue | A file appearing is the event: a CSV dropped for the nightly import, a JSON in a drop folder, an upload landing in S3. |

## Running any of them

They all reach something that is not part of the example — a broker, an API, a
folder — so each names what it needs in its own configuration. The broker is
the one they share:

```bash
docker run -d --rm -p 5672:5672 -p 15672:15672 rabbitmq:3-management

export RABBITMQ_HOST=localhost RABBITMQ_PORT=5672

mycel start --config ./examples/integration/rest-to-rabbit
```

Then, for that one:

```bash
curl -X POST http://localhost:3000/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id":"cus_1","items":[{"sku":"WIDGET-1","quantity":2}]}'
```

The message is on `order.created` — the management console on port 15672 shows
it arrive.
