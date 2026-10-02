# Aspects

Aspects are Mycel's mechanism for **cross-cutting concerns** — behavior that applies across many flows rather than living inside any single one. Audit logging, metrics, error alerting, response enrichment, deprecation notices: instead of repeating that logic in every flow, you declare it once as an aspect and bind it to flows by name pattern.

Aspects are a core, fully declarative part of the model — no plugins or external code involved. An `aspect` is a top-level block, like `flow`, `connector`, or `transform`, and it operates on the flow pipeline: Mycel matches an aspect's pattern against your flow names and weaves its behavior in at the requested point.

```hcl
aspect "audit_log" {
  when = "after"
  on   = ["create_*", "update_*"]

  action {
    connector = "audit_db"
    operation = "INSERT audit_logs"
    transform {
      flow      = "_flow"
      user_id   = "ctx.user_id"
      action    = "_operation"
      timestamp = "_timestamp"
    }
  }
}
```

That one block adds an audit-log write after every `create_*` and `update_*` flow — without touching a single flow definition.

## Aspect Timing

| `when` | Description |
|--------|-------------|
| `before` | Run before the flow executes |
| `after` | Run after the flow succeeds |
| `around` | Wrap the entire flow execution |
| `on_error` | Run when the flow fails |

## Aspect Variables

In aspect action transforms:

| Variable | Description |
|----------|-------------|
| `_flow` | Flow name |
| `_operation` | HTTP method or operation name |
| `_target` | Target connector/resource |
| `_timestamp` | Unix timestamp |
| `result.affected` | Rows the flow's write affected (after/on_error) |
| `result.data` | Rows the flow answered with (after/on_error) |
| `result.captured` | What the flow's `transaction` captured; an empty map when it captured nothing (after/on_error) |
| `error.message` | Error message string (on_error) |
| `error.code` | HTTP status code, e.g. 404, 500 (on_error) |
| `error.type` | Error category: `http`, `timeout`, `connection`, `validation`, `not_found`, `auth`, `flow`, `unknown` (on_error) |

## Reacting to What a Transaction Changed

A [`transaction`](flows.md#transactional-write-transaction) can compute something about the write while it runs: whether a value changed, the id it created. What it captures is `result.captured` to `after` aspects: in the `if` condition, the `action` transform, `invalidate` keys and `response` fields.

That is how a side effect runs only when the write changed something. An `after` aspect runs once the write committed, and its failure never changes what happens to the message:

```hcl
flow "item_update" {
  from {
    connector = "rabbit"
    operation = "items"
  }

  to {
    connector = "db"
    transaction {
      exec {
        query   = "SELECT (COALESCE((SELECT pre_sale FROM item WHERE sku = :sku), -1) <> :incoming) AS changed"
        params  = { sku = "input.body.sku", incoming = "input.body.pre_sale" }
        capture = "pre_sale_changed"
      }
      exec {
        query  = "UPDATE item SET pre_sale = :incoming WHERE sku = :sku"
        params = { sku = "input.body.sku", incoming = "input.body.pre_sale" }
      }
    }
  }
}

aspect "pre_sale_invalidate" {
  when = "after"
  on   = ["item_*"]
  if   = "has(result.captured.pre_sale_changed) && result.captured.pre_sale_changed == 1"

  action {
    connector = "products_api"
    operation = "POST /cache/invalidate"
    transform {
      sku = "input.body.sku"
    }
  }
}
```

- A flow that captured nothing (no transaction at all) sees `result.captured` as `{}`, so the `has(...)` condition is false, not an error.
- Write the condition for the type your driver returns. SQLite and MySQL answer a comparison as `0`/`1`; PostgreSQL answers `true`/`false` (`result.captured.changed == true`). A comparison between a boolean and a number is an evaluation error, and an aspect whose condition errors does not run.
- The captured values are also part of the flow's response: `{"affected": 1, "captured": {...}}`.

## Flow Invocation

Aspect actions can invoke flows directly instead of writing to connectors. Use `flow` instead of `connector` in the action block:

```hcl
aspect "notify_on_create" {
  when = "after"
  on   = ["create_*"]

  action {
    flow = "send_notification"
    transform {
      message = "'Created: ' + _flow"
      user_id = "input.user_id"
    }
  }
}
```

The invoked flow receives the transform output as its input. This is useful for:
- **Flow orchestration** — chain flows through aspects without coupling them directly
- **Internal flows** — flows without a `from` block that are only invocable from aspects
- **Error handling** — invoke recovery flows on failure

`connector` and `flow` are mutually exclusive in an action block.

## Response Enrichment

After aspects can include a `response` block to inject fields into every row of the flow result. This is useful for API versioning, deprecation notices, or adding metadata without modifying individual flows:

```hcl
aspect "v1_deprecation" {
  when = "after"
  on   = ["*_v1"]

  response {
    headers = {
      Deprecation = "true"
      Sunset      = "Thu, 01 Jun 2026 00:00:00 GMT"
    }

    _warning = "'This API version is deprecated. Migrate to v2.'"
  }
}
```

The `response` block supports two types of enrichment:
- **Body fields** — CEL expressions merged into every row of the response. Have access to `result.data`, `result.affected`, `result.captured`, `input`, `_flow`, and `_operation`
- **Headers** — key-value pairs set as HTTP headers (or protocol equivalent for gRPC metadata, etc.). Values are literal strings

The `response` block is only valid for `after` aspects. An aspect can have both an `action` and a `response` block — the action runs as a side-effect and the response enriches the output.

## Pattern Matching

The `on` attribute accepts glob patterns matching flow names:

```hcl
on = ["*"]                          # All flows
on = ["create_*", "update_*"]       # All create and update flows
on = ["get_product*"]               # Flows starting with "get_product"
```

## See Also

- [Flows](flows.md) — the unit of work aspects bind to
- [Transforms](transforms.md) — the CEL expressions used inside aspect actions
- [Error Handling](../guides/error-handling.md) — retries, DLQ, and `on_error` dispositions that complement `on_error` aspects
- [Aspects example](https://github.com/matutetandil/mycel/tree/main/examples/aspects)
